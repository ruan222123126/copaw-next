package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"copaw-next/apps/gateway/internal/domain"
	"copaw-next/apps/gateway/internal/provider"
)

func TestGenerateReplyDemo(t *testing.T) {
	r := New()
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello world"}},
		}},
	}, GenerateConfig{ProviderID: ProviderDemo, Model: "demo-chat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Echo: hello world" {
		t.Fatalf("unexpected reply: %s", got)
	}
}

func TestGenerateReplyOpenAISuccess(t *testing.T) {
	t.Parallel()
	var auth string
	var model string

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		model, _ = req["model"].(string)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from provider"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from provider" {
		t.Fatalf("unexpected reply: %s", got)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("unexpected auth header: %s", auth)
	}
	if model != "gpt-4o-mini" {
		t.Fatalf("unexpected model: %s", model)
	}
}

func TestGenerateReplyOpenAIMissingAPIKey(t *testing.T) {
	t.Parallel()
	r := New()
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
	})
	assertRunnerCode(t, err, ErrorCodeProviderNotConfigured)
}

func TestGenerateReplyOpenAIUpstreamFailure(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: ProviderOpenAI,
		Model:      "gpt-4o-mini",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
	})
	assertRunnerCode(t, err, ErrorCodeProviderRequestFailed)
}

func TestGenerateReplyUnsupportedProvider(t *testing.T) {
	t.Parallel()
	r := New()
	_, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: "unknown-provider",
		Model:      "demo-chat",
	})
	assertRunnerCode(t, err, ErrorCodeProviderNotSupported)
}

func TestGenerateReplyCustomProviderWithAdapter(t *testing.T) {
	t.Parallel()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from custom adapter"}}]}`))
	}))
	defer mock.Close()

	r := NewWithHTTPClient(mock.Client())
	got, err := r.GenerateReply(context.Background(), domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello"}},
		}},
	}, GenerateConfig{
		ProviderID: "custom-provider",
		Model:      "custom-model",
		APIKey:     "sk-test",
		BaseURL:    mock.URL,
		AdapterID:  provider.AdapterOpenAICompatible,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello from custom adapter" {
		t.Fatalf("unexpected reply: %s", got)
	}
}

func assertRunnerCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", want)
	}
	var rerr *RunnerError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected RunnerError, got: %T (%v)", err, err)
	}
	if rerr.Code != want {
		t.Fatalf("unexpected error code: got=%s want=%s", rerr.Code, want)
	}
}
