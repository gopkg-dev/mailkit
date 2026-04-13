package moemail

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

func TestCreateMailboxReturnsEmailAndEmailID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/config":
			if request.Header.Get("X-API-Key") != "api-key-value" {
				t.Fatalf("expected X-API-Key header to be present")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"emailDomains": "moemail.app, alt.moemail.app",
			})
		case request.Method == http.MethodPost && request.URL.Path == "/api/emails/generate":
			if request.Header.Get("X-API-Key") != "api-key-value" {
				t.Fatalf("expected X-API-Key header on generate request")
			}
			var payload map[string]any
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode generate request body: %v", err)
			}
			domain := payload["domain"].(string)
			name := payload["name"].(string)
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"id":    "email-id-1",
				"email": name + "@" + domain,
			})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL, "api-key-value")

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("expected create mailbox to succeed: %v", err)
	}
	if !strings.HasSuffix(mailbox.Email, "@moemail.app") && !strings.HasSuffix(mailbox.Email, "@alt.moemail.app") {
		t.Fatalf("expected moemail domain in email address, got %s", mailbox.Email)
	}
	if mailbox.Credential != "email-id-1" {
		t.Fatalf("expected credential email-id-1, got %s", mailbox.Credential)
	}
}

func TestWaitForOTPExtractsCodeFromMessageDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/emails/email-id-1":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"messages": []map[string]any{
					{"id": "msg-1"},
				},
			})
		case request.Method == http.MethodGet && request.URL.Path == "/api/emails/email-id-1/msg-1":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"message": map[string]any{
					"content": "Verification code: 556677",
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	provider := New(server.URL, "api-key-value")

	code, err := provider.WaitForOTP(context.Background(), mailkit.WaitForOTPInput{
		Email:        "user@moemail.app",
		Credential:   "email-id-1",
		Timeout:      300 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected wait for otp to succeed: %v", err)
	}
	if code != "556677" {
		t.Fatalf("expected otp 556677, got %s", code)
	}
}
