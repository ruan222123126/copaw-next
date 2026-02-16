package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"copaw-next/apps/gateway/internal/config"
	"copaw-next/apps/gateway/internal/domain"
	"copaw-next/apps/gateway/internal/repo"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "copaw-next-gateway-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return newTestServerWithDataDir(t, dir)
}

func newTestServerWithDataDir(t *testing.T, dataDir string) *Server {
	t.Helper()
	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
}

func TestAPIKeyAuthMiddleware(t *testing.T) {
	dir, err := os.MkdirTemp("", "copaw-next-gateway-auth-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dir, APIKey: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(healthW, healthReq)
	if healthW.Code != http.StatusOK {
		t.Fatalf("health endpoint should bypass auth, got=%d", healthW.Code)
	}

	noAuthReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	noAuthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(noAuthW, noAuthReq)
	if noAuthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got=%d body=%s", noAuthW.Code, noAuthW.Body.String())
	}
	if !strings.Contains(noAuthW.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected unauthorized body: %s", noAuthW.Body.String())
	}

	apiKeyReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	apiKeyReq.Header.Set("X-API-Key", "secret-token")
	apiKeyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(apiKeyW, apiKeyReq)
	if apiKeyW.Code != http.StatusOK {
		t.Fatalf("expected authorized status via X-API-Key, got=%d body=%s", apiKeyW.Code, apiKeyW.Body.String())
	}

	bearerReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	bearerReq.Header.Set("Authorization", "Bearer secret-token")
	bearerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(bearerW, bearerReq)
	if bearerW.Code != http.StatusOK {
		t.Fatalf("expected authorized status via bearer, got=%d body=%s", bearerW.Code, bearerW.Body.String())
	}
}

func TestChatCreateAndGetHistory(t *testing.T) {
	srv := newTestServer(t)

	createReq := `{"name":"A","session_id":"s1","user_id":"u1","channel":"console","meta":{}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(createReq)))
	if w1.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w1.Code, w1.Body.String())
	}

	var created map[string]interface{}
	if err := json.Unmarshal(w1.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	chatID, _ := created["id"].(string)
	if chatID == "" {
		t.Fatalf("empty chat id")
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"console","stream":false}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w2.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/chats/"+chatID, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), "assistant") {
		t.Fatalf("history should contain assistant message: %s", w3.Body.String())
	}
}

func TestProcessAgentRejectsUnsupportedChannel(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"sms","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"channel_not_supported"`) {
		t.Fatalf("unexpected error body: %s", w.Body.String())
	}
}

func TestProcessAgentDispatchesToWebhookChannel(t *testing.T) {
	var received atomic.Int32
	var gotBody map[string]interface{}
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.Header.Get("X-Test-Token") != "abc123" {
			t.Fatalf("unexpected webhook header: %s", r.Header.Get("X-Test-Token"))
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode webhook body failed: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhook.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"url":"` + webhook.URL + `","headers":{"X-Test-Token":"abc123"}}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/webhook", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello webhook"}]}],"session_id":"s1","user_id":"u1","channel":"webhook","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	if got := received.Load(); got != 1 {
		t.Fatalf("expected one webhook call, got=%d", got)
	}
	if gotBody["user_id"] != "u1" {
		t.Fatalf("unexpected webhook user_id: %#v", gotBody["user_id"])
	}
	if gotBody["session_id"] != "s1" {
		t.Fatalf("unexpected webhook session_id: %#v", gotBody["session_id"])
	}
	if text, _ := gotBody["text"].(string); !strings.Contains(text, "Echo: hello webhook") {
		t.Fatalf("unexpected webhook text: %#v", gotBody["text"])
	}
}

func TestWorkspaceUploadRejectUnsafePath(t *testing.T) {
	srv := newTestServer(t)

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	f, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", "workspace.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(part, bytes.NewReader(buf.Bytes()))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/workspace/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProcessAgentOpenAIRequiresAPIKey(t *testing.T) {
	srv := newTestServer(t)

	setActive := `{"provider_id":"openai","model":"gpt-4o-mini"}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w1.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", w1.Code, w1.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"console","stream":false}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"code":"provider_not_configured"`) {
		t.Fatalf("unexpected error body: %s", w2.Body.String())
	}
}

func TestProcessAgentOpenAIConfigured(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"provider reply"}}]}`))
	}))
	defer mock.Close()

	srv := newTestServer(t)

	configProvider := `{"api_key":"sk-test","base_url":"` + mock.URL + `"}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	setActive := `{"provider_id":"openai","model":"gpt-4o-mini"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", w2.Code, w2.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), `"provider reply"`) {
		t.Fatalf("unexpected body: %s", w3.Body.String())
	}
}

func TestModelsCatalogIncludesDefaults(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/models/catalog", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", w.Code, w.Body.String())
	}

	var out struct {
		Providers []domain.ProviderInfo  `json:"providers"`
		Defaults  map[string]string      `json:"defaults"`
		ActiveLLM domain.ModelSlotConfig `json:"active_llm"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode catalog failed: %v body=%s", err, w.Body.String())
	}
	if len(out.Providers) < 2 {
		t.Fatalf("expected at least 2 providers, got=%d", len(out.Providers))
	}
	if out.Defaults["openai"] == "" {
		t.Fatalf("expected openai default model, got=%v", out.Defaults["openai"])
	}
	if out.ActiveLLM.ProviderID == "" || out.ActiveLLM.Model == "" {
		t.Fatalf("expected active_llm, got=%+v", out.ActiveLLM)
	}
}

func TestSetActiveModelsResolvesAlias(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"model_aliases":{"my-fast":"gpt-4o-mini"}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	setActive := `{"provider_id":"openai","model":"my-fast"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"model":"gpt-4o-mini"`) {
		t.Fatalf("expected resolved alias model, body=%s", w2.Body.String())
	}
}

func TestSetActiveModelsRejectsDisabledProvider(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"enabled":false}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	setActive := `{"provider_id":"openai","model":"gpt-4o-mini"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"code":"provider_disabled"`) {
		t.Fatalf("unexpected body: %s", w2.Body.String())
	}
}

func TestProcessAgentCustomProviderOpenAICompatible(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"custom provider reply"}}]}`))
	}))
	defer mock.Close()

	srv := newTestServer(t)

	configProvider := `{"api_key":"sk-custom","base_url":"` + mock.URL + `","enabled":true}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/custom-openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	setActive := `{"provider_id":"custom-openai","model":"my-model"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active status=%d body=%s", w2.Code, w2.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello"}]}],"session_id":"s1","user_id":"u1","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if !strings.Contains(w3.Body.String(), `"custom provider reply"`) {
		t.Fatalf("unexpected body: %s", w3.Body.String())
	}
}

func TestCronSchedulerRunsIntervalJob(t *testing.T) {
	srv := newTestServer(t)
	createReq := `{
		"id":"job-interval",
		"name":"job-interval",
		"enabled":true,
		"schedule":{"type":"interval","cron":"1s"},
		"task_type":"text",
		"text":"hello cron",
		"dispatch":{"target":{"user_id":"u1","session_id":"s1"}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(createReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("create cron status=%d body=%s", w.Code, w.Body.String())
	}

	state := waitForCronState(t, srv, "job-interval", 5*time.Second, func(v map[string]interface{}) bool {
		got, _ := v["last_status"].(string)
		return got == cronStatusSucceeded
	})
	if got, _ := state["last_status"].(string); got != cronStatusSucceeded {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusSucceeded, state["last_status"])
	}
	if _, ok := state["next_run_at"].(string); !ok {
		t.Fatalf("expected next_run_at to be set: %+v", state)
	}
}

func TestRunCronJobDispatchesToWebhookChannel(t *testing.T) {
	var received atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"url":"` + webhook.URL + `"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/webhook", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	createReq := `{
		"id":"job-webhook",
		"name":"job-webhook",
		"enabled":false,
		"schedule":{"type":"interval","cron":"60s"},
		"task_type":"text",
		"text":"hello webhook cron",
		"dispatch":{"channel":"webhook","target":{"user_id":"u1","session_id":"s1"}},
		"runtime":{"max_concurrency":1,"timeout_seconds":5}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(createReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("create cron status=%d body=%s", w.Code, w.Body.String())
	}

	runW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(runW, httptest.NewRequest(http.MethodPost, "/cron/jobs/job-webhook/run", nil))
	if runW.Code != http.StatusOK {
		t.Fatalf("run cron status=%d body=%s", runW.Code, runW.Body.String())
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("expected one webhook dispatch, got=%d", got)
	}
}

func TestCronSchedulerRecoversPersistedDueJob(t *testing.T) {
	dir, err := os.MkdirTemp("", "copaw-next-gateway-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	if err := store.Write(func(state *repo.State) error {
		state.CronJobs["job-recover"] = domain.CronJobSpec{
			ID:      "job-recover",
			Name:    "job-recover",
			Enabled: true,
			Schedule: domain.CronScheduleSpec{
				Type: "interval",
				Cron: "1s",
			},
			TaskType: "text",
			Text:     "recover",
			Dispatch: domain.CronDispatchSpec{
				Target: domain.CronDispatchTarget{
					UserID:    "u1",
					SessionID: "s1",
				},
			},
		}
		state.CronStates["job-recover"] = domain.CronJobState{NextRunAt: &past}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	state := waitForCronState(t, srv, "job-recover", 5*time.Second, func(v map[string]interface{}) bool {
		got, _ := v["last_status"].(string)
		return got == cronStatusSucceeded
	})
	if got, _ := state["last_status"].(string); got != cronStatusSucceeded {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusSucceeded, state["last_status"])
	}
}

func TestCronSchedulerRunsCronExpressionJob(t *testing.T) {
	srv := newTestServer(t)
	createReq := `{
		"id":"job-cron",
		"name":"job-cron",
		"enabled":true,
		"schedule":{"type":"cron","cron":"*/1 * * * * *","timezone":"UTC"},
		"task_type":"text",
		"text":"hello cron expr",
		"dispatch":{"target":{"user_id":"u1","session_id":"s1"}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(createReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("create cron status=%d body=%s", w.Code, w.Body.String())
	}

	state := waitForCronState(t, srv, "job-cron", 6*time.Second, func(v map[string]interface{}) bool {
		got, _ := v["last_status"].(string)
		return got == cronStatusSucceeded
	})
	if got, _ := state["last_status"].(string); got != cronStatusSucceeded {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusSucceeded, state["last_status"])
	}
	if _, ok := state["next_run_at"].(string); !ok {
		t.Fatalf("expected next_run_at to be set: %+v", state)
	}
}

func TestCronSchedulerSkipsMisfireOutsideGrace(t *testing.T) {
	dir, err := os.MkdirTemp("", "copaw-next-gateway-misfire-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-15 * time.Second).Format(time.RFC3339)
	if err := store.Write(func(state *repo.State) error {
		state.CronJobs["job-misfire"] = domain.CronJobSpec{
			ID:      "job-misfire",
			Name:    "job-misfire",
			Enabled: true,
			Schedule: domain.CronScheduleSpec{
				Type: "interval",
				Cron: "1s",
			},
			TaskType: "text",
			Text:     "misfire",
			Runtime: domain.CronRuntimeSpec{
				MisfireGraceSeconds: 1,
			},
			Dispatch: domain.CronDispatchSpec{
				Target: domain.CronDispatchTarget{
					UserID:    "u1",
					SessionID: "s1",
				},
			},
		}
		state.CronStates["job-misfire"] = domain.CronJobState{NextRunAt: &past}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	state := waitForCronState(t, srv, "job-misfire", 5*time.Second, func(v map[string]interface{}) bool {
		msg, _ := v["last_error"].(string)
		return strings.Contains(msg, "misfire skipped")
	})
	if got, _ := state["last_status"].(string); got != cronStatusFailed {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusFailed, state["last_status"])
	}
	if _, ok := state["last_run_at"]; ok {
		t.Fatalf("expected last_run_at to be empty for misfire skip: %+v", state)
	}
}

func TestExecuteCronJobRespectsMaxConcurrency(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.store.Write(func(st *repo.State) error {
		st.CronJobs["job-max-concurrency"] = domain.CronJobSpec{
			ID:       "job-max-concurrency",
			Name:     "job-max-concurrency",
			Enabled:  false,
			TaskType: "text",
			Schedule: domain.CronScheduleSpec{Type: "interval", Cron: "60s"},
			Runtime: domain.CronRuntimeSpec{
				MaxConcurrency: 1,
				TimeoutSeconds: 5,
			},
		}
		st.CronStates["job-max-concurrency"] = domain.CronJobState{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	var calls atomic.Int32
	srv.cronTaskExecutor = func(ctx context.Context, _ domain.CronJobSpec) error {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}

	err1Ch := make(chan error, 1)
	go func() {
		err1Ch <- srv.executeCronJob("job-max-concurrency")
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first execution did not start in time")
	}

	err2Ch := make(chan error, 1)
	go func() {
		err2Ch <- srv.executeCronJob("job-max-concurrency")
	}()

	select {
	case err := <-err2Ch:
		if !errors.Is(err, errCronMaxConcurrencyReached) {
			t.Fatalf("expected max concurrency error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second execution did not return in time")
	}

	close(release)
	if err := <-err1Ch; err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected only one task execution, got=%d", got)
	}
}

func TestExecuteCronJobRespectsMaxConcurrencyAcrossServers(t *testing.T) {
	dir, err := os.MkdirTemp("", "copaw-next-gateway-distributed-lock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(func(st *repo.State) error {
		st.CronJobs["job-distributed-lock"] = domain.CronJobSpec{
			ID:       "job-distributed-lock",
			Name:     "job-distributed-lock",
			Enabled:  false,
			TaskType: "text",
			Schedule: domain.CronScheduleSpec{Type: "interval", Cron: "60s"},
			Runtime: domain.CronRuntimeSpec{
				MaxConcurrency: 1,
				TimeoutSeconds: 5,
			},
		}
		st.CronStates["job-distributed-lock"] = domain.CronJobState{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	srv1 := newTestServerWithDataDir(t, dir)
	srv2 := newTestServerWithDataDir(t, dir)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	srv1.cronTaskExecutor = func(ctx context.Context, _ domain.CronJobSpec) error {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	srv2.cronTaskExecutor = func(context.Context, domain.CronJobSpec) error {
		t.Fatal("second server should not execute task when max_concurrency is reached")
		return nil
	}

	err1Ch := make(chan error, 1)
	go func() {
		err1Ch <- srv1.executeCronJob("job-distributed-lock")
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first server execution did not start in time")
	}

	if err := srv2.executeCronJob("job-distributed-lock"); !errors.Is(err, errCronMaxConcurrencyReached) {
		t.Fatalf("expected max concurrency error from second server, got: %v", err)
	}

	close(release)
	if err := <-err1Ch; err != nil {
		t.Fatalf("first server execution failed: %v", err)
	}
}

func TestExecuteCronJobRespectsTimeout(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.store.Write(func(st *repo.State) error {
		st.CronJobs["job-timeout"] = domain.CronJobSpec{
			ID:       "job-timeout",
			Name:     "job-timeout",
			Enabled:  false,
			TaskType: "text",
			Schedule: domain.CronScheduleSpec{Type: "interval", Cron: "60s"},
			Runtime: domain.CronRuntimeSpec{
				MaxConcurrency: 1,
				TimeoutSeconds: 1,
			},
		}
		st.CronStates["job-timeout"] = domain.CronJobState{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	srv.cronTaskExecutor = func(ctx context.Context, _ domain.CronJobSpec) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
			return nil
		}
	}

	if err := srv.executeCronJob("job-timeout"); err != nil {
		t.Fatalf("execute cron failed: %v", err)
	}
	state := getCronState(t, srv, "job-timeout")
	if got, _ := state["last_status"].(string); got != cronStatusFailed {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusFailed, state["last_status"])
	}
	if errMsg, _ := state["last_error"].(string); !strings.Contains(errMsg, "timeout") {
		t.Fatalf("expected timeout error, got=%v", state["last_error"])
	}
}

func TestResolveCronNextRunAtDSTSpringForward(t *testing.T) {
	job := domain.CronJobSpec{
		Schedule: domain.CronScheduleSpec{
			Type:     "cron",
			Cron:     "30 2 8 3 *",
			Timezone: "America/New_York",
		},
	}
	now := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	next, dueAt, err := resolveCronNextRunAt(job, nil, now)
	if err != nil {
		t.Fatalf("resolve next run failed: %v", err)
	}
	if dueAt != nil {
		t.Fatalf("expected dueAt=nil, got=%v", dueAt)
	}
	expected := time.Date(2027, 3, 8, 7, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("unexpected next run at, expected=%s got=%s", expected.Format(time.RFC3339), next.Format(time.RFC3339))
	}
}

func TestResolveCronNextRunAtDSTFallBack(t *testing.T) {
	job := domain.CronJobSpec{
		Schedule: domain.CronScheduleSpec{
			Type:     "cron",
			Cron:     "30 1 1 11 *",
			Timezone: "America/New_York",
		},
	}
	now := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	next, dueAt, err := resolveCronNextRunAt(job, nil, now)
	if err != nil {
		t.Fatalf("resolve next run failed: %v", err)
	}
	if dueAt != nil {
		t.Fatalf("expected dueAt=nil, got=%v", dueAt)
	}
	expected := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("unexpected next run at, expected=%s got=%s", expected.Format(time.RFC3339), next.Format(time.RFC3339))
	}
}

func waitForCronState(t *testing.T, srv *Server, jobID string, timeout time.Duration, pred func(v map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		last = getCronState(t, srv, jobID)
		if pred(last) {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting cron state for %s: %+v", jobID, last)
	return nil
}

func getCronState(t *testing.T, srv *Server, jobID string) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cron/jobs/"+jobID+"/state", nil)
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get cron state status=%d body=%s", w.Code, w.Body.String())
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode cron state failed: %v body=%s", err, w.Body.String())
	}
	return out
}
