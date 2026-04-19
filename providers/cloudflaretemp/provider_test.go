package cloudflaretemp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mailkit "github.com/gopkg-dev/mailkit"
)

func TestCreateMailboxUsesRoundRobinDomainStrategy(t *testing.T) {
	var requestCount atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/admin/new_address" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("x-admin-auth") != "admin-secret" {
			t.Fatalf("expected admin auth header")
		}

		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		currentRequest := requestCount.Add(1)
		domain := payload["domain"].(string)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"address": payload["name"].(string) + "@" + domain,
			"jwt":     "jwt-token-" + string(rune('0'+currentRequest)),
		})
	}))
	defer server.Close()

	provider := New(server.URL, "admin-secret", []string{"one.example.com", "two.example.com"}, "round_robin", false)

	firstMailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("create first mailbox: %v", err)
	}
	secondMailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("create second mailbox: %v", err)
	}

	if !strings.HasSuffix(firstMailbox.Email, "@one.example.com") {
		t.Fatalf("expected first mailbox to use first domain, got %s", firstMailbox.Email)
	}
	if !strings.HasSuffix(secondMailbox.Email, "@two.example.com") {
		t.Fatalf("expected second mailbox to use second domain, got %s", secondMailbox.Email)
	}
}

func TestWaitForOTPExtractsCodeFromRawMIMEMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/mails" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer mailbox-jwt" {
			t.Fatalf("expected mailbox bearer token")
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id": "msg-1",
					"to": []map[string]any{
						{"address": "target@example.com"},
					},
					"raw": "From: OpenAI <noreply@openai.com>\r\nTo: target@example.com\r\nSubject: Verify\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYour verification code is 654321",
				},
			},
		})
	}))
	defer server.Close()

	provider := New(server.URL, "admin-secret", []string{"one.example.com"}, "random", false)

	code, err := provider.WaitForOTP(context.Background(), mailkit.WaitForOTPInput{
		Email:        "target@example.com",
		Credential:   "mailbox-jwt",
		Timeout:      250 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("wait for otp: %v", err)
	}
	if code != "654321" {
		t.Fatalf("expected otp 654321, got %s", code)
	}
}
