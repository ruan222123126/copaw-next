package app

import (
	"nextai/apps/gateway/internal/runner"
	"nextai/apps/gateway/internal/service/adapters"
	agentservice "nextai/apps/gateway/internal/service/agent"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Server) getAgentService() *agentservice.Service {
	if s.agentService == nil {
		s.agentService = s.newAgentService()
	}
	return s.agentService
}

func (s *Server) newAgentService() *agentservice.Service {
	return agentservice.NewService(agentservice.Dependencies{
		Runner: adapters.AgentRunner{Runner: s.runner},
		ToolRuntime: adapters.AgentToolRuntime{
			ListToolDefinitionsFunc: func() []runner.ToolDefinition {
				return s.listToolDefinitions()
			},
			ExecuteToolCallFunc: func(name string, input map[string]interface{}) (string, error) {
				return s.executeToolCall(toolCall{Name: name, Input: input})
			},
			RecoverInvalidProviderToolCallFunc: func(err error, step int) (ports.RecoverableProviderToolCall, bool) {
				recovered, ok := recoverInvalidProviderToolCall(err, step)
				if !ok {
					return ports.RecoverableProviderToolCall{}, false
				}
				return ports.RecoverableProviderToolCall{
					ID:           recovered.ID,
					Name:         recovered.Name,
					RawArguments: recovered.RawArguments,
					Input:        recovered.Input,
					Feedback:     recovered.Feedback,
				}, true
			},
			FormatToolErrorFeedbackFunc: func(err error) string {
				return formatToolErrorFeedback(err)
			},
		},
		ErrorMapper: adapters.AgentErrorMapper{
			MapToolErrorFunc: func(err error) (status int, code string, message string) {
				return mapToolError(err)
			},
			MapRunnerErrorFunc: func(err error) (status int, code string, message string) {
				return mapRunnerError(err)
			},
		},
	})
}
