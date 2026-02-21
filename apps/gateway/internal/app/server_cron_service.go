package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/service/adapters"
	cronservice "nextai/apps/gateway/internal/service/cron"
	"nextai/apps/gateway/internal/service/ports"
)

func (s *Server) getCronService() *cronservice.Service {
	if s.cronService == nil {
		s.cronService = s.newCronService()
	}
	return s.cronService
}

func (s *Server) newCronService() *cronservice.Service {
	return cronservice.NewService(cronservice.Dependencies{
		Store:   s.stateStore,
		DataDir: s.cfg.DataDir,
		ChannelResolver: adapters.ChannelResolver{
			ResolveChannelFunc: func(name string) (ports.Channel, map[string]interface{}, string, error) {
				return s.resolveChannel(name)
			},
		},
		ExecuteConsoleAgentTask: func(ctx context.Context, job domain.CronJobSpec, text string) error {
			return s.executeCronConsoleAgentTask(ctx, job, text)
		},
		ExecuteTask: func(ctx context.Context, job domain.CronJobSpec) (bool, error) {
			if s.cronTaskExecutor == nil {
				return false, nil
			}
			return true, s.cronTaskExecutor(ctx, job)
		},
	})
}

func (s *Server) executeCronConsoleAgentTask(ctx context.Context, job domain.CronJobSpec, text string) error {
	sessionID := strings.TrimSpace(job.Dispatch.Target.SessionID)
	userID := strings.TrimSpace(job.Dispatch.Target.UserID)
	if sessionID == "" || userID == "" {
		return errors.New("cron dispatch target requires non-empty session_id and user_id")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	agentReq := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: text},
				},
			},
		},
		SessionID: sessionID,
		UserID:    userID,
		Channel:   "console",
		Stream:    false,
		BizParams: cronservice.BuildBizParams(job),
	}

	body, err := json.Marshal(agentReq)
	if err != nil {
		return fmt.Errorf("cron console agent request marshal failed: %w", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/agent/process", nil).WithContext(ctx)
	s.processAgentWithBody(recorder, request, body)

	status := recorder.Result().StatusCode
	if status >= http.StatusBadRequest {
		return fmt.Errorf("cron console agent execution failed: status=%d body=%s", status, strings.TrimSpace(recorder.Body.String()))
	}

	return nil
}
