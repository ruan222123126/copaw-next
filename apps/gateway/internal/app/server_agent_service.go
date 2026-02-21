package app

import (
	"context"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/runner"
	agentservice "nextai/apps/gateway/internal/service/agent"
)

func (s *Server) getAgentService() *agentservice.Service {
	if s.agentService == nil {
		s.agentService = s.newAgentService()
	}
	return s.agentService
}

func (s *Server) newAgentService() *agentservice.Service {
	return agentservice.NewService(agentservice.Dependencies{
		GenerateTurn: func(ctx context.Context, req domain.AgentProcessRequest, cfg runner.GenerateConfig, tools []runner.ToolDefinition) (runner.TurnResult, error) {
			return s.runner.GenerateTurn(ctx, req, cfg, tools)
		},
		GenerateTurnStream: func(
			ctx context.Context,
			req domain.AgentProcessRequest,
			cfg runner.GenerateConfig,
			tools []runner.ToolDefinition,
			onDelta func(string),
		) (runner.TurnResult, error) {
			return s.runner.GenerateTurnStream(ctx, req, cfg, tools, onDelta)
		},
		ListToolDefinitions: func() []runner.ToolDefinition {
			return s.listToolDefinitions()
		},
		ExecuteToolCall: func(call agentservice.ToolCall) (string, error) {
			return s.executeToolCall(toolCall{Name: call.Name, Input: call.Input})
		},
		RecoverInvalidProviderToolCall: func(err error, step int) (agentservice.RecoverableProviderToolCall, bool) {
			recovered, ok := recoverInvalidProviderToolCall(err, step)
			if !ok {
				return agentservice.RecoverableProviderToolCall{}, false
			}
			return agentservice.RecoverableProviderToolCall{
				ID:           recovered.ID,
				Name:         recovered.Name,
				RawArguments: recovered.RawArguments,
				Input:        recovered.Input,
				Feedback:     recovered.Feedback,
			}, true
		},
		FormatToolErrorFeedback: func(err error) string {
			return formatToolErrorFeedback(err)
		},
		MapToolError: func(err error) (status int, code string, message string) {
			return mapToolError(err)
		},
		MapRunnerError: func(err error) (status int, code string, message string) {
			return mapRunnerError(err)
		},
	})
}
