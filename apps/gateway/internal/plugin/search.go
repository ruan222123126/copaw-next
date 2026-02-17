package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	searchToolDefaultTimeout  = 20 * time.Second
	searchToolMaxTimeout      = 120 * time.Second
	searchToolDefaultCount    = 5
	searchToolMaxCount        = 10
	searchToolMaxResponseSize = 256 * 1024

	searchProviderSerpAPI = "serpapi"
	searchProviderTavily  = "tavily"
	searchProviderBrave   = "brave"

	searchDefaultProviderEnv = "NEXTAI_SEARCH_DEFAULT_PROVIDER"

	searchSerpAPIKeyEnv  = "NEXTAI_SEARCH_SERPAPI_KEY"
	searchSerpAPIBaseEnv = "NEXTAI_SEARCH_SERPAPI_BASE_URL"
	searchTavilyKeyEnv   = "NEXTAI_SEARCH_TAVILY_KEY"
	searchTavilyBaseEnv  = "NEXTAI_SEARCH_TAVILY_BASE_URL"
	searchBraveKeyEnv    = "NEXTAI_SEARCH_BRAVE_KEY"
	searchBraveBaseEnv   = "NEXTAI_SEARCH_BRAVE_BASE_URL"

	searchSerpAPIDefaultURL = "https://serpapi.com/search.json"
	searchTavilyDefaultURL  = "https://api.tavily.com/search"
	searchBraveDefaultURL   = "https://api.search.brave.com/res/v1/web/search"
)

var (
	ErrSearchToolProvidersMissing     = errors.New("search_tool_providers_missing")
	ErrSearchToolItemsInvalid         = errors.New("search_tool_items_invalid")
	ErrSearchToolQueryMissing         = errors.New("search_tool_query_missing")
	ErrSearchToolProviderUnsupported  = errors.New("search_tool_provider_unsupported")
	ErrSearchToolProviderUnconfigured = errors.New("search_tool_provider_unconfigured")
)

type SearchTool struct {
	defaultProvider string
	providers       map[string]searchProviderConfig
	httpClient      *http.Client
}

type searchProviderConfig struct {
	Name    string
	APIKey  string
	BaseURL string
}

type searchItem struct {
	Query    string
	Provider string
	Count    int
	Timeout  time.Duration
}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Source  string `json:"source"`
}

func NewSearchToolFromEnv() (*SearchTool, error) {
	providers := map[string]searchProviderConfig{}
	if cfg, ok := searchProviderFromEnv(searchProviderSerpAPI, searchSerpAPIKeyEnv, searchSerpAPIBaseEnv, searchSerpAPIDefaultURL); ok {
		providers[cfg.Name] = cfg
	}
	if cfg, ok := searchProviderFromEnv(searchProviderTavily, searchTavilyKeyEnv, searchTavilyBaseEnv, searchTavilyDefaultURL); ok {
		providers[cfg.Name] = cfg
	}
	if cfg, ok := searchProviderFromEnv(searchProviderBrave, searchBraveKeyEnv, searchBraveBaseEnv, searchBraveDefaultURL); ok {
		providers[cfg.Name] = cfg
	}
	if len(providers) == 0 {
		return nil, ErrSearchToolProvidersMissing
	}

	defaultProvider := strings.ToLower(strings.TrimSpace(os.Getenv(searchDefaultProviderEnv)))
	if defaultProvider == "" {
		defaultProvider = pickDefaultSearchProvider(providers)
	}
	if !isSupportedSearchProvider(defaultProvider) {
		return nil, fmt.Errorf("%w: %s", ErrSearchToolProviderUnsupported, defaultProvider)
	}
	if _, ok := providers[defaultProvider]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrSearchToolProviderUnconfigured, defaultProvider)
	}

	return &SearchTool{
		defaultProvider: defaultProvider,
		providers:       providers,
		httpClient:      &http.Client{},
	}, nil
}

func (t *SearchTool) Name() string {
	return "search"
}

func (t *SearchTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	items, err := parseSearchItems(input, t.defaultProvider)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, 0, len(items))
	allOK := true
	for _, item := range items {
		one, oneErr := t.invokeOne(item)
		if oneErr != nil {
			return nil, oneErr
		}
		if ok, _ := one["ok"].(bool); !ok {
			allOK = false
		}
		results = append(results, one)
	}

	if len(results) == 1 {
		return results[0], nil
	}

	texts := make([]string, 0, len(results))
	for _, item := range results {
		if text, ok := item["text"].(string); ok {
			texts = append(texts, text)
		}
	}
	return map[string]interface{}{
		"ok":      allOK,
		"count":   len(results),
		"results": results,
		"text":    strings.Join(texts, "\n\n"),
	}, nil
}

func (t *SearchTool) invokeOne(item searchItem) (map[string]interface{}, error) {
	providerName := strings.ToLower(strings.TrimSpace(item.Provider))
	if providerName == "" {
		providerName = t.defaultProvider
	}
	if !isSupportedSearchProvider(providerName) {
		return nil, fmt.Errorf("%w: %s", ErrSearchToolProviderUnsupported, providerName)
	}
	providerCfg, ok := t.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSearchToolProviderUnconfigured, providerName)
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), item.Timeout)
	defer cancel()

	searchResults, err := t.searchWithProvider(ctx, providerCfg, item.Query, item.Count)
	durationMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		return map[string]interface{}{
			"ok":          false,
			"provider":    providerName,
			"query":       item.Query,
			"count":       item.Count,
			"duration_ms": durationMs,
			"error":       err.Error(),
			"text":        formatSearchFailureText(providerName, item.Query, err),
		}, nil
	}

	return map[string]interface{}{
		"ok":          true,
		"provider":    providerName,
		"query":       item.Query,
		"count":       item.Count,
		"total":       len(searchResults),
		"results":     searchResults,
		"duration_ms": durationMs,
		"text":        formatSearchSuccessText(providerName, item.Query, searchResults),
	}, nil
}

func (t *SearchTool) searchWithProvider(ctx context.Context, provider searchProviderConfig, query string, count int) ([]searchResult, error) {
	switch provider.Name {
	case searchProviderSerpAPI:
		return t.searchSerpAPI(ctx, provider, query, count)
	case searchProviderTavily:
		return t.searchTavily(ctx, provider, query, count)
	case searchProviderBrave:
		return t.searchBrave(ctx, provider, query, count)
	default:
		return nil, fmt.Errorf("%w: %s", ErrSearchToolProviderUnsupported, provider.Name)
	}
}

func (t *SearchTool) searchSerpAPI(ctx context.Context, provider searchProviderConfig, query string, count int) ([]searchResult, error) {
	endpoint, err := url.Parse(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("serpapi invalid base url: %w", err)
	}
	params := endpoint.Query()
	if strings.TrimSpace(params.Get("engine")) == "" {
		params.Set("engine", "google")
	}
	params.Set("q", query)
	params.Set("api_key", provider.APIKey)
	params.Set("num", strconv.Itoa(count))
	endpoint.RawQuery = params.Encode()

	body, status, reqErr := t.sendRequest(ctx, http.MethodGet, endpoint.String(), nil, nil)
	if reqErr != nil {
		return nil, fmt.Errorf("serpapi request failed: %w", formatSearchAPIError(status, body, reqErr))
	}

	var payload struct {
		OrganicResults []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("serpapi decode failed: %w", err)
	}

	results := make([]searchResult, 0, len(payload.OrganicResults))
	for _, item := range payload.OrganicResults {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" && link == "" {
			continue
		}
		results = append(results, searchResult{
			Title:   title,
			URL:     link,
			Snippet: strings.TrimSpace(item.Snippet),
			Source:  searchProviderSerpAPI,
		})
		if len(results) >= count {
			break
		}
	}
	return results, nil
}

func (t *SearchTool) searchTavily(ctx context.Context, provider searchProviderConfig, query string, count int) ([]searchResult, error) {
	requestBody := map[string]interface{}{
		"api_key":      provider.APIKey,
		"query":        query,
		"max_results":  count,
		"search_depth": "basic",
	}
	body, status, reqErr := t.sendRequest(ctx, http.MethodPost, provider.BaseURL, nil, requestBody)
	if reqErr != nil {
		return nil, fmt.Errorf("tavily request failed: %w", formatSearchAPIError(status, body, reqErr))
	}

	var payload struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("tavily decode failed: %w", err)
	}

	results := make([]searchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.URL)
		if title == "" && link == "" {
			continue
		}
		results = append(results, searchResult{
			Title:   title,
			URL:     link,
			Snippet: strings.TrimSpace(item.Content),
			Source:  searchProviderTavily,
		})
		if len(results) >= count {
			break
		}
	}
	return results, nil
}

func (t *SearchTool) searchBrave(ctx context.Context, provider searchProviderConfig, query string, count int) ([]searchResult, error) {
	endpoint, err := url.Parse(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("brave invalid base url: %w", err)
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("count", strconv.Itoa(count))
	endpoint.RawQuery = params.Encode()

	headers := map[string]string{
		"Accept":               "application/json",
		"X-Subscription-Token": provider.APIKey,
	}
	body, status, reqErr := t.sendRequest(ctx, http.MethodGet, endpoint.String(), headers, nil)
	if reqErr != nil {
		return nil, fmt.Errorf("brave request failed: %w", formatSearchAPIError(status, body, reqErr))
	}

	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("brave decode failed: %w", err)
	}

	results := make([]searchResult, 0, len(payload.Web.Results))
	for _, item := range payload.Web.Results {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.URL)
		if title == "" && link == "" {
			continue
		}
		results = append(results, searchResult{
			Title:   title,
			URL:     link,
			Snippet: strings.TrimSpace(item.Description),
			Source:  searchProviderBrave,
		})
		if len(results) >= count {
			break
		}
	}
	return results, nil
}

func (t *SearchTool) sendRequest(ctx context.Context, method, endpoint string, headers map[string]string, body map[string]interface{}) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	client := t.httpClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, searchToolMaxResponseSize))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, resp.StatusCode, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return respBody, resp.StatusCode, nil
}

func parseSearchItems(input map[string]interface{}, defaultProvider string) ([]searchItem, error) {
	rawItems, ok := input["items"]
	if !ok || rawItems == nil {
		return nil, ErrSearchToolItemsInvalid
	}
	entries, ok := rawItems.([]interface{})
	if !ok || len(entries) == 0 {
		return nil, ErrSearchToolItemsInvalid
	}

	out := make([]searchItem, 0, len(entries))
	for _, raw := range entries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return nil, ErrSearchToolItemsInvalid
		}
		query := strings.TrimSpace(stringValue(entry["query"]))
		if query == "" {
			query = strings.TrimSpace(stringValue(entry["q"]))
		}
		if query == "" {
			return nil, ErrSearchToolQueryMissing
		}

		provider := strings.ToLower(strings.TrimSpace(stringValue(entry["provider"])))
		if provider == "" {
			provider = defaultProvider
		}

		out = append(out, searchItem{
			Query:    query,
			Provider: provider,
			Count:    parseSearchCount(entry["count"]),
			Timeout:  parseSearchTimeout(entry["timeout_seconds"]),
		})
	}
	return out, nil
}

func parseSearchTimeout(raw interface{}) time.Duration {
	seconds := int64(searchToolDefaultTimeout / time.Second)
	switch value := raw.(type) {
	case float64:
		seconds = int64(value)
	case int:
		seconds = int64(value)
	case int64:
		seconds = value
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			seconds = parsed
		}
	}
	if seconds <= 0 {
		seconds = int64(searchToolDefaultTimeout / time.Second)
	}
	maxSeconds := int64(searchToolMaxTimeout / time.Second)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func parseSearchCount(raw interface{}) int {
	count := searchToolDefaultCount
	switch value := raw.(type) {
	case float64:
		count = int(value)
	case int:
		count = value
	case int64:
		count = int(value)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			count = parsed
		}
	}
	if count <= 0 {
		count = searchToolDefaultCount
	}
	if count > searchToolMaxCount {
		count = searchToolMaxCount
	}
	return count
}

func formatSearchAPIError(status int, body []byte, err error) error {
	if len(body) == 0 {
		if status > 0 {
			return fmt.Errorf("status=%d err=%w", status, err)
		}
		return err
	}
	compactBody := strings.Join(strings.Fields(string(body)), " ")
	compactBody = truncateOutput(compactBody, 600)
	if status > 0 {
		return fmt.Errorf("status=%d body=%s err=%w", status, compactBody, err)
	}
	return fmt.Errorf("body=%s err=%w", compactBody, err)
}

func formatSearchSuccessText(provider, query string, results []searchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("search via %s: query=%q, no results.", provider, query)
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("search via %s: query=%q, results=%d", provider, query, len(results)))
	for idx, item := range results {
		builder.WriteString(fmt.Sprintf("\n%d. %s", idx+1, coalesceSearchText(item.Title, "(untitled)")))
		if item.URL != "" {
			builder.WriteString(fmt.Sprintf("\n   %s", item.URL))
		}
		snippet := compactSearchSnippet(item.Snippet, 180)
		if snippet != "" {
			builder.WriteString(fmt.Sprintf("\n   %s", snippet))
		}
	}
	return builder.String()
}

func formatSearchFailureText(provider, query string, err error) string {
	return fmt.Sprintf("search via %s failed: query=%q error=%s", provider, query, strings.TrimSpace(err.Error()))
}

func compactSearchSnippet(raw string, maxLen int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if trimmed == "" || maxLen <= 0 {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= maxLen {
		return trimmed
	}
	return string(runes[:maxLen]) + "...(truncated)"
}

func coalesceSearchText(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func pickDefaultSearchProvider(providers map[string]searchProviderConfig) string {
	preferred := []string{searchProviderSerpAPI, searchProviderTavily, searchProviderBrave}
	for _, name := range preferred {
		if _, ok := providers[name]; ok {
			return name
		}
	}
	keys := make([]string, 0, len(providers))
	for key := range providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func isSupportedSearchProvider(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case searchProviderSerpAPI, searchProviderTavily, searchProviderBrave:
		return true
	default:
		return false
	}
}

func searchProviderFromEnv(name, keyEnv, baseEnv, defaultBase string) (searchProviderConfig, bool) {
	apiKey := strings.TrimSpace(os.Getenv(keyEnv))
	if apiKey == "" {
		return searchProviderConfig{}, false
	}
	baseURL := strings.TrimSpace(os.Getenv(baseEnv))
	if baseURL == "" {
		baseURL = defaultBase
	}
	return searchProviderConfig{
		Name:    name,
		APIKey:  apiKey,
		BaseURL: baseURL,
	}, true
}
