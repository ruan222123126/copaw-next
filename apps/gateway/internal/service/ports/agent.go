package ports

import (
	"context"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
)

type AgentRunner interface {
	GenerateTurn(
		ctx context.Context,
		req domain.AgentProcessRequest,
		cfg runner.GenerateConfig,
		tools []runner.ToolDefinition,
	) (runner.TurnResult, error)
	GenerateTurnStream(
		ctx context.Context,
		req domain.AgentProcessRequest,
		cfg runner.GenerateConfig,
		tools []runner.ToolDefinition,
		onDelta func(string),
	) (runner.TurnResult, error)
}

type RecoverableProviderToolCall struct {
	ID           string
	Name         string
	RawArguments string
	Input        map[string]interface{}
	Feedback     string
}

type AgentToolRuntime interface {
	ListToolDefinitions() []runner.ToolDefinition
	ExecuteToolCall(name string, input map[string]interface{}) (string, error)
	RecoverInvalidProviderToolCall(err error, step int) (RecoverableProviderToolCall, bool)
	FormatToolErrorFeedback(err error) string
}

type AgentErrorMapper interface {
	MapToolError(err error) (status int, code string, message string)
	MapRunnerError(err error) (status int, code string, message string)
}
