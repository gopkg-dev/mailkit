package mailtm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
	"github.com/gopkg-dev/mailkit/internal/providerutil"
	"github.com/imroc/req/v3"
)

func init() {
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "mailtm",
			DisplayName: "Mail.tm",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:        "api_base",
					Label:       "API Base",
					InputType:   "url",
					Placeholder: "https://api.mail.tm",
					Required:    true,
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
			return New(config.Get("api_base"), config.GetBool("debug")), nil
		},
	})
}

type Provider struct {
	apiBase string
	client  *req.Client
}

func New(apiBase string, debug bool) *Provider {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	client := req.C()
	if debug {
		client.DevMode()
	}
	client.SetBaseURL(baseURL)

	return &Provider{
		apiBase: baseURL,
		client:  client,
	}
}

func (provider *Provider) Name() string {
	return "mailtm"
}

func (provider *Provider) CreateMailbox(ctx context.Context, input mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	domains, err := provider.getDomains(ctx)
	if err != nil {
		return mailkit.Mailbox{}, err
	}
	if len(domains) == 0 {
		return mailkit.Mailbox{}, errors.New("no active mail.tm domains available")
	}

	domain := domains[0]
	localPartPrefix := strings.TrimSpace(input.MailboxPrefix)
	for attempt := 0; attempt < 5; attempt++ {
		timestamp := time.Now().UnixNano() + int64(attempt)*1_000_000
		localPart := fmt.Sprintf("oc%x", timestamp)
		if localPartPrefix != "" {
			localPart = fmt.Sprintf("%s%x", localPartPrefix, timestamp)
		}
		emailAddress := fmt.Sprintf("%s@%s", localPart, domain)
		password := fmt.Sprintf("pw%x", timestamp)

		createResponse, err := provider.newRequest(ctx, "", true).
			SetBody(map[string]any{
				"address":  emailAddress,
				"password": password,
			}).
			Post("/accounts")
		if err != nil {
			return mailkit.Mailbox{}, fmt.Errorf("create mail.tm account: %w", err)
		}
		if createResponse.StatusCode != providerutil.HTTPStatusOK && createResponse.StatusCode != providerutil.HTTPStatusCreated {
			continue
		}

		tokenResponse, err := provider.newRequest(ctx, "", true).
			SetBody(map[string]any{
				"address":  emailAddress,
				"password": password,
			}).
			Post("/token")
		if err != nil {
			return mailkit.Mailbox{}, fmt.Errorf("create mail.tm token: %w", err)
		}
		if tokenResponse.StatusCode != providerutil.HTTPStatusOK {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(tokenResponse.Bytes(), &payload); err != nil {
			return mailkit.Mailbox{}, fmt.Errorf("decode mail.tm token response: %w", err)
		}

		token := strings.TrimSpace(providerutil.AsString(payload["token"]))
		if token == "" {
			continue
		}

		return mailkit.Mailbox{
			Email:      emailAddress,
			Credential: token,
		}, nil
	}

	return mailkit.Mailbox{}, errors.New("failed to create mail.tm mailbox")
}

func (provider *Provider) WaitForOTP(ctx context.Context, input mailkit.WaitForOTPInput) (string, error) {
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	pollInterval := input.PollInterval
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}

	seenMessageIDs := make(map[string]struct{})
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		messagesResponse, err := provider.newRequest(ctx, input.Credential, false).Get("/messages")
		if err != nil {
			return "", fmt.Errorf("list mail.tm messages: %w", err)
		}
		if messagesResponse.StatusCode == providerutil.HTTPStatusOK {
			var payload any
			if err := json.Unmarshal(messagesResponse.Bytes(), &payload); err != nil {
				return "", fmt.Errorf("decode mail.tm messages response: %w", err)
			}

			for _, message := range providerutil.NormalizeHydraMessages(payload) {
				messageID := providerutil.NormalizeMessageID(message["id"], message["@id"])
				if messageID == "" {
					continue
				}
				if _, exists := seenMessageIDs[messageID]; exists {
					continue
				}

				detailResponse, err := provider.newRequest(ctx, input.Credential, false).Get("/messages/" + messageID)
				if err != nil {
					return "", fmt.Errorf("get mail.tm message detail: %w", err)
				}
				if detailResponse.StatusCode != providerutil.HTTPStatusOK {
					continue
				}
				seenMessageIDs[messageID] = struct{}{}

				var detailPayload map[string]any
				if err := json.Unmarshal(detailResponse.Bytes(), &detailPayload); err != nil {
					return "", fmt.Errorf("decode mail.tm message detail: %w", err)
				}

				content := buildMailContent(detailPayload)
				sender := strings.ToLower(providerutil.AsString(providerutil.MapValue(detailPayload["from"], "address")))
				if !strings.Contains(sender, "openai") && !strings.Contains(strings.ToLower(content), "openai") {
					continue
				}

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

func (provider *Provider) TestConnection(ctx context.Context, input mailkit.CreateMailboxInput) error {
	_, err := provider.CreateMailbox(ctx, input)
	return err
}

func (provider *Provider) getDomains(ctx context.Context) ([]string, error) {
	response, err := provider.newRequest(ctx, "", false).Get("/domains")
	if err != nil {
		return nil, fmt.Errorf("list mail.tm domains: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK {
		return nil, fmt.Errorf("list mail.tm domains returned status %d", response.StatusCode)
	}

	var payload any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return nil, fmt.Errorf("decode mail.tm domains response: %w", err)
	}

	domains := make([]string, 0)
	for _, item := range providerutil.NormalizeHydraMessages(payload) {
		domain := strings.TrimSpace(providerutil.AsString(item["domain"]))
		if domain == "" {
			continue
		}
		if providerutil.IsBoolFalse(item["isActive"]) || providerutil.IsBoolTrue(item["isPrivate"]) {
			continue
		}
		domains = append(domains, domain)
	}
	return domains, nil
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

func buildMailContent(message map[string]any) string {
	parts := []string{
		providerutil.AsString(message["subject"]),
		providerutil.AsString(message["intro"]),
		providerutil.AsString(message["text"]),
	}

	switch htmlValue := message["html"].(type) {
	case string:
		parts = append(parts, htmlValue)
	case []any:
		for _, item := range htmlValue {
			parts = append(parts, providerutil.AsString(item))
		}
	}

	return strings.Join(parts, "\n")
}
