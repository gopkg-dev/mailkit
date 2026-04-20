package mailkit_test

import (
	"context"
	"testing"

	mailkit "github.com/gopkg-dev/mailkit"
	_ "github.com/gopkg-dev/mailkit/providers/cloudflaretemp"
	_ "github.com/gopkg-dev/mailkit/providers/duckmail"
	_ "github.com/gopkg-dev/mailkit/providers/mailtm"
	_ "github.com/gopkg-dev/mailkit/providers/moemail"
	_ "github.com/gopkg-dev/mailkit/providers/tempmaillol"
)

func TestProviderSpecsExposeBuiltinProviders(t *testing.T) {
	providerSpecs := mailkit.ProviderSpecs()

	expectedFieldsByProvider := map[string][]string{
		"mailtm":                {"api_base", "debug"},
		"moemail":               {"api_base", "api_key", "debug"},
		"duckmail":              {"api_base", "bearer_token", "domain", "debug"},
		"cloudflare_temp_email": {"api_base", "admin_password", "domains", "domain_strategy", "debug"},
		"tempmail_lol":          {"api_base", "debug"},
	}

	for providerName, expectedFields := range expectedFieldsByProvider {
		providerSpec, ok := providerSpecs[providerName]
		if !ok {
			t.Fatalf("expected provider spec for %s", providerName)
		}
		if !providerSpec.Supported {
			t.Fatalf("expected %s to be supported", providerName)
		}
		if len(providerSpec.Fields) != len(expectedFields) {
			t.Fatalf("expected %d fields for %s, got %d", len(expectedFields), providerName, len(providerSpec.Fields))
		}
		for index, fieldName := range expectedFields {
			if providerSpec.Fields[index].Name != fieldName {
				t.Fatalf("expected field %s at index %d for %s, got %s", fieldName, index, providerName, providerSpec.Fields[index].Name)
			}
		}
	}
}

func TestNewProviderBuildsMailTMProviderFromRegistry(t *testing.T) {
	provider, err := mailkit.NewProvider("mailtm", mailkit.ProviderConfig{
		"api_base": mailkit.StringValue("https://api.mail.tm"),
	}, mailkit.FactoryDependencies{})
	if err != nil {
		t.Fatalf("expected mailtm provider creation to succeed: %v", err)
	}
	if provider.Name() != "mailtm" {
		t.Fatalf("expected provider name mailtm, got %s", provider.Name())
	}
}

func TestMustRegisterPanicsOnDuplicateProvider(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected duplicate registration to panic")
		}
	}()

	mailkit.MustRegister(mailkit.Registration{
		Spec: mailkit.ProviderSpec{
			Name:        "mailtm",
			DisplayName: "Duplicate Mail.tm",
			Supported:   true,
		},
		Factory: func(_ mailkit.ProviderConfig, _ mailkit.FactoryDependencies) (mailkit.Provider, error) {
			return fakeProvider{name: "mailtm"}, nil
		},
	})
}

type fakeProvider struct {
	name string
}

func (provider fakeProvider) Name() string {
	return provider.name
}

func (provider fakeProvider) CreateMailbox(_ context.Context, _ mailkit.CreateMailboxInput) (mailkit.Mailbox, error) {
	return mailkit.Mailbox{}, nil
}

func (provider fakeProvider) WaitForContent(_ context.Context, _ mailkit.WaitForContentInput) (string, error) {
	return "", nil
}

func (provider fakeProvider) TestConnection(_ context.Context, _ mailkit.CreateMailboxInput) error {
	return nil
}
