package runner

import (
	"testing"

	"copaw-next/apps/gateway/internal/domain"
)

func TestGenerateReply(t *testing.T) {
	r := New()
	got := r.GenerateReply(domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{{
			Role:    "user",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: "hello world"}},
		}},
	})
	if got != "Echo: hello world" {
		t.Fatalf("unexpected reply: %s", got)
	}
}
