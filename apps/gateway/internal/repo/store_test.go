package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeepsCustomProviderAndActiveProvider(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {
    "Custom-OpenAI": {
      "api_key": "sk-legacy",
      "base_url": "http://127.0.0.1:19002/v1",
      "display_name": "Legacy Gateway",
      "enabled": true,
      "headers": {"X-Test": "1"},
      "timeout_ms": 12000,
      "model_aliases": {"fast": "gpt-4o-mini"}
    }
  },
  "active_llm": {"provider_id": "Custom-OpenAI", "model": "legacy-model"}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if len(st.Providers) != 1 {
			t.Fatalf("expected custom provider to remain, got=%d", len(st.Providers))
		}
		custom, ok := st.Providers["custom-openai"]
		if !ok {
			t.Fatalf("custom provider should exist")
		}
		if custom.DisplayName != "Legacy Gateway" {
			t.Fatalf("expected display_name preserved, got=%q", custom.DisplayName)
		}
		if custom.APIKey != "sk-legacy" {
			t.Fatalf("expected api_key preserved, got=%q", custom.APIKey)
		}
		if custom.BaseURL != "http://127.0.0.1:19002/v1" {
			t.Fatalf("expected base_url preserved, got=%q", custom.BaseURL)
		}
		if custom.TimeoutMS != 12000 {
			t.Fatalf("expected timeout_ms preserved, got=%d", custom.TimeoutMS)
		}
		if custom.ModelAliases["fast"] != "gpt-4o-mini" {
			t.Fatalf("expected model_aliases preserved, got=%v", custom.ModelAliases)
		}

		if st.ActiveLLM.ProviderID != "custom-openai" {
			t.Fatalf("expected active provider preserved, got=%q", st.ActiveLLM.ProviderID)
		}
		if st.ActiveLLM.Model != "legacy-model" {
			t.Fatalf("expected active model preserved, got=%q", st.ActiveLLM.Model)
		}
	})
}

func TestLoadKeepsEmptyProvidersAndEmptyActive(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {},
  "active_llm": {"provider_id": "", "model": ""}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if len(st.Providers) != 0 {
			t.Fatalf("expected providers to stay empty, got=%d", len(st.Providers))
		}
		if st.ActiveLLM.ProviderID != "" || st.ActiveLLM.Model != "" {
			t.Fatalf("expected empty active_llm, got=%+v", st.ActiveLLM)
		}
	})
}

func TestLoadDropsLegacyDemoProvider(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw := `{
  "providers": {
    "demo": {"enabled": true},
    "openai": {"enabled": true}
  },
  "active_llm": {"provider_id": "demo", "model": "demo-chat"}
}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}

	store.Read(func(st *State) {
		if _, ok := st.Providers["demo"]; ok {
			t.Fatalf("expected legacy demo provider to be removed")
		}
		if _, ok := st.Providers["openai"]; !ok {
			t.Fatalf("expected openai provider to remain")
		}
		if st.ActiveLLM.ProviderID != "" || st.ActiveLLM.Model != "" {
			t.Fatalf("expected active_llm to be cleared when demo is removed, got=%+v", st.ActiveLLM)
		}
	})
}
