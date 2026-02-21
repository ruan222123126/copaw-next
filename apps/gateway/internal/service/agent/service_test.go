package agent

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/adapters"
)

func TestProcessToolCallSuccess(t *testing.T) {
	t.Parallel()

	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurn should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called when has tool call")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func() []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(name string, _ map[string]interface{}) (string, error) {
				if name != "shell" {
					t.Fatalf("unexpected tool name: %s", name)
				}
				return "ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	result, processErr := svc.Process(context.Background(), ProcessParams{
		HasToolCall:       true,
		RequestedToolCall: ToolCall{Name: "shell", Input: map[string]interface{}{"command": "echo ok"}},
		ReplyChunkSize:    32,
	}, nil)
	if processErr != nil {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
	if result.Reply != "ok" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.Events) != 5 {
		t.Fatalf("unexpected events count: %d", len(result.Events))
	}
	if result.Events[0].Type != "step_started" || result.Events[1].Type != "tool_call" || result.Events[2].Type != "tool_result" || result.Events[4].Type != "completed" {
		t.Fatalf("unexpected event sequence: %#v", result.Events)
	}
}

func TestProcessRunnerLoopWithToolCallAndStreamDelta(t *testing.T) {
	t.Parallel()

	step := 0
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurn should not be called for streaming mode")
				return runner.TurnResult{}, nil
			},
			GenerateTurnStreamFunc: func(_ context.Context, _ domain.AgentProcessRequest, _ runner.GenerateConfig, _ []runner.ToolDefinition, onDelta func(string)) (runner.TurnResult, error) {
				step++
				if step == 1 {
					return runner.TurnResult{
						ToolCalls: []runner.ToolCall{
							{
								ID:        "call_1",
								Name:      "view",
								Arguments: map[string]interface{}{"path": "/tmp/a.txt"},
							},
						},
					}, nil
				}
				onDelta("hello")
				return runner.TurnResult{Text: "hello"}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func() []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(name string, _ map[string]interface{}) (string, error) {
				if name != "view" {
					t.Fatalf("unexpected tool name: %s", name)
				}
				return "tool-ok", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc:   func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) { return http.StatusBadGateway, "runner_error", err.Error() },
		},
	})

	emitted := make([]domain.AgentEvent, 0, 8)
	result, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{Input: []domain.AgentInputMessage{{Role: "user", Type: "message"}}},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message", Content: []domain.RuntimeContent{{Type: "text", Text: "hi"}}}},
		Streaming:      true,
		ReplyChunkSize: 12,
	}, func(evt domain.AgentEvent) {
		emitted = append(emitted, evt)
	})
	if processErr != nil {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
	if result.Reply != "hello" {
		t.Fatalf("unexpected reply: %q", result.Reply)
	}
	if len(result.Events) == 0 || len(emitted) == 0 {
		t.Fatalf("expected streamed events")
	}
	if len(result.Events) != len(emitted) {
		t.Fatalf("result/emitted mismatch: %d vs %d", len(result.Events), len(emitted))
	}
	last := result.Events[len(result.Events)-1]
	if last.Type != "completed" {
		t.Fatalf("unexpected last event: %#v", last)
	}
}

func TestProcessRunnerErrorMapped(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	svc := NewService(Dependencies{
		Runner: adapters.AgentRunner{
			GenerateTurnFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error) {
				return runner.TurnResult{}, boom
			},
			GenerateTurnStreamFunc: func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error) {
				t.Fatalf("GenerateTurnStream should not be called")
				return runner.TurnResult{}, nil
			},
		},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func() []runner.ToolDefinition { return nil },
			ExecuteToolCallFunc: func(string, map[string]interface{}) (string, error) {
				t.Fatalf("ExecuteToolCall should not be called")
				return "", nil
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc: func(err error) (int, string, string) { return http.StatusBadRequest, "tool_error", err.Error() },
			MapRunnerErrorFunc: func(err error) (int, string, string) {
				if !errors.Is(err, boom) {
					t.Fatalf("unexpected error: %v", err)
				}
				return http.StatusBadGateway, "provider_invalid_reply", "provider invalid reply"
			},
		},
	})

	_, processErr := svc.Process(context.Background(), ProcessParams{
		Request:        domain.AgentProcessRequest{},
		EffectiveInput: []domain.AgentInputMessage{{Role: "user", Type: "message"}},
		Streaming:      false,
	}, nil)
	if processErr == nil {
		t.Fatalf("expected process error")
	}
	if processErr.Status != http.StatusBadGateway || processErr.Code != "provider_invalid_reply" {
		t.Fatalf("unexpected process error: %+v", processErr)
	}
}
