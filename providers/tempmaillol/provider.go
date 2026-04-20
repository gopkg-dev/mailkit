package tempmaillol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
	"github.com/gopkg-dev/mailkit/internal/providerutil"
	"github.com/imroc/req/v3"
)

const (
	defaultAPIBase      = "https://api.tempmail.lol"
	defaultTimeout      = 120 * time.Second
	defaultPollInterval = 3 * time.Second
)

func init() {
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "tempmail_lol",
			DisplayName: "TempMail.lol",
			Note:        "已接入 Go provider，使用官方 v2 inbox API，支持 runtime static_proxy。",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:        "api_base",
					Label:       "API Base",
					InputType:   "url",
					Placeholder: defaultAPIBase,
					Required:    false,
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
			return New(config.GetStringOr("api_base", defaultAPIBase), config.GetBool("debug")), nil
		},
	})
}

type Provider struct {
	apiBase string
	client  *req.Client
}

func New(apiBase string, debug bool) *Provider {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if baseURL == "" {
		baseURL = defaultAPIBase
	}

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
	return "tempmail_lol"
}

func (provider *Provider) CreateMailbox(ctx context.Context, input mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	request, err := provider.newRequest(ctx, true, input.StaticProxy)
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("configure tempmail.lol create mailbox request: %w", err)
	}

	response, err := request.SetBody(map[string]any{}).Post("/v2/inbox/create")
	if err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("create tempmail.lol mailbox: %w", err)
	}
	if response.StatusCode != providerutil.HTTPStatusOK && response.StatusCode != providerutil.HTTPStatusCreated {
		return mailkit.Mailbox{}, fmt.Errorf("create tempmail.lol mailbox returned status %d", response.StatusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
		return mailkit.Mailbox{}, fmt.Errorf("decode tempmail.lol mailbox response: %w", err)
	}

	emailAddress := strings.TrimSpace(providerutil.AsString(payload["address"]))
	inboxToken := strings.TrimSpace(providerutil.AsString(payload["token"]))
	if emailAddress == "" || inboxToken == "" {
		return mailkit.Mailbox{}, errors.New("tempmail.lol mailbox response missing address/token")
	}

	return mailkit.Mailbox{
		Email:      emailAddress,
		Credential: inboxToken,
	}, nil
}

func (provider *Provider) WaitForContent(ctx context.Context, input mailkit.WaitForContentInput) (string, error) {
	inboxToken := strings.TrimSpace(input.Credential)
	if inboxToken == "" {
		return "", errors.New("tempmail.lol inbox token is required")
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

		request, err := provider.newRequest(ctx, false, input.StaticProxy)
		if err != nil {
			return "", fmt.Errorf("configure tempmail.lol inbox request: %w", err)
		}

		response, err := request.SetQueryParam("token", inboxToken).Get("/v2/inbox")
		if err != nil {
			return "", fmt.Errorf("list tempmail.lol messages: %w", err)
		}
		if response.StatusCode != providerutil.HTTPStatusOK {
			return "", fmt.Errorf("list tempmail.lol messages returned status %d", response.StatusCode)
		}

		var payload map[string]any
		if err := json.Unmarshal(response.Bytes(), &payload); err != nil {
			return "", fmt.Errorf("decode tempmail.lol messages response: %w", err)
		}

		for _, message := range normalizeMessages(payload) {
			messageID := providerutil.NormalizeMessageID(message["id"], message["_id"], message["message_id"])
			if messageID != "" {
				if _, exists := seenMessageIDs[messageID]; exists {
					continue
				}
				seenMessageIDs[messageID] = struct{}{}
			}

			if content := buildMessageContent(message); strings.TrimSpace(content) != "" {
				return content, nil
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

func (provider *Provider) TestConnection(ctx context.Context, input mailkit.CreateMailboxInput) error {
	_, err := provider.CreateMailbox(ctx, input)
	return err
}

func (provider *Provider) clientForProxy(staticProxy string) (*req.Client, error) {
	proxyURL := strings.TrimSpace(staticProxy)
	if proxyURL == "" {
		return provider.client, nil
	}

	parsedProxyURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse static proxy: %w", err)
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		return nil, errors.New("static proxy must include scheme and host")
	}

	return provider.client.Clone().SetProxyURL(parsedProxyURL.String()), nil
}

func (provider *Provider) newRequest(ctx context.Context, useJSON bool, staticProxy string) (*req.Request, error) {
	client, err := provider.clientForProxy(staticProxy)
	if err != nil {
		return nil, err
	}

	request := client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json")

	if useJSON {
		request.SetHeader("Content-Type", "application/json")
	}

	return request, nil
}

func normalizeMessages(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	if items, ok := payload["emails"].([]any); ok {
		return providerutil.NormalizeItems(items)
	}
	return nil
}

func buildMessageContent(message map[string]any) string {
	parts := []string{
		strings.TrimSpace(providerutil.AsString(message["subject"])),
		strings.TrimSpace(providerutil.AsString(message["body"])),
		strings.TrimSpace(providerutil.AsString(message["html"])),
	}

	return strings.Join(parts, "\n")
}
