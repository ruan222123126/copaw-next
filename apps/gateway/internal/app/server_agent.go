package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	agentservice "nextai/apps/gateway/internal/service/agent"
)

func (s *Server) processAgent(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	s.processAgentWithBody(w, r, bodyBytes)
}

type agentSystemLayerView struct {
	Name            string `json:"name"`
	Role            string `json:"role"`
	Source          string `json:"source,omitempty"`
	ContentPreview  string `json:"content_preview,omitempty"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

type agentSystemLayersResponse struct {
	Version              string                 `json:"version"`
	Layers               []agentSystemLayerView `json:"layers"`
	EstimatedTokensTotal int                    `json:"estimated_tokens_total"`
}

func (s *Server) getAgentSystemLayers(w http.ResponseWriter, _ *http.Request) {
	if !s.cfg.EnablePromptContextIntrospect {
		writeErr(w, http.StatusNotFound, "feature_disabled", "prompt context introspection is disabled", nil)
		return
	}

	layers, err := s.buildSystemLayers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_tool_guide_unavailable", "ai tools guide is unavailable", nil)
		return
	}

	resp := agentSystemLayersResponse{
		Version: "v1",
		Layers:  make([]agentSystemLayerView, 0, len(layers)),
	}
	for _, layer := range layers {
		tokens := estimatePromptTokenCount(layer.Content)
		resp.EstimatedTokensTotal += tokens
		resp.Layers = append(resp.Layers, agentSystemLayerView{
			Name:            layer.Name,
			Role:            layer.Role,
			Source:          layer.Source,
			ContentPreview:  summarizeLayerPreview(layer.Content, 160),
			EstimatedTokens: tokens,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) processQQInbound(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}

	event, err := parseQQInboundEvent(bodyBytes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_qq_event", err.Error(), nil)
		return
	}
	if strings.TrimSpace(event.Text) == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"accepted": false,
			"reason":   "empty_text",
		})
		return
	}

	request := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: event.Text},
				},
			},
		},
		SessionID: event.SessionID,
		UserID:    event.UserID,
		Channel:   "qq",
		Stream:    false,
		BizParams: map[string]interface{}{
			"channel": map[string]interface{}{
				"target_type": event.TargetType,
				"target_id":   event.TargetID,
				"msg_id":      event.MessageID,
			},
		},
	}

	agentBody, err := json.Marshal(request)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qq_inbound_marshal_failed", "failed to build agent request", nil)
		return
	}
	s.processAgentWithBody(w, r, agentBody)
}

func (s *Server) processAgentWithBody(w http.ResponseWriter, r *http.Request, bodyBytes []byte) {

	var req domain.AgentProcessRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	rawRequest := map[string]interface{}{}
	if err := json.Unmarshal(bodyBytes, &rawRequest); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.SessionID == "" || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "session_id and user_id are required", nil)
		return
	}
	req.Channel = resolveProcessRequestChannel(r, req.Channel)
	channelPlugin, channelCfg, channelName, err := s.resolveChannel(req.Channel)
	if err != nil {
		status, code, message := mapChannelError(err)
		writeErr(w, status, code, message, nil)
		return
	}
	req.Channel = channelName
	if isContextResetCommand(req.Input) {
		if err := s.clearChatContext(req.SessionID, req.UserID, req.Channel); err != nil {
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		dispatchCfg := mergeChannelDispatchConfig(channelName, channelCfg, req.BizParams)
		if err := channelPlugin.SendText(r.Context(), req.UserID, req.SessionID, contextResetReply, dispatchCfg); err != nil {
			status, code, message := mapChannelError(&channelError{
				Code:    "channel_dispatch_failed",
				Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
				Err:     err,
			})
			writeErr(w, status, code, message, nil)
			return
		}
		writeImmediateAgentResponse(w, req.Stream, contextResetReply)
		return
	}

	systemLayers, err := s.buildSystemLayers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_tool_guide_unavailable", "ai tools guide is unavailable", nil)
		return
	}

	cronChatMeta := cronChatMetaFromBizParams(req.BizParams)
	chatID := ""
	activeLLM := domain.ModelSlotConfig{}
	providerSetting := repo.ProviderSetting{}
	historyInput := []domain.AgentInputMessage{}
	if err := s.store.Write(func(state *repo.State) error {
		for id, c := range state.Chats {
			if c.SessionID == req.SessionID && c.UserID == req.UserID && c.Channel == req.Channel {
				chatID = id
				break
			}
		}
		if chatID == "" {
			chatID = newID("chat")
			now := nowISO()
			state.Chats[chatID] = domain.ChatSpec{
				ID: chatID, Name: "New Chat", SessionID: req.SessionID, UserID: req.UserID, Channel: req.Channel,
				Meta: map[string]interface{}{}, CreatedAt: now, UpdatedAt: now,
			}
		}
		if len(cronChatMeta) > 0 {
			chat := state.Chats[chatID]
			if chat.Meta == nil {
				chat.Meta = map[string]interface{}{}
			}
			for key, value := range cronChatMeta {
				chat.Meta[key] = value
			}
			state.Chats[chatID] = chat
		}
		for _, input := range req.Input {
			state.Histories[chatID] = append(state.Histories[chatID], domain.RuntimeMessage{
				ID:      newID("msg"),
				Role:    input.Role,
				Type:    input.Type,
				Content: toRuntimeContents(input.Content),
			})
		}
		historyInput = runtimeHistoryToAgentInputMessages(state.Histories[chatID])
		activeLLM = state.ActiveLLM
		activeLLM.ProviderID = normalizeProviderID(activeLLM.ProviderID)
		providerSetting = getProviderSettingByID(state, activeLLM.ProviderID)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	requestedToolCall, hasToolCall, err := parseToolCall(req.BizParams, rawRequest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_tool_input", err.Error(), nil)
		return
	}

	streaming := req.Stream
	var flusher http.Flusher
	streamStarted := false
	if streaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		var ok bool
		flusher, ok = w.(http.Flusher)
		if !ok {
			writeErr(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported", nil)
			return
		}
	}

	streamFail := func(status int, code, message string, details interface{}) {
		if !streaming || !streamStarted {
			writeErr(w, status, code, message, details)
			return
		}
		meta := map[string]interface{}{
			"code":    code,
			"message": message,
		}
		if details != nil {
			meta["details"] = details
		}
		payload, _ := json.Marshal(domain.AgentEvent{
			Type: "error",
			Meta: meta,
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}

	reply := ""
	events := make([]domain.AgentEvent, 0, 12)
	emitEvent := func(evt domain.AgentEvent) {
		if !streaming {
			return
		}
		payload, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		streamStarted = true
	}
	effectiveInput := []domain.AgentInputMessage{}
	generateConfig := runner.GenerateConfig{}
	if !hasToolCall {
		if activeLLM.ProviderID == "" || strings.TrimSpace(activeLLM.Model) == "" {
			generateConfig = runner.GenerateConfig{
				ProviderID: runner.ProviderDemo,
				Model:      "demo-chat",
				AdapterID:  provider.AdapterDemo,
			}
		} else {
			if !providerEnabled(providerSetting) {
				streamFail(http.StatusBadRequest, "provider_disabled", "active provider is disabled", nil)
				return
			}
			resolvedModel, ok := provider.ResolveModelID(activeLLM.ProviderID, activeLLM.Model, providerSetting.ModelAliases)
			if !ok {
				streamFail(http.StatusBadRequest, "model_not_found", "active model is not available for provider", nil)
				return
			}
			activeLLM.Model = resolvedModel
			generateConfig = runner.GenerateConfig{
				ProviderID: activeLLM.ProviderID,
				Model:      activeLLM.Model,
				APIKey:     resolveProviderAPIKey(activeLLM.ProviderID, providerSetting),
				BaseURL:    resolveProviderBaseURL(activeLLM.ProviderID, providerSetting),
				AdapterID:  provider.ResolveAdapter(activeLLM.ProviderID),
				Headers:    sanitizeStringMap(providerSetting.Headers),
				TimeoutMS:  providerSetting.TimeoutMS,
			}
		}
		if len(historyInput) > 0 {
			effectiveInput = prependSystemLayers(historyInput, systemLayers)
		} else {
			effectiveInput = prependSystemLayers(req.Input, systemLayers)
		}
	}

	processResult, processErr := s.getAgentService().Process(
		r.Context(),
		agentservice.ProcessParams{
			Request: req,
			RequestedToolCall: agentservice.ToolCall{
				Name:  requestedToolCall.Name,
				Input: requestedToolCall.Input,
			},
			HasToolCall:    hasToolCall,
			Streaming:      streaming,
			ReplyChunkSize: replyChunkSizeDefault,
			GenerateConfig: generateConfig,
			EffectiveInput: effectiveInput,
		},
		emitEvent,
	)
	if processErr != nil {
		streamFail(processErr.Status, processErr.Code, processErr.Message, processErr.Details)
		return
	}
	reply = processResult.Reply
	events = processResult.Events

	assistant := domain.RuntimeMessage{
		ID:      newID("msg"),
		Role:    "assistant",
		Type:    "message",
		Content: []domain.RuntimeContent{{Type: "text", Text: reply}},
	}
	if metadata := buildAssistantMessageMetadata(events); len(metadata) > 0 {
		assistant.Metadata = metadata
	}

	_ = s.store.Write(func(state *repo.State) error {
		state.Histories[chatID] = append(state.Histories[chatID], assistant)
		chat := state.Chats[chatID]
		chat.UpdatedAt = nowISO()
		if chat.Name == "New Chat" && len(req.Input) > 0 && len(req.Input[0].Content) > 0 {
			first := strings.TrimSpace(req.Input[0].Content[0].Text)
			if first != "" {
				if len([]rune(first)) > 20 {
					chat.Name = string([]rune(first)[:20])
				} else {
					chat.Name = first
				}
			}
		}
		state.Chats[chatID] = chat
		return nil
	})

	dispatchCfg := mergeChannelDispatchConfig(channelName, channelCfg, req.BizParams)
	if err := channelPlugin.SendText(r.Context(), req.UserID, req.SessionID, reply, dispatchCfg); err != nil {
		status, code, message := mapChannelError(&channelError{
			Code:    "channel_dispatch_failed",
			Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
			Err:     err,
		})
		streamFail(status, code, message, nil)
		return
	}

	if !streaming {
		writeJSON(w, http.StatusOK, domain.AgentProcessResponse{
			Reply:  reply,
			Events: events,
		})
		return
	}

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func isContextResetCommand(input []domain.AgentInputMessage) bool {
	for _, msg := range input {
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			continue
		}
		for _, part := range msg.Content {
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			return strings.EqualFold(text, contextResetCommand)
		}
	}
	return false
}

func (s *Server) clearChatContext(sessionID, userID, channel string) error {
	return s.store.Write(func(state *repo.State) error {
		for chatID, spec := range state.Chats {
			if spec.SessionID != sessionID || spec.UserID != userID || spec.Channel != channel {
				continue
			}
			delete(state.Chats, chatID)
			delete(state.Histories, chatID)
		}
		return nil
	})
}

func writeImmediateAgentResponse(w http.ResponseWriter, streaming bool, reply string) {
	if !streaming {
		writeJSON(w, http.StatusOK, domain.AgentProcessResponse{
			Reply: reply,
			Events: []domain.AgentEvent{
				{Type: "step_started", Step: 1},
				{Type: "assistant_delta", Step: 1, Delta: reply},
				{Type: "completed", Step: 1, Reply: reply},
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported", nil)
		return
	}

	stepStartedPayload, _ := json.Marshal(domain.AgentEvent{
		Type: "step_started",
		Step: 1,
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", stepStartedPayload)
	flusher.Flush()

	for _, chunk := range splitReplyChunks(reply, replyChunkSizeDefault) {
		deltaPayload, _ := json.Marshal(domain.AgentEvent{
			Type:  "assistant_delta",
			Step:  1,
			Delta: chunk,
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", deltaPayload)
		flusher.Flush()
	}

	completedPayload, _ := json.Marshal(domain.AgentEvent{
		Type:  "completed",
		Step:  1,
		Reply: reply,
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", completedPayload)
	flusher.Flush()

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func resolveProcessRequestChannel(r *http.Request, requestedChannel string) string {
	if isQQInboundRequest(r) {
		return qqChannelName
	}
	if requested := strings.ToLower(strings.TrimSpace(requestedChannel)); requested != "" {
		return requested
	}
	return defaultProcessChannel
}

func isQQInboundRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.URL.Path)), qqInboundPath)
}

type qqInboundEvent struct {
	Text       string
	UserID     string
	SessionID  string
	TargetType string
	TargetID   string
	MessageID  string
}

func parseQQInboundEvent(body []byte) (qqInboundEvent, error) {
	raw := map[string]interface{}{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return qqInboundEvent{}, errors.New("invalid request body")
	}

	payload := raw
	if nested, ok := qqMap(raw["d"]); ok {
		payload = nested
	} else if nested, ok := qqMap(raw["data"]); ok {
		payload = nested
	}

	eventName := strings.ToUpper(qqFirst(
		qqString(raw["event"]),
		qqString(raw["event_type"]),
		qqString(raw["type"]),
		qqString(raw["t"]),
	))
	targetType, _ := normalizeQQTargetTypeAlias(strings.ToLower(eventName))
	if targetType == "" {
		targetType, _ = normalizeQQTargetTypeAlias(qqFirst(
			qqString(payload["message_type"]),
			qqString(payload["target_type"]),
		))
	}

	switch eventName {
	case "C2C_MESSAGE_CREATE":
		targetType = "c2c"
	case "GROUP_AT_MESSAGE_CREATE":
		targetType = "group"
	case "AT_MESSAGE_CREATE", "DIRECT_MESSAGE_CREATE":
		targetType = "guild"
	}
	if targetType == "" {
		return qqInboundEvent{}, errors.New("unsupported qq event type")
	}

	author, _ := qqMap(payload["author"])
	sender, _ := qqMap(payload["sender"])
	text := strings.TrimSpace(qqFirst(qqString(payload["content"]), qqString(payload["text"])))
	if text == "" {
		return qqInboundEvent{}, nil
	}

	event := qqInboundEvent{
		Text:      text,
		MessageID: strings.TrimSpace(qqString(payload["id"])),
	}

	switch targetType {
	case "c2c":
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["user_openid"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		targetID := strings.TrimSpace(qqFirst(
			qqString(payload["target_id"]),
			senderID,
		))
		if targetID == "" {
			return qqInboundEvent{}, errors.New("qq c2c event missing sender id")
		}
		userID := strings.TrimSpace(qqFirst(senderID, targetID))
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:c2c:%s", targetID),
		))
		event.UserID = userID
		event.SessionID = sessionID
		event.TargetType = "c2c"
		event.TargetID = targetID
	case "group":
		groupID := strings.TrimSpace(qqFirst(
			qqString(payload["group_openid"]),
			qqString(payload["target_id"]),
			qqString(payload["group_id"]),
		))
		if groupID == "" {
			return qqInboundEvent{}, errors.New("qq group event missing group_openid")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["member_openid"]),
			qqString(author["user_openid"]),
			qqString(author["id"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = groupID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:group:%s:%s", groupID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "group"
		event.TargetID = groupID
	case "guild":
		channelID := strings.TrimSpace(qqFirst(
			qqString(payload["channel_id"]),
			qqString(payload["target_id"]),
		))
		if channelID == "" {
			return qqInboundEvent{}, errors.New("qq guild event missing channel_id")
		}
		senderID := strings.TrimSpace(qqFirst(
			qqString(author["id"]),
			qqString(author["username"]),
			qqString(sender["id"]),
			qqString(payload["user_id"]),
		))
		if senderID == "" {
			senderID = channelID
		}
		sessionID := strings.TrimSpace(qqFirst(
			qqString(payload["session_id"]),
			fmt.Sprintf("qq:guild:%s:%s", channelID, senderID),
		))
		event.UserID = senderID
		event.SessionID = sessionID
		event.TargetType = "guild"
		event.TargetID = channelID
	}

	if event.UserID == "" || event.SessionID == "" || event.TargetID == "" {
		return qqInboundEvent{}, errors.New("qq inbound event missing required fields")
	}
	return event, nil
}

func mergeChannelDispatchConfig(channelName string, cfg map[string]interface{}, bizParams map[string]interface{}) map[string]interface{} {
	if channelName != "qq" || len(bizParams) == 0 {
		return cfg
	}
	raw, ok := bizParams["channel"]
	if !ok || raw == nil {
		return cfg
	}
	body, ok := raw.(map[string]interface{})
	if !ok {
		return cfg
	}
	merged := cloneChannelConfig(cfg)
	updated := false

	if canonical, ok := normalizeQQTargetTypeAlias(qqString(body["target_type"])); ok {
		merged["target_type"] = canonical
		updated = true
	}
	if targetID := strings.TrimSpace(qqString(body["target_id"])); targetID != "" {
		merged["target_id"] = targetID
		updated = true
	}
	if msgID := strings.TrimSpace(qqString(body["msg_id"])); msgID != "" {
		merged["msg_id"] = msgID
		updated = true
	}
	if botPrefix := qqString(body["bot_prefix"]); strings.TrimSpace(botPrefix) != "" {
		merged["bot_prefix"] = botPrefix
		updated = true
	}
	if !updated {
		return cfg
	}
	return merged
}

func cronChatMetaFromBizParams(bizParams map[string]interface{}) map[string]interface{} {
	if len(bizParams) == 0 {
		return nil
	}
	raw, ok := bizParams["cron"]
	if !ok || raw == nil {
		return nil
	}
	cronPayload, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(qqString(cronPayload["job_id"]))
	jobName := strings.TrimSpace(qqString(cronPayload["job_name"]))
	if jobID == "" && jobName == "" {
		return nil
	}
	meta := map[string]interface{}{
		"source": "cron",
	}
	if jobID != "" {
		meta["cron_job_id"] = jobID
	}
	if jobName != "" {
		meta["cron_job_name"] = jobName
	}
	return meta
}

func qqMap(raw interface{}) (map[string]interface{}, bool) {
	value, ok := raw.(map[string]interface{})
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func qqString(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

func qqFirst(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeQQTargetTypeAlias(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "c2c", "user", "private":
		return "c2c", true
	case "group":
		return "group", true
	case "guild", "channel", "dm":
		return "guild", true
	default:
		return "", false
	}
}

func toRuntimeContents(in []domain.RuntimeContent) []domain.RuntimeContent {
	if in == nil {
		return []domain.RuntimeContent{}
	}
	return in
}

func runtimeHistoryToAgentInputMessages(history []domain.RuntimeMessage) []domain.AgentInputMessage {
	if len(history) == 0 {
		return []domain.AgentInputMessage{}
	}
	out := make([]domain.AgentInputMessage, 0, len(history))
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}
		msgType := strings.TrimSpace(msg.Type)
		if msgType == "" {
			msgType = "message"
		}
		item := domain.AgentInputMessage{
			Role:    role,
			Type:    msgType,
			Content: append([]domain.RuntimeContent{}, msg.Content...),
		}
		if msg.Metadata != nil {
			data, err := json.Marshal(msg.Metadata)
			if err == nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err == nil {
					item.Metadata = meta
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func splitReplyChunks(text string, chunkSize int) []string {
	if chunkSize <= 0 {
		chunkSize = 12
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)+chunkSize-1)/chunkSize)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func (s *Server) listToolDefinitions() []runner.ToolDefinition {
	if len(s.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		if s.toolDisabled(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]runner.ToolDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, buildToolDefinition(name))
	}
	return out
}

func buildToolDefinition(name string) runner.ToolDefinition {
	switch name {
	case "view":
		return runner.ToolDefinition{
			Name:        "view",
			Description: "Read line ranges for one or multiple files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of view operations; pass one item for single-file view.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
							},
							"required":             []string{"path", "start", "end"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "edit":
		return runner.ToolDefinition{
			Name:        "edit",
			Description: "Replace line ranges for one or multiple files. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of edit operations; pass one item for single-file edit.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Absolute file path on local filesystem.",
								},
								"start": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based starting line number (inclusive).",
								},
								"end": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "1-based ending line number (inclusive).",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "Replacement text for the selected line range.",
								},
							},
							"required":             []string{"path", "start", "end", "content"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "shell":
		return runner.ToolDefinition{
			Name:        "shell",
			Description: "Execute one or multiple shell commands under server security controls. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of shell command operations; pass one item for single command.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"command": map[string]interface{}{
									"type": "string",
								},
								"cwd": map[string]interface{}{
									"type": "string",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"command"},
							"additionalProperties": false,
						},
					},
				},
				"required": []string{"items"},
			},
		}
	case "browser":
		return runner.ToolDefinition{
			Name:        "browser",
			Description: "Delegate browser tasks to local Playwright agent script. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of browser tasks; pass one item for single task.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"task": map[string]interface{}{
									"type":        "string",
									"description": "Natural language task for browser agent.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required":             []string{"task"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	case "search":
		return runner.ToolDefinition{
			Name:        "search",
			Description: "Search the web via configured search APIs. input must be an array.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Array of search requests; pass one item for single query.",
						"minItems":    1,
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"query": map[string]interface{}{
									"type":        "string",
									"description": "Search query text.",
								},
								"provider": map[string]interface{}{
									"type":        "string",
									"description": "Optional provider override: serpapi | tavily | brave.",
								},
								"count": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional max results per query.",
								},
								"timeout_seconds": map[string]interface{}{
									"type":        "integer",
									"minimum":     1,
									"description": "Optional timeout for a single query.",
								},
							},
							"required":             []string{"query"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"items"},
				"additionalProperties": false,
			},
		}
	default:
		return runner.ToolDefinition{
			Name: name,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
		}
	}
}

func toAgentToolCallMetadata(calls []runner.ToolCall) []map[string]interface{} {
	if len(calls) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			continue
		}
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			callID = newID("tool-call")
		}
		args, _ := json.Marshal(safeMap(call.Arguments))
		out = append(out, map[string]interface{}{
			"id":   callID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      call.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}

func cloneAgentInputMessages(input []domain.AgentInputMessage) []domain.AgentInputMessage {
	if len(input) == 0 {
		return []domain.AgentInputMessage{}
	}
	out := make([]domain.AgentInputMessage, 0, len(input))
	for _, item := range input {
		cloned := domain.AgentInputMessage{
			Role:    item.Role,
			Type:    item.Type,
			Content: append([]domain.RuntimeContent{}, item.Content...),
		}
		if item.Metadata != nil {
			data, err := json.Marshal(item.Metadata)
			if err == nil {
				var meta map[string]interface{}
				if err := json.Unmarshal(data, &meta); err == nil {
					cloned.Metadata = meta
				}
			}
		}
		out = append(out, cloned)
	}
	return out
}

func summarizeAgentEventText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 160 {
		return trimmed
	}
	return string(runes[:160]) + "..."
}

func buildAssistantMessageMetadata(events []domain.AgentEvent) map[string]interface{} {
	notices := make([]map[string]interface{}, 0, 2)
	textOrder := 0
	toolOrder := 0
	for idx, evt := range events {
		switch evt.Type {
		case "assistant_delta":
			if textOrder == 0 {
				textOrder = idx + 1
			}
		case "tool_call":
			if evt.ToolCall == nil {
				continue
			}
			if toolOrder == 0 {
				toolOrder = idx + 1
			}
			raw, err := json.Marshal(domain.AgentEvent{
				Type:     "tool_call",
				Step:     evt.Step,
				ToolCall: evt.ToolCall,
			})
			if err != nil {
				continue
			}
			notices = append(notices, map[string]interface{}{"raw": string(raw)})
		}
	}
	if len(notices) == 0 {
		return nil
	}
	out := map[string]interface{}{
		"tool_call_notices": notices,
	}
	if textOrder > 0 {
		out["text_order"] = textOrder
	}
	if toolOrder > 0 {
		out["tool_order"] = toolOrder
	}
	return out
}

type channelError struct {
	Code    string
	Message string
	Err     error
}

type toolCall struct {
	Name  string
	Input map[string]interface{}
}

type recoverableProviderToolCall struct {
	ID           string
	Name         string
	RawArguments string
	Input        map[string]interface{}
	Feedback     string
}

type toolError struct {
	Code    string
	Message string
	Err     error
}

func recoverInvalidProviderToolCall(err error, step int) (recoverableProviderToolCall, bool) {
	invalid, ok := runner.InvalidToolCallFromError(err)
	if !ok {
		return recoverableProviderToolCall{}, false
	}

	callID := strings.TrimSpace(invalid.CallID)
	if callID == "" {
		callID = fmt.Sprintf("call_invalid_%d", step)
	}
	callName := strings.TrimSpace(invalid.Name)
	if callName == "" {
		callName = "unknown_tool"
	}
	rawArguments := strings.TrimSpace(invalid.ArgumentsRaw)
	if rawArguments == "" {
		rawArguments = "{}"
	}
	parseErr := "invalid json arguments"
	if invalid.Err != nil {
		parseErr = strings.TrimSpace(invalid.Err.Error())
	}
	parseErr = compactFeedbackField(parseErr, 160)
	input := map[string]interface{}{
		"raw_arguments": rawArguments,
	}
	if parseErr != "" {
		input["parse_error"] = parseErr
	}

	return recoverableProviderToolCall{
		ID:           callID,
		Name:         callName,
		RawArguments: rawArguments,
		Input:        input,
		Feedback:     formatProviderToolArgumentsErrorFeedback(callName, rawArguments, parseErr),
	}, true
}

func (e *channelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *channelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *toolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *toolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func parseToolCall(bizParams map[string]interface{}, rawRequest map[string]interface{}) (toolCall, bool, error) {
	if call, ok, err := parseBizParamsToolCall(bizParams); ok || err != nil {
		return call, ok, err
	}
	return parseShortcutToolCall(rawRequest)
}

func parseBizParamsToolCall(bizParams map[string]interface{}) (toolCall, bool, error) {
	if len(bizParams) == 0 {
		return toolCall{}, false, nil
	}
	raw, ok := bizParams["tool"]
	if !ok || raw == nil {
		return toolCall{}, false, nil
	}
	toolBody, ok := raw.(map[string]interface{})
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool must be an object")
	}
	rawName, ok := toolBody["name"]
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool.name is required")
	}
	name, ok := rawName.(string)
	if !ok {
		return toolCall{}, false, errors.New("biz_params.tool.name must be a string")
	}
	name = normalizeToolName(strings.ToLower(strings.TrimSpace(name)))
	if name == "" {
		return toolCall{}, false, errors.New("biz_params.tool.name cannot be empty")
	}
	rawInput, hasInput := toolBody["input"]
	if !hasInput {
		body := map[string]interface{}{}
		for key, value := range toolBody {
			if key == "name" {
				continue
			}
			body[key] = value
		}
		rawInput = body
	}
	input, err := parseToolPayload(rawInput, "biz_params.tool")
	if err != nil {
		return toolCall{}, false, err
	}
	return toolCall{Name: name, Input: input}, true, nil
}

func parseShortcutToolCall(rawRequest map[string]interface{}) (toolCall, bool, error) {
	if len(rawRequest) == 0 {
		return toolCall{}, false, nil
	}
	shortcuts := []string{"view", "edit", "shell", "browser", "search"}
	matched := make([]string, 0, 1)
	for _, key := range shortcuts {
		if raw, ok := rawRequest[key]; ok && raw != nil {
			matched = append(matched, key)
		}
	}
	if len(matched) == 0 {
		return toolCall{}, false, nil
	}
	if len(matched) > 1 {
		return toolCall{}, false, errors.New("only one shortcut tool key is allowed")
	}
	name := matched[0]
	input, err := parseToolPayload(rawRequest[name], name)
	if err != nil {
		return toolCall{}, false, err
	}
	return toolCall{Name: normalizeToolName(name), Input: input}, true, nil
}

func parseToolPayload(raw interface{}, path string) (map[string]interface{}, error) {
	if raw == nil {
		return map[string]interface{}{}, nil
	}
	switch value := raw.(type) {
	case []interface{}:
		return map[string]interface{}{"items": value}, nil
	case map[string]interface{}:
		if nested, ok := value["input"]; ok {
			return parseToolPayload(nested, path+".input")
		}
		return safeMap(value), nil
	default:
		return nil, fmt.Errorf("%s must be an object or array", path)
	}
}

func normalizeToolName(name string) string {
	switch name {
	case "view_file_lines", "view_file_lins", "view_file":
		return "view"
	case "edit_file_lines", "edit_file_lins", "edit_file":
		return "edit"
	case "web_browser", "browser_use", "browser_tool":
		return "browser"
	case "web_search", "search_api", "search_tool":
		return "search"
	default:
		return name
	}
}

func (s *Server) executeToolCall(call toolCall) (string, error) {
	if s.toolDisabled(call.Name) {
		return "", &toolError{
			Code:    "tool_disabled",
			Message: fmt.Sprintf("tool %q is disabled by server config", call.Name),
		}
	}
	plug, ok := s.tools[call.Name]
	if !ok {
		return "", &toolError{
			Code:    "tool_not_supported",
			Message: fmt.Sprintf("tool %q is not supported", call.Name),
		}
	}

	result, err := plug.Invoke(call.Input)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invoke_failed",
			Message: fmt.Sprintf("tool %q invocation failed", call.Name),
			Err:     err,
		}
	}

	if text, ok := result["text"].(string); ok && strings.TrimSpace(text) != "" {
		return text, nil
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return "", &toolError{
			Code:    "tool_invalid_result",
			Message: fmt.Sprintf("tool %q returned invalid result", call.Name),
			Err:     err,
		}
	}
	return string(encoded), nil
}

func (s *Server) resolveChannel(name string) (plugin.ChannelPlugin, map[string]interface{}, string, error) {
	channelName := strings.ToLower(strings.TrimSpace(name))
	if channelName == "" {
		return nil, nil, "", &channelError{Code: "invalid_channel", Message: "channel is required"}
	}
	plug, ok := s.channels[channelName]
	if !ok {
		return nil, nil, "", &channelError{
			Code:    "channel_not_supported",
			Message: fmt.Sprintf("channel %q is not supported", channelName),
		}
	}

	cfg := map[string]interface{}{}
	s.store.Read(func(st *repo.State) {
		if st.Channels == nil {
			return
		}
		raw := st.Channels[channelName]
		cfg = cloneChannelConfig(raw)
	})

	if !channelEnabled(channelName, cfg) {
		return nil, nil, "", &channelError{
			Code:    "channel_disabled",
			Message: fmt.Sprintf("channel %q is disabled", channelName),
		}
	}
	return plug, cfg, channelName, nil
}

func channelEnabled(name string, cfg map[string]interface{}) bool {
	if raw, ok := cfg["enabled"]; ok {
		return parseBool(raw)
	}
	return name == "console"
}

func parseBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

func cloneChannelConfig(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		out := map[string]interface{}{}
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		fallback := map[string]interface{}{}
		for key, value := range in {
			fallback[key] = value
		}
		return fallback
	}
	return out
}

func mapChannelError(err error) (status int, code string, message string) {
	var chErr *channelError
	if errors.As(err, &chErr) {
		switch chErr.Code {
		case "invalid_channel", "channel_not_supported", "channel_disabled":
			return http.StatusBadRequest, chErr.Code, chErr.Message
		case "channel_dispatch_failed":
			return http.StatusBadGateway, chErr.Code, chErr.Message
		default:
			return http.StatusBadGateway, "channel_dispatch_failed", "channel dispatch failed"
		}
	}
	return http.StatusBadGateway, "channel_dispatch_failed", "channel dispatch failed"
}
