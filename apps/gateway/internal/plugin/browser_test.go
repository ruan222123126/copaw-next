package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewBrowserToolRequiresAgentDir(t *testing.T) {
	_, err := NewBrowserTool("")
	if !errors.Is(err, ErrBrowserToolAgentDirMissing) {
		t.Fatalf("expected ErrBrowserToolAgentDirMissing, got=%v", err)
	}
}

func TestBrowserToolInvokeParsesRunMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.js"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed agent.js failed: %v", err)
	}

	tool, err := NewBrowserTool(dir)
	if err != nil {
		t.Fatalf("new browser tool failed: %v", err)
	}
	tool.runFn = func(_ context.Context, _ string, task string, _ time.Duration) (string, int, error) {
		if task != "打开 bing 搜索 nextai" {
			t.Fatalf("unexpected task: %q", task)
		}
		return "=== 任务结果 ===\n完成\nrun_id: run-1\nlog: /tmp/run-1.jsonl\nshots: /tmp/shots\n", 0, nil
	}

	out, invokeErr := tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"task": "打开 bing 搜索 nextai"},
		},
	})
	if invokeErr != nil {
		t.Fatalf("invoke failed: %v", invokeErr)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%#v", out["ok"])
	}
	if got, _ := out["run_id"].(string); got != "run-1" {
		t.Fatalf("unexpected run_id: %q", got)
	}
	if got, _ := out["log_path"].(string); got != "/tmp/run-1.jsonl" {
		t.Fatalf("unexpected log_path: %q", got)
	}
	if got, _ := out["shots_path"].(string); got != "/tmp/shots" {
		t.Fatalf("unexpected shots_path: %q", got)
	}
}

func TestBrowserToolInvokeRejectsMissingTask(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.js"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed agent.js failed: %v", err)
	}

	tool, err := NewBrowserTool(dir)
	if err != nil {
		t.Fatalf("new browser tool failed: %v", err)
	}

	_, invokeErr := tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{},
		},
	})
	if !errors.Is(invokeErr, ErrBrowserToolTaskMissing) {
		t.Fatalf("expected ErrBrowserToolTaskMissing, got=%v", invokeErr)
	}
}
