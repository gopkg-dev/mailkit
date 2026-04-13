package mailkit

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	name string
}

func (provider fakeProvider) Name() string {
	return provider.name
}

func (provider fakeProvider) CreateMailbox(_ context.Context, _ CreateMailboxInput) (Mailbox, error) {
	return Mailbox{}, nil
}

func (provider fakeProvider) WaitForOTP(_ context.Context, _ WaitForOTPInput) (string, error) {
	return "", nil
}

func (provider fakeProvider) TestConnection(_ context.Context, _ CreateMailboxInput) error {
	return nil
}

func TestRouterRoundRobinRotatesProviders(t *testing.T) {
	router, err := NewRouter(Config{
		Strategy: "round_robin",
		Providers: []Provider{
			fakeProvider{name: "mailtm"},
			fakeProvider{name: "duckmail"},
		},
	})
	if err != nil {
		t.Fatalf("expected router creation to succeed: %v", err)
	}

	firstProvider, err := router.NextProvider()
	if err != nil {
		t.Fatalf("expected first provider selection to succeed: %v", err)
	}
	secondProvider, err := router.NextProvider()
	if err != nil {
		t.Fatalf("expected second provider selection to succeed: %v", err)
	}

	if firstProvider.Name() != "mailtm" {
		t.Fatalf("expected first provider mailtm, got %s", firstProvider.Name())
	}
	if secondProvider.Name() != "duckmail" {
		t.Fatalf("expected second provider duckmail, got %s", secondProvider.Name())
	}
}

func TestRouterFailoverPrefersLowerFailureProvider(t *testing.T) {
	router, err := NewRouter(Config{
		Strategy: "failover",
		Providers: []Provider{
			fakeProvider{name: "mailtm"},
			fakeProvider{name: "duckmail"},
		},
	})
	if err != nil {
		t.Fatalf("expected router creation to succeed: %v", err)
	}

	router.ReportFailure("mailtm", errors.New("mailtm unavailable"))

	selectedProvider, err := router.NextProvider()
	if err != nil {
		t.Fatalf("expected provider selection to succeed: %v", err)
	}

	if selectedProvider.Name() != "duckmail" {
		t.Fatalf("expected failover to select duckmail, got %s", selectedProvider.Name())
	}
}
