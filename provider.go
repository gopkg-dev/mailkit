package mailkit

import (
	"context"
	"slices"
	"strings"
	"time"
)

type Mailbox struct {
	Email      string `json:"email"`
	Credential string `json:"credential"`
}

type CreateMailboxInput struct {
	StaticProxy string `json:"static_proxy"`
}

type WaitForOTPInput struct {
	Email        string        `json:"email"`
	Credential   string        `json:"credential"`
	StaticProxy  string        `json:"static_proxy"`
	Timeout      time.Duration `json:"-"`
	PollInterval time.Duration `json:"-"`
}

type Provider interface {
	Name() string
	CreateMailbox(ctx context.Context, input CreateMailboxInput) (Mailbox, error)
	WaitForOTP(ctx context.Context, input WaitForOTPInput) (string, error)
	TestConnection(ctx context.Context, input CreateMailboxInput) error
}

type ProviderValue struct {
	String  string
	Strings []string
}

type ProviderConfig map[string]ProviderValue

func StringValue(value string) ProviderValue {
	return ProviderValue{String: value}
}

func StringsValue(values ...string) ProviderValue {
	return ProviderValue{Strings: values}
}

func (config ProviderConfig) Get(name string) string {
	return config.GetString(name)
}

func (config ProviderConfig) GetString(name string) string {
	if config == nil {
		return ""
	}
	return config[normalizeProviderKey(name)].GetString()
}

func (config ProviderConfig) GetStringOr(name string, fallback string) string {
	value := config.GetString(name)
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func (config ProviderConfig) GetStrings(name string) []string {
	if config == nil {
		return nil
	}
	return config[normalizeProviderKey(name)].GetStrings()
}

func (config ProviderConfig) HasValue(name string) bool {
	if config == nil {
		return false
	}
	return config[normalizeProviderKey(name)].HasValue()
}

func (config ProviderConfig) normalized() ProviderConfig {
	if len(config) == 0 {
		return ProviderConfig{}
	}

	normalizedConfig := make(ProviderConfig, len(config))
	for rawKey, rawValue := range config {
		key := normalizeProviderKey(rawKey)
		if key == "" {
			continue
		}
		normalizedConfig[key] = rawValue.normalized()
	}
	return normalizedConfig
}

func (value ProviderValue) GetString() string {
	normalizedString := strings.TrimSpace(value.String)
	if normalizedString != "" {
		return normalizedString
	}
	for _, item := range value.GetStrings() {
		if item != "" {
			return item
		}
	}
	return ""
}

func (value ProviderValue) GetStrings() []string {
	if len(value.Strings) > 0 {
		normalizedValues := make([]string, 0, len(value.Strings))
		for _, rawItem := range value.Strings {
			item := strings.TrimSpace(rawItem)
			if item == "" {
				continue
			}
			normalizedValues = append(normalizedValues, item)
		}
		if len(normalizedValues) == 0 {
			return nil
		}
		return normalizedValues
	}

	normalizedString := strings.TrimSpace(value.String)
	if normalizedString == "" {
		return nil
	}
	return []string{normalizedString}
}

func (value ProviderValue) HasValue() bool {
	if value.GetString() != "" {
		return true
	}
	return len(value.GetStrings()) > 0
}

func (value ProviderValue) normalized() ProviderValue {
	normalizedStrings := value.GetStrings()
	normalizedValue := ProviderValue{
		String: strings.TrimSpace(value.String),
	}
	if len(normalizedStrings) > 0 {
		normalizedValue.Strings = slices.Clone(normalizedStrings)
	}
	if normalizedValue.String != "" && len(normalizedValue.Strings) > 0 {
		normalizedValue.String = normalizedValue.Strings[0]
	}
	return normalizedValue
}
