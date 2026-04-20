package mailkit_test

import (
	"context"
	"strings"
	"testing"

	mailkit "github.com/gopkg-dev/mailkit"
)

func TestProviderConfigReadsStructuredValues(t *testing.T) {
	config := mailkit.ProviderConfig{
		"api_base":        mailkit.StringValue(" https://mail.example.com "),
		"domains":         mailkit.StringsValue(" alpha.example.com ", "beta.example.com"),
		"domain_strategy": mailkit.StringValue(" round_robin "),
	}

	if config.GetString("api_base") != "https://mail.example.com" {
		t.Fatalf("expected trimmed api_base string")
	}

	domains := config.GetStrings("domains")
	if len(domains) != 2 || domains[0] != "alpha.example.com" || domains[1] != "beta.example.com" {
		t.Fatalf("expected structured domains slice, got %#v", domains)
	}

	if config.GetStringOr("domain_strategy", "random") != "round_robin" {
		t.Fatalf("expected domain strategy from config")
	}
	if config.GetStringOr("missing", "random") != "random" {
		t.Fatalf("expected fallback value for missing string field")
	}

	if !(mailkit.ProviderConfig{"debug": mailkit.StringValue("true")}).GetBool("debug") {
		t.Fatalf("expected debug=true to parse as bool true")
	}
	if !(mailkit.ProviderConfig{"debug": mailkit.StringValue("1")}).GetBool("debug") {
		t.Fatalf("expected debug=1 to parse as bool true")
	}
	if (mailkit.ProviderConfig{"debug": mailkit.StringValue("invalid")}).GetBool("debug") {
		t.Fatalf("expected invalid debug value to parse as bool false")
	}
	if (mailkit.ProviderConfig{}).GetBool("debug") {
		t.Fatalf("expected missing debug value to parse as bool false")
	}
}

func TestNewProviderRejectsMissingRequiredStringSliceField(t *testing.T) {
	providerName := strings.ToLower(t.Name())
	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        providerName,
			DisplayName: "Structured Config Provider",
			Fields: []mailkit.ProviderFieldSpec{
				{
					Name:      "domains",
					Label:     "Domains",
					InputType: "textarea",
					Required:  true,
				},
			},
		},
		Factory: func(_ mailkit.ProviderConfig, _ mailkit.FactoryDependencies) (mailkit.Provider, error) {
			return structuredFakeProvider{name: providerName}, nil
		},
	})

	_, err := mailkit.NewProvider(providerName, mailkit.ProviderConfig{}, mailkit.FactoryDependencies{})
	if err == nil {
		t.Fatalf("expected required domains validation error")
	}
	if err.Error() != "provider "+providerName+" requires domains" {
		t.Fatalf("unexpected error: %v", err)
	}
}

type structuredFakeProvider struct {
	name string
}

func (provider structuredFakeProvider) Name() string {
	return provider.name
}

func (provider structuredFakeProvider) CreateMailbox(_ context.Context, _ mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	return mailkit.Mailbox{}, nil
}

func (provider structuredFakeProvider) WaitForContent(_ context.Context, _ mailkit.WaitForContentInput) (string, error) {
	return "", nil
}

func (provider structuredFakeProvider) TestConnection(_ context.Context, _ mailkit.CreateMailboxInput) error {
	return nil
}
