package duckmail

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
)

func init() {
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "duckmail",
			DisplayName: "DuckMail",
			Note:        "已接入 Go provider，可创建临时邮箱并轮询 OTP。",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:        "api_base",
					Label:       "API Base",
					InputType:   "url",
					Placeholder: "https://api.duckmail.sbs",
					Required:    true,
				},
				{
					Name:      "bearer_token",
					Label:     "Bearer Token",
					InputType: "password",
					Required:  false,
				},
				{
					Name:        "domain",
					Label:       "Domain",
					InputType:   "text",
					Placeholder: "duckmail.app",
					Required:    false,
				},
			},
		},
		Factory: func(config mailkit.ProviderConfig, _ mailkit.FactoryDependencies) (mailkit.Provider, error) {
			return New(config.Get("api_base"), config.Get("bearer_token"), config.Get("domain")), nil
		},
	})
}

type Provider struct {
	apiBase      string
	bearerToken  string
	domain       string
	client       *req.Client
	randomSource *rand.Rand
}

func New(apiBase string, bearerToken string, domain string) *Provider {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	client := req.C()
	client.SetBaseURL(baseURL)

	return &Provider{
		apiBase:      baseURL,
		bearerToken:  strings.TrimSpace(bearerToken),
		domain:       strings.TrimSpace(domain),
		client:       client,
		randomSource: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (provider *Provider) Name() string {
	return "duckmail"
}

func (provider *Provider) CreateMailbox(ctx context.Context, _ mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	domain, err := provider.getDomain(ctx)
	if err != nil {
		return mailkit.Mailbox{}, err
	}
	if domain == "" {
		return mailkit.Mailbox{}, errors.New("no duckmail domains available")
	}

	emailAddress := fmt.Sprintf("oc%x@%s", time.Now().UnixNano(), domain)
	password := fmt.Sprintf("pw%x", time.Now().UnixNano())

	createResponse, err := provider.newRequest(ctx, provider.bearerToken, true).
		SetBody(map[string]any{
			"address":  emailAddress,
			"password": password,
		}).
		Post("/accounts")
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("create duckmail mailbox: %w", err)
	}
	if createResponse.StatusCode != providerutil.HTTPStatusOK && createResponse.StatusCode != providerutil.HTTPStatusCreated {
		return mailkit.Mailbox{}, fmt.Errorf("create duckmail mailbox returned status %d", createResponse.StatusCode)
	}

	tokenResponse, err := provider.newRequest(ctx, provider.bearerToken, true).
		SetBody(map[string]any{
			"address":  emailAddress,
			"password": password,
		}).
		Post("/token")
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("create duckmail token: %w", err)
	}
	if tokenResponse.StatusCode != providerutil.HTTPStatusOK {
		return mailkit.Mailbox{}, fmt.Errorf("create duckmail token returned status %d", tokenResponse.StatusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal(tokenResponse.Bytes(), &payload); err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("decode duckmail token response: %w", err)
	}

	token := strings.TrimSpace(providerutil.AsString(payload["token"]))
	if token == "" {
		return mailkit.Mailbox{}, errors.New("duckmail token response missing token")
	}

	return mailkit.Mailbox{
		Email:      emailAddress,
		Credential: token,
	}, nil
}

func (provider *Provider) WaitForOTP(ctx context.Context, input mailkit.WaitForOTPInput) (string, error) {
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	pollInterval := input.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	token := strings.TrimSpace(input.Credential)
	if token == "" {
		return "", errors.New("duckmail mailbox token is required")
	}

	seenMessageIDs := make(map[string]struct{})
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		response, err := provider.newRequest(ctx, token, false).Get("/messages")
		if err != nil {
			return "", fmt.Errorf("list duckmail messages: %w", err)
		}
		if response.StatusCode == providerutil.HTTPStatusOK {
			var payload map[string]any
			if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
				return "", fmt.Errorf("decode duckmail messages response: %w", err)
			}

			rawMessages := payload["hydra:member"]
			if rawMessages == nil {
				rawMessages = payload["member"]
			}
			if rawMessages == nil {
				rawMessages = payload["data"]
			}

			for _, message := range providerutil.NormalizeItems(providerutil.AsSlice(rawMessages)) {
				messageID := providerutil.NormalizeMessageID(message["id"], message["@id"])
				if messageID == "" {
					continue
				}
				if _, exists := seenMessageIDs[messageID]; exists {
					continue
				}

				detailResponse, err := provider.newRequest(ctx, token, false).Get("/messages/" + normalizeMessageID(messageID))
				if err != nil {
					return "", fmt.Errorf("get duckmail message detail: %w", err)
				}
				if detailResponse.StatusCode != providerutil.HTTPStatusOK {
					continue
				}
				seenMessageIDs[messageID] = struct{}{}

				var detailPayload map[string]any
				if err := json.Unmarshal(detailResponse.Bytes(), &detailPayload); err != nil {
					return "", fmt.Errorf("decode duckmail message detail: %w", err)
				}

				content := strings.Join([]string{
					providerutil.AsString(detailPayload["text"]),
					providerutil.AsString(detailPayload["html"]),
				}, "\n")
				if matches := providerutil.OTPCodePattern.FindStringSubmatch(content); len(matches) == 2 {
					return matches[1], nil
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

func (provider *Provider) TestConnection(ctx context.Context, _ mailkit.CreateMailboxInput) error {
	_, err := provider.getDomain(ctx)
	return err
}

func (provider *Provider) getDomain(ctx context.Context) (string, error) {
	response, err := provider.newRequest(ctx, "", false).Get("/domains")
	if err != nil {
		return "", fmt.Errorf("list duckmail domains: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK {
		return "", fmt.Errorf("list duckmail domains returned status %d", response.StatusCode)
	}

	var payload any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return "", fmt.Errorf("decode duckmail domains response: %w", err)
	}

	domains := make([]string, 0)
	for _, item := range providerutil.NormalizeHydraMessages(payload) {
		domain := strings.TrimSpace(providerutil.AsString(item["domain"]))
		if domain == "" || providerutil.IsBoolFalse(item["isActive"]) {
			continue
		}
		domains = append(domains, domain)
	}
	if len(domains) == 0 {
		return "", nil
	}
	if provider.domain != "" {
		for _, domain := range domains {
			if strings.EqualFold(domain, provider.domain) {
				return domain, nil
			}
		}
		return "", fmt.Errorf("configured duckmail domain %q is not available", provider.domain)
	}
	return domains[provider.randomSource.Intn(len(domains))], nil
}

func (provider *Provider) newRequest(ctx context.Context, token string, useJSON bool) *req.Request {
	request := provider.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json")

	if useJSON {
		request.SetHeader("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		request.SetHeader("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	return request
}

func normalizeMessageID(messageID string) string {
	normalizedID := strings.TrimSpace(messageID)
	if strings.HasPrefix(normalizedID, "/") {
		return providerutil.NormalizeMessageID(normalizedID)
	}
	return normalizedID
}
