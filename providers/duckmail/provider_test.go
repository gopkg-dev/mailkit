package duckmail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
)

func TestCreateMailboxReturnsEmailAndToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/domains":
			if request.Header.Get("Authorization") != "Bearer provider-token" {
				t.Fatalf("expected provider bearer token on /domains")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"hydra:member": []map[string]any{
					{
						"domain":   "duckmail.app",
						"isActive": true,
					},
				},
			})
		case request.Method == http.MethodPost && request.URL.Path == "/accounts":
			if request.Header.Get("Authorization") != "Bearer provider-token" {
				t.Fatalf("expected provider bearer token on /accounts")
			}
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
		case request.Method == http.MethodPost && request.URL.Path == "/token":
			if request.Header.Get("Authorization") != "Bearer provider-token" {
				t.Fatalf("expected provider bearer token on /token")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"token": "mail-token"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL, "provider-token", "", false)

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("expected create mailbox to succeed: %v", err)
	}
	if !strings.HasSuffix(mailbox.Email, "@duckmail.app") {
		t.Fatalf("expected duckmail domain in email address, got %s", mailbox.Email)
	}
	if mailbox.Credential != "mail-token" {
		t.Fatalf("expected credential mail-token, got %s", mailbox.Credential)
	}
}

func TestWaitForOTPExtractsCodeFromMessageDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/messages":
			if request.Header.Get("Authorization") != "Bearer mail-token" {
				t.Fatalf("expected mailbox bearer token on /messages")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"hydra:member": []map[string]any{
					{"id": "msg-1"},
				},
			})
		case request.Method == http.MethodGet && request.URL.Path == "/messages/msg-1":
			if request.Header.Get("Authorization") != "Bearer mail-token" {
				t.Fatalf("expected mailbox bearer token on message detail")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"text": "Your verification code is 443322",
			})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL, "provider-token", "", false)

	code, err := provider.WaitForOTP(context.Background(), mailkit.WaitForOTPInput{
		Email:        "user@duckmail.app",
		Credential:   "mail-token",
		Timeout:      300 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected wait for otp to succeed: %v", err)
	}
	if code != "443322" {
		t.Fatalf("expected otp 443322, got %s", code)
	}
}

func TestCreateMailboxUsesConfiguredDomain(t *testing.T) {
	const configuredDomain = "custom.duckmail.app"

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/domains":
			if request.Header.Get("Authorization") != "Bearer provider-token" {
				t.Fatalf("expected provider bearer token on /domains")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"hydra:member": []map[string]any{
					{"domain": "duckmail.app", "isActive": true},
					{"domain": "custom.duckmail.app", "isActive": true},
				},
			})
		case request.Method == http.MethodPost && request.URL.Path == "/accounts":
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			address, _ := payload["address"].(string)
			if !strings.HasSuffix(address, "@"+configuredDomain) {
				t.Fatalf("expected configured domain in address, got %s", address)
			}
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
		case request.Method == http.MethodPost && request.URL.Path == "/token":
			_ = json.NewEncoder(writer).Encode(map[string]any{"token": "mail-token"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL, "provider-token", configuredDomain, false)

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("expected create mailbox to succeed: %v", err)
	}
	if !strings.HasSuffix(mailbox.Email, "@"+configuredDomain) {
		t.Fatalf("expected configured duckmail domain in email address, got %s", mailbox.Email)
	}
}

func TestCreateMailboxReturnsErrorWhenConfiguredDomainUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/domains" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-token" {
			t.Fatalf("expected provider bearer token on /domains")
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"hydra:member": []map[string]any{
				{"domain": "duckmail.app", "isActive": true},
			},
		})
	}))
	defer server.Close()

	provider := New(server.URL, "provider-token", "custom.duckmail.app", false)

	_, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err == nil {
		t.Fatalf("expected create mailbox to fail when configured domain is unavailable")
	}
	if !errors.Is(err, errConfiguredDomainUnavailable) {
		t.Fatalf("unexpected error: %v", err)
	}
}
