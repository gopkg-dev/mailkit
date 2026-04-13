package mailtm

import (
	"context"
	"encoding/json"
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
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"hydra:member": []map[string]any{
					{
						"domain":    "example.com",
						"isActive":  true,
						"isPrivate": false,
					},
				},
			})
		case request.Method == http.MethodPost && request.URL.Path == "/accounts":
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
		case request.Method == http.MethodPost && request.URL.Path == "/token":
			_ = json.NewEncoder(writer).Encode(map[string]any{"token": "mail-token-value"})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL)

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("expected create mailbox to succeed: %v", err)
	}
	if !strings.HasSuffix(mailbox.Email, "@example.com") {
		t.Fatalf("expected email domain example.com, got %s", mailbox.Email)
	}
	if mailbox.Credential != "mail-token-value" {
		t.Fatalf("expected token mail-token-value, got %s", mailbox.Credential)
	}
}

func TestWaitForOTPExtractsCodeFromOpenAIMail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/messages":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"hydra:member": []map[string]any{
					{"id": "msg-1"},
				},
			})
		case request.Method == http.MethodGet && request.URL.Path == "/messages/msg-1":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"from":    map[string]any{"address": "noreply@openai.com"},
				"subject": "Your OpenAI code",
				"text":    "Verification code: 654321",
			})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL)

	code, err := provider.WaitForOTP(context.Background(), mailkit.WaitForOTPInput{
		Email:        "target@example.com",
		Credential:   "mail-token-value",
		Timeout:      300 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected wait for otp to succeed: %v", err)
	}
	if code != "654321" {
		t.Fatalf("expected otp 654321, got %s", code)
	}
}
