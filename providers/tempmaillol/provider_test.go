package tempmaillol

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
		if request.Method != http.MethodPost || request.URL.Path != "/v2/inbox/create" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode create mailbox body: %v", err)
		}
		if len(payload) != 0 {
			t.Fatalf("expected empty JSON object body, got %#v", payload)
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{
			"address": "user@tempmail.lol",
			"token":   "mailbox-token",
		})
	}))
	defer server.Close()

	provider := New(server.URL, false)

	mailbox, err := provider.CreateMailbox(context.Background(), mailkit.CreateMailboxInput{})
	if err != nil {
		t.Fatalf("expected create mailbox to succeed: %v", err)
	}
	if mailbox.Email != "user@tempmail.lol" {
		t.Fatalf("expected email user@tempmail.lol, got %s", mailbox.Email)
	}
	if mailbox.Credential != "mailbox-token" {
		t.Fatalf("expected credential mailbox-token, got %s", mailbox.Credential)
	}
}

func TestWaitForContentReturnsInboxMessageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v2/inbox" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.URL.Query().Get("token") != "mailbox-token" {
			t.Fatalf("expected mailbox token query param")
		}

		_ = json.NewEncoder(writer).Encode(map[string]any{
			"emails": []map[string]any{
				{
					"id":      "msg-1",
					"from":    "noreply@openai.com",
					"subject": "Your OpenAI verification code",
					"body":    "Verification code: 112233",
				},
			},
		})
	}))
	defer server.Close()

	provider := New(server.URL, false)

	content, err := provider.WaitForContent(context.Background(), mailkit.WaitForContentInput{
		Email:        "user@tempmail.lol",
		Credential:   "mailbox-token",
		Timeout:      300 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("expected wait for content to succeed: %v", err)
	}
	if !strings.Contains(content, "112233") {
		t.Fatalf("expected content to include 112233, got %s", content)
	}
}

func TestClientForProxyConfiguresValidatedProxyURL(t *testing.T) {
	provider := New("https://api.tempmail.lol", false)

	defaultClient, err := provider.clientForProxy("")
	if err != nil {
		t.Fatalf("expected empty proxy to succeed: %v", err)
	}
	if defaultClient != provider.client {
		t.Fatalf("expected empty proxy to reuse base client")
	}

	proxyClient, err := provider.clientForProxy("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("expected valid proxy to succeed: %v", err)
	}
	if proxyClient == provider.client {
		t.Fatalf("expected valid proxy to clone client")
	}
	if proxyClient.GetTransport().Proxy == nil {
		t.Fatalf("expected proxy transport function to be configured")
	}

	proxyURL, err := proxyClient.GetTransport().Proxy(httptest.NewRequest(http.MethodGet, "https://api.tempmail.lol/v2/inbox", nil))
	if err != nil {
		t.Fatalf("resolve proxy url: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:8080" {
		t.Fatalf("expected configured proxy url, got %#v", proxyURL)
	}

	if _, err := provider.clientForProxy("://bad-proxy"); err == nil {
		t.Fatalf("expected malformed proxy url to fail")
	}
}
