package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nextai/apps/gateway/internal/config"
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR", "true")
	dir, err := os.MkdirTemp("", "nextai-gateway-test-")
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

func newToolTestPath(t *testing.T, prefix string) (string, string) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join("apps/gateway/.data/tool-tests", fmt.Sprintf("%s-%d.txt", prefix, time.Now().UnixNano())))
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
	return rel, abs
}

func newDocsAITestPath(t *testing.T, prefix string) (string, string) {
	t.Helper()
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.ToSlash(filepath.Join("docs/AI", fmt.Sprintf("%s-%d.md", prefix, time.Now().UnixNano())))
	abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(abs) })
	return rel, abs
}

type streamingProbeWriter struct {
	header      http.Header
	status      int
	body        strings.Builder
	signal      chan struct{}
	signalOnce  sync.Once
	mutex       sync.Mutex
	wroteHeader bool
}

func newStreamingProbeWriter() *streamingProbeWriter {
	return &streamingProbeWriter{
		header: make(http.Header),
		signal: make(chan struct{}, 1),
		status: http.StatusOK,
	}
}

func (w *streamingProbeWriter) Header() http.Header {
	return w.header
}

func (w *streamingProbeWriter) WriteHeader(statusCode int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.status = statusCode
	w.wroteHeader = true
}

func (w *streamingProbeWriter) Write(p []byte) (int, error) {
	w.mutex.Lock()
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	n, err := w.body.Write(p)
	w.mutex.Unlock()
	w.notify()
	return n, err
}

func (w *streamingProbeWriter) Flush() {
	w.notify()
}

func (w *streamingProbeWriter) BodyString() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.body.String()
}

func (w *streamingProbeWriter) notify() {
	w.signalOnce.Do(func() {
		select {
		case w.signal <- struct{}{}:
		default:
		}
	})
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
	dir, err := os.MkdirTemp("", "nextai-gateway-auth-")
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

func TestProcessAgentReusesChatHistoryContext(t *testing.T) {
	srv := newTestServer(t)

	firstReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"1+1等于几"}]}],"session_id":"s-context","user_id":"u-context","channel":"console","stream":false}`
	firstW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(firstReq)))
	if firstW.Code != http.StatusOK {
		t.Fatalf("first process status=%d body=%s", firstW.Code, firstW.Body.String())
	}

	secondReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"把你之前回答的数学问题再回答一次"}]}],"session_id":"s-context","user_id":"u-context","channel":"console","stream":false}`
	secondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(secondReq)))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second process status=%d body=%s", secondW.Code, secondW.Body.String())
	}

	var secondResp domain.AgentProcessResponse
	if err := json.Unmarshal(secondW.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response failed: %v body=%s", err, secondW.Body.String())
	}
	if !strings.Contains(secondResp.Reply, "1+1等于几") {
		t.Fatalf("expected second reply to include previous user context, got=%q", secondResp.Reply)
	}
	if !strings.Contains(secondResp.Reply, "把你之前回答的数学问题再回答一次") {
		t.Fatalf("expected second reply to include latest user input, got=%q", secondResp.Reply)
	}
}

func TestProcessAgentPersistsToolCallNoticesInHistory(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "history-tool-call")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	createReq := `{"name":"A","session_id":"s-history-tool","user_id":"u-history-tool","channel":"console","meta":{}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(createReq)))
	if w1.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w1.Code, w1.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(w1.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	chatID, _ := created["id"].(string)
	if strings.TrimSpace(chatID) == "" {
		t.Fatalf("empty chat id: %v", created)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view history tool call"}]}],
		"session_id":"s-history-tool",
		"user_id":"u-history-tool",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":1,"end":1}]
	}`, absPath)
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

	var history domain.ChatHistory
	if err := json.Unmarshal(w3.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history failed: %v body=%s", err, w3.Body.String())
	}
	if len(history.Messages) == 0 {
		t.Fatalf("expected non-empty history, body=%s", w3.Body.String())
	}
	assistant := history.Messages[len(history.Messages)-1]
	if assistant.Role != "assistant" {
		t.Fatalf("expected assistant message at tail, got=%q", assistant.Role)
	}
	if len(assistant.Metadata) == 0 {
		t.Fatalf("expected assistant metadata, body=%s", w3.Body.String())
	}
	rawNotices, ok := assistant.Metadata["tool_call_notices"].([]interface{})
	if !ok || len(rawNotices) == 0 {
		t.Fatalf("expected tool_call_notices metadata, got=%#v", assistant.Metadata["tool_call_notices"])
	}
	first, _ := rawNotices[0].(map[string]interface{})
	raw, _ := first["raw"].(string)
	if !strings.Contains(raw, `"type":"tool_call"`) || !strings.Contains(raw, `"name":"view"`) {
		t.Fatalf("unexpected persisted tool notice raw: %q", raw)
	}
	toolOrder, ok := assistant.Metadata["tool_order"].(float64)
	if !ok || toolOrder <= 0 {
		t.Fatalf("expected positive tool_order, got=%#v", assistant.Metadata["tool_order"])
	}
	textOrder, ok := assistant.Metadata["text_order"].(float64)
	if !ok || textOrder <= 0 {
		t.Fatalf("expected positive text_order, got=%#v", assistant.Metadata["text_order"])
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

func TestProcessAgentRespectsRequestedChannelForWebSource(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-web-auto/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	configW := httptest.NewRecorder()
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("config qq status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello from web source"}]}],"session_id":"s-web-auto","user_id":"u-web-auto","channel":"qq","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq))
	req.Header.Set(channelSourceHeader, "web")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-web-auto&channel=qq", nil))
	if chatsW.Code != http.StatusOK {
		t.Fatalf("list qq chats status=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
	var qqChats []domain.ChatSpec
	if err := json.Unmarshal(chatsW.Body.Bytes(), &qqChats); err != nil {
		t.Fatalf("decode qq chats failed: %v body=%s", err, chatsW.Body.String())
	}
	if len(qqChats) != 1 {
		t.Fatalf("expected one qq chat, got=%d body=%s", len(qqChats), chatsW.Body.String())
	}
	if qqChats[0].Channel != "qq" {
		t.Fatalf("expected chat channel qq, got=%q", qqChats[0].Channel)
	}

	consoleChatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(consoleChatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-web-auto&channel=console", nil))
	if consoleChatsW.Code != http.StatusOK {
		t.Fatalf("list console chats status=%d body=%s", consoleChatsW.Code, consoleChatsW.Body.String())
	}
	var consoleChats []domain.ChatSpec
	if err := json.Unmarshal(consoleChatsW.Body.Bytes(), &consoleChats); err != nil {
		t.Fatalf("decode console chats failed: %v body=%s", err, consoleChatsW.Body.String())
	}
	if len(consoleChats) != 0 {
		t.Fatalf("expected no console chats, got=%d body=%s", len(consoleChats), consoleChatsW.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq message call, got=%d", got)
	}
}

func TestProcessAgentDefaultsToConsoleForCLISourceWithoutChannel(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello from cli source"}]}],"session_id":"s-cli-auto","user_id":"u-cli-auto","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq))
	req.Header.Set(channelSourceHeader, "cli")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	chatsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-cli-auto&channel=console", nil))
	if chatsW.Code != http.StatusOK {
		t.Fatalf("list console chats status=%d body=%s", chatsW.Code, chatsW.Body.String())
	}
	var chats []domain.ChatSpec
	if err := json.Unmarshal(chatsW.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, chatsW.Body.String())
	}
	if len(chats) != 1 {
		t.Fatalf("expected one console chat, got=%d body=%s", len(chats), chatsW.Body.String())
	}
	if chats[0].Channel != "console" {
		t.Fatalf("expected chat channel console, got=%q", chats[0].Channel)
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

func TestProcessAgentQQChannelDispatchesOutboundMessage(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u1/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","bot_prefix":"[BOT] ","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello qq"}]}],"session_id":"s1","user_id":"u1","channel":"qq","stream":false}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Echo: hello qq") {
		t.Fatalf("unexpected process body: %s", w.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq message call, got=%d", got)
	}
}

func TestProcessAgentNewCommandClearsSessionContext(t *testing.T) {
	srv := newTestServer(t)

	firstReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello before reset"}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	firstW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(firstReq)))
	if firstW.Code != http.StatusOK {
		t.Fatalf("first process status=%d body=%s", firstW.Code, firstW.Body.String())
	}

	chatsBeforeResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsBeforeResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsBeforeResetW.Code != http.StatusOK {
		t.Fatalf("list chats before reset status=%d body=%s", chatsBeforeResetW.Code, chatsBeforeResetW.Body.String())
	}

	var chatsBeforeReset []domain.ChatSpec
	if err := json.Unmarshal(chatsBeforeResetW.Body.Bytes(), &chatsBeforeReset); err != nil {
		t.Fatalf("decode chats before reset failed: %v body=%s", err, chatsBeforeResetW.Body.String())
	}
	if len(chatsBeforeReset) != 1 {
		t.Fatalf("expected one chat before reset, got=%d body=%s", len(chatsBeforeReset), chatsBeforeResetW.Body.String())
	}
	originalChat := chatsBeforeReset[0]

	originalHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(originalHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+originalChat.ID, nil))
	if originalHistoryW.Code != http.StatusOK {
		t.Fatalf("get original history status=%d body=%s", originalHistoryW.Code, originalHistoryW.Body.String())
	}
	var originalHistory domain.ChatHistory
	if err := json.Unmarshal(originalHistoryW.Body.Bytes(), &originalHistory); err != nil {
		t.Fatalf("decode original history failed: %v body=%s", err, originalHistoryW.Body.String())
	}
	if !chatHistoryContainsText(originalHistory, "hello before reset") {
		t.Fatalf("expected original history to contain first user text, body=%s", originalHistoryW.Body.String())
	}

	resetReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":" /new "}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	resetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resetW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(resetReq)))
	if resetW.Code != http.StatusOK {
		t.Fatalf("reset process status=%d body=%s", resetW.Code, resetW.Body.String())
	}
	var resetResp domain.AgentProcessResponse
	if err := json.Unmarshal(resetW.Body.Bytes(), &resetResp); err != nil {
		t.Fatalf("decode reset response failed: %v body=%s", err, resetW.Body.String())
	}
	if !strings.Contains(resetResp.Reply, "上下文已清理") {
		t.Fatalf("unexpected reset reply: %#v", resetResp.Reply)
	}

	chatsAfterResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsAfterResetW.Code != http.StatusOK {
		t.Fatalf("list chats after reset status=%d body=%s", chatsAfterResetW.Code, chatsAfterResetW.Body.String())
	}
	var chatsAfterReset []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterResetW.Body.Bytes(), &chatsAfterReset); err != nil {
		t.Fatalf("decode chats after reset failed: %v body=%s", err, chatsAfterResetW.Body.String())
	}
	if len(chatsAfterReset) != 0 {
		t.Fatalf("expected no chats after reset, got=%d body=%s", len(chatsAfterReset), chatsAfterResetW.Body.String())
	}

	secondReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello after reset"}]}],"session_id":"s-reset","user_id":"u-reset","channel":"console","stream":false}`
	secondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondW, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(secondReq)))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second process status=%d body=%s", secondW.Code, secondW.Body.String())
	}

	chatsAfterSecondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterSecondW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-reset&channel=console", nil))
	if chatsAfterSecondW.Code != http.StatusOK {
		t.Fatalf("list chats after second message status=%d body=%s", chatsAfterSecondW.Code, chatsAfterSecondW.Body.String())
	}
	var chatsAfterSecond []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterSecondW.Body.Bytes(), &chatsAfterSecond); err != nil {
		t.Fatalf("decode chats after second message failed: %v body=%s", err, chatsAfterSecondW.Body.String())
	}
	if len(chatsAfterSecond) != 1 {
		t.Fatalf("expected one chat after second message, got=%d body=%s", len(chatsAfterSecond), chatsAfterSecondW.Body.String())
	}
	if chatsAfterSecond[0].ID == originalChat.ID {
		t.Fatalf("expected a new chat id after reset, got unchanged id=%s", chatsAfterSecond[0].ID)
	}

	newHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(newHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+chatsAfterSecond[0].ID, nil))
	if newHistoryW.Code != http.StatusOK {
		t.Fatalf("get new history status=%d body=%s", newHistoryW.Code, newHistoryW.Body.String())
	}
	var newHistory domain.ChatHistory
	if err := json.Unmarshal(newHistoryW.Body.Bytes(), &newHistory); err != nil {
		t.Fatalf("decode new history failed: %v body=%s", err, newHistoryW.Body.String())
	}
	if chatHistoryContainsText(newHistory, "hello before reset") {
		t.Fatalf("expected previous context to be cleared, body=%s", newHistoryW.Body.String())
	}
	if !chatHistoryContainsText(newHistory, "hello after reset") {
		t.Fatalf("expected new history to contain post-reset text, body=%s", newHistoryW.Body.String())
	}
}

func TestQQInboundC2CEventTriggersOutboundDispatch(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-c2c/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	inboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-1","content":"hello inbound c2c","author":{"user_openid":"u-c2c"}}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", w.Code, w.Body.String())
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := messageCalls.Load(); got != 1 {
		t.Fatalf("expected one qq c2c dispatch, got=%d", got)
	}
}

func TestQQInboundGroupEventTriggersOutboundDispatch(t *testing.T) {
	var tokenCalls atomic.Int32
	var groupCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/groups/group-openid-1/messages":
			groupCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	inboundReq := `{"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"m-group-1","content":"hello inbound group","group_openid":"group-openid-1","author":{"member_openid":"u-group-1"}}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", w.Code, w.Body.String())
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call, got=%d", got)
	}
	if got := groupCalls.Load(); got != 1 {
		t.Fatalf("expected one qq group dispatch, got=%d", got)
	}
}

func TestQQInboundNewCommandClearsSessionContext(t *testing.T) {
	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32

	qqAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":7200}`))
		case "/v2/users/u-c2c-reset/messages":
			messageCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected qq path: %s", r.URL.Path)
		}
	}))
	defer qqAPI.Close()

	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","token_url":"` + qqAPI.URL + `/token","api_base":"` + qqAPI.URL + `","target_type":"c2c"}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	firstInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-1","content":"hello inbound before reset","author":{"user_openid":"u-c2c-reset"}}}`
	firstInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(firstInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(firstInboundReq)))
	if firstInboundW.Code != http.StatusOK {
		t.Fatalf("first inbound status=%d body=%s", firstInboundW.Code, firstInboundW.Body.String())
	}

	chatsBeforeResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsBeforeResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsBeforeResetW.Code != http.StatusOK {
		t.Fatalf("list qq chats before reset status=%d body=%s", chatsBeforeResetW.Code, chatsBeforeResetW.Body.String())
	}
	var chatsBeforeReset []domain.ChatSpec
	if err := json.Unmarshal(chatsBeforeResetW.Body.Bytes(), &chatsBeforeReset); err != nil {
		t.Fatalf("decode qq chats before reset failed: %v body=%s", err, chatsBeforeResetW.Body.String())
	}
	if len(chatsBeforeReset) != 1 {
		t.Fatalf("expected one qq chat before reset, got=%d body=%s", len(chatsBeforeReset), chatsBeforeResetW.Body.String())
	}
	originalChat := chatsBeforeReset[0]

	resetInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-2","content":" /new ","author":{"user_openid":"u-c2c-reset"}}}`
	resetInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resetInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(resetInboundReq)))
	if resetInboundW.Code != http.StatusOK {
		t.Fatalf("reset inbound status=%d body=%s", resetInboundW.Code, resetInboundW.Body.String())
	}
	var resetResp domain.AgentProcessResponse
	if err := json.Unmarshal(resetInboundW.Body.Bytes(), &resetResp); err != nil {
		t.Fatalf("decode reset inbound response failed: %v body=%s", err, resetInboundW.Body.String())
	}
	if !strings.Contains(resetResp.Reply, "上下文已清理") {
		t.Fatalf("unexpected reset inbound reply: %#v", resetResp.Reply)
	}

	chatsAfterResetW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterResetW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsAfterResetW.Code != http.StatusOK {
		t.Fatalf("list qq chats after reset status=%d body=%s", chatsAfterResetW.Code, chatsAfterResetW.Body.String())
	}
	var chatsAfterReset []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterResetW.Body.Bytes(), &chatsAfterReset); err != nil {
		t.Fatalf("decode qq chats after reset failed: %v body=%s", err, chatsAfterResetW.Body.String())
	}
	if len(chatsAfterReset) != 0 {
		t.Fatalf("expected no qq chats after reset, got=%d body=%s", len(chatsAfterReset), chatsAfterResetW.Body.String())
	}

	secondInboundReq := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"m-c2c-3","content":"hello inbound after reset","author":{"user_openid":"u-c2c-reset"}}}`
	secondInboundW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(secondInboundW, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(secondInboundReq)))
	if secondInboundW.Code != http.StatusOK {
		t.Fatalf("second inbound status=%d body=%s", secondInboundW.Code, secondInboundW.Body.String())
	}

	chatsAfterSecondW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(chatsAfterSecondW, httptest.NewRequest(http.MethodGet, "/chats?user_id=u-c2c-reset&channel=qq", nil))
	if chatsAfterSecondW.Code != http.StatusOK {
		t.Fatalf("list qq chats after second message status=%d body=%s", chatsAfterSecondW.Code, chatsAfterSecondW.Body.String())
	}
	var chatsAfterSecond []domain.ChatSpec
	if err := json.Unmarshal(chatsAfterSecondW.Body.Bytes(), &chatsAfterSecond); err != nil {
		t.Fatalf("decode qq chats after second message failed: %v body=%s", err, chatsAfterSecondW.Body.String())
	}
	if len(chatsAfterSecond) != 1 {
		t.Fatalf("expected one qq chat after second message, got=%d body=%s", len(chatsAfterSecond), chatsAfterSecondW.Body.String())
	}
	if chatsAfterSecond[0].ID == originalChat.ID {
		t.Fatalf("expected a new qq chat id after reset, got unchanged id=%s", chatsAfterSecond[0].ID)
	}

	newHistoryW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(newHistoryW, httptest.NewRequest(http.MethodGet, "/chats/"+chatsAfterSecond[0].ID, nil))
	if newHistoryW.Code != http.StatusOK {
		t.Fatalf("get new qq history status=%d body=%s", newHistoryW.Code, newHistoryW.Body.String())
	}
	var newHistory domain.ChatHistory
	if err := json.Unmarshal(newHistoryW.Body.Bytes(), &newHistory); err != nil {
		t.Fatalf("decode new qq history failed: %v body=%s", err, newHistoryW.Body.String())
	}
	if chatHistoryContainsText(newHistory, "hello inbound before reset") {
		t.Fatalf("expected qq previous context to be cleared, body=%s", newHistoryW.Body.String())
	}
	if !chatHistoryContainsText(newHistory, "hello inbound after reset") {
		t.Fatalf("expected qq new history to contain post-reset text, body=%s", newHistoryW.Body.String())
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one token call across qq reset flow, got=%d", got)
	}
	if got := messageCalls.Load(); got != 3 {
		t.Fatalf("expected three qq dispatches across reset flow, got=%d", got)
	}
}

func TestQQInboundRejectsUnsupportedEvent(t *testing.T) {
	srv := newTestServer(t)
	inboundReq := `{"t":"MESSAGE_DELETE","d":{"id":"m-delete"}}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", strings.NewReader(inboundReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_qq_event"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func chatHistoryContainsText(history domain.ChatHistory, want string) bool {
	for _, msg := range history.Messages {
		for _, content := range msg.Content {
			if strings.Contains(content.Text, want) {
				return true
			}
		}
	}
	return false
}

func TestQQInboundStateEndpointReturnsRuntimeSnapshot(t *testing.T) {
	srv := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channels/qq/state", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state body failed: %v", err)
	}
	if _, ok := body["configured"].(bool); !ok {
		t.Fatalf("missing configured bool: %#v", body["configured"])
	}
	if _, ok := body["running"].(bool); !ok {
		t.Fatalf("missing running bool: %#v", body["running"])
	}
	if _, ok := body["connected"].(bool); !ok {
		t.Fatalf("missing connected bool: %#v", body["connected"])
	}
	if _, ok := body["config"].(map[string]interface{}); !ok {
		t.Fatalf("missing config map: %#v", body["config"])
	}
}

func TestQQInboundStateEndpointReflectsConfiguredIntents(t *testing.T) {
	srv := newTestServer(t)
	channelConfig := `{"enabled":true,"app_id":"app-1","client_secret":"secret-1","inbound_enabled":true,"inbound_intents":42}`
	configW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(configW, httptest.NewRequest(http.MethodPut, "/config/channels/qq", strings.NewReader(channelConfig)))
	if configW.Code != http.StatusOK {
		t.Fatalf("set qq channel config status=%d body=%s", configW.Code, configW.Body.String())
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channels/qq/state", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode state body failed: %v", err)
	}
	if configured, _ := body["configured"].(bool); !configured {
		t.Fatalf("expected configured=true, got=%#v", body["configured"])
	}
	configObj, _ := body["config"].(map[string]interface{})
	if intents, _ := configObj["intents"].(float64); intents != 42 {
		t.Fatalf("expected config intents=42, got=%#v", configObj["intents"])
	}
}

func TestProcessAgentRunsShellTool(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell printf hello"}]}],
		"session_id":"s-shell",
		"user_id":"u-shell",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"printf hello"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("expected shell output in reply body, got=%s", w.Body.String())
	}
}

func TestProcessAgentRejectsUnknownTool(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"run desktop"}]}],
		"session_id":"s-tool-unknown",
		"user_id":"u-tool-unknown",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"desktop","input":{}}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"tool_not_supported"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsShellToolWhenDisabled(t *testing.T) {
	t.Setenv("NEXTAI_DISABLED_TOOLS", "shell")
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell pwd"}]}],
		"session_id":"s-shell-disabled",
		"user_id":"u-shell-disabled",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"pwd"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"tool_disabled"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsShellToolWithoutCommand(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"/shell"}]}],
		"session_id":"s-shell-empty",
		"user_id":"u-shell-empty",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"shell","items":[{}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestWorkspaceFilesListIncludesConfigAndSkillFiles(t *testing.T) {
	srv := newTestServer(t)
	docsPath, docsAbsPath := newDocsAITestPath(t, "workspace-list")
	if err := os.WriteFile(docsAbsPath, []byte("# workspace list test\n"), 0o644); err != nil {
		t.Fatalf("seed docs/AI file failed: %v", err)
	}

	createSkill := `{"name":"demo-skill","content":"## skill content"}`
	createW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createW, httptest.NewRequest(http.MethodPost, "/skills", strings.NewReader(createSkill)))
	if createW.Code != http.StatusOK {
		t.Fatalf("create skill status=%d body=%s", createW.Code, createW.Body.String())
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workspace/files", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list files status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Files []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	paths := map[string]string{}
	for _, item := range body.Files {
		paths[item.Path] = item.Kind
	}
	if got := paths["config/envs.json"]; got != "config" {
		t.Fatalf("expected config/envs.json to be config, got=%q", got)
	}
	if got := paths["skills/demo-skill.json"]; got != "skill" {
		t.Fatalf("expected skills/demo-skill.json to be skill, got=%q", got)
	}
	if got := paths[docsPath]; got != "config" {
		t.Fatalf("expected %s to be config, got=%q", docsPath, got)
	}
}

func TestWorkspaceFileConfigAndSkillCRUD(t *testing.T) {
	srv := newTestServer(t)
	docsPath, docsAbsPath := newDocsAITestPath(t, "workspace-crud")
	if err := os.WriteFile(docsAbsPath, []byte("# before update\n"), 0o644); err != nil {
		t.Fatalf("seed docs/AI file failed: %v", err)
	}

	envBody := `{"OPENAI_API_KEY":"sk-test"}`
	putEnvsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(putEnvsW, httptest.NewRequest(http.MethodPut, "/workspace/files/config/envs.json", strings.NewReader(envBody)))
	if putEnvsW.Code != http.StatusOK {
		t.Fatalf("put envs status=%d body=%s", putEnvsW.Code, putEnvsW.Body.String())
	}

	getEnvsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getEnvsW, httptest.NewRequest(http.MethodGet, "/workspace/files/config/envs.json", nil))
	if getEnvsW.Code != http.StatusOK {
		t.Fatalf("get envs status=%d body=%s", getEnvsW.Code, getEnvsW.Body.String())
	}
	if !strings.Contains(getEnvsW.Body.String(), "OPENAI_API_KEY") {
		t.Fatalf("env file response should contain OPENAI_API_KEY: %s", getEnvsW.Body.String())
	}

	skillBody := `{"content":"## test skill","source":"customized","enabled":true}`
	putSkillW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(putSkillW, httptest.NewRequest(http.MethodPut, "/workspace/files/skills/hello.json", strings.NewReader(skillBody)))
	if putSkillW.Code != http.StatusOK {
		t.Fatalf("put skill status=%d body=%s", putSkillW.Code, putSkillW.Body.String())
	}

	getSkillW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getSkillW, httptest.NewRequest(http.MethodGet, "/workspace/files/skills/hello.json", nil))
	if getSkillW.Code != http.StatusOK {
		t.Fatalf("get skill status=%d body=%s", getSkillW.Code, getSkillW.Body.String())
	}
	if !strings.Contains(getSkillW.Body.String(), `"name":"hello"`) {
		t.Fatalf("skill file should include normalized name: %s", getSkillW.Body.String())
	}

	getDocsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getDocsW, httptest.NewRequest(http.MethodGet, "/workspace/files/"+docsPath, nil))
	if getDocsW.Code != http.StatusOK {
		t.Fatalf("get docs/AI file status=%d body=%s", getDocsW.Code, getDocsW.Body.String())
	}
	if !strings.Contains(getDocsW.Body.String(), "# before update") {
		t.Fatalf("docs/AI file should return text content: %s", getDocsW.Body.String())
	}

	putDocsBody := `{"content":"# after update"}`
	putDocsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(putDocsW, httptest.NewRequest(http.MethodPut, "/workspace/files/"+docsPath, strings.NewReader(putDocsBody)))
	if putDocsW.Code != http.StatusOK {
		t.Fatalf("put docs/AI file status=%d body=%s", putDocsW.Code, putDocsW.Body.String())
	}
	updatedDocsRaw, err := os.ReadFile(docsAbsPath)
	if err != nil {
		t.Fatalf("read updated docs/AI file failed: %v", err)
	}
	if strings.TrimSpace(string(updatedDocsRaw)) != "# after update" {
		t.Fatalf("unexpected docs/AI file content: %s", string(updatedDocsRaw))
	}

	delSkillW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delSkillW, httptest.NewRequest(http.MethodDelete, "/workspace/files/skills/hello.json", nil))
	if delSkillW.Code != http.StatusOK {
		t.Fatalf("delete skill status=%d body=%s", delSkillW.Code, delSkillW.Body.String())
	}

	delConfigW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delConfigW, httptest.NewRequest(http.MethodDelete, "/workspace/files/config/envs.json", nil))
	if delConfigW.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete config status=%d body=%s", delConfigW.Code, delConfigW.Body.String())
	}

	delDocsW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(delDocsW, httptest.NewRequest(http.MethodDelete, "/workspace/files/"+docsPath, nil))
	if delDocsW.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete docs/AI file status=%d body=%s", delDocsW.Code, delDocsW.Body.String())
	}
}

func TestWorkspaceImportReplace(t *testing.T) {
	srv := newTestServer(t)

	importBody := `{
		"mode":"replace",
		"payload":{
			"version":"v1",
			"skills":{"imported":{"content":"# imported","source":"customized","enabled":true}},
			"config":{
				"envs":{"NEW_ENV":"ok"},
				"channels":{"console":{"enabled":true},"webhook":{"enabled":false}},
				"models":{
					"providers":{"openai":{"api_key":"sk-imported","enabled":true,"headers":{},"model_aliases":{}}},
					"active_llm":{"provider_id":"openai","model":"gpt-4o-mini"}
				}
			}
		}
	}`
	importW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(importW, httptest.NewRequest(http.MethodPost, "/workspace/import", strings.NewReader(importBody)))
	if importW.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importW.Code, importW.Body.String())
	}

	exportW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(exportW, httptest.NewRequest(http.MethodGet, "/workspace/export", nil))
	if exportW.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exportW.Code, exportW.Body.String())
	}
	if !strings.Contains(exportW.Body.String(), `"NEW_ENV":"ok"`) {
		t.Fatalf("export should contain imported env: %s", exportW.Body.String())
	}
	if !strings.Contains(exportW.Body.String(), `"name":"imported"`) {
		t.Fatalf("export should contain imported skill: %s", exportW.Body.String())
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

func TestProcessAgentOmitsBlacklistedToolsFromModelRequest(t *testing.T) {
	t.Setenv("NEXTAI_DISABLED_TOOLS", "shell")

	var requestBody map[string]interface{}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request body failed: %v", err)
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

	rawTools, ok := requestBody["tools"].([]interface{})
	if !ok || len(rawTools) == 0 {
		t.Fatalf("expected non-shell tools to remain, got=%#v", requestBody["tools"])
	}
	names := map[string]bool{}
	for _, item := range rawTools {
		def, _ := item.(map[string]interface{})
		fn, _ := def["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if strings.TrimSpace(name) != "" {
			names[name] = true
		}
	}
	if names["shell"] {
		t.Fatalf("expected shell to be excluded when blacklisted, got=%#v", names)
	}
	if !names["view"] || !names["edit"] {
		t.Fatalf("expected line tools in model request, got=%#v", names)
	}
}

func TestProcessAgentViewsSpecificFileLines(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "view-lines")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\nline-3\nline-4\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view lines"}]}],
		"session_id":"s-view-lines",
		"user_id":"u-view-lines",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":2,"end":3}]
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "2: line-2") || !strings.Contains(w.Body.String(), "3: line-3") {
		t.Fatalf("expected selected line output, got=%s", w.Body.String())
	}
}

func TestProcessAgentViewOutOfBoundsFallsBackToFullFile(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "view-lines-fallback")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\nline-3\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view out of bounds"}]}],
		"session_id":"s-view-lines-fallback",
		"user_id":"u-view-lines-fallback",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":1,"end":100}]
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "1: line-1") || !strings.Contains(body, "3: line-3") {
		t.Fatalf("expected full file output, got=%s", body)
	}
	if !strings.Contains(body, "fallback from requested [1-100]") {
		t.Fatalf("expected fallback marker in output, got=%s", body)
	}
}

func TestProcessAgentViewOutOfBoundsFallsBackToEmptyFile(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "view-lines-empty-fallback")
	if err := os.WriteFile(absPath, []byte(""), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view empty out of bounds"}]}],
		"session_id":"s-view-lines-empty-fallback",
		"user_id":"u-view-lines-empty-fallback",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":1,"end":100}]
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "view "+absPath+" [empty] (fallback from requested [1-100], total=0)") {
		t.Fatalf("expected empty fallback marker in output, got=%s", body)
	}
}

func TestProcessAgentEditsSpecificFileLines(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "edit-lines")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\nline-3\nline-4\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"edit lines"}]}],
		"session_id":"s-edit-lines",
		"user_id":"u-edit-lines",
		"channel":"console",
		"stream":false,
		"edit":[{"path":%q,"start":2,"end":3,"content":"new-2\nnew-3"}]
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	updated, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read updated file failed: %v", err)
	}
	if string(updated) != "line-1\nnew-2\nnew-3\nline-4\n" {
		t.Fatalf("unexpected updated file content: %q", string(updated))
	}
}

func TestProcessAgentViewsMultipleFilesWithInputItems(t *testing.T) {
	srv := newTestServer(t)
	_, absPathA := newToolTestPath(t, "view-multi-a")
	_, absPathB := newToolTestPath(t, "view-multi-b")
	if err := os.WriteFile(absPathA, []byte("a-1\na-2\na-3\n"), 0o644); err != nil {
		t.Fatalf("seed first tool test file failed: %v", err)
	}
	if err := os.WriteFile(absPathB, []byte("b-1\nb-2\nb-3\n"), 0o644); err != nil {
		t.Fatalf("seed second tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view multiple files"}]}],
		"session_id":"s-view-multi",
		"user_id":"u-view-multi",
		"channel":"console",
		"stream":false,
		"view":[
			{"path":%q,"start":1,"end":1},
			{"path":%q,"start":2,"end":2}
		]
	}`, absPathA, absPathB)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1: a-1") || !strings.Contains(w.Body.String(), "2: b-2") {
		t.Fatalf("expected multi file output, got=%s", w.Body.String())
	}
}

func TestProcessAgentEditsMultipleFilesWithInputItems(t *testing.T) {
	srv := newTestServer(t)
	_, absPathA := newToolTestPath(t, "edit-multi-a")
	_, absPathB := newToolTestPath(t, "edit-multi-b")
	if err := os.WriteFile(absPathA, []byte("a-1\na-2\n"), 0o644); err != nil {
		t.Fatalf("seed first tool test file failed: %v", err)
	}
	if err := os.WriteFile(absPathB, []byte("b-1\nb-2\n"), 0o644); err != nil {
		t.Fatalf("seed second tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"edit multiple files"}]}],
		"session_id":"s-edit-multi",
		"user_id":"u-edit-multi",
		"channel":"console",
		"stream":false,
		"edit":[
			{"path":%q,"start":1,"end":1,"content":"a-x"},
			{"path":%q,"start":2,"end":2,"content":"b-y"}
		]
	}`, absPathA, absPathB)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}

	updatedA, err := os.ReadFile(absPathA)
	if err != nil {
		t.Fatalf("read first updated file failed: %v", err)
	}
	updatedB, err := os.ReadFile(absPathB)
	if err != nil {
		t.Fatalf("read second updated file failed: %v", err)
	}
	if string(updatedA) != "a-x\na-2\n" {
		t.Fatalf("unexpected first updated file content: %q", string(updatedA))
	}
	if string(updatedB) != "b-1\nb-y\n" {
		t.Fatalf("unexpected second updated file content: %q", string(updatedB))
	}
}

func TestProcessAgentRejectsSingleEditObjectInput(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "edit-object-invalid")
	if err := os.WriteFile(absPath, []byte("x-1\nx-2\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"edit object invalid"}]}],
		"session_id":"s-edit-object-invalid",
		"user_id":"u-edit-object-invalid",
		"channel":"console",
		"stream":false,
		"edit":{"input":{"path":%q,"start":1,"end":1,"content":"x-y"}}
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsSingleViewObjectInput(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "view-object-invalid")
	if err := os.WriteFile(absPath, []byte("x-1\nx-2\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view object invalid"}]}],
		"session_id":"s-view-object-invalid",
		"user_id":"u-view-object-invalid",
		"channel":"console",
		"stream":false,
		"view":{"input":{"path":%q,"start":1,"end":1}}
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsSingleShellObjectInput(t *testing.T) {
	srv := newTestServer(t)
	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"shell object invalid"}]}],
		"session_id":"s-shell-object-invalid",
		"user_id":"u-shell-object-invalid",
		"channel":"console",
		"stream":false,
		"shell":{"input":{"command":"printf hi"}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsBrowserToolWithoutTask(t *testing.T) {
	t.Setenv("NEXTAI_ENABLE_BROWSER_TOOL", "true")
	agentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(agentDir, "agent.js"), []byte("// test"), 0o644); err != nil {
		t.Fatalf("seed browser agent entry failed: %v", err)
	}
	t.Setenv("NEXTAI_BROWSER_AGENT_DIR", agentDir)

	srv := newTestServer(t)
	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"browser object invalid"}]}],
		"session_id":"s-browser-object-invalid",
		"user_id":"u-browser-object-invalid",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"browser","items":[{}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsSearchToolWithoutQuery(t *testing.T) {
	t.Setenv("NEXTAI_ENABLE_SEARCH_TOOL", "true")
	t.Setenv("NEXTAI_SEARCH_SERPAPI_KEY", "test-key")

	srv := newTestServer(t)
	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"search object invalid"}]}],
		"session_id":"s-search-object-invalid",
		"user_id":"u-search-object-invalid",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"search","items":[{}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRejectsSearchToolWithUnsupportedProvider(t *testing.T) {
	t.Setenv("NEXTAI_ENABLE_SEARCH_TOOL", "true")
	t.Setenv("NEXTAI_SEARCH_SERPAPI_KEY", "test-key")

	srv := newTestServer(t)
	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"search provider invalid"}]}],
		"session_id":"s-search-provider-invalid",
		"user_id":"u-search-provider-invalid",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"search","items":[{"query":"nextai","provider":"duckduckgo"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentRunsMultiStepAgentLoop(t *testing.T) {
	var calls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_shell","type":"function","function":{"name":"shell","arguments":"{\"items\":[{\"command\":\"printf from-tool\"}]}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"final answer from tool"}}]}`))
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

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"read then answer"}]}],"session_id":"s-agent-loop","user_id":"u-agent-loop","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 model calls, got=%d", calls)
	}

	var out domain.AgentProcessResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w3.Body.String())
	}
	if out.Reply != "final answer from tool" {
		t.Fatalf("unexpected final reply: %q", out.Reply)
	}
	hasToolCall := false
	hasToolResult := false
	for _, evt := range out.Events {
		if evt.Type == "tool_call" && evt.ToolCall != nil && evt.ToolCall.Name == "shell" {
			hasToolCall = true
		}
		if evt.Type == "tool_result" && evt.ToolResult != nil && evt.ToolResult.Name == "shell" {
			hasToolResult = true
		}
	}
	if !hasToolCall || !hasToolResult {
		t.Fatalf("expected tool_call/tool_result events, got=%+v", out.Events)
	}
}

func TestProcessAgentContinuesAfterToolInputError(t *testing.T) {
	_, absPath := newToolTestPath(t, "edit-lines-error-continue")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}
	toolArgs, err := json.Marshal(map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"path":    absPath,
				"start":   9,
				"end":     9,
				"content": "x",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal tool args failed: %v", err)
	}

	var calls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_edit",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "edit",
										"arguments": string(toolArgs),
									},
								},
							},
						},
					},
				},
			})
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request failed: %v", err)
		}
		messages, _ := req["messages"].([]interface{})
		hasToolErrorFeedback := false
		for _, item := range messages {
			msg, _ := item.(map[string]interface{})
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			if role == "tool" &&
				strings.Contains(content, "tool_error code=invalid_tool_input") &&
				strings.Contains(content, "tool input line range is out of file bounds") {
				hasToolErrorFeedback = true
				break
			}
		}
		if !hasToolErrorFeedback {
			t.Fatalf("expected tool error feedback in second provider request, got=%#v", req["messages"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "fixed after tool error",
					},
				},
			},
		})
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

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"edit then recover"}]}],"session_id":"s-tool-error-recover","user_id":"u-tool-error-recover","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 model calls, got=%d", calls)
	}

	var out domain.AgentProcessResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w3.Body.String())
	}
	if out.Reply != "fixed after tool error" {
		t.Fatalf("unexpected final reply: %q", out.Reply)
	}

	hasFailedToolResult := false
	for _, evt := range out.Events {
		if evt.Type == "tool_result" && evt.ToolResult != nil && evt.ToolResult.Name == "edit" && !evt.ToolResult.OK {
			hasFailedToolResult = true
			if !strings.Contains(evt.ToolResult.Summary, "tool input line range is out of file bounds") {
				t.Fatalf("unexpected tool error summary: %q", evt.ToolResult.Summary)
			}
		}
	}
	if !hasFailedToolResult {
		t.Fatalf("expected failed tool_result event, got=%+v", out.Events)
	}
}

func TestProcessAgentContinuesAfterToolPathError(t *testing.T) {
	var calls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_view",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "view",
										"arguments": `{"items":[{"path":"docs/contracts.md","start":1,"end":2}]}`,
									},
								},
							},
						},
					},
				},
			})
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request failed: %v", err)
		}
		messages, _ := req["messages"].([]interface{})
		hasToolErrorFeedback := false
		for _, item := range messages {
			msg, _ := item.(map[string]interface{})
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			if role == "tool" &&
				strings.Contains(content, "tool_error code=invalid_tool_input") &&
				strings.Contains(content, "tool input path is invalid") {
				hasToolErrorFeedback = true
				break
			}
		}
		if !hasToolErrorFeedback {
			t.Fatalf("expected path error feedback in second provider request, got=%#v", req["messages"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "fixed after path error",
					},
				},
			},
		})
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

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view then recover"}]}],"session_id":"s-tool-path-recover","user_id":"u-tool-path-recover","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 model calls, got=%d", calls)
	}

	var out domain.AgentProcessResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w3.Body.String())
	}
	if out.Reply != "fixed after path error" {
		t.Fatalf("unexpected final reply: %q", out.Reply)
	}
}

func TestProcessAgentContinuesAfterProviderToolArgumentParseError(t *testing.T) {
	var calls int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_view",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "view",
										"arguments": `{"items":[{"path":"/tmp/a","start":1,"end":2}]`,
									},
								},
							},
						},
					},
				},
			})
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode provider request failed: %v", err)
		}
		messages, _ := req["messages"].([]interface{})
		hasToolErrorFeedback := false
		for _, item := range messages {
			msg, _ := item.(map[string]interface{})
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			if role == "tool" &&
				strings.Contains(content, "tool_error code=invalid_tool_input") &&
				strings.Contains(content, "provider tool call arguments for view are invalid") &&
				strings.Contains(content, "unexpected end of JSON input") &&
				strings.Contains(content, `raw_arguments={"items":[{"path":"/tmp/a","start":1,"end":2}]`) {
				hasToolErrorFeedback = true
				break
			}
		}
		if !hasToolErrorFeedback {
			t.Fatalf("expected provider tool-argument error feedback in second provider request, got=%#v", req["messages"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "fixed after provider tool argument error",
					},
				},
			},
		})
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

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view then recover from malformed args"}]}],"session_id":"s-provider-tool-args-recover","user_id":"u-provider-tool-args-recover","channel":"console","stream":false}`
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w3.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w3.Code, w3.Body.String())
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 model calls, got=%d", calls)
	}

	var out domain.AgentProcessResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w3.Body.String())
	}
	if out.Reply != "fixed after provider tool argument error" {
		t.Fatalf("unexpected final reply: %q", out.Reply)
	}

	hasFailedToolCall := false
	hasFailedToolResult := false
	for _, evt := range out.Events {
		if evt.Type == "tool_call" && evt.ToolCall != nil && evt.ToolCall.Name == "view" {
			if raw, _ := evt.ToolCall.Input["raw_arguments"].(string); strings.Contains(raw, `{"items":[{"path":"/tmp/a","start":1,"end":2}]`) {
				hasFailedToolCall = true
			}
		}
		if evt.Type == "tool_result" && evt.ToolResult != nil && evt.ToolResult.Name == "view" && !evt.ToolResult.OK {
			if strings.Contains(evt.ToolResult.Summary, "unexpected end of JSON input") {
				hasFailedToolResult = true
			}
		}
	}
	if !hasFailedToolCall || !hasFailedToolResult {
		t.Fatalf("expected failed tool_call/tool_result events for malformed arguments, got=%+v", out.Events)
	}
}

func TestProcessAgentStillSupportsLegacyBizParamsToolFormat(t *testing.T) {
	srv := newTestServer(t)
	_, absPath := newToolTestPath(t, "legacy-view-lines")
	if err := os.WriteFile(absPath, []byte("line-1\nline-2\nline-3\n"), 0o644); err != nil {
		t.Fatalf("seed tool test file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"legacy view"}]}],
		"session_id":"s-legacy-view",
		"user_id":"u-legacy-view",
		"channel":"console",
		"stream":false,
		"biz_params":{"tool":{"name":"view","items":[{"path":%q,"start":1,"end":1}]}}
	}`, absPath)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1: line-1") {
		t.Fatalf("expected legacy format output, got=%s", w.Body.String())
	}
}

func TestProcessAgentAllowsAbsolutePathOutsideRepositoryForViewTool(t *testing.T) {
	srv := newTestServer(t)
	outside := filepath.Join(t.TempDir(), "outside-view.txt")
	if err := os.WriteFile(outside, []byte("outside-1\noutside-2\n"), 0o644); err != nil {
		t.Fatalf("seed outside file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"view outside path"}]}],
		"session_id":"s-outside-view",
		"user_id":"u-outside-view",
		"channel":"console",
		"stream":false,
		"view":[{"path":%q,"start":1,"end":2}]
	}`, outside)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1: outside-1") {
		t.Fatalf("expected outside path output, got=%s", w.Body.String())
	}
}

func TestProcessAgentAllowsAbsolutePathOutsideRepositoryForEditTool(t *testing.T) {
	srv := newTestServer(t)
	outside := filepath.Join(t.TempDir(), "outside-edit.txt")
	if err := os.WriteFile(outside, []byte("old-1\nold-2\nold-3\n"), 0o644); err != nil {
		t.Fatalf("seed outside file failed: %v", err)
	}

	procReq := fmt.Sprintf(`{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"edit outside path"}]}],
		"session_id":"s-outside-edit",
		"user_id":"u-outside-edit",
		"channel":"console",
		"stream":false,
		"edit":[{"path":%q,"start":2,"end":2,"content":"new-2"}]
	}`, outside)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file failed: %v", err)
	}
	if string(raw) != "old-1\nnew-2\nold-3\n" {
		t.Fatalf("unexpected file content: %q", string(raw))
	}
}

func TestProcessAgentRejectsRelativePathForViewTool(t *testing.T) {
	srv := newTestServer(t)
	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"relative path"}]}],
		"session_id":"s-relative-path",
		"user_id":"u-relative-path",
		"channel":"console",
		"stream":false,
		"view":[{"path":"docs/contracts.md","start":1,"end":1}]
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"invalid_tool_input"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestProcessAgentStreamIncludesStructuredEvents(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"stream tool"}]}],
		"session_id":"s-stream-events",
		"user_id":"u-stream-events",
		"channel":"console",
		"stream":true,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"printf stream"}]}}
	}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w.Code != http.StatusOK {
		t.Fatalf("process status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"type":"tool_call"`) {
		t.Fatalf("expected tool_call event, body=%s", body)
	}
	if !strings.Contains(body, `"type":"tool_result"`) {
		t.Fatalf("expected tool_result event, body=%s", body)
	}
	if !strings.Contains(body, `"type":"assistant_delta"`) {
		t.Fatalf("expected assistant_delta event, body=%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected DONE marker, body=%s", body)
	}
}

func TestProcessAgentStreamFlushesEventsInRealTime(t *testing.T) {
	srv := newTestServer(t)

	procReq := `{
		"input":[{"role":"user","type":"message","content":[{"type":"text","text":"stream realtime"}]}],
		"session_id":"s-stream-realtime",
		"user_id":"u-stream-realtime",
		"channel":"console",
		"stream":true,
		"biz_params":{"tool":{"name":"shell","items":[{"command":"sleep 1; printf stream"}]}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq))
	writer := newStreamingProbeWriter()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(writer, req)
		close(done)
	}()

	select {
	case <-writer.signal:
	case <-time.After(350 * time.Millisecond):
		t.Fatalf("expected SSE chunk before tool execution finished, body=%s", writer.BodyString())
	}

	select {
	case <-done:
		t.Fatalf("expected request to keep running after first SSE event, body=%s", writer.BodyString())
	default:
	}

	partialBody := writer.BodyString()
	if !strings.Contains(partialBody, `"type":"step_started"`) {
		t.Fatalf("expected step_started in early stream chunk, body=%s", partialBody)
	}
	if strings.Contains(partialBody, "data: [DONE]") {
		t.Fatalf("unexpected DONE before tool finished, body=%s", partialBody)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("stream request timeout, partial body=%s", writer.BodyString())
	}

	body := writer.BodyString()
	if !strings.Contains(body, `"type":"tool_call"`) {
		t.Fatalf("expected tool_call event, body=%s", body)
	}
	if !strings.Contains(body, `"type":"completed"`) {
		t.Fatalf("expected completed event, body=%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected DONE marker, body=%s", body)
	}
}

func TestModelsCatalogReflectsStateProviders(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"display_name":"My Custom Gateway","enabled":true}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/custom-openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config custom provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	wDelete := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wDelete, httptest.NewRequest(http.MethodDelete, "/models/openai", nil))
	if wDelete.Code != http.StatusOK {
		t.Fatalf("delete openai status=%d body=%s", wDelete.Code, wDelete.Body.String())
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/models/catalog", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", w.Code, w.Body.String())
	}

	var out struct {
		Providers     []domain.ProviderInfo     `json:"providers"`
		ProviderTypes []domain.ProviderTypeInfo `json:"provider_types"`
		Defaults      map[string]string         `json:"defaults"`
		ActiveLLM     domain.ModelSlotConfig    `json:"active_llm"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode catalog failed: %v body=%s", err, w.Body.String())
	}
	if len(out.Providers) != 1 {
		t.Fatalf("expected 1 provider from state, got=%d", len(out.Providers))
	}
	providersByID := map[string]domain.ProviderInfo{}
	for _, item := range out.Providers {
		providersByID[item.ID] = item
	}
	if providersByID["openai"].ID != "" {
		t.Fatalf("expected deleted openai provider not to appear in catalog")
	}
	if providersByID["custom-openai"].ID == "" {
		t.Fatalf("expected custom provider in catalog")
	}
	if !providersByID["custom-openai"].OpenAICompatible {
		t.Fatalf("expected custom provider to be openai-compatible")
	}
	if _, ok := out.Defaults["custom-openai"]; !ok {
		t.Fatalf("expected custom default key to exist")
	}
	if out.ActiveLLM.ProviderID != "" || out.ActiveLLM.Model != "" {
		t.Fatalf("expected empty active_llm, got=%+v", out.ActiveLLM)
	}
	if len(out.ProviderTypes) == 0 {
		t.Fatalf("expected provider_types not empty")
	}
	typesByID := map[string]domain.ProviderTypeInfo{}
	for _, item := range out.ProviderTypes {
		typesByID[item.ID] = item
	}
	if typesByID["openai"].ID == "" {
		t.Fatalf("expected provider_type openai")
	}
	if typesByID[provider.AdapterOpenAICompatible].ID == "" {
		t.Fatalf("expected provider_type %q", provider.AdapterOpenAICompatible)
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

func TestConfigureProviderPersistsDisplayName(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"display_name":"My OpenAI Gateway","enabled":true}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config provider status=%d body=%s", w1.Code, w1.Body.String())
	}
	if !strings.Contains(w1.Body.String(), `"display_name":"My OpenAI Gateway"`) {
		t.Fatalf("expected display_name persisted, body=%s", w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/models", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("list providers status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"display_name":"My OpenAI Gateway"`) {
		t.Fatalf("expected display_name in catalog, body=%s", w2.Body.String())
	}
}

func TestConfigureProviderCreatesCustomProvider(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"enabled":true,"display_name":"Custom Provider"}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/custom-openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config custom provider status=%d body=%s", w1.Code, w1.Body.String())
	}
	if !strings.Contains(w1.Body.String(), `"id":"custom-openai"`) {
		t.Fatalf("expected custom provider id in response, body=%s", w1.Body.String())
	}

	setActive := `{"provider_id":"custom-openai","model":"my-model"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active custom provider status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"provider_id":"custom-openai"`) {
		t.Fatalf("expected active provider updated, body=%s", w2.Body.String())
	}
}

func TestSetActiveModelsResolvesAliasForCustomProvider(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"enabled":true,"model_aliases":{"fast":"gpt-4o-mini"}}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/custom-openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config custom provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	setActive := `{"provider_id":"custom-openai","model":"fast"}`
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w2.Code != http.StatusOK {
		t.Fatalf("set active custom provider status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"model":"gpt-4o-mini"`) {
		t.Fatalf("expected custom alias resolved model, body=%s", w2.Body.String())
	}
}

func TestDeleteProviderAllowsDeleteAllAndClearsActive(t *testing.T) {
	srv := newTestServer(t)

	setActiveReq := `{"provider_id":"openai","model":"gpt-4o-mini"}`
	setActiveW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(setActiveW, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActiveReq)))
	if setActiveW.Code != http.StatusOK {
		t.Fatalf("set active openai status=%d body=%s", setActiveW.Code, setActiveW.Body.String())
	}

	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodDelete, "/models/openai", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("delete openai status=%d body=%s", w1.Code, w1.Body.String())
	}
	if !strings.Contains(w1.Body.String(), `"deleted":true`) {
		t.Fatalf("expected openai deleted=true, body=%s", w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/models/active", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("get active models status=%d body=%s", w2.Code, w2.Body.String())
	}
	var activeOut struct {
		ActiveLLM domain.ModelSlotConfig `json:"active_llm"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &activeOut); err != nil {
		t.Fatalf("decode active models failed: %v body=%s", err, w2.Body.String())
	}
	if activeOut.ActiveLLM.ProviderID != "" || activeOut.ActiveLLM.Model != "" {
		t.Fatalf("expected empty active_llm after deleting active provider, got=%+v", activeOut.ActiveLLM)
	}

	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/models/catalog", nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", w3.Code, w3.Body.String())
	}
	var catalogOut struct {
		Providers []domain.ProviderInfo `json:"providers"`
	}
	if err := json.Unmarshal(w3.Body.Bytes(), &catalogOut); err != nil {
		t.Fatalf("decode catalog failed: %v body=%s", err, w3.Body.String())
	}
	if len(catalogOut.Providers) != 0 {
		t.Fatalf("expected providers to be empty after deleting all, got=%d", len(catalogOut.Providers))
	}

	procReq := `{"input":[{"role":"user","type":"message","content":[{"type":"text","text":"hello after delete"}]}],"session_id":"s-delete","user_id":"u-delete","channel":"console","stream":false}`
	w4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w4, httptest.NewRequest(http.MethodPost, "/agent/process", strings.NewReader(procReq)))
	if w4.Code != http.StatusOK {
		t.Fatalf("process after deleting all providers status=%d body=%s", w4.Code, w4.Body.String())
	}
	if !strings.Contains(w4.Body.String(), `"Echo: hello after delete"`) {
		t.Fatalf("expected demo echo fallback after deleting all providers, body=%s", w4.Body.String())
	}
}

func TestDeleteProviderDeletesCustomProvider(t *testing.T) {
	srv := newTestServer(t)

	configProvider := `{"enabled":true}`
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, httptest.NewRequest(http.MethodPut, "/models/custom-openai/config", strings.NewReader(configProvider)))
	if w1.Code != http.StatusOK {
		t.Fatalf("config custom provider status=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest(http.MethodDelete, "/models/custom-openai", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("delete custom provider status=%d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"deleted":true`) {
		t.Fatalf("expected custom deleted=true, body=%s", w2.Body.String())
	}
}

func TestSetActiveModelsRejectsProviderNotInState(t *testing.T) {
	srv := newTestServer(t)

	setActive := `{"provider_id":"custom-openai","model":"my-model"}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/models/active", strings.NewReader(setActive)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"provider_not_found"`) {
		t.Fatalf("unexpected body: %s", w.Body.String())
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

func TestRunCronJobQQChannelFailsFast(t *testing.T) {
	srv := newTestServer(t)
	createReq := `{
		"id":"job-qq",
		"name":"job-qq",
		"enabled":false,
		"schedule":{"type":"interval","cron":"60s"},
		"task_type":"text",
		"text":"hello qq cron",
		"dispatch":{"channel":"qq","target":{"user_id":"u-qq-cron","session_id":"s-qq-cron"}},
		"runtime":{"max_concurrency":1,"timeout_seconds":5}
	}`
	createW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createW, httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(createReq)))
	if createW.Code != http.StatusOK {
		t.Fatalf("create cron status=%d body=%s", createW.Code, createW.Body.String())
	}
	var created domain.CronJobSpec
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created cron failed: %v body=%s", err, createW.Body.String())
	}
	if created.Dispatch.Channel != "qq" {
		t.Fatalf("expected created dispatch channel=qq, got=%q", created.Dispatch.Channel)
	}

	runW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(runW, httptest.NewRequest(http.MethodPost, "/cron/jobs/job-qq/run", nil))
	if runW.Code != http.StatusInternalServerError {
		t.Fatalf("expected run cron status=500, got=%d body=%s", runW.Code, runW.Body.String())
	}
	if !strings.Contains(runW.Body.String(), "inbound-only") {
		t.Fatalf("expected qq inbound-only error, body=%s", runW.Body.String())
	}

	state := getCronState(t, srv, "job-qq")
	if got, _ := state["last_status"].(string); got != cronStatusFailed {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusFailed, state["last_status"])
	}
	if errMsg, _ := state["last_error"].(string); !strings.Contains(errMsg, "inbound-only") {
		t.Fatalf("expected inbound-only last_error, got=%v", state["last_error"])
	}
}

func TestRunCronJobRoutesConsoleTextThroughAgent(t *testing.T) {
	srv := newTestServer(t)

	createChatReq := `{"name":"existing-chat","session_id":"s-cron-console","user_id":"u-cron-console","channel":"console","meta":{}}`
	createChatW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createChatW, httptest.NewRequest(http.MethodPost, "/chats", strings.NewReader(createChatReq)))
	if createChatW.Code != http.StatusOK {
		t.Fatalf("create chat status=%d body=%s", createChatW.Code, createChatW.Body.String())
	}

	var created domain.ChatSpec
	if err := json.Unmarshal(createChatW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created chat failed: %v body=%s", err, createChatW.Body.String())
	}
	if created.ID == "" {
		t.Fatalf("created chat id is empty")
	}

	createCronReq := `{
		"id":"job-console",
		"name":"job-console",
		"enabled":false,
		"schedule":{"type":"interval","cron":"60s"},
		"task_type":"text",
		"text":"hello console cron",
		"dispatch":{"channel":"console","target":{"user_id":"u-cron-console","session_id":"s-cron-console"}},
		"runtime":{"max_concurrency":1,"timeout_seconds":5}
	}`
	createCronW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createCronW, httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(createCronReq)))
	if createCronW.Code != http.StatusOK {
		t.Fatalf("create cron status=%d body=%s", createCronW.Code, createCronW.Body.String())
	}

	runW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(runW, httptest.NewRequest(http.MethodPost, "/cron/jobs/job-console/run", nil))
	if runW.Code != http.StatusOK {
		t.Fatalf("run cron status=%d body=%s", runW.Code, runW.Body.String())
	}

	state := getCronState(t, srv, "job-console")
	if got, _ := state["last_status"].(string); got != cronStatusSucceeded {
		t.Fatalf("expected last_status=%q, got=%v", cronStatusSucceeded, state["last_status"])
	}

	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/chats?channel=console&user_id=u-cron-console", nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list chats status=%d body=%s", listW.Code, listW.Body.String())
	}

	var chats []domain.ChatSpec
	if err := json.Unmarshal(listW.Body.Bytes(), &chats); err != nil {
		t.Fatalf("decode chats failed: %v body=%s", err, listW.Body.String())
	}
	if len(chats) != 1 {
		t.Fatalf("expected one chat, got=%d", len(chats))
	}
	if got, _ := chats[0].Meta["source"].(string); got != "cron" {
		t.Fatalf("expected chat meta source=cron, got=%v", chats[0].Meta["source"])
	}
	if got, _ := chats[0].Meta["cron_job_id"].(string); got != "job-console" {
		t.Fatalf("expected chat meta cron_job_id=job-console, got=%v", chats[0].Meta["cron_job_id"])
	}

	historyW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(historyW, httptest.NewRequest(http.MethodGet, "/chats/"+created.ID, nil))
	if historyW.Code != http.StatusOK {
		t.Fatalf("get history status=%d body=%s", historyW.Code, historyW.Body.String())
	}

	var history domain.ChatHistory
	if err := json.Unmarshal(historyW.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history failed: %v body=%s", err, historyW.Body.String())
	}
	if len(history.Messages) < 2 {
		t.Fatalf("expected user+assistant history messages after cron run, got=%d", len(history.Messages))
	}

	userInput := history.Messages[len(history.Messages)-2]
	if userInput.Role != "user" {
		t.Fatalf("expected user role for cron prompt, got=%q", userInput.Role)
	}
	if len(userInput.Content) == 0 || userInput.Content[0].Text != "hello console cron" {
		t.Fatalf("unexpected cron prompt content: %+v", userInput.Content)
	}

	last := history.Messages[len(history.Messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("expected assistant role, got=%q", last.Role)
	}
	if len(last.Content) == 0 || last.Content[0].Text != "Echo: hello console cron" {
		t.Fatalf("unexpected last message content: %+v", last.Content)
	}
}

func TestCronSchedulerRecoversPersistedDueJob(t *testing.T) {
	dir, err := os.MkdirTemp("", "nextai-gateway-recovery-")
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
	dir, err := os.MkdirTemp("", "nextai-gateway-misfire-")
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
	dir, err := os.MkdirTemp("", "nextai-gateway-distributed-lock-")
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

	if err := srv.executeCronJob("job-timeout"); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got=%v", err)
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
