package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/runner"
)

func TestContractRegressionMapRunnerError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name: "provider_not_configured",
			err: &runner.RunnerError{
				Code:    runner.ErrorCodeProviderNotConfigured,
				Message: "model is required for active provider",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    runner.ErrorCodeProviderNotConfigured,
			wantMessage: "model is required for active provider",
		},
		{
			name: "provider_not_supported",
			err: &runner.RunnerError{
				Code:    runner.ErrorCodeProviderNotSupported,
				Message: "provider is not supported",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    runner.ErrorCodeProviderNotSupported,
			wantMessage: "provider is not supported",
		},
		{
			name: "provider_request_failed",
			err: &runner.RunnerError{
				Code:    runner.ErrorCodeProviderRequestFailed,
				Message: "provider request failed",
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    runner.ErrorCodeProviderRequestFailed,
			wantMessage: "provider request failed",
		},
		{
			name: "provider_invalid_reply",
			err: &runner.RunnerError{
				Code:    runner.ErrorCodeProviderInvalidReply,
				Message: "provider response has empty content",
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    runner.ErrorCodeProviderInvalidReply,
			wantMessage: "provider response has empty content",
		},
		{
			name: "unknown_runner_error_code",
			err: &runner.RunnerError{
				Code:    "unexpected_code",
				Message: "unexpected",
			},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "runner_error",
			wantMessage: "runner execution failed",
		},
		{
			name:        "non_runner_error",
			err:         errors.New("boom"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "runner_error",
			wantMessage: "runner execution failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, code, message := mapRunnerError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status=%d, want=%d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Fatalf("code=%q, want=%q", code, tc.wantCode)
			}
			if message != tc.wantMessage {
				t.Fatalf("message=%q, want=%q", message, tc.wantMessage)
			}
		})
	}
}

func TestContractRegressionMapChannelError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name: "invalid_channel",
			err: &channelError{
				Code:    "invalid_channel",
				Message: "channel is required",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_channel",
			wantMessage: "channel is required",
		},
		{
			name: "channel_not_supported",
			err: &channelError{
				Code:    "channel_not_supported",
				Message: "channel is not supported",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "channel_not_supported",
			wantMessage: "channel is not supported",
		},
		{
			name: "channel_disabled",
			err: &channelError{
				Code:    "channel_disabled",
				Message: "channel is disabled",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "channel_disabled",
			wantMessage: "channel is disabled",
		},
		{
			name: "channel_dispatch_failed",
			err: &channelError{
				Code:    "channel_dispatch_failed",
				Message: "dispatch failed",
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    "channel_dispatch_failed",
			wantMessage: "dispatch failed",
		},
		{
			name: "unknown_channel_error_code",
			err: &channelError{
				Code:    "unexpected_channel_code",
				Message: "unexpected",
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    "channel_dispatch_failed",
			wantMessage: "channel dispatch failed",
		},
		{
			name:        "non_channel_error",
			err:         errors.New("boom"),
			wantStatus:  http.StatusBadGateway,
			wantCode:    "channel_dispatch_failed",
			wantMessage: "channel dispatch failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, code, message := mapChannelError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status=%d, want=%d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Fatalf("code=%q, want=%q", code, tc.wantCode)
			}
			if message != tc.wantMessage {
				t.Fatalf("message=%q, want=%q", message, tc.wantMessage)
			}
		})
	}
}

func TestContractRegressionMapToolError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name: "tool_disabled",
			err: &toolError{
				Code:    "tool_disabled",
				Message: "tool shell is disabled",
			},
			wantStatus:  http.StatusForbidden,
			wantCode:    "tool_disabled",
			wantMessage: "tool shell is disabled",
		},
		{
			name: "tool_not_supported",
			err: &toolError{
				Code:    "tool_not_supported",
				Message: "tool is not supported",
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "tool_not_supported",
			wantMessage: "tool is not supported",
		},
		{
			name: "tool_invoke_failed_shell_command_missing",
			err: &toolError{
				Code: "tool_invoke_failed",
				Err:  plugin.ErrShellToolCommandMissing,
			},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_tool_input",
			wantMessage: "tool input command is required",
		},
		{
			name: "tool_invoke_failed_shell_executor_unavailable",
			err: &toolError{
				Code: "tool_invoke_failed",
				Err:  plugin.ErrShellToolExecutorUnavailable,
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    "tool_runtime_unavailable",
			wantMessage: "shell executor is unavailable on current host",
		},
		{
			name: "tool_invoke_failed_generic",
			err: &toolError{
				Code:    "tool_invoke_failed",
				Message: "tool invoke failed",
				Err:     errors.New("network timeout"),
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    "tool_invoke_failed",
			wantMessage: "tool invoke failed",
		},
		{
			name: "tool_invalid_result",
			err: &toolError{
				Code:    "tool_invalid_result",
				Message: "tool result format invalid",
			},
			wantStatus:  http.StatusBadGateway,
			wantCode:    "tool_invalid_result",
			wantMessage: "tool result format invalid",
		},
		{
			name: "unknown_tool_error_code",
			err: &toolError{
				Code:    "unexpected_tool_code",
				Message: "unexpected",
			},
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "tool_error",
			wantMessage: "tool execution failed",
		},
		{
			name:        "non_tool_error",
			err:         errors.New("boom"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "tool_error",
			wantMessage: "tool execution failed",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, code, message := mapToolError(tc.err)
			if status != tc.wantStatus {
				t.Fatalf("status=%d, want=%d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Fatalf("code=%q, want=%q", code, tc.wantCode)
			}
			if message != tc.wantMessage {
				t.Fatalf("message=%q, want=%q", message, tc.wantMessage)
			}
		})
	}
}

func TestContractRegressionProcessAgentStreamEventOrder(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"stream tool regression"}]}],
		"session_id":"s-stream-events-regression",
		"user_id":"u-stream-events-regression",
		"channel":"console",
		"stream":true,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"printf stream-regression"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	sequence, err := collectSSEDataSequence(w.Body.String())
	if err != nil {
		t.Fatalf("parse sse sequence failed: %v body=%s", err, w.Body.String())
	}
	if len(sequence) == 0 {
		t.Fatalf("expected non-empty sse sequence, body=%s", w.Body.String())
	}
	if got := sequence[len(sequence)-1]; got != "[DONE]" {
		t.Fatalf("expected final marker [DONE], got=%q sequence=%v", got, sequence)
	}

	mustHaveOrdered := []string{"step_started", "tool_call", "tool_result", "assistant_delta", "completed", "[DONE]"}
	last := -1
	for _, eventType := range mustHaveOrdered {
		idx := indexOfEvent(sequence, eventType)
		if idx < 0 {
			t.Fatalf("missing event %q, sequence=%v", eventType, sequence)
		}
		if idx <= last {
			t.Fatalf("event order broken at %q, sequence=%v", eventType, sequence)
		}
		last = idx
	}
}

func TestContractRegressionProcessAgentProviderInvalidReply(t *testing.T) {
	var calls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer mock.Close()

	srv := newTestServer(t)

	configProvider := `{"api_key":"sk-test","base_url":"` + mock.URL + `"}`
	wConfig := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wConfig, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if wConfig.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", wConfig.Code, wConfig.Body.String())
	}
	wActive := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wActive, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(`{"provider_id":"openai","model":"gpt-4o-mini"}`)))
	if wActive.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", wActive.Code, wActive.Body.String())
	}

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"provider invalid reply"}]}],
		"session_id":"s-provider-invalid-reply",
		"user_id":"u-provider-invalid-reply",
		"channel":"console",
		"stream":false
	}`
	wProcess := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wProcess, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if wProcess.Code != http.StatusBadGateway {
		t.Fatalf("expected status=502, got=%d body=%s", wProcess.Code, wProcess.Body.String())
	}
	if calls == 0 {
		t.Fatalf("expected provider to be called at least once")
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(wProcess.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode process error failed: %v body=%s", err, wProcess.Body.String())
	}
	if resp.Error.Code != runner.ErrorCodeProviderInvalidReply {
		t.Fatalf("error.code=%q, want=%q body=%s", resp.Error.Code, runner.ErrorCodeProviderInvalidReply, wProcess.Body.String())
	}
	if resp.Error.Message != "provider response has empty content" {
		t.Fatalf("error.message=%q, want=%q body=%s", resp.Error.Message, "provider response has empty content", wProcess.Body.String())
	}
}

func TestContractRegressionCronEndpointErrors(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "create_invalid_json",
			method:      http.MethodPost,
			path:        "/cron/jobs",
			body:        "{",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
			wantMessage: "invalid request body",
		},
		{
			name:   "create_invalid_task_type",
			method: http.MethodPost,
			path:   "/cron/jobs",
			body: `{
				"id":"job-invalid-task-type",
				"name":"job-invalid-task-type",
				"task_type":"unknown",
				"schedule":{"type":"interval","cron":"60s"}
			}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_cron_task_type",
			wantMessage: `unsupported task_type="unknown"`,
		},
		{
			name:        "update_job_id_mismatch",
			method:      http.MethodPut,
			path:        "/cron/jobs/job-a",
			body:        `{"id":"job-b","name":"job-b","task_type":"text","text":"hello"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "job_id_mismatch",
			wantMessage: "job_id mismatch",
		},
		{
			name:        "get_not_found",
			method:      http.MethodGet,
			path:        "/cron/jobs/not-exists",
			body:        "",
			wantStatus:  http.StatusNotFound,
			wantCode:    "not_found",
			wantMessage: "cron job not found",
		},
		{
			name:        "run_not_found",
			method:      http.MethodPost,
			path:        "/cron/jobs/not-exists/run",
			body:        "",
			wantStatus:  http.StatusNotFound,
			wantCode:    "not_found",
			wantMessage: "cron job not found",
		},
		{
			name:        "delete_default_cron_protected",
			method:      http.MethodDelete,
			path:        "/cron/jobs/" + domain.DefaultCronJobID,
			body:        "",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "default_cron_protected",
			wantMessage: "default cron job cannot be deleted",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			w := callJSONEndpoint(srv, tc.method, tc.path, tc.body)
			assertAPIError(t, w, tc.wantStatus, tc.wantCode, tc.wantMessage)
		})
	}
}

func TestContractRegressionWorkspaceEndpointErrors(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "get_invalid_path",
			method:      http.MethodGet,
			path:        "/workspace/files/%2e%2e/oops",
			body:        "",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_path",
			wantMessage: "invalid workspace file path",
		},
		{
			name:        "get_not_found",
			method:      http.MethodGet,
			path:        "/workspace/files/skills/not-exists.json",
			body:        "",
			wantStatus:  http.StatusNotFound,
			wantCode:    "not_found",
			wantMessage: "workspace file not found",
		},
		{
			name:        "put_config_invalid_json",
			method:      http.MethodPut,
			path:        "/workspace/files/config/envs.json",
			body:        "{",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
			wantMessage: "invalid request body",
		},
		{
			name:        "put_active_llm_invalid_model_slot",
			method:      http.MethodPut,
			path:        "/workspace/files/config/active-llm.json",
			body:        `{"provider_id":"openai"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_model_slot",
			wantMessage: "provider_id and model must be set together",
		},
		{
			name:        "put_active_llm_provider_not_found",
			method:      http.MethodPut,
			path:        "/workspace/files/config/active-llm.json",
			body:        `{"provider_id":"ghost-provider","model":"ghost-model"}`,
			wantStatus:  http.StatusNotFound,
			wantCode:    "provider_not_found",
			wantMessage: "provider not found",
		},
		{
			name:        "delete_config_file_method_not_allowed",
			method:      http.MethodDelete,
			path:        "/workspace/files/config/envs.json",
			body:        "",
			wantStatus:  http.StatusMethodNotAllowed,
			wantCode:    "method_not_allowed",
			wantMessage: "config files cannot be deleted",
		},
		{
			name:        "import_invalid_mode",
			method:      http.MethodPost,
			path:        "/workspace/import",
			body:        `{"mode":"merge","payload":{"version":"v1"}}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_import_mode",
			wantMessage: "mode must be replace",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			w := callJSONEndpoint(srv, tc.method, tc.path, tc.body)
			assertAPIError(t, w, tc.wantStatus, tc.wantCode, tc.wantMessage)
		})
	}
}

func TestContractRegressionModelsEndpointErrors(t *testing.T) {
	cases := []struct {
		name        string
		prepare     func(t *testing.T, srv *Server)
		method      string
		path        string
		body        string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "configure_invalid_json",
			method:      http.MethodPut,
			path:        "/models/openai/config",
			body:        "{",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
			wantMessage: "invalid request body",
		},
		{
			name:        "configure_negative_timeout",
			method:      http.MethodPut,
			path:        "/models/openai/config",
			body:        `{"timeout_ms":-1}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_provider_config",
			wantMessage: "timeout_ms must be >= 0",
		},
		{
			name:        "configure_invalid_aliases",
			method:      http.MethodPut,
			path:        "/models/openai/config",
			body:        `{"model_aliases":{"":"gpt-4o-mini"}}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_provider_config",
			wantMessage: "model_aliases requires non-empty key and value",
		},
		{
			name:        "set_active_invalid_json",
			method:      http.MethodPut,
			path:        "/models/active",
			body:        "{",
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_json",
			wantMessage: "invalid request body",
		},
		{
			name:        "set_active_invalid_model_slot",
			method:      http.MethodPut,
			path:        "/models/active",
			body:        `{"provider_id":"openai"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_model_slot",
			wantMessage: "provider_id and model are required",
		},
		{
			name:        "set_active_provider_not_found",
			method:      http.MethodPut,
			path:        "/models/active",
			body:        `{"provider_id":"ghost-provider","model":"ghost-model"}`,
			wantStatus:  http.StatusNotFound,
			wantCode:    "provider_not_found",
			wantMessage: "provider not found",
		},
		{
			name: "set_active_provider_disabled",
			prepare: func(t *testing.T, srv *Server) {
				t.Helper()
				w := callJSONEndpoint(srv, http.MethodPut, "/models/openai/config", `{"enabled":false}`)
				if w.Code != http.StatusOK {
					t.Fatalf("prepare disable openai failed: status=%d body=%s", w.Code, w.Body.String())
				}
			},
			method:      http.MethodPut,
			path:        "/models/active",
			body:        `{"provider_id":"openai","model":"gpt-4o-mini"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "provider_disabled",
			wantMessage: "provider is disabled",
		},
		{
			name: "set_active_model_not_found",
			prepare: func(t *testing.T, srv *Server) {
				t.Helper()
				w := callJSONEndpoint(srv, http.MethodPut, "/models/openai/config", `{"enabled":true}`)
				if w.Code != http.StatusOK {
					t.Fatalf("prepare enable openai failed: status=%d body=%s", w.Code, w.Body.String())
				}
			},
			method:      http.MethodPut,
			path:        "/models/active",
			body:        `{"provider_id":"openai","model":"model-not-exists"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "model_not_found",
			wantMessage: "model not found for provider",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			if tc.prepare != nil {
				tc.prepare(t, srv)
			}
			w := callJSONEndpoint(srv, tc.method, tc.path, tc.body)
			assertAPIError(t, w, tc.wantStatus, tc.wantCode, tc.wantMessage)
		})
	}
}

func TestContractRegressionListChatsResponseShape(t *testing.T) {
	srv := newTestServer(t)

	createBody := `{
		"id":"chat-shape-stable",
		"name":"shape-stable-chat",
		"session_id":"s-shape-stable",
		"user_id":"u-shape-stable",
		"channel":"console",
		"meta":{"source":"contract-regression"}
	}`
	wCreate := callJSONEndpoint(srv, http.MethodPost, "/chats", createBody)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create chat failed: status=%d body=%s", wCreate.Code, wCreate.Body.String())
	}

	wList := callJSONEndpoint(srv, http.MethodGet, "/chats?user_id=u-shape-stable&channel=console", "")
	if wList.Code != http.StatusOK {
		t.Fatalf("list chats failed: status=%d body=%s", wList.Code, wList.Body.String())
	}
	chats := decodeJSONArrayObjects(t, wList)
	if len(chats) != 1 {
		t.Fatalf("expected exactly 1 chat, got=%d body=%s", len(chats), wList.Body.String())
	}
	chat := chats[0]
	assertObjectHasExactKeys(t, chat, []string{
		"id", "name", "session_id", "user_id", "channel", "created_at", "updated_at", "meta",
	})
	assertStringField(t, chat, "id")
	assertStringField(t, chat, "name")
	assertStringField(t, chat, "session_id")
	assertStringField(t, chat, "user_id")
	assertStringField(t, chat, "channel")
	assertStringField(t, chat, "created_at")
	assertStringField(t, chat, "updated_at")
	assertObjectField(t, chat, "meta")
}

func TestContractRegressionListCronJobsResponseShape(t *testing.T) {
	srv := newTestServer(t)

	w := callJSONEndpoint(srv, http.MethodGet, "/cron/jobs", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list cron jobs failed: status=%d body=%s", w.Code, w.Body.String())
	}
	jobs := decodeJSONArrayObjects(t, w)
	if len(jobs) == 0 {
		t.Fatalf("expected non-empty cron jobs list, body=%s", w.Body.String())
	}

	var defaultJob map[string]interface{}
	for _, item := range jobs {
		if id, _ := item["id"].(string); id == domain.DefaultCronJobID {
			defaultJob = item
			break
		}
	}
	if defaultJob == nil {
		t.Fatalf("expected default cron job %q in list, body=%s", domain.DefaultCronJobID, w.Body.String())
	}

	assertObjectHasKeys(t, defaultJob, []string{
		"id", "name", "enabled", "schedule", "task_type", "text", "dispatch", "runtime", "meta",
	})

	schedule := assertObjectField(t, defaultJob, "schedule")
	assertObjectHasExactKeys(t, schedule, []string{"type", "cron", "timezone"})

	dispatch := assertObjectField(t, defaultJob, "dispatch")
	assertObjectHasExactKeys(t, dispatch, []string{"type", "channel", "target", "mode", "meta"})
	target := assertObjectField(t, dispatch, "target")
	assertObjectHasExactKeys(t, target, []string{"user_id", "session_id"})

	runtime := assertObjectField(t, defaultJob, "runtime")
	assertObjectHasExactKeys(t, runtime, []string{"max_concurrency", "timeout_seconds", "misfire_grace_seconds"})

	meta := assertObjectField(t, defaultJob, "meta")
	if raw, ok := meta[domain.CronMetaSystemDefault]; !ok || raw != true {
		t.Fatalf("expected meta.%s=true, got=%v", domain.CronMetaSystemDefault, meta[domain.CronMetaSystemDefault])
	}
}

func TestContractRegressionListWorkspaceFilesResponseShape(t *testing.T) {
	srv := newTestServer(t)

	putSkillBody := `{
		"name":"shape-contract",
		"content":"shape stable content",
		"enabled":true
	}`
	wPut := callJSONEndpoint(srv, http.MethodPut, "/workspace/files/skills/shape-contract.json", putSkillBody)
	if wPut.Code != http.StatusOK {
		t.Fatalf("put skill failed: status=%d body=%s", wPut.Code, wPut.Body.String())
	}

	w := callJSONEndpoint(srv, http.MethodGet, "/workspace/files", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list workspace files failed: status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeJSONObject(t, w)
	assertObjectHasExactKeys(t, resp, []string{"files"})

	filesRaw, ok := resp["files"].([]interface{})
	if !ok {
		t.Fatalf("files field is not array, body=%s", w.Body.String())
	}
	if len(filesRaw) == 0 {
		t.Fatalf("expected non-empty files list, body=%s", w.Body.String())
	}

	paths := map[string]bool{}
	for idx, item := range filesRaw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("files[%d] is not object: %#v", idx, item)
		}
		assertObjectHasExactKeys(t, obj, []string{"path", "kind", "size"})
		path := assertStringField(t, obj, "path")
		_ = assertStringField(t, obj, "kind")
		assertWholeNumberField(t, obj, "size")
		paths[path] = true
	}

	for _, required := range []string{
		"config/envs.json",
		"config/channels.json",
		"config/models.json",
		"config/active-llm.json",
		"skills/shape-contract.json",
	} {
		if !paths[required] {
			t.Fatalf("expected file path %q in list, body=%s", required, w.Body.String())
		}
	}
}

func TestContractRegressionModelCatalogResponseShape(t *testing.T) {
	srv := newTestServer(t)

	w := callJSONEndpoint(srv, http.MethodGet, "/models/catalog", "")
	if w.Code != http.StatusOK {
		t.Fatalf("models catalog failed: status=%d body=%s", w.Code, w.Body.String())
	}
	catalog := decodeJSONObject(t, w)
	assertObjectHasExactKeys(t, catalog, []string{"providers", "defaults", "active_llm", "provider_types"})

	active := assertObjectField(t, catalog, "active_llm")
	assertObjectHasExactKeys(t, active, []string{"provider_id", "model"})

	defaults := assertObjectField(t, catalog, "defaults")
	openaiDefault, ok := defaults["openai"].(string)
	if !ok || strings.TrimSpace(openaiDefault) == "" {
		t.Fatalf("expected defaults.openai to be a non-empty string, body=%s", w.Body.String())
	}

	providerTypesRaw, ok := catalog["provider_types"].([]interface{})
	if !ok || len(providerTypesRaw) == 0 {
		t.Fatalf("expected non-empty provider_types, body=%s", w.Body.String())
	}
	for idx, item := range providerTypesRaw {
		obj, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("provider_types[%d] is not object: %#v", idx, item)
		}
		assertObjectHasExactKeys(t, obj, []string{"id", "display_name"})
		assertStringField(t, obj, "id")
		assertStringField(t, obj, "display_name")
	}

	providers := decodeObjectArrayField(t, catalog, "providers")
	if len(providers) == 0 {
		t.Fatalf("expected non-empty providers, body=%s", w.Body.String())
	}

	var openai map[string]interface{}
	for _, p := range providers {
		if id, _ := p["id"].(string); id == "openai" {
			openai = p
			break
		}
	}
	if openai == nil {
		t.Fatalf("expected provider openai in catalog, body=%s", w.Body.String())
	}

	assertObjectHasKeys(t, openai, []string{
		"id",
		"name",
		"display_name",
		"openai_compatible",
		"api_key_prefix",
		"models",
		"allow_custom_base_url",
		"enabled",
		"has_api_key",
		"current_api_key",
		"current_base_url",
	})

	modelsRaw, ok := openai["models"].([]interface{})
	if !ok || len(modelsRaw) == 0 {
		t.Fatalf("expected non-empty openai.models, body=%s", w.Body.String())
	}
	firstModel, ok := modelsRaw[0].(map[string]interface{})
	if !ok {
		t.Fatalf("openai.models[0] is not object: %#v", modelsRaw[0])
	}
	assertObjectHasKeys(t, firstModel, []string{"id", "name"})
}

func TestContractRegressionSuccessResponseShapes(t *testing.T) {
	t.Run("create_cron_job_success_shape", func(t *testing.T) {
		srv := newTestServer(t)

		body := `{
			"id":"job-shape-success",
			"name":"job-shape-success",
			"enabled":true,
			"schedule":{"type":"interval","cron":"60s"},
			"task_type":"text",
			"text":"hello shape",
			"dispatch":{"channel":"console","target":{"session_id":"s-shape-success","user_id":"u-shape-success"}}
		}`
		w := callJSONEndpoint(srv, http.MethodPost, "/cron/jobs", body)
		if w.Code != http.StatusOK {
			t.Fatalf("create cron job failed: status=%d body=%s", w.Code, w.Body.String())
		}
		created := decodeJSONObject(t, w)
		assertObjectHasKeys(t, created, []string{
			"id", "name", "enabled", "schedule", "task_type", "text", "dispatch", "runtime", "meta",
		})
	})

	t.Run("workspace_put_envs_success_shape", func(t *testing.T) {
		srv := newTestServer(t)
		w := callJSONEndpoint(srv, http.MethodPut, "/workspace/files/config/envs.json", `{"A":"B"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("workspace put envs failed: status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSONObject(t, w)
		assertObjectHasExactKeys(t, resp, []string{"updated"})
		if updated, ok := resp["updated"].(bool); !ok || !updated {
			t.Fatalf("expected updated=true, body=%s", w.Body.String())
		}
	})

	t.Run("delete_provider_success_shape", func(t *testing.T) {
		srv := newTestServer(t)
		w := callJSONEndpoint(srv, http.MethodDelete, "/models/openai", "")
		if w.Code != http.StatusOK {
			t.Fatalf("delete provider failed: status=%d body=%s", w.Code, w.Body.String())
		}
		resp := decodeJSONObject(t, w)
		assertObjectHasExactKeys(t, resp, []string{"deleted"})
		if deleted, ok := resp["deleted"].(bool); !ok || !deleted {
			t.Fatalf("expected deleted=true, body=%s", w.Body.String())
		}
	})
}

func collectSSEDataSequence(body string) ([]string, error) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			out = append(out, "[DONE]")
			continue
		}
		var evt struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			return nil, err
		}
		if strings.TrimSpace(evt.Type) != "" {
			out = append(out, evt.Type)
		}
	}
	return out, nil
}

func indexOfEvent(sequence []string, target string) int {
	for idx, value := range sequence {
		if value == target {
			return idx
		}
	}
	return -1
}

func callJSONEndpoint(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if strings.TrimSpace(body) != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode, wantMessage string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status=%d, want=%d body=%s", w.Code, wantStatus, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response failed: %v body=%s", err, w.Body.String())
	}
	if resp.Error.Code != wantCode {
		t.Fatalf("error.code=%q, want=%q body=%s", resp.Error.Code, wantCode, w.Body.String())
	}
	if resp.Error.Message != wantMessage {
		t.Fatalf("error.message=%q, want=%q body=%s", resp.Error.Message, wantMessage, w.Body.String())
	}
}

func decodeJSONObject(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode json object failed: %v body=%s", err, w.Body.String())
	}
	return out
}

func decodeJSONArrayObjects(t *testing.T, w *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var raw []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode json array failed: %v body=%s", err, w.Body.String())
	}
	return raw
}

func decodeObjectArrayField(t *testing.T, obj map[string]interface{}, key string) []map[string]interface{} {
	t.Helper()
	raw, ok := obj[key].([]interface{})
	if !ok {
		t.Fatalf("field %q is not array: %#v", key, obj[key])
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for idx, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("field %q[%d] is not object: %#v", key, idx, item)
		}
		out = append(out, m)
	}
	return out
}

func assertObjectField(t *testing.T, obj map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	raw, ok := obj[key]
	if !ok {
		t.Fatalf("missing object field %q in %#v", key, obj)
	}
	out, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("field %q is not object: %#v", key, raw)
	}
	return out
}

func assertStringField(t *testing.T, obj map[string]interface{}, key string) string {
	t.Helper()
	raw, ok := obj[key]
	if !ok {
		t.Fatalf("missing string field %q in %#v", key, obj)
	}
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("field %q is not string: %#v", key, raw)
	}
	return value
}

func assertWholeNumberField(t *testing.T, obj map[string]interface{}, key string) int {
	t.Helper()
	raw, ok := obj[key]
	if !ok {
		t.Fatalf("missing numeric field %q in %#v", key, obj)
	}
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("field %q is not number: %#v", key, raw)
	}
	whole := int(value)
	if value != float64(whole) {
		t.Fatalf("field %q is not whole number: %v", key, value)
	}
	return whole
}

func assertObjectHasKeys(t *testing.T, obj map[string]interface{}, required []string) {
	t.Helper()
	for _, key := range required {
		if _, ok := obj[key]; !ok {
			t.Fatalf("missing required key %q in object %#v", key, obj)
		}
	}
}

func assertObjectHasExactKeys(t *testing.T, obj map[string]interface{}, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(obj))
	for key := range obj {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	expectedCopy := append([]string{}, expected...)
	sort.Strings(expectedCopy)
	if len(actual) != len(expectedCopy) {
		t.Fatalf("object keys mismatch: actual=%v expected=%v", actual, expectedCopy)
	}
	for idx := range actual {
		if actual[idx] != expectedCopy[idx] {
			t.Fatalf("object keys mismatch: actual=%v expected=%v", actual, expectedCopy)
		}
	}
}
