package plugin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSearchToolFromEnvRequiresProvider(t *testing.T) {
	_, err := NewSearchToolFromEnv()
	if !errors.Is(err, ErrSearchToolProvidersMissing) {
		t.Fatalf("expected ErrSearchToolProvidersMissing, got=%v", err)
	}
}

func TestSearchToolInvokeSerpAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "nextai" {
			t.Fatalf("unexpected query: %q", got)
		}
		if got := r.URL.Query().Get("api_key"); got != "serp-key" {
			t.Fatalf("unexpected api_key: %q", got)
		}
		_, _ = w.Write([]byte(`{"organic_results":[{"title":"NextAI","link":"https://example.com","snippet":"desc"}]}`))
	}))
	defer server.Close()

	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	t.Setenv(searchSerpAPIBaseEnv, server.URL+"/search.json")
	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	out, invokeErr := tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"query": "nextai", "count": 3},
		},
	})
	if invokeErr != nil {
		t.Fatalf("invoke failed: %v", invokeErr)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%#v", out["ok"])
	}
	results, _ := out["results"].([]searchResult)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got=%d", len(results))
	}
	if results[0].URL != "https://example.com" {
		t.Fatalf("unexpected url: %q", results[0].URL)
	}
}

func TestSearchToolInvokeTavilyWithProviderOverride(t *testing.T) {
	serp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"organic_results":[{"title":"Serp","link":"https://serp.example"}]}`))
	}))
	defer serp.Close()

	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if body["query"] != "multi provider" {
			t.Fatalf("unexpected query in tavily body: %#v", body["query"])
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Tavily","url":"https://tavily.example","content":"snippet"}]}`))
	}))
	defer tavily.Close()

	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	t.Setenv(searchSerpAPIBaseEnv, serp.URL)
	t.Setenv(searchTavilyKeyEnv, "tavily-key")
	t.Setenv(searchTavilyBaseEnv, tavily.URL)
	t.Setenv(searchDefaultProviderEnv, searchProviderSerpAPI)

	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	out, invokeErr := tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"query":    "multi provider",
				"provider": "tavily",
			},
		},
	})
	if invokeErr != nil {
		t.Fatalf("invoke failed: %v", invokeErr)
	}
	if got, _ := out["provider"].(string); got != "tavily" {
		t.Fatalf("expected provider=tavily, got=%q", got)
	}
	results, _ := out["results"].([]searchResult)
	if len(results) != 1 || results[0].Title != "Tavily" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestSearchToolRejectsInvalidInput(t *testing.T) {
	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	_, invokeErr := tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{},
		},
	})
	if !errors.Is(invokeErr, ErrSearchToolQueryMissing) {
		t.Fatalf("expected ErrSearchToolQueryMissing, got=%v", invokeErr)
	}

	_, invokeErr = tool.Invoke(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"query": "nextai", "provider": "duckduckgo"},
		},
	})
	if !errors.Is(invokeErr, ErrSearchToolProviderUnsupported) {
		t.Fatalf("expected ErrSearchToolProviderUnsupported, got=%v", invokeErr)
	}
}
