package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"copaw-next/apps/gateway/internal/config"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "copaw-next-gateway-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	srv, err := NewServer(config.Config{Host: "127.0.0.1", Port: "0", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
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
