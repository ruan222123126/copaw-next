package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/ports"
)

type ToolCall struct {
	Name  string
	Input map[string]interface{}
}

type ProcessParams struct {
	Request           domain.AgentProcessRequest
	EffectiveInput    []domain.AgentInputMessage
	GenerateConfig    runner.GenerateConfig
	HasToolCall       bool
	RequestedToolCall ToolCall
	Streaming         bool
	ReplyChunkSize    int
}

type ProcessResult struct {
	Reply  string
	Events []domain.AgentEvent
}

type ProcessError struct {
	Status  int
	Code    string
	Message string
	Details interface{}
}

func (e *ProcessError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message)
}

type Dependencies struct {
	Runner      ports.AgentRunner
	ToolRuntime ports.AgentToolRuntime
	ErrorMapper ports.AgentErrorMapper
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) Process(
	ctx context.Context,
	params ProcessParams,
	emit func(evt domain.AgentEvent),
) (ProcessResult, *ProcessError) {
	if s == nil {
		return ProcessResult{}, &ProcessError{
			Status:  500,
			Code:    "agent_service_unavailable",
			Message: "agent service is unavailable",
		}
	}
	if err := s.validateDependencies(); err != nil {
		return ProcessResult{}, &ProcessError{
			Status:  500,
			Code:    "agent_service_misconfigured",
			Message: err.Error(),
		}
	}

	reply := ""
	events := make([]domain.AgentEvent, 0, 12)
	appendEvent := func(evt domain.AgentEvent) {
		events = append(events, evt)
		if emit != nil {
			emit(evt)
		}
	}
	replyChunkSize := params.ReplyChunkSize
	if replyChunkSize <= 0 {
		replyChunkSize = 12
	}
	appendReplyDeltas := func(step int, text string) {
		for _, chunk := range splitReplyChunks(text, replyChunkSize) {
			appendEvent(domain.AgentEvent{
				Type:  "assistant_delta",
				Step:  step,
				Delta: chunk,
			})
		}
	}

	if params.HasToolCall {
		step := 1
		appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
		appendEvent(domain.AgentEvent{
			Type: "tool_call",
			Step: step,
			ToolCall: &domain.AgentToolCallPayload{
				Name:  params.RequestedToolCall.Name,
				Input: safeMap(params.RequestedToolCall.Input),
			},
		})
		toolReply, err := s.deps.ToolRuntime.ExecuteToolCall(params.RequestedToolCall.Name, params.RequestedToolCall.Input)
		if err != nil {
			status, code, message := s.deps.ErrorMapper.MapToolError(err)
			return ProcessResult{}, &ProcessError{Status: status, Code: code, Message: message}
		}
		reply = toolReply
		appendEvent(domain.AgentEvent{
			Type: "tool_result",
			Step: step,
			ToolResult: &domain.AgentToolResultPayload{
				Name:    params.RequestedToolCall.Name,
				OK:      true,
				Summary: summarizeAgentEventText(reply),
			},
		})
		appendReplyDeltas(step, reply)
		appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
		return ProcessResult{Reply: reply, Events: events}, nil
	}

	workflowInput := cloneAgentInputMessages(params.EffectiveInput)
	step := 1

	for {
		appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
		turnReq := params.Request
		turnReq.Input = workflowInput

		stepHadStreamingDelta := false
		var (
			turn   runner.TurnResult
			runErr error
		)
		if params.Streaming {
			turn, runErr = s.deps.Runner.GenerateTurnStream(ctx, turnReq, params.GenerateConfig, s.deps.ToolRuntime.ListToolDefinitions(), func(delta string) {
				if delta == "" {
					return
				}
				stepHadStreamingDelta = true
				appendEvent(domain.AgentEvent{
					Type:  "assistant_delta",
					Step:  step,
					Delta: delta,
				})
			})
		} else {
			turn, runErr = s.deps.Runner.GenerateTurn(ctx, turnReq, params.GenerateConfig, s.deps.ToolRuntime.ListToolDefinitions())
		}
		if runErr != nil {
			if recoveredCall, recovered := s.deps.ToolRuntime.RecoverInvalidProviderToolCall(runErr, step); recovered {
				appendEvent(domain.AgentEvent{
					Type: "tool_call",
					Step: step,
					ToolCall: &domain.AgentToolCallPayload{
						Name:  recoveredCall.Name,
						Input: safeMap(recoveredCall.Input),
					},
				})
				appendEvent(domain.AgentEvent{
					Type: "tool_result",
					Step: step,
					ToolResult: &domain.AgentToolResultPayload{
						Name:    recoveredCall.Name,
						OK:      false,
						Summary: summarizeAgentEventText(recoveredCall.Feedback),
					},
				})
				workflowInput = append(workflowInput,
					domain.AgentInputMessage{
						Role:    "assistant",
						Type:    "message",
						Content: []domain.RuntimeContent{},
						Metadata: map[string]interface{}{
							"tool_calls": []map[string]interface{}{
								{
									"id":   recoveredCall.ID,
									"type": "function",
									"function": map[string]interface{}{
										"name":      recoveredCall.Name,
										"arguments": recoveredCall.RawArguments,
									},
								},
							},
						},
					},
					domain.AgentInputMessage{
						Role:    "tool",
						Type:    "message",
						Content: []domain.RuntimeContent{{Type: "text", Text: recoveredCall.Feedback}},
						Metadata: map[string]interface{}{
							"tool_call_id": recoveredCall.ID,
							"name":         recoveredCall.Name,
						},
					},
				)
				step++
				continue
			}
			status, code, message := s.deps.ErrorMapper.MapRunnerError(runErr)
			return ProcessResult{}, &ProcessError{Status: status, Code: code, Message: message}
		}
		if len(turn.ToolCalls) == 0 {
			reply = strings.TrimSpace(turn.Text)
			if reply == "" {
				reply = "(empty reply)"
			}
			if !params.Streaming || !stepHadStreamingDelta {
				appendReplyDeltas(step, reply)
			}
			appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
			break
		}

		assistantMessage := domain.AgentInputMessage{
			Role:     "assistant",
			Type:     "message",
			Content:  []domain.RuntimeContent{},
			Metadata: map[string]interface{}{"tool_calls": toAgentToolCallMetadata(turn.ToolCalls)},
		}
		if text := strings.TrimSpace(turn.Text); text != "" {
			assistantMessage.Content = []domain.RuntimeContent{{Type: "text", Text: text}}
		}
		workflowInput = append(workflowInput, assistantMessage)

		for _, call := range turn.ToolCalls {
			appendEvent(domain.AgentEvent{
				Type: "tool_call",
				Step: step,
				ToolCall: &domain.AgentToolCallPayload{
					Name:  call.Name,
					Input: safeMap(call.Arguments),
				},
			})
			toolReply, toolErr := s.deps.ToolRuntime.ExecuteToolCall(call.Name, safeMap(call.Arguments))
			if toolErr != nil {
				toolReply = s.deps.ToolRuntime.FormatToolErrorFeedback(toolErr)
				appendEvent(domain.AgentEvent{
					Type: "tool_result",
					Step: step,
					ToolResult: &domain.AgentToolResultPayload{
						Name:    call.Name,
						OK:      false,
						Summary: summarizeAgentEventText(toolReply),
					},
				})
				workflowInput = append(workflowInput, domain.AgentInputMessage{
					Role:    "tool",
					Type:    "message",
					Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
					Metadata: map[string]interface{}{
						"tool_call_id": call.ID,
						"name":         call.Name,
					},
				})
				continue
			}
			appendEvent(domain.AgentEvent{
				Type: "tool_result",
				Step: step,
				ToolResult: &domain.AgentToolResultPayload{
					Name:    call.Name,
					OK:      true,
					Summary: summarizeAgentEventText(toolReply),
				},
			})
			workflowInput = append(workflowInput, domain.AgentInputMessage{
				Role:    "tool",
				Type:    "message",
				Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
				Metadata: map[string]interface{}{
					"tool_call_id": call.ID,
					"name":         call.Name,
				},
			})
		}
		step++
	}

	return ProcessResult{Reply: reply, Events: events}, nil
}

func (s *Service) validateDependencies() error {
	switch {
	case s.deps.Runner == nil:
		return errors.New("missing agent runner dependency")
	case s.deps.ToolRuntime == nil:
		return errors.New("missing agent tool runtime dependency")
	case s.deps.ErrorMapper == nil:
		return errors.New("missing agent error mapper dependency")
	default:
		return nil
	}
}

func splitReplyChunks(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = 12
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func cloneAgentInputMessages(input []domain.AgentInputMessage) []domain.AgentInputMessage {
	if len(input) == 0 {
		return []domain.AgentInputMessage{}
	}
	out := make([]domain.AgentInputMessage, 0, len(input))
	for _, item := range input {
		cloned := domain.AgentInputMessage{
			Role:    item.Role,
			Type:    item.Type,
			Content: append([]domain.RuntimeContent{}, item.Content...),
		}
		if item.Metadata != nil {
			data, err := json.Marshal(item.Metadata)
			if err == nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err == nil {
					cloned.Metadata = meta
				}
			}
		}
		out = append(out, cloned)
	}
	return out
}

func toAgentToolCallMetadata(calls []runner.ToolCall) []map[string]interface{} {
	if len(calls) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = fmt.Sprintf("tool-call-%s", call.Name)
		}
		args, _ := json.Marshal(safeMap(call.Arguments))
		out = append(out, map[string]interface{}{
			"id":   callID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      call.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}

func summarizeAgentEventText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 160 {
		return trimmed
	}
	return string(runes[:160]) + "..."
}

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		out := map[string]interface{}{}
		for key, value := range v {
			out[key] = value
		}
		return out
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		fallback := map[string]interface{}{}
		for key, value := range v {
			fallback[key] = value
		}
		return fallback
	}
	return out
}
