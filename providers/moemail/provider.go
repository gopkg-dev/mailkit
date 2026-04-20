package moemail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
	"github.com/gopkg-dev/mailkit/internal/providerutil"
	"github.com/imroc/req/v3"
)

const (
	defaultTimeout      = 120 * time.Second
	defaultPollInterval = 3 * time.Second
	defaultExpiryTime   = 0
	minNameLength       = 8
	maxNameLength       = 13
)

func init() {
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "moemail",
			DisplayName: "MoeMail",
			Note:        "已接入 Go provider，可创建临时邮箱并轮询邮件内容。",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:        "api_base",
					Label:       "API Base",
					InputType:   "url",
					Placeholder: "https://your-moemail-api.example.com",
					Required:    true,
				},
				{
					Name:      "api_key",
					Label:     "API Key",
					InputType: "password",
					Required:  true,
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
			return New(config.Get("api_base"), config.Get("api_key"), config.GetBool("debug")), nil
		},
	})
}

type Provider struct {
	apiBase      string
	apiKey       string
	client       *req.Client
	randomSource *rand.Rand
}

func New(apiBase string, apiKey string, debug bool) *Provider {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	client := req.C()
	if debug {
		client.DevMode()
	}
	client.SetBaseURL(baseURL)

	return &Provider{
		apiBase:      baseURL,
		apiKey:       strings.TrimSpace(apiKey),
		client:       client,
		randomSource: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (provider *Provider) Name() string {
	return "moemail"
}

func (provider *Provider) CreateMailbox(ctx context.Context, input mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	domain, err := provider.getDomain(ctx)
	if err != nil {
		return mailkit.Mailbox{}, err
	}
	if domain == "" {
		return mailkit.Mailbox{}, errors.New("no moemail domains available")
	}

	namePrefix := strings.TrimSpace(input.MailboxPrefix)
	if namePrefix == "" {
		nameLength := minNameLength
		if maxNameLength > minNameLength {
			nameLength += provider.randomSource.Intn(maxNameLength - minNameLength + 1)
		}
		namePrefix = provider.randomName(nameLength)
	}

	response, err := provider.newRequest(ctx).
		SetBody(map[string]any{
			"name":       namePrefix,
			"domain":     domain,
			"expiryTime": defaultExpiryTime,
		}).
		Post("/api/emails/generate")
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("create moemail mailbox: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK && response.StatusCode != providerutil.HTTPStatusCreated {
		return mailkit.Mailbox{}, fmt.Errorf("create moemail mailbox returned status %d", response.StatusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("decode moemail mailbox response: %w", err)
	}

	emailID := strings.TrimSpace(providerutil.AsString(payload["id"]))
	emailAddress := strings.TrimSpace(providerutil.AsString(payload["email"]))
	if emailID == "" || emailAddress == "" {
		return mailkit.Mailbox{}, errors.New("moemail mailbox response missing id/email")
	}

	return mailkit.Mailbox{
		Email:      emailAddress,
		Credential: emailID,
	}, nil
}

func (provider *Provider) WaitForContent(ctx context.Context, input mailkit.WaitForContentInput) (string, error) {
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	pollInterval := input.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	emailID := strings.TrimSpace(input.Credential)
	if emailID == "" {
		return "", errors.New("moemail email id is required")
	}

	seenMessageIDs := make(map[string]struct{})
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		response, err := provider.newRequest(ctx).Get("/api/emails/" + emailID)
		if err != nil {
			return "", fmt.Errorf("list moemail messages: %w", err)
		}
		if response.StatusCode == providerutil.HTTPStatusOK {
			var payload map[string]any
			if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
				return "", fmt.Errorf("decode moemail messages response: %w", err)
			}

			for _, message := range providerutil.NormalizeItems(providerutil.AsSlice(payload["messages"])) {
				messageID := strings.TrimSpace(providerutil.AsString(message["id"]))
				if messageID == "" {
					continue
				}
				if _, exists := seenMessageIDs[messageID]; exists {
					continue
				}

				detailResponse, err := provider.newRequest(ctx).Get("/api/emails/" + emailID + "/" + messageID)
				if err != nil {
					return "", fmt.Errorf("get moemail message detail: %w", err)
				}
				if detailResponse.StatusCode != providerutil.HTTPStatusOK {
					continue
				}
				seenMessageIDs[messageID] = struct{}{}

				var detailPayload map[string]any
				if err := json.Unmarshal(detailResponse.Bytes(), &detailPayload); err != nil {
					return "", fmt.Errorf("decode moemail message detail: %w", err)
				}

				content := buildMailContent(detailPayload)
				if strings.TrimSpace(content) != "" {
					return content, nil
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

	return "", errors.New("mail content not received before timeout")
}

func (provider *Provider) TestConnection(ctx context.Context, _ mailkit.CreateMailboxInput) error {
	_, err := provider.getDomain(ctx)
	return err
}

func (provider *Provider) getDomain(ctx context.Context) (string, error) {
	response, err := provider.newRequest(ctx).Get("/api/config")
	if err != nil {
		return "", fmt.Errorf("get moemail config: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK {
		return "", fmt.Errorf("get moemail config returned status %d", response.StatusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return "", fmt.Errorf("decode moemail config response: %w", err)
	}

	domainEntries := strings.Split(providerutil.AsString(payload["emailDomains"]), ",")
	domains := make([]string, 0, len(domainEntries))
	for _, rawDomain := range domainEntries {
		domain := strings.TrimSpace(rawDomain)
		if domain != "" {
			domains = append(domains, domain)
		}
	}
	if len(domains) == 0 {
		return "", nil
	}
	return domains[provider.randomSource.Intn(len(domains))], nil
}

func (provider *Provider) newRequest(ctx context.Context) *req.Request {
	return provider.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		SetHeader("X-API-Key", provider.apiKey).
		SetHeader("Content-Type", "application/json")
}

func (provider *Provider) randomName(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	if length <= 0 {
		length = minNameLength
	}

	builder := strings.Builder{}
	builder.Grow(length)
	for index := 0; index < length; index++ {
		builder.WriteByte(alphabet[provider.randomSource.Intn(len(alphabet))])
	}
	return builder.String()
}

func buildMailContent(message map[string]any) string {
	messageObject, _ := message["message"].(map[string]any)
	parts := []string{
		providerutil.AsString(providerutil.MapValue(messageObject, "content")),
		providerutil.AsString(providerutil.MapValue(messageObject, "html")),
		providerutil.AsString(message["text"]),
		providerutil.AsString(message["html"]),
	}
	return strings.Join(parts, "\n")
}
