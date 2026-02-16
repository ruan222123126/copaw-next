package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"copaw-next/apps/gateway/internal/domain"
	"copaw-next/apps/gateway/internal/provider"
)

const (
	ProviderDemo   = "demo"
	ProviderOpenAI = "openai"

	defaultOpenAIBaseURL = "https://api.openai.com/v1"

	ErrorCodeProviderNotConfigured = "provider_not_configured"
	ErrorCodeProviderNotSupported  = "provider_not_supported"
	ErrorCodeProviderRequestFailed = "provider_request_failed"
	ErrorCodeProviderInvalidReply  = "provider_invalid_reply"
)

type RunnerError struct {
	Code    string
	Message string
	Err     error
}

func (e *RunnerError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e *RunnerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type GenerateConfig struct {
	ProviderID string
	Model      string
	APIKey     string
	BaseURL    string
	AdapterID  string
	Headers    map[string]string
	TimeoutMS  int
}

type ProviderAdapter interface {
	ID() string
	GenerateReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, runner *Runner) (string, error)
}

type Runner struct {
	httpClient *http.Client
	adapters   map[string]ProviderAdapter
}

func New() *Runner {
	return NewWithHTTPClient(&http.Client{Timeout: 30 * time.Second})
}

func NewWithHTTPClient(client *http.Client) *Runner {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	r := &Runner{
		httpClient: client,
		adapters:   map[string]ProviderAdapter{},
	}
	r.registerAdapter(&demoAdapter{})
	r.registerAdapter(&openAICompatibleAdapter{})
	return r
}

func (r *Runner) registerAdapter(adapter ProviderAdapter) {
	if adapter == nil {
		return
	}
	id := strings.TrimSpace(adapter.ID())
	if id == "" {
		return
	}
	r.adapters[id] = adapter
}

func (r *Runner) GenerateReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig) (string, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
	if providerID == "" {
		providerID = ProviderDemo
	}

	adapterID := strings.TrimSpace(cfg.AdapterID)
	if adapterID == "" {
		adapterID = defaultAdapterForProvider(providerID)
	}
	if adapterID == "" {
		return "", &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("provider %q is not supported", providerID),
		}
	}

	if adapterID != provider.AdapterDemo && strings.TrimSpace(cfg.Model) == "" {
		return "", &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "model is required for active provider"}
	}

	adapter, ok := r.adapters[adapterID]
	if !ok {
		return "", &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("adapter %q is not supported", adapterID),
		}
	}
	return adapter.GenerateReply(ctx, req, cfg, r)
}

type demoAdapter struct{}

func (a *demoAdapter) ID() string {
	return provider.AdapterDemo
}

func (a *demoAdapter) GenerateReply(_ context.Context, req domain.AgentProcessRequest, _ GenerateConfig, _ *Runner) (string, error) {
	return generateDemoReply(req), nil
}

type openAICompatibleAdapter struct{}

func (a *openAICompatibleAdapter) ID() string {
	return provider.AdapterOpenAICompatible
}

func (a *openAICompatibleAdapter) GenerateReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, runner *Runner) (string, error) {
	return runner.generateOpenAICompatibleReply(ctx, req, cfg)
}

func defaultAdapterForProvider(providerID string) string {
	switch providerID {
	case "", ProviderDemo:
		return provider.AdapterDemo
	case ProviderOpenAI:
		return provider.AdapterOpenAICompatible
	default:
		return ""
	}
}

func generateDemoReply(req domain.AgentProcessRequest) string {
	parts := make([]string, 0, len(req.Input))
	for _, msg := range req.Input {
		if msg.Role != "user" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "Echo: (empty input)"
	}
	return "Echo: " + strings.Join(parts, " ")
}

func (r *Runner) generateOpenAICompatibleReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig) (string, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return "", &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	payload := openAIChatRequest{
		Model:    cfg.Model,
		Messages: toOpenAIMessages(req.Input),
	}
	if len(payload.Messages) == 0 {
		return generateDemoReply(req), nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to encode provider request",
			Err:     err,
		}
	}

	requestCtx := ctx
	cancel := func() {}
	if cfg.TimeoutMS > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to create provider request",
			Err:     err,
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range cfg.Headers {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return "", &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to read provider response",
			Err:     err,
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d", resp.StatusCode),
		}
	}

	var completion openAIChatResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response is not valid json",
			Err:     err,
		}
	}
	if len(completion.Choices) == 0 {
		return "", &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has no choices",
		}
	}

	text := strings.TrimSpace(extractOpenAIContent(completion.Choices[0].Message.Content))
	if text == "" {
		return "", &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}
	return text, nil
}

type openAIChatRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func toOpenAIMessages(input []domain.AgentInputMessage) []openAIMessage {
	out := make([]openAIMessage, 0, len(input))
	for _, msg := range input {
		content := strings.TrimSpace(flattenText(msg.Content))
		if content == "" {
			continue
		}
		out = append(out, openAIMessage{
			Role:    normalizeRole(msg.Role),
			Content: content,
		})
	}
	return out
}

func flattenText(content []domain.RuntimeContent) string {
	parts := make([]string, 0, len(content))
	for _, c := range content {
		if c.Type != "text" {
			continue
		}
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "assistant", "user", "tool":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "user"
	}
}

func extractOpenAIContent(raw json.RawMessage) string {
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		parts := make([]string, 0, len(arr))
		for _, item := range arr {
			if item.Type != "text" {
				continue
			}
			text := strings.TrimSpace(item.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
