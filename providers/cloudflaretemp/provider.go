package cloudflaretemp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
	"github.com/gopkg-dev/mailkit/internal/providerutil"
	"github.com/imroc/req/v3"
)

const (
	defaultTimeout        = 120 * time.Second
	defaultPollInterval   = 3 * time.Second
	defaultDomainStrategy = "random"
)

func init() {
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "cloudflare_temp_email",
			DisplayName: "Cloudflare Temp Email",
			Note:        "已接入 Go provider，支持多域名策略与 raw MIME OTP 提取。",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:        "api_base",
					Label:       "API Base",
					InputType:   "url",
					Placeholder: "https://mail.example.com",
					Required:    true,
				},
				{
					Name:      "admin_password",
					Label:     "Admin Password",
					InputType: "password",
					Required:  true,
				},
				{
					Name:        "domains",
					Label:       "Domains",
					InputType:   "textarea",
					Placeholder: "alpha.example.com\nbeta.example.com",
					Required:    true,
				},
				{
					Name:      "domain_strategy",
					Label:     "Domain Strategy",
					InputType: "select",
					Required:  true,
					Options: []mailkit.ProviderFieldOption{
						{Value: "random", Label: "Random"},
						{Value: "round_robin", Label: "Round Robin"},
					},
				},
				{
					Name:      "debug",
					Label:     "Debug",
					InputType: "select",
					Required:  false,
					Options: []mailkit.ProviderFieldOption{
						{Value: "false", Label: "Off"},
						{Value: "true", Label: "On"},
					},
				},
			},
		},
		Factory: func(config mailkit.ProviderConfig, _ mailkit.FactoryDependencies) (mailkit.Provider, error) {
			return New(
				config.GetString("api_base"),
				config.GetString("admin_password"),
				config.GetStrings("domains"),
				config.GetStringOr("domain_strategy", defaultDomainStrategy),
				config.GetBool("debug"),
			), nil
		},
	})
}

type Provider struct {
	apiBase        string
	adminPassword  string
	domains        []string
	domainStrategy string
	client         *req.Client
	randomSource   *rand.Rand
	randomMu       sync.Mutex
	domainCounter  atomic.Uint64
}

func New(apiBase string, adminPassword string, domains []string, domainStrategy string, debug bool) *Provider {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	client := req.C()
	if debug {
		client.DevMode()
	}
	client.SetBaseURL(baseURL)

	normalizedDomains := make([]string, 0, len(domains))
	for _, rawDomain := range domains {
		domain := strings.TrimSpace(rawDomain)
		if domain != "" {
			normalizedDomains = append(normalizedDomains, domain)
		}
	}

	strategy := strings.TrimSpace(domainStrategy)
	if strategy == "" {
		strategy = defaultDomainStrategy
	}

	return &Provider{
		apiBase:        baseURL,
		adminPassword:  strings.TrimSpace(adminPassword),
		domains:        normalizedDomains,
		domainStrategy: strategy,
		client:         client,
		randomSource:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (provider *Provider) Name() string {
	return "cloudflare_temp_email"
}

func (provider *Provider) CreateMailbox(ctx context.Context, input mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	selectedDomain, err := provider.nextDomain()
	if err != nil {
		return mailkit.Mailbox{}, err
	}
	namePrefix := strings.TrimSpace(input.MailboxPrefix)
	if namePrefix == "" {
		namePrefix = provider.generateMailboxName()
	}

	response, err := provider.newRequest(ctx, "", true).
		SetBody(map[string]any{
			"enablePrefix": true,
			"name":         namePrefix,
			"domain":       selectedDomain,
		}).
		Post("/admin/new_address")
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("create cloudflare mailbox: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK && response.StatusCode != providerutil.HTTPStatusCreated {
		return mailkit.Mailbox{}, fmt.Errorf("create cloudflare mailbox returned status %d", response.StatusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("decode cloudflare mailbox response: %w", err)
	}

	emailAddress := strings.TrimSpace(providerutil.AsString(payload["address"]))
	jwtToken := strings.TrimSpace(providerutil.AsString(payload["jwt"]))
	if emailAddress == "" || jwtToken == "" {
		return mailkit.Mailbox{}, errors.New("cloudflare mailbox response missing address/jwt")
	}

	return mailkit.Mailbox{
		Email:      emailAddress,
		Credential: jwtToken,
	}, nil
}

func (provider *Provider) WaitForOTP(ctx context.Context, input mailkit.WaitForOTPInput) (string, error) {
	token := strings.TrimSpace(input.Credential)
	if token == "" {
		return "", errors.New("cloudflare mailbox token is required")
	}

	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	pollInterval := input.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	seenMessageIDs := make(map[string]struct{})
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		response, err := provider.newRequest(ctx, token, false).
			SetQueryParam("limit", "10").
			SetQueryParam("offset", "0").
			Get("/api/mails")
		if err != nil {
			return "", fmt.Errorf("list cloudflare mails: %w", err)
		}
		if response.StatusCode == providerutil.HTTPStatusOK {
			var payload any
			if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
				return "", fmt.Errorf("decode cloudflare mails response: %w", err)
			}

			for _, message := range normalizeMessages(payload) {
				if !messageMatchesEmail(message, input.Email) {
					continue
				}
				messageID := strings.TrimSpace(providerutil.AsString(message["id"]))
				if messageID == "" {
					continue
				}
				if _, exists := seenMessageIDs[messageID]; exists {
					continue
				}
				seenMessageIDs[messageID] = struct{}{}

				content := buildMessageContent(message)
				if code := providerutil.FindOTPCode(content); code != "" {
					return code, nil
				}
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	return "", errors.New("otp not received before timeout")
}

func (provider *Provider) TestConnection(ctx context.Context, input mailkit.CreateMailboxInput) error {
	_, err := provider.CreateMailbox(ctx, input)
	return err
}

func (provider *Provider) nextDomain() (string, error) {
	if len(provider.domains) == 0 {
		return "", errors.New("cloudflare domains are required")
	}

	if provider.domainStrategy == "round_robin" {
		index := provider.domainCounter.Add(1) - 1
		return provider.domains[index%uint64(len(provider.domains))], nil
	}

	provider.randomMu.Lock()
	defer provider.randomMu.Unlock()
	return provider.domains[provider.randomSource.Intn(len(provider.domains))], nil
}

func (provider *Provider) generateMailboxName() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"

	provider.randomMu.Lock()
	defer provider.randomMu.Unlock()

	builder := strings.Builder{}
	builder.Grow(11)
	for index := 0; index < 5; index++ {
		builder.WriteByte(letters[provider.randomSource.Intn(len(letters))])
	}

	numberCount := provider.randomSource.Intn(3) + 1
	for index := 0; index < numberCount; index++ {
		builder.WriteByte(digits[provider.randomSource.Intn(len(digits))])
	}

	letterCount := provider.randomSource.Intn(3) + 1
	for index := 0; index < letterCount; index++ {
		builder.WriteByte(letters[provider.randomSource.Intn(len(letters))])
	}
	return builder.String()
}

func (provider *Provider) newRequest(ctx context.Context, token string, useJSON bool) *req.Request {
	request := provider.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json")

	if useJSON {
		request.SetHeader("Content-Type", "application/json")
	}
	if strings.TrimSpace(provider.adminPassword) != "" {
		request.SetHeader("x-admin-auth", provider.adminPassword)
	}
	if strings.TrimSpace(token) != "" {
		request.SetHeader("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	return request
}

func normalizeMessages(payload any) []map[string]any {
	switch typedPayload := payload.(type) {
	case []any:
		return providerutil.NormalizeItems(typedPayload)
	case map[string]any:
		if items, ok := typedPayload["results"].([]any); ok {
			return providerutil.NormalizeItems(items)
		}
	}
	return nil
}

func messageMatchesEmail(message map[string]any, targetEmail string) bool {
	target := strings.ToLower(strings.TrimSpace(targetEmail))
	if target == "" {
		return true
	}

	candidates := make([]string, 0)
	for _, key := range []string{"to", "mailTo", "receiver", "receivers", "address", "email", "envelope_to"} {
		candidates = append(candidates, collectTextCandidates(message[key])...)
	}
	if len(candidates) == 0 {
		return true
	}

	for _, candidate := range candidates {
		text := strings.ToLower(strings.TrimSpace(candidate))
		if text != "" && strings.Contains(text, target) {
			return true
		}
	}
	return false
}

func collectTextCandidates(value any) []string {
	switch typedValue := value.(type) {
	case string:
		return []string{typedValue}
	case map[string]any:
		candidates := make([]string, 0)
		for _, key := range []string{"address", "email", "name", "value"} {
			candidates = append(candidates, collectTextCandidates(typedValue[key])...)
		}
		return candidates
	case []any:
		candidates := make([]string, 0)
		for _, item := range typedValue {
			candidates = append(candidates, collectTextCandidates(item)...)
		}
		return candidates
	default:
		return nil
	}
}

func buildMessageContent(message map[string]any) string {
	parts := []string{
		strings.TrimSpace(providerutil.AsString(message["text"])),
		strings.TrimSpace(providerutil.AsString(message["html"])),
	}
	content := strings.Join(parts, "\n")
	if strings.TrimSpace(content) != "" {
		return content
	}

	rawMessage := strings.TrimSpace(providerutil.AsString(message["raw"]))
	if rawMessage == "" {
		return ""
	}

	parsedContent, err := providerutil.ExtractBodyFromRawMIME(rawMessage)
	if err != nil {
		return rawMessage
	}
	if strings.TrimSpace(parsedContent) == "" {
		return rawMessage
	}
	return parsedContent
}
