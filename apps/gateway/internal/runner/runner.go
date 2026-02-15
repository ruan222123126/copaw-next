package runner

import (
	"strings"

	"copaw-next/apps/gateway/internal/domain"
)

type Runner struct{}

func New() *Runner {
	return &Runner{}
}

func (r *Runner) GenerateReply(req domain.AgentProcessRequest) string {
	parts := make([]string, 0, len(req.Input))
	for _, msg := range req.Input {
		if msg.Role != "user" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "Echo: (empty input)"
	}
	return "Echo: " + strings.Join(parts, " ")
}
