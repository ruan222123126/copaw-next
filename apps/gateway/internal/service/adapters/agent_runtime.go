package adapters

import (
	"context"
	"errors"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/ports"
)

type AgentRunner struct {
	Runner                 *runner.Runner
	GenerateTurnFunc       func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition) (runner.TurnResult, error)
	GenerateTurnStreamFunc func(context.Context, domain.AgentProcessRequest, runner.GenerateConfig, []runner.ToolDefinition, func(string)) (runner.TurnResult, error)
}

func (a AgentRunner) GenerateTurn(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg runner.GenerateConfig,
	tools []runner.ToolDefinition,
) (runner.TurnResult, error) {
	if a.GenerateTurnFunc != nil {
		return a.GenerateTurnFunc(ctx, req, cfg, tools)
	}
	if a.Runner == nil {
		return runner.TurnResult{}, errors.New("agent runner is unavailable")
	}
	return a.Runner.GenerateTurn(ctx, req, cfg, tools)
}

func (a AgentRunner) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg runner.GenerateConfig,
	tools []runner.ToolDefinition,
	onDelta func(string),
) (runner.TurnResult, error) {
	if a.GenerateTurnStreamFunc != nil {
		return a.GenerateTurnStreamFunc(ctx, req, cfg, tools, onDelta)
	}
	if a.Runner == nil {
		return runner.TurnResult{}, errors.New("agent runner is unavailable")
	}
	return a.Runner.GenerateTurnStream(ctx, req, cfg, tools, onDelta)
}

type AgentToolRuntime struct {
	ListToolDefinitionsFunc            func() []runner.ToolDefinition
	ExecuteToolCallFunc                func(name string, input map[string]interface{}) (string, error)
	RecoverInvalidProviderToolCallFunc func(err error, step int) (ports.RecoverableProviderToolCall, bool)
	FormatToolErrorFeedbackFunc        func(err error) string
}

func (a AgentToolRuntime) ListToolDefinitions() []runner.ToolDefinition {
	if a.ListToolDefinitionsFunc == nil {
		return nil
	}
	return a.ListToolDefinitionsFunc()
}

func (a AgentToolRuntime) ExecuteToolCall(name string, input map[string]interface{}) (string, error) {
	if a.ExecuteToolCallFunc == nil {
		return "", errors.New("agent tool runtime is unavailable")
	}
	return a.ExecuteToolCallFunc(name, input)
}

func (a AgentToolRuntime) RecoverInvalidProviderToolCall(err error, step int) (ports.RecoverableProviderToolCall, bool) {
	if a.RecoverInvalidProviderToolCallFunc == nil {
		return ports.RecoverableProviderToolCall{}, false
	}
	return a.RecoverInvalidProviderToolCallFunc(err, step)
}

func (a AgentToolRuntime) FormatToolErrorFeedback(err error) string {
	if a.FormatToolErrorFeedbackFunc != nil {
		return a.FormatToolErrorFeedbackFunc(err)
	}
	return defaultErrorMessage(err, "tool invocation failed")
}

type AgentErrorMapper struct {
	MapToolErrorFunc   func(err error) (status int, code string, message string)
	MapRunnerErrorFunc func(err error) (status int, code string, message string)
}

func (a AgentErrorMapper) MapToolError(err error) (status int, code string, message string) {
	if a.MapToolErrorFunc != nil {
		return a.MapToolErrorFunc(err)
	}
	return 500, "tool_error", defaultErrorMessage(err, "tool invocation failed")
}

func (a AgentErrorMapper) MapRunnerError(err error) (status int, code string, message string) {
	if a.MapRunnerErrorFunc != nil {
		return a.MapRunnerErrorFunc(err)
	}
	return 502, "runner_error", defaultErrorMessage(err, "runner execution failed")
}

func defaultErrorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return fallback
	}
	return msg
}
