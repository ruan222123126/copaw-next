package channel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestQQChannelSendTextC2C(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32
	var messageBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-1/messages":
			messageCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "QQBot qq-token" {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			defer r.Body.Close()
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode message body failed: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	channel := NewQQChannel()
	cfg := map[string]interface{}{
		"app_id":        "app-1",
		"client_secret": "secret-1",
		"bot_prefix":    "[BOT] ",
		"token_url":     server.URL + "/token",
		"api_base":      server.URL,
		"target_type":   "c2c",
	}

	if err := channel.SendText(context.Background(), "u-1", "s-1", "hello", cfg); err != nil {
		t.Fatalf("send text failed: %v", err)
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one message call, got=%d", got)
	}
	if messageBody["content"] != "[BOT] hello" {
		t.Fatalf("unexpected content: %#v", messageBody["content"])
	}
	if got, ok := messageBody["msg_type"].(float64); !ok || got != 0 {
		t.Fatalf("unexpected msg_type: %#v", messageBody["msg_type"])
	}
	if got, ok := messageBody["msg_seq"].(float64); !ok || got != 1 {
		t.Fatalf("unexpected msg_seq: %#v", messageBody["msg_seq"])
	}
}

func TestQQChannelCachesTokenAcrossCalls(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"cached-token","expires_in":7200}`))
		case "/v2/groups/group-1/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	channel := NewQQChannel()
	cfg := map[string]interface{}{
		"app_id":        "app-1",
		"client_secret": "secret-1",
		"token_url":     server.URL + "/token",
		"api_base":      server.URL,
		"target_type":   "group",
		"target_id":     "group-1",
	}

	if err := channel.SendText(context.Background(), "ignored", "s-1", "hello-1", cfg); err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if err := channel.SendText(context.Background(), "ignored", "s-2", "hello-2", cfg); err != nil {
		t.Fatalf("second send failed: %v", err)
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call due to cache, got=%d", got)
	}
	if got := messageCalls.Load(); got != 2 {
		t.Fatalf("expected two message calls, got=%d", got)
	}
}
