package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
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

type InvalidToolCallError struct {
	Index        int
	CallID       string
	Name         string
	ArgumentsRaw string
	Err          error
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

func (e *InvalidToolCallError) Error() string {
	if e == nil {
		return ""
	}
	name := strings.TrimSpace(e.Name)
	detail := "invalid arguments"
	if e.Err != nil {
		detail = e.Err.Error()
	}
	if name != "" {
		return fmt.Sprintf("provider tool call %q has invalid arguments: %s", name, detail)
	}
	return fmt.Sprintf("provider tool call[%d] has invalid arguments: %s", e.Index, detail)
}

func (e *InvalidToolCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func InvalidToolCallFromError(err error) (*InvalidToolCallError, bool) {
	var invalidErr *InvalidToolCallError
	if !errors.As(err, &invalidErr) || invalidErr == nil {
		return nil, false
	}
	return invalidErr, true
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

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

type TurnResult struct {
	Text      string
	ToolCalls []ToolCall
}

type ProviderAdapter interface {
	ID() string
	GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner) (TurnResult, error)
}

type StreamProviderAdapter interface {
	ProviderAdapter
	GenerateTurnStream(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner, onDelta func(string)) (TurnResult, error)
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

func (r *Runner) GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition) (TurnResult, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
	if providerID == "" {
		providerID = ProviderDemo
	}

	adapterID := strings.TrimSpace(cfg.AdapterID)
	if adapterID == "" {
		adapterID = defaultAdapterForProvider(providerID)
	}
	if adapterID == "" {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("provider %q is not supported", providerID),
		}
	}

	if adapterID != provider.AdapterDemo && strings.TrimSpace(cfg.Model) == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "model is required for active provider"}
	}

	adapter, ok := r.adapters[adapterID]
	if !ok {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("adapter %q is not supported", adapterID),
		}
	}
	return adapter.GenerateTurn(ctx, req, cfg, tools, r)
}

func (r *Runner) GenerateReply(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig) (string, error) {
	turn, err := r.GenerateTurn(ctx, req, cfg, nil)
	if err != nil {
		return "", err
	}
	if len(turn.ToolCalls) > 0 {
		return "", &RunnerError{Code: ErrorCodeProviderInvalidReply, Message: "provider response contains tool calls but tool support is disabled"}
	}
	text := strings.TrimSpace(turn.Text)
	if text == "" {
		return "", &RunnerError{Code: ErrorCodeProviderInvalidReply, Message: "provider response has empty content"}
	}
	return text, nil
}

func (r *Runner) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	onDelta func(string),
) (TurnResult, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
	if providerID == "" {
		providerID = ProviderDemo
	}

	adapterID := strings.TrimSpace(cfg.AdapterID)
	if adapterID == "" {
		adapterID = defaultAdapterForProvider(providerID)
	}
	if adapterID == "" {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("provider %q is not supported", providerID),
		}
	}

	if adapterID != provider.AdapterDemo && strings.TrimSpace(cfg.Model) == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "model is required for active provider"}
	}

	adapter, ok := r.adapters[adapterID]
	if !ok {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderNotSupported,
			Message: fmt.Sprintf("adapter %q is not supported", adapterID),
		}
	}

	if streamAdapter, ok := adapter.(StreamProviderAdapter); ok {
		return streamAdapter.GenerateTurnStream(ctx, req, cfg, tools, r, onDelta)
	}

	turn, err := adapter.GenerateTurn(ctx, req, cfg, tools, r)
	if err != nil {
		return TurnResult{}, err
	}
	if onDelta != nil && turn.Text != "" {
		onDelta(turn.Text)
	}
	return turn, nil
}

type demoAdapter struct{}

func (a *demoAdapter) ID() string {
	return provider.AdapterDemo
}

func (a *demoAdapter) GenerateTurn(_ context.Context, req domain.AgentProcessRequest, _ GenerateConfig, _ []ToolDefinition, _ *Runner) (TurnResult, error) {
	return TurnResult{Text: generateDemoReply(req)}, nil
}

type openAICompatibleAdapter struct{}

func (a *openAICompatibleAdapter) ID() string {
	return provider.AdapterOpenAICompatible
}

func (a *openAICompatibleAdapter) GenerateTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition, runner *Runner) (TurnResult, error) {
	return runner.generateOpenAICompatibleTurn(ctx, req, cfg, tools)
}

func (a *openAICompatibleAdapter) GenerateTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	runner *Runner,
	onDelta func(string),
) (TurnResult, error) {
	return runner.generateOpenAICompatibleTurnStream(ctx, req, cfg, tools, onDelta)
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

func (r *Runner) generateOpenAICompatibleTurn(ctx context.Context, req domain.AgentProcessRequest, cfg GenerateConfig, tools []ToolDefinition) (TurnResult, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	payload := openAIChatRequest{
		Model:    cfg.Model,
		Messages: toOpenAIMessages(req.Input),
		Tools:    toOpenAITools(tools),
	}
	if len(payload.Messages) == 0 {
		return TurnResult{Text: generateDemoReply(req)}, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, &RunnerError{
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
		return TurnResult{}, &RunnerError{
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
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to read provider response",
			Err:     err,
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d", resp.StatusCode),
		}
	}

	var completion openAIChatResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response is not valid json",
			Err:     err,
		}
	}
	if len(completion.Choices) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has no choices",
		}
	}

	message := completion.Choices[0].Message
	text := strings.TrimSpace(extractOpenAIContent(message.Content))
	toolCalls, err := parseOpenAIToolCalls(message.ToolCalls)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: err.Error(),
			Err:     err,
		}
	}
	if text == "" && len(toolCalls) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}

	return TurnResult{Text: text, ToolCalls: toolCalls}, nil
}

func (r *Runner) generateOpenAICompatibleTurnStream(
	ctx context.Context,
	req domain.AgentProcessRequest,
	cfg GenerateConfig,
	tools []ToolDefinition,
	onDelta func(string),
) (TurnResult, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return TurnResult{}, &RunnerError{Code: ErrorCodeProviderNotConfigured, Message: "provider api_key is required"}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}

	payload := openAIChatRequest{
		Model:    cfg.Model,
		Messages: toOpenAIMessages(req.Input),
		Tools:    toOpenAITools(tools),
		Stream:   true,
	}
	if len(payload.Messages) == 0 {
		return TurnResult{Text: generateDemoReply(req)}, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return TurnResult{}, &RunnerError{
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
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "failed to create provider request",
			Err:     err,
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
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
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: "provider request failed",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderRequestFailed,
			Message: fmt.Sprintf("provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		}
	}

	var replyBuilder strings.Builder
	toolCalls := map[int]*openAIToolCall{}
	processData := func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		var chunk openAIChatStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("provider stream chunk is not valid json: %w", err)
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		for _, choice := range chunk.Choices {
			delta := extractOpenAIDeltaContent(choice.Delta.Content)
			if delta != "" {
				replyBuilder.WriteString(delta)
				if onDelta != nil {
					onDelta(delta)
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				if idx < 0 {
					idx = 0
				}
				current, ok := toolCalls[idx]
				if !ok {
					current = &openAIToolCall{}
					toolCalls[idx] = current
				}
				if strings.TrimSpace(tc.ID) != "" {
					current.ID = strings.TrimSpace(tc.ID)
				}
				if strings.TrimSpace(tc.Type) != "" {
					current.Type = strings.TrimSpace(tc.Type)
				}
				if strings.TrimSpace(tc.Function.Name) != "" {
					current.Function.Name = strings.TrimSpace(tc.Function.Name)
				}
				if tc.Function.Arguments != "" {
					current.Function.Arguments += tc.Function.Arguments
				}
			}
		}
		return nil
	}

	if err := consumeSSEData(resp.Body, processData); err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider stream response is invalid",
			Err:     err,
		}
	}

	orderedIndexes := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		orderedIndexes = append(orderedIndexes, idx)
	}
	sort.Ints(orderedIndexes)
	aggregatedToolCalls := make([]openAIToolCall, 0, len(orderedIndexes))
	for _, idx := range orderedIndexes {
		tc := toolCalls[idx]
		if tc == nil {
			continue
		}
		aggregatedToolCalls = append(aggregatedToolCalls, *tc)
	}

	parsedToolCalls, err := parseOpenAIToolCalls(aggregatedToolCalls)
	if err != nil {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: err.Error(),
			Err:     err,
		}
	}

	reply := replyBuilder.String()
	if strings.TrimSpace(reply) == "" && len(parsedToolCalls) == 0 {
		return TurnResult{}, &RunnerError{
			Code:    ErrorCodeProviderInvalidReply,
			Message: "provider response has empty content",
		}
	}

	return TurnResult{Text: reply, ToolCalls: parsedToolCalls}, nil
}

type openAIChatRequest struct {
	Model    string                 `json:"model"`
	Messages []openAIMessage        `json:"messages"`
	Tools    []openAIToolDefinition `json:"tools,omitempty"`
	Stream   bool                   `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolDefinition struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   json.RawMessage  `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
}

type openAIChatStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content   json.RawMessage        `json:"content"`
			ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
}

type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

func toOpenAIMessages(input []domain.AgentInputMessage) []openAIMessage {
	out := make([]openAIMessage, 0, len(input))
	for _, msg := range input {
		role := normalizeRole(msg.Role)
		content := strings.TrimSpace(flattenText(msg.Content))

		switch role {
		case "assistant":
			toolCalls := parseToolCallsFromMetadata(msg.Metadata)
			item := openAIMessage{Role: role}
			if content != "" {
				item.Content = content
			}
			if len(toolCalls) > 0 {
				item.ToolCalls = toolCalls
			}
			if item.Content == nil && len(item.ToolCalls) == 0 {
				continue
			}
			out = append(out, item)
		case "tool":
			item := openAIMessage{
				Role:    role,
				Content: content,
			}
			if item.Content == nil {
				item.Content = ""
			}
			if toolCallID := metadataString(msg.Metadata, "tool_call_id"); toolCallID != "" {
				item.ToolCallID = toolCallID
			}
			if name := metadataString(msg.Metadata, "name"); name != "" {
				item.Name = name
			}
			out = append(out, item)
		default:
			if content == "" {
				continue
			}
			out = append(out, openAIMessage{Role: role, Content: content})
		}
	}
	return out
}

func toOpenAITools(tools []ToolDefinition) []openAIToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAIToolDefinition, 0, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		params := normalizeToolParameters(item.Parameters)
		out = append(out, openAIToolDefinition{
			Type: "function",
			Function: openAIToolFunction{
				Name:        name,
				Description: strings.TrimSpace(item.Description),
				Parameters:  params,
			},
		})
	}
	return out
}

func parseOpenAIToolCalls(in []openAIToolCall) ([]ToolCall, error) {
	if len(in) == 0 {
		return nil, nil
	}
	calls := make([]ToolCall, 0, len(in))
	for i, item := range in {
		name := strings.TrimSpace(item.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("provider tool call[%d] name is empty", i)
		}
		callID := strings.TrimSpace(item.ID)
		if callID == "" {
			callID = fmt.Sprintf("call_%d", i+1)
		}
		argumentsRaw := strings.TrimSpace(item.Function.Arguments)
		if argumentsRaw == "" {
			argumentsRaw = "{}"
		}
		var arguments map[string]interface{}
		if err := json.Unmarshal([]byte(argumentsRaw), &arguments); err != nil {
			return nil, &InvalidToolCallError{
				Index:        i,
				CallID:       callID,
				Name:         name,
				ArgumentsRaw: argumentsRaw,
				Err:          err,
			}
		}
		if arguments == nil {
			arguments = map[string]interface{}{}
		}
		calls = append(calls, ToolCall{ID: callID, Name: name, Arguments: arguments})
	}
	return calls, nil
}

func parseToolCallsFromMetadata(metadata map[string]interface{}) []openAIToolCall {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["tool_calls"]
	if !ok || raw == nil {
		return nil
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []openAIToolCall
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil
	}
	valid := make([]openAIToolCall, 0, len(out))
	for _, call := range out {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		if strings.TrimSpace(call.ID) == "" {
			continue
		}
		args := strings.TrimSpace(call.Function.Arguments)
		if args == "" {
			call.Function.Arguments = "{}"
		}
		if strings.TrimSpace(call.Type) == "" {
			call.Type = "function"
		}
		valid = append(valid, call)
	}
	return valid
}

func metadataString(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func normalizeToolParameters(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
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

func extractOpenAIDeltaContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var direct string
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var out strings.Builder
		for _, item := range arr {
			if item.Type != "text" || item.Text == "" {
				continue
			}
			out.WriteString(item.Text)
		}
		return out.String()
	}
	return ""
}

func consumeSSEData(reader io.Reader, onData func(string) error) error {
	if reader == nil {
		return fmt.Errorf("stream reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	dataLines := make([]string, 0, 4)
	flushBlock := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if onData == nil {
			return nil
		}
		return onData(payload)
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flushBlock(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		dataLines = append(dataLines, payload)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flushBlock(); err != nil {
		return err
	}
	return nil
}
