package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	cronv3 "github.com/robfig/cron/v3"

	"nextai/apps/gateway/internal/channel"
	"nextai/apps/gateway/internal/config"
	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/observability"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
)

const version = "0.1.0"

const (
	cronTickInterval = time.Second

	cronStatusPaused    = "paused"
	cronStatusResumed   = "resumed"
	cronStatusRunning   = "running"
	cronStatusSucceeded = "succeeded"
	cronStatusFailed    = "failed"

	cronLeaseDirName = "cron-leases"

	aiToolsGuideRelativePath         = "docs/AI/AGENTS.md"
	aiToolsGuideLegacyRelativePath   = "docs/AI/ai-tools.md"
	aiToolsGuideLegacyV0RelativePath = "docs/ai-tools.md"
	aiToolsGuidePathEnv              = "NEXTAI_AI_TOOLS_GUIDE_PATH"
	disabledToolsEnv                 = "NEXTAI_DISABLED_TOOLS"
	enableBrowserToolEnv             = "NEXTAI_ENABLE_BROWSER_TOOL"
	browserToolAgentDirEnv           = "NEXTAI_BROWSER_AGENT_DIR"
	enableSearchToolEnv              = "NEXTAI_ENABLE_SEARCH_TOOL"
	disableQQInboundSupervisorEnv    = "NEXTAI_DISABLE_QQ_INBOUND_SUPERVISOR"

	replyChunkSizeDefault = 12
	contextResetCommand   = "/new"
	contextResetReply     = "上下文已清理，已开始新会话。"

	defaultProcessChannel = "console"
	qqChannelName         = "qq"
	channelSourceHeader   = "X-NextAI-Source"
	qqInboundPath         = "/channels/qq/inbound"
	defaultWebDirName     = "web"
)

var errCronJobNotFound = errors.New("cron_job_not_found")
var errCronMaxConcurrencyReached = errors.New("cron_max_concurrency_reached")

type Server struct {
	cfg      config.Config
	store    *repo.Store
	runner   *runner.Runner
	channels map[string]plugin.ChannelPlugin
	tools    map[string]plugin.ToolPlugin

	disabledTools map[string]struct{}
	qqInboundMu   sync.RWMutex
	qqInbound     qqInboundRuntimeState

	cronStop chan struct{}
	cronDone chan struct{}
	cronWG   sync.WaitGroup

	cronTaskExecutor func(context.Context, domain.CronJobSpec) error
	closeOnce        sync.Once
}

func NewServer(cfg config.Config) (*Server, error) {
	store, err := repo.NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:      cfg,
		store:    store,
		runner:   runner.New(),
		channels: map[string]plugin.ChannelPlugin{},
		tools:    map[string]plugin.ToolPlugin{},
		disabledTools: parseDisabledTools(
			os.Getenv(disabledToolsEnv),
		),
		cronStop: make(chan struct{}),
		cronDone: make(chan struct{}),
	}
	srv.registerChannelPlugin(channel.NewConsoleChannel())
	srv.registerChannelPlugin(channel.NewWebhookChannel())
	srv.registerChannelPlugin(channel.NewQQChannel())
	srv.registerToolPlugin(plugin.NewShellTool())
	srv.registerToolPlugin(plugin.NewViewFileLinesTool(""))
	srv.registerToolPlugin(plugin.NewEditFileLinesTool(""))
	if parseBool(os.Getenv(enableBrowserToolEnv)) {
		browserTool, toolErr := plugin.NewBrowserTool(strings.TrimSpace(os.Getenv(browserToolAgentDirEnv)))
		if toolErr != nil {
			return nil, fmt.Errorf("init browser tool failed: %w", toolErr)
		}
		srv.registerToolPlugin(browserTool)
	}
	if parseBool(os.Getenv(enableSearchToolEnv)) {
		searchTool, toolErr := plugin.NewSearchToolFromEnv()
		if toolErr != nil {
			return nil, fmt.Errorf("init search tool failed: %w", toolErr)
		}
		srv.registerToolPlugin(searchTool)
	}
	srv.startCronScheduler()
	if !parseBool(os.Getenv(disableQQInboundSupervisorEnv)) {
		srv.startQQInboundSupervisor()
	}
	return srv, nil
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.cronStop)
		<-s.cronDone
		s.cronWG.Wait()
	})
}

func (s *Server) registerChannelPlugin(ch plugin.ChannelPlugin) {
	if ch == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(ch.Name()))
	if name == "" {
		return
	}
	s.channels[name] = ch
}

func (s *Server) registerToolPlugin(tp plugin.ToolPlugin) {
	if tp == nil {
		return
	}
	name := strings.ToLower(strings.TrimSpace(tp.Name()))
	if name == "" {
		return
	}
	s.tools[name] = tp
}

func parseDisabledTools(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func (s *Server) toolDisabled(name string) bool {
	if s == nil {
		return false
	}
	if len(s.disabledTools) == 0 {
		return false
	}
	_, ok := s.disabledTools[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(observability.RequestID)
	r.Use(observability.Logging)
	r.Use(cors)

	r.Get("/version", s.handleVersion)
	r.Get("/healthz", s.handleHealthz)

	r.Group(func(api chi.Router) {
		api.Use(observability.APIKey(s.cfg.APIKey))

		api.Route("/chats", func(r chi.Router) {
			r.Get("/", s.listChats)
			r.Post("/", s.createChat)
			r.Post("/batch-delete", s.batchDeleteChats)
			r.Get("/{chat_id}", s.getChat)
			r.Put("/{chat_id}", s.updateChat)
			r.Delete("/{chat_id}", s.deleteChat)
		})

		api.Post("/agent/process", s.processAgent)
		api.Post("/channels/qq/inbound", s.processQQInbound)
		api.Get("/channels/qq/state", s.getQQInboundState)

		api.Route("/cron", func(r chi.Router) {
			r.Get("/jobs", s.listCronJobs)
			r.Post("/jobs", s.createCronJob)
			r.Get("/jobs/{job_id}", s.getCronJob)
			r.Put("/jobs/{job_id}", s.updateCronJob)
			r.Delete("/jobs/{job_id}", s.deleteCronJob)
			r.Post("/jobs/{job_id}/pause", s.pauseCronJob)
			r.Post("/jobs/{job_id}/resume", s.resumeCronJob)
			r.Post("/jobs/{job_id}/run", s.runCronJob)
			r.Get("/jobs/{job_id}/state", s.getCronJobState)
		})

		api.Route("/models", func(r chi.Router) {
			r.Get("/", s.listProviders)
			r.Get("/catalog", s.getModelCatalog)
			r.Put("/{provider_id}/config", s.configureProvider)
			r.Delete("/{provider_id}", s.deleteProvider)
			r.Get("/active", s.getActiveModels)
			r.Put("/active", s.setActiveModels)
		})

		api.Route("/envs", func(r chi.Router) {
			r.Get("/", s.listEnvs)
			r.Put("/", s.putEnvs)
			r.Delete("/{key}", s.deleteEnv)
		})

		api.Route("/skills", func(r chi.Router) {
			r.Get("/", s.listSkills)
			r.Get("/available", s.listAvailableSkills)
			r.Post("/batch-disable", s.batchDisableSkills)
			r.Post("/batch-enable", s.batchEnableSkills)
			r.Post("/", s.createSkill)
			r.Post("/{skill_name}/disable", s.disableSkill)
			r.Post("/{skill_name}/enable", s.enableSkill)
			r.Delete("/{skill_name}", s.deleteSkill)
			r.Get("/{skill_name}/files/{source}/{file_path}", s.loadSkillFile)
		})

		api.Route("/workspace", func(r chi.Router) {
			r.Get("/files", s.listWorkspaceFiles)
			r.Get("/files/*", s.getWorkspaceFile)
			r.Put("/files/*", s.putWorkspaceFile)
			r.Delete("/files/*", s.deleteWorkspaceFile)
			r.Get("/export", s.exportWorkspace)
			r.Post("/import", s.importWorkspace)
		})

		api.Route("/config", func(r chi.Router) {
			r.Get("/channels", s.listChannels)
			r.Get("/channels/types", s.listChannelTypes)
			r.Put("/channels", s.putChannels)
			r.Get("/channels/{channel_name}", s.getChannel)
			r.Put("/channels/{channel_name}", s.putChannel)
		})
	})

	if webHandler := webStaticHandler(s.cfg.WebDir); webHandler != nil {
		r.Get("/*", webHandler)
	}

	return r
}

func (s *Server) startCronScheduler() {
	go func() {
		defer close(s.cronDone)
		s.cronSchedulerTick()

		ticker := time.NewTicker(cronTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.cronSchedulerTick()
			case <-s.cronStop:
				return
			}
		}
	}()
}

type dueCronExecution struct {
	JobID string
}

func (s *Server) cronSchedulerTick() {
	now := time.Now().UTC()
	stateUpdates := map[string]domain.CronJobState{}
	dueJobs := make([]dueCronExecution, 0)
	s.store.Read(func(st *repo.State) {
		for id, job := range st.CronJobs {
			current := st.CronStates[id]
			next := normalizeCronPausedState(current)
			if !cronJobSchedulable(job, next) {
				next.NextRunAt = nil
				if !cronStateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRunAt, dueAt, err := resolveCronNextRunAt(job, next.NextRunAt, now)
			if err != nil {
				msg := err.Error()
				next.LastError = &msg
				next.NextRunAt = nil
				if !cronStateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRun := nextRunAt.Format(time.RFC3339)
			next.NextRunAt = &nextRun
			next.LastError = nil
			if dueAt != nil && cronMisfireExceeded(dueAt, cronRuntimeSpec(job), now) {
				failed := cronStatusFailed
				msg := fmt.Sprintf("misfire skipped: scheduled_at=%s", dueAt.Format(time.RFC3339))
				next.LastStatus = &failed
				next.LastError = &msg
				dueAt = nil
			}
			if !cronStateEqual(current, next) {
				stateUpdates[id] = next
			}
			if dueAt != nil {
				dueJobs = append(dueJobs, dueCronExecution{JobID: id})
			}
		}
	})
	if len(stateUpdates) > 0 {
		if err := s.store.Write(func(st *repo.State) error {
			for id, next := range stateUpdates {
				if _, ok := st.CronJobs[id]; !ok {
					continue
				}
				st.CronStates[id] = next
			}
			return nil
		}); err != nil {
			log.Printf("cron scheduler tick failed: %v", err)
			return
		}
	}

	for _, due := range dueJobs {
		s.cronWG.Add(1)
		go func(jobID string) {
			defer s.cronWG.Done()
			if err := s.executeCronJob(jobID); err != nil &&
				!errors.Is(err, errCronJobNotFound) &&
				!errors.Is(err, errCronMaxConcurrencyReached) {
				log.Printf("cron job %s execute failed: %v", jobID, err)
			}
		}(due.JobID)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-Id,X-NextAI-Source")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func webStaticHandler(configuredWebDir string) http.HandlerFunc {
	webDir, ok := resolveWebDir(configuredWebDir)
	if !ok {
		return nil
	}
	fileServer := http.FileServer(http.Dir(webDir))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		relPath := strings.TrimPrefix(cleanPath, "/")
		if relPath != "" {
			targetPath := filepath.Join(webDir, filepath.FromSlash(relPath))
			if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		indexPath := filepath.Join(webDir, "index.html")
		if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, indexPath)
	}
}

func resolveWebDir(configuredWebDir string) (string, bool) {
	raw := strings.TrimSpace(configuredWebDir)
	if raw == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		raw = filepath.Join(cwd, defaultWebDirName)
	}
	if !filepath.IsAbs(raw) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false
		}
		raw = filepath.Join(cwd, raw)
	}
	info, err := os.Stat(raw)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return raw, true
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	channel := r.URL.Query().Get("channel")
	out := make([]domain.ChatSpec, 0)
	s.store.Read(func(state *repo.State) {
		for _, v := range state.Chats {
			if userID != "" && v.UserID != userID {
				continue
			}
			if channel != "" && v.Channel != channel {
				continue
			}
			out = append(out, v)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request) {
	var req domain.ChatSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID == "" {
		req.ID = newID("chat")
	}
	if req.Name == "" {
		req.Name = "New Chat"
	}
	if req.SessionID == "" || req.UserID == "" || req.Channel == "" {
		writeErr(w, http.StatusBadRequest, "invalid_chat", "session_id, user_id, channel are required", nil)
		return
	}
	if req.Meta == nil {
		req.Meta = map[string]interface{}{}
	}
	now := nowISO()
	req.CreatedAt = now
	req.UpdatedAt = now
	if err := s.store.Write(func(state *repo.State) error {
		state.Chats[req.ID] = req
		if _, ok := state.Histories[req.ID]; !ok {
			state.Histories[req.ID] = []domain.RuntimeMessage{}
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) batchDeleteChats(w http.ResponseWriter, r *http.Request) {
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(state *repo.State) error {
		for _, id := range ids {
			delete(state.Chats, id)
			delete(state.Histories, id)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	var history []domain.RuntimeMessage
	found := false
	s.store.Read(func(state *repo.State) {
		if _, ok := state.Chats[id]; ok {
			history = state.Histories[id]
			found = true
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "chat not found", map[string]string{"chat_id": id})
		return
	}
	writeJSON(w, http.StatusOK, domain.ChatHistory{Messages: history})
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	var req domain.ChatSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		writeErr(w, http.StatusBadRequest, "chat_id_mismatch", "chat_id mismatch", nil)
		return
	}
	if err := s.store.Write(func(state *repo.State) error {
		old, ok := state.Chats[id]
		if !ok {
			return errors.New("not_found")
		}
		req.CreatedAt = old.CreatedAt
		req.UpdatedAt = nowISO()
		state.Chats[id] = req
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "chat not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "chat_id")
	deleted := false
	if err := s.store.Write(func(state *repo.State) error {
		if _, ok := state.Chats[id]; ok {
			deleted = true
			delete(state.Chats, id)
			delete(state.Histories, id)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !deleted {
		writeErr(w, http.StatusNotFound, "not_found", "chat not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) processAgent(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	s.processAgentWithBody(w, r, bodyBytes)
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

	aiToolsGuide, err := loadAIToolsGuide()
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
	appendEvent := func(evt domain.AgentEvent) {
		events = append(events, evt)
		if !streaming {
			return
		}
		payload, _ := json.Marshal(evt)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		streamStarted = true
	}
	replyChunkSize := replyChunkSizeDefault
	appendReplyDeltas := func(step int, text string) {
		for _, chunk := range splitReplyChunks(text, replyChunkSize) {
			appendEvent(domain.AgentEvent{
				Type:  "assistant_delta",
				Step:  step,
				Delta: chunk,
			})
		}
	}

	if hasToolCall {
		step := 1
		appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
		appendEvent(domain.AgentEvent{
			Type: "tool_call",
			Step: step,
			ToolCall: &domain.AgentToolCallPayload{
				Name:  requestedToolCall.Name,
				Input: safeMap(requestedToolCall.Input),
			},
		})
		reply, err = s.executeToolCall(requestedToolCall)
		if err != nil {
			status, code, message := mapToolError(err)
			streamFail(status, code, message, nil)
			return
		}
		appendEvent(domain.AgentEvent{
			Type: "tool_result",
			Step: step,
			ToolResult: &domain.AgentToolResultPayload{
				Name:    requestedToolCall.Name,
				OK:      true,
				Summary: summarizeAgentEventText(reply),
			},
		})
		appendReplyDeltas(step, reply)
		appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
	} else {
		generateConfig := runner.GenerateConfig{}
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

		effectiveReq := req
		if len(historyInput) > 0 {
			effectiveReq.Input = prependAIToolsGuide(historyInput, aiToolsGuide)
		} else {
			effectiveReq.Input = prependAIToolsGuide(req.Input, aiToolsGuide)
		}
		workflowInput := cloneAgentInputMessages(effectiveReq.Input)
		step := 1

		for {
			appendEvent(domain.AgentEvent{Type: "step_started", Step: step})
			turnReq := effectiveReq
			turnReq.Input = workflowInput
			stepHadStreamingDelta := false
			turn, runErr := runner.TurnResult{}, error(nil)
			if streaming {
				turn, runErr = s.runner.GenerateTurnStream(r.Context(), turnReq, generateConfig, s.listToolDefinitions(), func(delta string) {
					if delta == "" {
						return
					}
					stepHadStreamingDelta = true
					appendEvent(domain.AgentEvent{
						Type:  "assistant_delta",
						Step:  step,
						Delta: delta,
					})
				})
			} else {
				turn, runErr = s.runner.GenerateTurn(r.Context(), turnReq, generateConfig, s.listToolDefinitions())
			}
			if runErr != nil {
				if recoveredCall, recovered := recoverInvalidProviderToolCall(runErr, step); recovered {
					appendEvent(domain.AgentEvent{
						Type: "tool_call",
						Step: step,
						ToolCall: &domain.AgentToolCallPayload{
							Name:  recoveredCall.Name,
							Input: safeMap(recoveredCall.Input),
						},
					})
					appendEvent(domain.AgentEvent{
						Type: "tool_result",
						Step: step,
						ToolResult: &domain.AgentToolResultPayload{
							Name:    recoveredCall.Name,
							OK:      false,
							Summary: summarizeAgentEventText(recoveredCall.Feedback),
						},
					})
					workflowInput = append(workflowInput,
						domain.AgentInputMessage{
							Role:    "assistant",
							Type:    "message",
							Content: []domain.RuntimeContent{},
							Metadata: map[string]interface{}{
								"tool_calls": []map[string]interface{}{
									{
										"id":   recoveredCall.ID,
										"type": "function",
										"function": map[string]interface{}{
											"name":      recoveredCall.Name,
											"arguments": recoveredCall.RawArguments,
										},
									},
								},
							},
						},
						domain.AgentInputMessage{
							Role:    "tool",
							Type:    "message",
							Content: []domain.RuntimeContent{{Type: "text", Text: recoveredCall.Feedback}},
							Metadata: map[string]interface{}{
								"tool_call_id": recoveredCall.ID,
								"name":         recoveredCall.Name,
							},
						},
					)
					step++
					continue
				}
				status, code, message := mapRunnerError(runErr)
				streamFail(status, code, message, nil)
				return
			}
			if len(turn.ToolCalls) == 0 {
				reply = strings.TrimSpace(turn.Text)
				if reply == "" {
					reply = "(empty reply)"
				}
				if !streaming || !stepHadStreamingDelta {
					appendReplyDeltas(step, reply)
				}
				appendEvent(domain.AgentEvent{Type: "completed", Step: step, Reply: reply})
				break
			}

			assistantMessage := domain.AgentInputMessage{
				Role:     "assistant",
				Type:     "message",
				Content:  []domain.RuntimeContent{},
				Metadata: map[string]interface{}{"tool_calls": toAgentToolCallMetadata(turn.ToolCalls)},
			}
			if text := strings.TrimSpace(turn.Text); text != "" {
				assistantMessage.Content = []domain.RuntimeContent{{Type: "text", Text: text}}
			}
			workflowInput = append(workflowInput, assistantMessage)

			for _, call := range turn.ToolCalls {
				appendEvent(domain.AgentEvent{
					Type: "tool_call",
					Step: step,
					ToolCall: &domain.AgentToolCallPayload{
						Name:  call.Name,
						Input: safeMap(call.Arguments),
					},
				})
				toolReply, toolErr := s.executeToolCall(toolCall{Name: call.Name, Input: safeMap(call.Arguments)})
				if toolErr != nil {
					toolReply = formatToolErrorFeedback(toolErr)
					appendEvent(domain.AgentEvent{
						Type: "tool_result",
						Step: step,
						ToolResult: &domain.AgentToolResultPayload{
							Name:    call.Name,
							OK:      false,
							Summary: summarizeAgentEventText(toolReply),
						},
					})
					workflowInput = append(workflowInput, domain.AgentInputMessage{
						Role:    "tool",
						Type:    "message",
						Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
						Metadata: map[string]interface{}{
							"tool_call_id": call.ID,
							"name":         call.Name,
						},
					})
					continue
				}
				appendEvent(domain.AgentEvent{
					Type: "tool_result",
					Step: step,
					ToolResult: &domain.AgentToolResultPayload{
						Name:    call.Name,
						OK:      true,
						Summary: summarizeAgentEventText(toolReply),
					},
				})
				workflowInput = append(workflowInput, domain.AgentInputMessage{
					Role:    "tool",
					Type:    "message",
					Content: []domain.RuntimeContent{{Type: "text", Text: toolReply}},
					Metadata: map[string]interface{}{
						"tool_call_id": call.ID,
						"name":         call.Name,
					},
				})
			}
			step++
		}
	}
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

func (s *Server) listCronJobs(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.CronJobSpec, 0)
	s.store.Read(func(state *repo.State) {
		for _, job := range state.CronJobs {
			out = append(out, job)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createCronJob(w http.ResponseWriter, r *http.Request) {
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "invalid_cron", "id and name are required", nil)
		return
	}
	now := time.Now().UTC()
	if err := s.store.Write(func(state *repo.State) error {
		state.CronJobs[req.ID] = req
		existing := state.CronStates[req.ID]
		state.CronStates[req.ID] = alignCronStateForMutation(req, normalizeCronPausedState(existing), now)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) getCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var spec domain.CronJobSpec
	var state domain.CronJobState
	found := false
	s.store.Read(func(st *repo.State) {
		spec, found = st.CronJobs[id]
		if found {
			state = st.CronStates[id]
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, domain.CronJobView{Spec: spec, State: state})
}

func (s *Server) updateCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	var req domain.CronJobSpec
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.ID != id {
		writeErr(w, http.StatusBadRequest, "job_id_mismatch", "job_id mismatch", nil)
		return
	}
	now := time.Now().UTC()
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errors.New("not_found")
		}
		st.CronJobs[id] = req
		state := normalizeCronPausedState(st.CronStates[id])
		st.CronStates[id] = alignCronStateForMutation(req, state, now)
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) deleteCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; ok {
			delete(st.CronJobs, id)
			delete(st.CronStates, id)
			deleted = true
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) pauseCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusPaused)
}

func (s *Server) resumeCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), cronStatusResumed)
}

func (s *Server) runCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	if err := s.executeCronJob(id); err != nil {
		if errors.Is(err, errCronJobNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		if errors.Is(err, errCronMaxConcurrencyReached) {
			writeErr(w, http.StatusConflict, "cron_busy", "cron job reached max_concurrency", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"started": true})
}

func (s *Server) getCronJobState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	found := false
	var state domain.CronJobState
	s.store.Read(func(st *repo.State) {
		if _, ok := st.CronJobs[id]; ok {
			found = true
			state = st.CronStates[id]
		}
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) updateCronStatus(w http.ResponseWriter, id, status string) {
	now := time.Now().UTC()
	if err := s.store.Write(func(st *repo.State) error {
		job, ok := st.CronJobs[id]
		if !ok {
			return errors.New("not_found")
		}
		state := normalizeCronPausedState(st.CronStates[id])
		switch status {
		case cronStatusPaused:
			state.Paused = true
			state.NextRunAt = nil
		case cronStatusResumed:
			state.Paused = false
			state = alignCronStateForMutation(job, state, now)
		}
		state.LastStatus = &status
		st.CronStates[id] = state
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	key := "paused"
	if status == cronStatusResumed {
		key = "resumed"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) executeCronJob(id string) error {
	var job domain.CronJobSpec
	found := false
	s.store.Read(func(st *repo.State) {
		job, found = st.CronJobs[id]
	})
	if !found {
		return errCronJobNotFound
	}

	runtime := cronRuntimeSpec(job)
	slot, acquired, err := s.tryAcquireCronSlot(id, runtime)
	if err != nil {
		return err
	}
	if !acquired {
		if err := s.markCronExecutionSkipped(id, fmt.Sprintf("max_concurrency limit reached (%d)", runtime.MaxConcurrency)); err != nil {
			return err
		}
		return errCronMaxConcurrencyReached
	}
	defer s.releaseCronSlot(slot)

	startedAt := nowISO()
	running := cronStatusRunning

	if err := s.store.Write(func(st *repo.State) error {
		target, ok := st.CronJobs[id]
		if !ok {
			return errCronJobNotFound
		}
		job = target
		state := normalizeCronPausedState(st.CronStates[id])
		state.LastRunAt = &startedAt
		state.LastStatus = &running
		state.LastError = nil
		st.CronStates[id] = state
		return nil
	}); err != nil {
		return err
	}

	execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(runtime.TimeoutSeconds)*time.Second)
	defer cancel()
	execErr := s.executeCronTask(execCtx, job)
	if errors.Is(execErr, context.DeadlineExceeded) {
		execErr = fmt.Errorf("cron execution timeout after %ds", runtime.TimeoutSeconds)
	}

	finalStatus := cronStatusSucceeded
	var finalErr *string
	if execErr != nil {
		finalStatus = cronStatusFailed
		msg := execErr.Error()
		finalErr = &msg
	}

	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return nil
		}
		state := st.CronStates[id]
		state.LastStatus = &finalStatus
		state.LastError = finalErr
		st.CronStates[id] = state
		return nil
	}); err != nil {
		return err
	}

	return execErr
}

func (s *Server) executeCronTask(ctx context.Context, job domain.CronJobSpec) error {
	if s.cronTaskExecutor != nil {
		return s.cronTaskExecutor(ctx, job)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if job.TaskType == "text" && strings.TrimSpace(job.Text) != "" {
		channelName := strings.ToLower(resolveCronDispatchChannel(job))
		if channelName == qqChannelName {
			return errors.New("cron dispatch channel \"qq\" is inbound-only; use channel \"console\" to persist chat history")
		}
		channelPlugin, channelCfg, resolvedChannelName, err := s.resolveChannel(channelName)
		if err != nil {
			return err
		}
		if resolvedChannelName == "console" {
			return s.executeCronConsoleAgentTask(ctx, job)
		}
		if err := channelPlugin.SendText(ctx, job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, job.Text, channelCfg); err != nil {
			return &channelError{
				Code:    "channel_dispatch_failed",
				Message: fmt.Sprintf("failed to dispatch cron job to channel %q", resolvedChannelName),
				Err:     err,
			}
		}
	}
	return nil
}

func (s *Server) executeCronConsoleAgentTask(ctx context.Context, job domain.CronJobSpec) error {
	sessionID := strings.TrimSpace(job.Dispatch.Target.SessionID)
	userID := strings.TrimSpace(job.Dispatch.Target.UserID)
	if sessionID == "" || userID == "" {
		return errors.New("cron dispatch target requires non-empty session_id and user_id")
	}

	text := strings.TrimSpace(job.Text)
	if text == "" {
		return nil
	}

	agentReq := domain.AgentProcessRequest{
		Input: []domain.AgentInputMessage{
			{
				Role: "user",
				Type: "message",
				Content: []domain.RuntimeContent{
					{Type: "text", Text: text},
				},
			},
		},
		SessionID: sessionID,
		UserID:    userID,
		Channel:   "console",
		Stream:    false,
		BizParams: buildCronBizParams(job),
	}

	body, err := json.Marshal(agentReq)
	if err != nil {
		return fmt.Errorf("cron console agent request marshal failed: %w", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/agent/process", nil).WithContext(ctx)
	s.processAgentWithBody(recorder, request, body)

	status := recorder.Result().StatusCode
	if status >= http.StatusBadRequest {
		return fmt.Errorf("cron console agent execution failed: status=%d body=%s", status, strings.TrimSpace(recorder.Body.String()))
	}

	return nil
}

func resolveCronDispatchChannel(job domain.CronJobSpec) string {
	channelName := strings.TrimSpace(job.Dispatch.Channel)
	if channelName == "" {
		return "console"
	}
	return channelName
}

func buildCronBizParams(job domain.CronJobSpec) map[string]interface{} {
	jobID := strings.TrimSpace(job.ID)
	jobName := strings.TrimSpace(job.Name)
	if jobID == "" && jobName == "" {
		return nil
	}
	cronPayload := map[string]interface{}{}
	if jobID != "" {
		cronPayload["job_id"] = jobID
	}
	if jobName != "" {
		cronPayload["job_name"] = jobName
	}
	return map[string]interface{}{
		"cron": cronPayload,
	}
}

func alignCronStateForMutation(job domain.CronJobSpec, state domain.CronJobState, now time.Time) domain.CronJobState {
	if !cronJobSchedulable(job, state) {
		state.NextRunAt = nil
		return state
	}
	nextRunAt, _, err := resolveCronNextRunAt(job, nil, now)
	if err != nil {
		msg := err.Error()
		state.LastError = &msg
		state.NextRunAt = nil
		return state
	}

	nextRunAtText := nextRunAt.Format(time.RFC3339)
	state.NextRunAt = &nextRunAtText
	state.LastError = nil
	return state
}

func normalizeCronPausedState(state domain.CronJobState) domain.CronJobState {
	if !state.Paused && state.LastStatus != nil && *state.LastStatus == cronStatusPaused {
		state.Paused = true
	}
	return state
}

func cronStateEqual(a, b domain.CronJobState) bool {
	return cronStringPtrEqual(a.NextRunAt, b.NextRunAt) &&
		cronStringPtrEqual(a.LastRunAt, b.LastRunAt) &&
		cronStringPtrEqual(a.LastStatus, b.LastStatus) &&
		cronStringPtrEqual(a.LastError, b.LastError) &&
		a.Paused == b.Paused
}

func cronStringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cronJobSchedulable(job domain.CronJobSpec, state domain.CronJobState) bool {
	return job.Enabled && !state.Paused
}

type cronLeaseSlot struct {
	LeaseID string `json:"lease_id"`
	JobID   string `json:"job_id"`
	Owner   string `json:"owner"`
	Slot    int    `json:"slot"`

	AcquiredAt string `json:"acquired_at"`
	ExpiresAt  string `json:"expires_at"`
}

type cronLeaseHandle struct {
	Path    string
	LeaseID string
}

func (s *Server) tryAcquireCronSlot(jobID string, runtime domain.CronRuntimeSpec) (*cronLeaseHandle, bool, error) {
	maxConcurrency := runtime.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	now := time.Now().UTC()
	ttl := time.Duration(runtime.TimeoutSeconds)*time.Second + 30*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	leaseID := newCronLeaseID()
	dir := filepath.Join(s.cfg.DataDir, cronLeaseDirName, encodeCronJobID(jobID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}

	for slot := 0; slot < maxConcurrency; slot++ {
		path := filepath.Join(dir, fmt.Sprintf("slot-%d.json", slot))
		if err := cleanupExpiredCronLease(path, now); err != nil {
			return nil, false, err
		}

		lease := cronLeaseSlot{
			LeaseID:    leaseID,
			JobID:      jobID,
			Owner:      fmt.Sprintf("pid:%d", os.Getpid()),
			Slot:       slot,
			AcquiredAt: now.Format(time.RFC3339Nano),
			ExpiresAt:  now.Add(ttl).Format(time.RFC3339Nano),
		}
		body, err := json.Marshal(lease)
		if err != nil {
			return nil, false, err
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, false, err
		}

		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			_ = removeIfExists(path)
			return nil, false, err
		}
		if err := f.Close(); err != nil {
			_ = removeIfExists(path)
			return nil, false, err
		}
		return &cronLeaseHandle{Path: path, LeaseID: leaseID}, true, nil
	}
	return nil, false, nil
}

func (s *Server) releaseCronSlot(slot *cronLeaseHandle) {
	if slot == nil || strings.TrimSpace(slot.Path) == "" {
		return
	}

	body, err := os.ReadFile(slot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("release cron lease read failed: path=%s err=%v", slot.Path, err)
		return
	}

	var lease cronLeaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		if rmErr := removeIfExists(slot.Path); rmErr != nil {
			log.Printf("release cron lease cleanup failed: path=%s err=%v", slot.Path, rmErr)
		}
		return
	}
	if lease.LeaseID != slot.LeaseID {
		return
	}
	if err := os.Remove(slot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("release cron lease failed: path=%s err=%v", slot.Path, err)
	}
}

func cleanupExpiredCronLease(path string, now time.Time) error {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var lease cronLeaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		return removeIfExists(path)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lease.ExpiresAt))
	if err != nil {
		return removeIfExists(path)
	}
	if !now.After(expiresAt.UTC()) {
		return nil
	}
	return removeIfExists(path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func encodeCronJobID(jobID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(jobID))
}

func newCronLeaseID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", os.Getpid(), buf)
}

func (s *Server) markCronExecutionSkipped(id, message string) error {
	failed := cronStatusFailed
	return s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errCronJobNotFound
		}
		state := normalizeCronPausedState(st.CronStates[id])
		state.LastStatus = &failed
		state.LastError = &message
		st.CronStates[id] = state
		return nil
	})
}

func cronRuntimeSpec(job domain.CronJobSpec) domain.CronRuntimeSpec {
	out := job.Runtime
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 1
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 30
	}
	if out.MisfireGraceSeconds < 0 {
		out.MisfireGraceSeconds = 0
	}
	return out
}

func cronScheduleType(job domain.CronJobSpec) string {
	scheduleType := strings.ToLower(strings.TrimSpace(job.Schedule.Type))
	if scheduleType == "" {
		return "interval"
	}
	return scheduleType
}

func cronInterval(job domain.CronJobSpec) (time.Duration, error) {
	if cronScheduleType(job) != "interval" {
		return 0, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}

	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return 0, errors.New("schedule.cron is required for interval jobs")
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, errors.New("schedule interval must be greater than 0")
		}
		return time.Duration(secs) * time.Second, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid schedule interval: %q", raw)
	}
	return parsed, nil
}

func resolveCronNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	switch cronScheduleType(job) {
	case "interval":
		interval, err := cronInterval(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveIntervalNextRunAt(current, interval, now)
		return next, dueAt, nil
	case "cron":
		schedule, loc, err := cronExpression(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveExpressionNextRunAt(current, schedule, loc, now)
		return next, dueAt, nil
	default:
		return time.Time{}, nil, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}
}

func cronExpression(job domain.CronJobSpec) (cronv3.Schedule, *time.Location, error) {
	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return nil, nil, errors.New("schedule.cron is required for cron jobs")
	}

	loc := time.UTC
	if tz := strings.TrimSpace(job.Schedule.Timezone); tz != "" {
		nextLoc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid schedule.timezone=%q", job.Schedule.Timezone)
		}
		loc = nextLoc
	}

	parser := cronv3.NewParser(cronv3.SecondOptional | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)
	schedule, err := parser.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return schedule, loc, nil
}

func resolveIntervalNextRunAt(current *string, interval time.Duration, now time.Time) (time.Time, *time.Time) {
	next := now.Add(interval)
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	for !parsed.After(now) {
		parsed = parsed.Add(interval)
	}
	return parsed, &dueAt
}

func resolveExpressionNextRunAt(current *string, schedule cronv3.Schedule, loc *time.Location, now time.Time) (time.Time, *time.Time) {
	nowInLoc := now.In(loc)
	next := schedule.Next(nowInLoc).UTC()
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	cursor := parsed.In(loc)
	for i := 0; i < 2048 && !cursor.After(nowInLoc); i++ {
		nextCursor := schedule.Next(cursor)
		if !nextCursor.After(cursor) {
			return schedule.Next(nowInLoc).UTC(), &dueAt
		}
		cursor = nextCursor
	}
	if !cursor.After(nowInLoc) {
		cursor = schedule.Next(nowInLoc)
	}
	return cursor.UTC(), &dueAt
}

func cronMisfireExceeded(dueAt *time.Time, runtime domain.CronRuntimeSpec, now time.Time) bool {
	if dueAt == nil {
		return false
	}
	if runtime.MisfireGraceSeconds <= 0 {
		return false
	}
	grace := time.Duration(runtime.MisfireGraceSeconds) * time.Second
	return now.Sub(dueAt.UTC()) > grace
}

func (s *Server) listProviders(w http.ResponseWriter, _ *http.Request) {
	providers, _, _ := s.collectProviderCatalog()
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) getModelCatalog(w http.ResponseWriter, _ *http.Request) {
	providers, defaults, active := s.collectProviderCatalog()
	providerTypes := provider.ListProviderTypes()
	typeOut := make([]domain.ProviderTypeInfo, 0, len(providerTypes))
	for _, item := range providerTypes {
		typeOut = append(typeOut, domain.ProviderTypeInfo{
			ID:          item.ID,
			DisplayName: item.DisplayName,
		})
	}
	writeJSON(w, http.StatusOK, domain.ModelCatalogInfo{
		Providers:     providers,
		Defaults:      defaults,
		ActiveLLM:     active,
		ProviderTypes: typeOut,
	})
}

func (s *Server) configureProvider(w http.ResponseWriter, r *http.Request) {
	providerID := normalizeProviderID(chi.URLParam(r, "provider_id"))
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_provider_id", "provider_id is required", nil)
		return
	}
	var body struct {
		APIKey       *string            `json:"api_key"`
		BaseURL      *string            `json:"base_url"`
		DisplayName  *string            `json:"display_name"`
		Enabled      *bool              `json:"enabled"`
		Headers      *map[string]string `json:"headers"`
		TimeoutMS    *int               `json:"timeout_ms"`
		ModelAliases *map[string]string `json:"model_aliases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if body.TimeoutMS != nil && *body.TimeoutMS < 0 {
		writeErr(w, http.StatusBadRequest, "invalid_provider_config", "timeout_ms must be >= 0", nil)
		return
	}
	sanitizedAliases, aliasErr := sanitizeModelAliases(body.ModelAliases)
	if aliasErr != nil {
		writeErr(w, http.StatusBadRequest, "invalid_provider_config", aliasErr.Error(), nil)
		return
	}
	var out domain.ProviderInfo
	if err := s.store.Write(func(st *repo.State) error {
		setting := getProviderSettingByID(st, providerID)
		normalizeProviderSetting(&setting)
		if body.APIKey != nil {
			setting.APIKey = strings.TrimSpace(*body.APIKey)
		}
		if body.BaseURL != nil {
			setting.BaseURL = strings.TrimSpace(*body.BaseURL)
		}
		if body.DisplayName != nil {
			setting.DisplayName = strings.TrimSpace(*body.DisplayName)
		}
		if body.Enabled != nil {
			enabled := *body.Enabled
			setting.Enabled = &enabled
		}
		if body.Headers != nil {
			setting.Headers = sanitizeStringMap(*body.Headers)
		}
		if body.TimeoutMS != nil {
			setting.TimeoutMS = *body.TimeoutMS
		}
		if body.ModelAliases != nil {
			setting.ModelAliases = sanitizedAliases
		}
		st.Providers[providerID] = setting
		out = buildProviderInfo(providerID, setting)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	providerID := normalizeProviderID(chi.URLParam(r, "provider_id"))
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_provider_id", "provider_id is required", nil)
		return
	}

	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		for key := range st.Providers {
			if normalizeProviderID(key) == providerID {
				delete(st.Providers, key)
				deleted = true
			}
		}
		if deleted && normalizeProviderID(st.ActiveLLM.ProviderID) == providerID {
			st.ActiveLLM = domain.ModelSlotConfig{}
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) getActiveModels(w http.ResponseWriter, _ *http.Request) {
	var out domain.ActiveModelsInfo
	s.store.Read(func(st *repo.State) {
		out = domain.ActiveModelsInfo{ActiveLLM: st.ActiveLLM}
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setActiveModels(w http.ResponseWriter, r *http.Request) {
	var body domain.ModelSlotConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	body.ProviderID = normalizeProviderID(body.ProviderID)
	body.Model = strings.TrimSpace(body.Model)
	if body.ProviderID == "" || body.Model == "" {
		writeErr(w, http.StatusBadRequest, "invalid_model_slot", "provider_id and model are required", nil)
		return
	}
	var out domain.ModelSlotConfig
	if err := s.store.Write(func(st *repo.State) error {
		setting, ok := findProviderSettingByID(st, body.ProviderID)
		if !ok {
			return errors.New("provider_not_found")
		}
		normalizeProviderSetting(&setting)
		if !providerEnabled(setting) {
			return errors.New("provider_disabled")
		}
		resolvedModel, ok := provider.ResolveModelID(body.ProviderID, body.Model, setting.ModelAliases)
		if !ok {
			return errors.New("model_not_found")
		}
		out = domain.ModelSlotConfig{
			ProviderID: body.ProviderID,
			Model:      resolvedModel,
		}
		st.ActiveLLM = out
		return nil
	}); err != nil {
		switch err.Error() {
		case "provider_not_found":
			writeErr(w, http.StatusNotFound, "provider_not_found", "provider not found", nil)
			return
		case "provider_disabled":
			writeErr(w, http.StatusBadRequest, "provider_disabled", "provider is disabled", nil)
			return
		case "model_not_found":
			writeErr(w, http.StatusBadRequest, "model_not_found", "model not found for provider", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, domain.ActiveModelsInfo{ActiveLLM: out})
}

func (s *Server) listEnvs(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.EnvVar, 0)
	s.store.Read(func(st *repo.State) {
		for k, v := range st.Envs {
			out = append(out, domain.EnvVar{Key: k, Value: v})
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putEnvs(w http.ResponseWriter, r *http.Request) {
	body := map[string]string{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	for k := range body {
		if strings.TrimSpace(k) == "" {
			writeErr(w, http.StatusBadRequest, "invalid_env_key", "env key cannot be empty", nil)
			return
		}
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Envs = map[string]string{}
		for k, v := range body {
			st.Envs[strings.TrimSpace(k)] = v
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	s.listEnvs(w, r)
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	exists := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Envs[key]; ok {
			exists = true
			delete(st.Envs, key)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "env key not found", nil)
		return
	}
	s.listEnvs(w, r)
}

func (s *Server) listSkills(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.SkillSpec, 0)
	s.store.Read(func(st *repo.State) {
		for _, spec := range st.Skills {
			out = append(out, spec)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAvailableSkills(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.SkillSpec, 0)
	s.store.Read(func(st *repo.State) {
		for _, spec := range st.Skills {
			if spec.Enabled {
				out = append(out, spec)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) batchDisableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, false)
}

func (s *Server) batchEnableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, true)
}

func (s *Server) batchSetSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	var names []string
	if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		for _, name := range names {
			v, ok := st.Skills[name]
			if !ok {
				continue
			}
			v.Enabled = enabled
			st.Skills[name] = v
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string                 `json:"name"`
		Content    string                 `json:"content"`
		References map[string]interface{} `json:"references"`
		Scripts    map[string]interface{} `json:"scripts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Content) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_skill", "name and content are required", nil)
		return
	}
	created := false
	if err := s.store.Write(func(st *repo.State) error {
		name := strings.TrimSpace(body.Name)
		st.Skills[name] = domain.SkillSpec{
			Name:       name,
			Content:    body.Content,
			Source:     "customized",
			Path:       filepath.Join(s.cfg.DataDir, "skills", name),
			References: safeMap(body.References),
			Scripts:    safeMap(body.Scripts),
			Enabled:    true,
		}
		created = true
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"created": created})
}

func (s *Server) disableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), false)
}

func (s *Server) enableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), true)
}

func (s *Server) setSkillEnabled(w http.ResponseWriter, name string, enabled bool) {
	exists := false
	if err := s.store.Write(func(st *repo.State) error {
		v, ok := st.Skills[name]
		if !ok {
			return nil
		}
		exists = true
		v.Enabled = enabled
		st.Skills[name] = v
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "skill not found", nil)
		return
	}
	key := "enabled"
	if !enabled {
		key = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "skill_name")
	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Skills[name]; ok {
			delete(st.Skills, name)
			deleted = true
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) loadSkillFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "skill_name")
	filePath := chi.URLParam(r, "file_path")
	var content string
	found := false
	s.store.Read(func(st *repo.State) {
		skill, ok := st.Skills[name]
		if !ok {
			return
		}
		content, found = readSkillVirtualFile(skill, filePath)
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "skill file not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func readSkillVirtualFile(skill domain.SkillSpec, filePath string) (string, bool) {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	var node interface{}
	switch parts[0] {
	case "references":
		node = skill.References
	case "scripts":
		node = skill.Scripts
	default:
		return "", false
	}
	for _, p := range parts[1:] {
		m, ok := node.(map[string]interface{})
		if !ok {
			return "", false
		}
		n, ok := m[p]
		if !ok {
			return "", false
		}
		node = n
	}
	s, ok := node.(string)
	return s, ok
}

const (
	workspaceFileEnvs      = "config/envs.json"
	workspaceFileChannels  = "config/channels.json"
	workspaceFileModels    = "config/models.json"
	workspaceFileActiveLLM = "config/active-llm.json"
	workspaceFileAITools   = aiToolsGuideRelativePath
	workspaceDocsAIDir     = "docs/AI"
)

type workspaceFileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

type workspaceFileListResponse struct {
	Files []workspaceFileEntry `json:"files"`
}

type workspaceExportModels struct {
	Providers map[string]repo.ProviderSetting `json:"providers"`
	ActiveLLM domain.ModelSlotConfig          `json:"active_llm"`
}

type workspaceExportConfig struct {
	Envs     map[string]string       `json:"envs"`
	Channels domain.ChannelConfigMap `json:"channels"`
	Models   workspaceExportModels   `json:"models"`
}

type workspaceExportPayload struct {
	Version string                      `json:"version"`
	Skills  map[string]domain.SkillSpec `json:"skills"`
	Config  workspaceExportConfig       `json:"config"`
}

type workspaceImportRequest struct {
	Mode    string                 `json:"mode"`
	Payload workspaceExportPayload `json:"payload"`
}

func (s *Server) listWorkspaceFiles(w http.ResponseWriter, _ *http.Request) {
	out := workspaceFileListResponse{Files: []workspaceFileEntry{}}
	s.store.Read(func(st *repo.State) {
		out.Files = collectWorkspaceFiles(st)
	})
	out.Files = mergeWorkspaceFileEntries(out.Files, collectWorkspaceTextFileEntries()...)
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	if isWorkspaceTextFilePath(filePath) {
		resolvedPath, content, err := readWorkspaceTextFileRawForPath(filePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeErr(w, http.StatusNotFound, "not_found", "workspace file not found", nil)
				return
			}
			writeErr(w, http.StatusInternalServerError, "file_error", err.Error(), nil)
			return
		}
		if filePath != resolvedPath {
			writeErr(w, http.StatusNotFound, "not_found", "workspace file not found", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"content": content})
		return
	}

	var data interface{}
	found := false
	s.store.Read(func(st *repo.State) {
		data, found = readWorkspaceFileData(st, filePath)
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "workspace file not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) putWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	if isWorkspaceTextFilePath(filePath) {
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			writeErr(w, http.StatusBadRequest, "invalid_ai_tools_guide", "content is required", nil)
			return
		}
		if err := writeWorkspaceTextFileRawForPath(filePath, body.Content); err != nil {
			writeErr(w, http.StatusInternalServerError, "file_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
		return
	}

	switch filePath {
	case workspaceFileEnvs:
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
			return
		}
		envs, err := normalizeWorkspaceEnvs(body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_env_key", err.Error(), nil)
			return
		}
		if err := s.store.Write(func(st *repo.State) error {
			st.Envs = envs
			return nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
		return
	case workspaceFileChannels:
		var body domain.ChannelConfigMap
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
			return
		}
		channels, err := normalizeWorkspaceChannels(body, s.channels)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "channel_not_supported", err.Error(), nil)
			return
		}
		if err := s.store.Write(func(st *repo.State) error {
			st.Channels = channels
			return nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
		return
	case workspaceFileModels:
		var body map[string]repo.ProviderSetting
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
			return
		}
		providers, err := normalizeWorkspaceProviders(body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_provider_config", err.Error(), nil)
			return
		}
		if err := s.store.Write(func(st *repo.State) error {
			st.Providers = providers
			if st.ActiveLLM.ProviderID != "" {
				if _, ok := findProviderSettingByID(st, st.ActiveLLM.ProviderID); !ok {
					st.ActiveLLM = domain.ModelSlotConfig{}
				}
			}
			return nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
		return
	case workspaceFileActiveLLM:
		var body domain.ModelSlotConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
			return
		}
		body.ProviderID = normalizeProviderID(body.ProviderID)
		body.Model = strings.TrimSpace(body.Model)
		if (body.ProviderID == "") != (body.Model == "") {
			writeErr(w, http.StatusBadRequest, "invalid_model_slot", "provider_id and model must be set together", nil)
			return
		}
		if err := s.store.Write(func(st *repo.State) error {
			if body.ProviderID == "" {
				st.ActiveLLM = domain.ModelSlotConfig{}
				return nil
			}
			if _, ok := findProviderSettingByID(st, body.ProviderID); !ok {
				return errors.New("provider_not_found")
			}
			st.ActiveLLM = body
			return nil
		}); err != nil {
			if err.Error() == "provider_not_found" {
				writeErr(w, http.StatusNotFound, "provider_not_found", "provider not found", nil)
				return
			}
			writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
		return
	}

	name, ok := workspaceSkillNameFromPath(filePath)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	var body domain.SkillSpec
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if body.Name != "" && strings.TrimSpace(body.Name) != name {
		writeErr(w, http.StatusBadRequest, "invalid_skill", "skill name in body must match file path", nil)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_skill", "content is required", nil)
		return
	}

	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = "customized"
	}
	spec := domain.SkillSpec{
		Name:       name,
		Content:    body.Content,
		Source:     source,
		Path:       filepath.Join(s.cfg.DataDir, "skills", name),
		References: safeMap(body.References),
		Scripts:    safeMap(body.Scripts),
		Enabled:    body.Enabled,
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Skills[name] = spec
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	if isWorkspaceConfigFile(filePath) {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "config files cannot be deleted", nil)
		return
	}
	name, ok := workspaceSkillNameFromPath(filePath)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}

	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Skills[name]; ok {
			delete(st.Skills, name)
			deleted = true
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) exportWorkspace(w http.ResponseWriter, _ *http.Request) {
	out := workspaceExportPayload{
		Version: "v1",
		Skills:  map[string]domain.SkillSpec{},
		Config: workspaceExportConfig{
			Envs:     map[string]string{},
			Channels: domain.ChannelConfigMap{},
			Models: workspaceExportModels{
				Providers: map[string]repo.ProviderSetting{},
				ActiveLLM: domain.ModelSlotConfig{},
			},
		},
	}
	s.store.Read(func(st *repo.State) {
		out.Skills = cloneWorkspaceSkills(st.Skills)
		out.Config.Envs = cloneWorkspaceEnvs(st.Envs)
		out.Config.Channels = cloneWorkspaceChannels(st.Channels)
		out.Config.Models.Providers = cloneWorkspaceProviders(st.Providers)
		out.Config.Models.ActiveLLM = st.ActiveLLM
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) importWorkspace(w http.ResponseWriter, r *http.Request) {
	var body workspaceImportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if strings.ToLower(strings.TrimSpace(body.Mode)) != "replace" {
		writeErr(w, http.StatusBadRequest, "invalid_import_mode", "mode must be replace", nil)
		return
	}

	skills, err := normalizeWorkspaceSkills(body.Payload.Skills, s.cfg.DataDir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_skill", err.Error(), nil)
		return
	}
	envs, err := normalizeWorkspaceEnvs(body.Payload.Config.Envs)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_env_key", err.Error(), nil)
		return
	}
	channels, err := normalizeWorkspaceChannels(body.Payload.Config.Channels, s.channels)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "channel_not_supported", err.Error(), nil)
		return
	}
	providers, err := normalizeWorkspaceProviders(body.Payload.Config.Models.Providers)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_provider_config", err.Error(), nil)
		return
	}
	active, err := normalizeWorkspaceActiveLLM(body.Payload.Config.Models.ActiveLLM, providers)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_model_slot", err.Error(), nil)
		return
	}

	if err := s.store.Write(func(st *repo.State) error {
		st.Skills = skills
		st.Envs = envs
		st.Channels = channels
		st.Providers = providers
		st.ActiveLLM = active
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"imported": true})
}

func collectWorkspaceFiles(st *repo.State) []workspaceFileEntry {
	files := []workspaceFileEntry{
		{Path: workspaceFileEnvs, Kind: "config", Size: jsonSize(cloneWorkspaceEnvs(st.Envs))},
		{Path: workspaceFileChannels, Kind: "config", Size: jsonSize(cloneWorkspaceChannels(st.Channels))},
		{Path: workspaceFileModels, Kind: "config", Size: jsonSize(cloneWorkspaceProviders(st.Providers))},
		{Path: workspaceFileActiveLLM, Kind: "config", Size: jsonSize(st.ActiveLLM)},
	}
	for name, spec := range st.Skills {
		files = append(files, workspaceFileEntry{
			Path: workspaceSkillFilePath(name),
			Kind: "skill",
			Size: jsonSize(cloneWorkspaceSkill(spec)),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func collectWorkspaceTextFileEntries() []workspaceFileEntry {
	files := collectWorkspaceDocsAIFileEntries()
	if aiToolsFile, ok := workspaceAIToolsFileEntry(); ok {
		files = append(files, aiToolsFile)
	}
	return mergeWorkspaceFileEntries(nil, files...)
}

func collectWorkspaceDocsAIFileEntries() []workspaceFileEntry {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil
	}
	docsDir := filepath.Join(repoRoot, filepath.FromSlash(workspaceDocsAIDir))
	info, err := os.Stat(docsDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	files := []workspaceFileEntry{}
	_ = filepath.WalkDir(docsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)
		if !isWorkspaceDocsAIFilePath(relPath) {
			return nil
		}
		files = append(files, workspaceFileEntry{
			Path: relPath,
			Kind: "config",
			Size: int(info.Size()),
		})
		return nil
	})
	return files
}

func mergeWorkspaceFileEntries(base []workspaceFileEntry, extra ...workspaceFileEntry) []workspaceFileEntry {
	out := make([]workspaceFileEntry, 0, len(base)+len(extra))
	indexByPath := map[string]int{}
	entries := append(append([]workspaceFileEntry{}, base...), extra...)
	for _, item := range entries {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		item.Path = path
		if idx, ok := indexByPath[path]; ok {
			if out[idx].Size <= 0 && item.Size > 0 {
				out[idx] = item
			}
			continue
		}
		indexByPath[path] = len(out)
		out = append(out, item)
	}
	return out
}

func readWorkspaceFileData(st *repo.State, filePath string) (interface{}, bool) {
	switch filePath {
	case workspaceFileEnvs:
		return cloneWorkspaceEnvs(st.Envs), true
	case workspaceFileChannels:
		return cloneWorkspaceChannels(st.Channels), true
	case workspaceFileModels:
		return cloneWorkspaceProviders(st.Providers), true
	case workspaceFileActiveLLM:
		return st.ActiveLLM, true
	default:
		name, ok := workspaceSkillNameFromPath(filePath)
		if !ok {
			return nil, false
		}
		spec, exists := st.Skills[name]
		if !exists {
			return nil, false
		}
		return cloneWorkspaceSkill(spec), true
	}
}

func workspaceFilePathFromRequest(r *http.Request) (string, bool) {
	raw := chi.URLParam(r, "*")
	if raw == "" {
		raw = chi.URLParam(r, "file_path")
	}
	return normalizeWorkspaceFilePath(raw)
}

func normalizeWorkspaceFilePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(trimmed); err == nil {
		trimmed = unescaped
	}
	trimmed = filepath.ToSlash(trimmed)
	parts := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", false
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), true
}

func workspaceSkillNameFromPath(filePath string) (string, bool) {
	if !strings.HasPrefix(filePath, "skills/") || !strings.HasSuffix(filePath, ".json") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(filePath, "skills/"), ".json"))
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func workspaceSkillFilePath(name string) string {
	return "skills/" + strings.TrimSpace(name) + ".json"
}

func isWorkspaceConfigFile(filePath string) bool {
	return filePath == workspaceFileEnvs ||
		filePath == workspaceFileChannels ||
		filePath == workspaceFileModels ||
		filePath == workspaceFileActiveLLM ||
		isWorkspaceTextFilePath(filePath)
}

func normalizeWorkspaceEnvs(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			return nil, errors.New("env key cannot be empty")
		}
		out[k] = value
	}
	return out, nil
}

func normalizeWorkspaceChannels(in domain.ChannelConfigMap, supported map[string]plugin.ChannelPlugin) (domain.ChannelConfigMap, error) {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, errors.New("channel name cannot be empty")
		}
		if _, ok := supported[normalized]; !ok {
			return nil, fmt.Errorf("channel %q is not supported", name)
		}
		out[normalized] = cloneWorkspaceJSONMap(cfg)
	}
	return out, nil
}

func normalizeWorkspaceProviders(in map[string]repo.ProviderSetting) (map[string]repo.ProviderSetting, error) {
	out := map[string]repo.ProviderSetting{}
	for rawID, rawSetting := range in {
		id := normalizeProviderID(rawID)
		if id == "" {
			return nil, errors.New("provider id cannot be empty")
		}
		if id == "demo" {
			continue
		}
		setting := rawSetting
		normalizeProviderSetting(&setting)
		if setting.TimeoutMS < 0 {
			return nil, fmt.Errorf("provider %q timeout_ms must be >= 0", rawID)
		}
		setting.Headers = sanitizeStringMap(setting.Headers)
		setting.ModelAliases = sanitizeStringMap(setting.ModelAliases)
		out[id] = setting
	}
	return out, nil
}

func normalizeWorkspaceSkills(in map[string]domain.SkillSpec, dataDir string) (map[string]domain.SkillSpec, error) {
	out := map[string]domain.SkillSpec{}
	for rawName, rawSpec := range in {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, errors.New("skill name cannot be empty")
		}
		content := strings.TrimSpace(rawSpec.Content)
		if content == "" {
			return nil, fmt.Errorf("skill %q content is required", name)
		}
		source := strings.TrimSpace(rawSpec.Source)
		if source == "" {
			source = "customized"
		}
		out[name] = domain.SkillSpec{
			Name:       name,
			Content:    rawSpec.Content,
			Source:     source,
			Path:       filepath.Join(dataDir, "skills", name),
			References: safeMap(rawSpec.References),
			Scripts:    safeMap(rawSpec.Scripts),
			Enabled:    rawSpec.Enabled,
		}
	}
	return out, nil
}

func normalizeWorkspaceActiveLLM(in domain.ModelSlotConfig, providers map[string]repo.ProviderSetting) (domain.ModelSlotConfig, error) {
	providerID := normalizeProviderID(in.ProviderID)
	modelID := strings.TrimSpace(in.Model)
	if providerID == "" && modelID == "" {
		return domain.ModelSlotConfig{}, nil
	}
	if providerID == "" || modelID == "" {
		return domain.ModelSlotConfig{}, errors.New("provider_id and model must be set together")
	}
	if _, ok := providers[providerID]; !ok {
		return domain.ModelSlotConfig{}, errors.New("active_llm provider not found")
	}
	return domain.ModelSlotConfig{ProviderID: providerID, Model: modelID}, nil
}

func cloneWorkspaceEnvs(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneWorkspaceChannels(in domain.ChannelConfigMap) domain.ChannelConfigMap {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		out[name] = cloneWorkspaceJSONMap(cfg)
	}
	return out
}

func cloneWorkspaceProviders(in map[string]repo.ProviderSetting) map[string]repo.ProviderSetting {
	out := map[string]repo.ProviderSetting{}
	for id, raw := range in {
		setting := raw
		normalizeProviderSetting(&setting)
		headers := map[string]string{}
		for key, value := range setting.Headers {
			headers[key] = value
		}
		aliases := map[string]string{}
		for key, value := range setting.ModelAliases {
			aliases[key] = value
		}
		setting.Headers = headers
		setting.ModelAliases = aliases
		if setting.Enabled != nil {
			enabled := *setting.Enabled
			setting.Enabled = &enabled
		}
		out[id] = setting
	}
	return out
}

func cloneWorkspaceSkills(in map[string]domain.SkillSpec) map[string]domain.SkillSpec {
	out := map[string]domain.SkillSpec{}
	for name, spec := range in {
		out[name] = cloneWorkspaceSkill(spec)
	}
	return out
}

func cloneWorkspaceSkill(in domain.SkillSpec) domain.SkillSpec {
	return domain.SkillSpec{
		Name:       in.Name,
		Content:    in.Content,
		Source:     in.Source,
		Path:       in.Path,
		References: cloneWorkspaceJSONMap(in.References),
		Scripts:    cloneWorkspaceJSONMap(in.Scripts),
		Enabled:    in.Enabled,
	}
}

func cloneWorkspaceJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]interface{}{}
	}
	if out == nil {
		return map[string]interface{}{}
	}
	return out
}

func jsonSize(v interface{}) int {
	buf, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(buf)
}

func (s *Server) listChannels(w http.ResponseWriter, _ *http.Request) {
	var out domain.ChannelConfigMap
	s.store.Read(func(st *repo.State) {
		out = st.Channels
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listChannelTypes(w http.ResponseWriter, _ *http.Request) {
	out := make([]string, 0, len(s.channels))
	for name := range s.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannels(w http.ResponseWriter, r *http.Request) {
	var body domain.ChannelConfigMap
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	for name := range body {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if _, ok := s.channels[normalized]; !ok {
			writeErr(w, http.StatusBadRequest, "channel_not_supported", fmt.Sprintf("channel %q is not supported", name), nil)
			return
		}
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Channels = domain.ChannelConfigMap{}
		for name, cfg := range body {
			st.Channels[strings.ToLower(strings.TrimSpace(name))] = cfg
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	normalized := strings.ToLower(strings.TrimSpace(name))
	found := false
	var out map[string]interface{}
	s.store.Read(func(st *repo.State) {
		out, found = st.Channels[normalized]
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "channel not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := s.channels[normalized]; !ok {
		writeErr(w, http.StatusBadRequest, "channel_not_supported", fmt.Sprintf("channel %q is not supported", name), nil)
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		if st.Channels == nil {
			st.Channels = domain.ChannelConfigMap{}
		}
		st.Channels[normalized] = body
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func mapRunnerError(err error) (status int, code string, message string) {
	var runnerErr *runner.RunnerError
	if errors.As(err, &runnerErr) {
		switch runnerErr.Code {
		case runner.ErrorCodeProviderNotConfigured:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderNotSupported:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderRequestFailed:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderInvalidReply:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		default:
			return http.StatusInternalServerError, "runner_error", "runner execution failed"
		}
	}
	return http.StatusInternalServerError, "runner_error", "runner execution failed"
}

func mapToolError(err error) (status int, code string, message string) {
	var te *toolError
	if errors.As(err, &te) {
		switch te.Code {
		case "tool_disabled":
			return http.StatusForbidden, te.Code, te.Message
		case "tool_not_supported":
			return http.StatusBadRequest, te.Code, te.Message
		case "tool_invoke_failed":
			switch {
			case errors.Is(te.Err, plugin.ErrShellToolCommandMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input command is required"
			case errors.Is(te.Err, plugin.ErrShellToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrShellToolExecutorUnavailable):
				return http.StatusBadGateway, "tool_runtime_unavailable", "shell executor is unavailable on current host"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrFileLinesToolStartInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input start must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolEndInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input end must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeTooLarge):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is too large"
			case errors.Is(te.Err, plugin.ErrFileLinesToolContentMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input content is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolOutOfRange):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is out of file bounds"
			case errors.Is(te.Err, plugin.ErrFileLinesToolFileNotFound):
				return http.StatusBadRequest, "invalid_tool_input", "target file does not exist"
			case errors.Is(te.Err, plugin.ErrBrowserToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrBrowserToolTaskMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input task is required"
			case errors.Is(te.Err, plugin.ErrSearchToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrSearchToolQueryMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input query is required"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnsupported):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is unsupported"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnconfigured):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is not configured"
			default:
				return http.StatusBadGateway, te.Code, te.Message
			}
		case "tool_invalid_result":
			return http.StatusBadGateway, te.Code, te.Message
		default:
			return http.StatusInternalServerError, "tool_error", "tool execution failed"
		}
	}
	return http.StatusInternalServerError, "tool_error", "tool execution failed"
}

func compactFeedbackField(raw string, maxLen int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	compact := strings.Join(strings.Fields(trimmed), " ")
	if maxLen <= 0 {
		return compact
	}
	runes := []rune(compact)
	if len(runes) <= maxLen {
		return compact
	}
	return string(runes[:maxLen]) + "...(truncated)"
}

func formatProviderToolArgumentsErrorFeedback(toolName, rawArguments, parseErr string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown_tool"
	}
	detail := compactFeedbackField(parseErr, 160)
	if detail == "" {
		detail = "invalid json arguments"
	}
	raw := compactFeedbackField(rawArguments, 320)
	if raw == "" {
		raw = "{}"
	}
	return fmt.Sprintf(
		"tool_error code=invalid_tool_input message=provider tool call arguments for %s are invalid detail=%s raw_arguments=%s",
		name,
		detail,
		raw,
	)
}

func formatToolErrorFeedback(err error) string {
	if err == nil {
		return "tool_error code=tool_error message=tool execution failed"
	}
	_, code, message := mapToolError(err)
	if strings.TrimSpace(code) == "" {
		code = "tool_error"
	}
	if strings.TrimSpace(message) == "" {
		message = "tool execution failed"
	}
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == message {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	return fmt.Sprintf("tool_error code=%s message=%s detail=%s", code, message, detail)
}

func (s *Server) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig) {
	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.store.Read(func(st *repo.State) {
		active = st.ActiveLLM
		settingsByID := map[string]repo.ProviderSetting{}

		for rawID, setting := range st.Providers {
			id := normalizeProviderID(rawID)
			if id == "" {
				continue
			}
			normalizeProviderSetting(&setting)
			settingsByID[id] = setting
		}

		ids := make([]string, 0, len(settingsByID))
		for id := range settingsByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			setting := settingsByID[id]
			out = append(out, buildProviderInfo(id, setting))
			defaults[id] = provider.DefaultModelID(id)
		}
	})
	return out, defaults, active
}

func buildProviderInfo(providerID string, setting repo.ProviderSetting) domain.ProviderInfo {
	normalizeProviderSetting(&setting)
	spec := provider.ResolveProvider(providerID)
	apiKey := resolveProviderAPIKey(providerID, setting)
	return domain.ProviderInfo{
		ID:                 providerID,
		Name:               spec.Name,
		DisplayName:        resolveProviderDisplayName(setting, spec.Name),
		OpenAICompatible:   provider.ResolveAdapter(providerID) == provider.AdapterOpenAICompatible,
		APIKeyPrefix:       spec.APIKeyPrefix,
		Models:             provider.ResolveModels(providerID, setting.ModelAliases),
		AllowCustomBaseURL: spec.AllowCustomBaseURL,
		Enabled:            providerEnabled(setting),
		HasAPIKey:          strings.TrimSpace(apiKey) != "",
		CurrentAPIKey:      maskKey(apiKey),
		CurrentBaseURL:     resolveProviderBaseURL(providerID, setting),
	}
}

func resolveProviderAPIKey(providerID string, setting repo.ProviderSetting) string {
	if key := strings.TrimSpace(setting.APIKey); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_API_KEY"))
}

func resolveProviderBaseURL(providerID string, setting repo.ProviderSetting) string {
	if baseURL := strings.TrimSpace(setting.BaseURL); baseURL != "" {
		return baseURL
	}
	if envBaseURL := strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_BASE_URL")); envBaseURL != "" {
		return envBaseURL
	}
	return provider.ResolveProvider(providerID).DefaultBaseURL
}

func resolveProviderDisplayName(setting repo.ProviderSetting, defaultName string) string {
	if displayName := strings.TrimSpace(setting.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(defaultName)
}

func providerEnvPrefix(providerID string) string {
	return provider.EnvPrefix(providerID)
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func providerEnabled(setting repo.ProviderSetting) bool {
	if setting.Enabled == nil {
		return true
	}
	return *setting.Enabled
}

func normalizeProviderSetting(setting *repo.ProviderSetting) {
	if setting == nil {
		return
	}
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimSpace(setting.BaseURL)
	if setting.Enabled == nil {
		enabled := true
		setting.Enabled = &enabled
	}
	if setting.Headers == nil {
		setting.Headers = map[string]string{}
	}
	if setting.ModelAliases == nil {
		setting.ModelAliases = map[string]string{}
	}
}

func sanitizeStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeModelAliases(raw *map[string]string) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	for key, value := range *raw {
		alias := strings.TrimSpace(key)
		modelID := strings.TrimSpace(value)
		if alias == "" || modelID == "" {
			return nil, errors.New("model_aliases requires non-empty key and value")
		}
		out[alias] = modelID
	}
	return out, nil
}

func getProviderSettingByID(st *repo.State, providerID string) repo.ProviderSetting {
	if setting, ok := findProviderSettingByID(st, providerID); ok {
		return setting
	}
	setting := repo.ProviderSetting{}
	normalizeProviderSetting(&setting)
	return setting
}

func findProviderSettingByID(st *repo.State, providerID string) (repo.ProviderSetting, bool) {
	if st == nil {
		return repo.ProviderSetting{}, false
	}
	if setting, ok := st.Providers[providerID]; ok {
		return setting, true
	}
	for key, setting := range st.Providers {
		if normalizeProviderID(key) == providerID {
			return setting, true
		}
	}
	return repo.ProviderSetting{}, false
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, code int, errCode, message string, details interface{}) {
	writeJSON(w, code, domain.APIErrorBody{Error: domain.APIError{Code: errCode, Message: message, Details: details}})
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func maskKey(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

func prependAIToolsGuide(input []domain.AgentInputMessage, guide string) []domain.AgentInputMessage {
	effective := make([]domain.AgentInputMessage, 0, len(input)+1)
	effective = append(effective, domain.AgentInputMessage{
		Role:    "system",
		Type:    "message",
		Content: []domain.RuntimeContent{{Type: "text", Text: guide}},
	})
	effective = append(effective, input...)
	return effective
}

func loadAIToolsGuide() (string, error) {
	loadRequired := func(relativePath string) (string, error) {
		_, content, err := readWorkspaceTextFileRawForPath(relativePath)
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			return "", fmt.Errorf("ai tools guide is empty: %s", relativePath)
		}
		return trimmed, nil
	}

	agentsGuide, err := loadRequired(aiToolsGuideRelativePath)
	if err != nil {
		return "", err
	}
	toolsGuide, err := loadRequired(aiToolsGuideLegacyRelativePath)
	if err != nil {
		return "", err
	}

	return strings.Join([]string{
		fmt.Sprintf("## %s\n%s", aiToolsGuideRelativePath, agentsGuide),
		fmt.Sprintf("## %s\n%s", aiToolsGuideLegacyRelativePath, toolsGuide),
	}, "\n\n"), nil
}

func workspaceAIToolsFileEntry() (workspaceFileEntry, bool) {
	relativePath, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return workspaceFileEntry{}, false
	}
	return workspaceFileEntry{
		Path: relativePath,
		Kind: "config",
		Size: len([]byte(content)),
	}, true
}

func readAIToolsGuideRaw() (string, error) {
	_, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return "", err
	}
	return content, nil
}

func readAIToolsGuideRawWithPath() (string, string, error) {
	guidePath, relativePath, err := resolveAIToolsGuidePathForRead()
	if err != nil {
		return "", "", err
	}
	content, err := os.ReadFile(guidePath)
	if err != nil {
		return "", "", err
	}
	return relativePath, string(content), nil
}

func writeAIToolsGuideRaw(content string) error {
	return writeAIToolsGuideRawForPath("", content)
}

func writeAIToolsGuideRawForPath(relativePath, content string) error {
	guidePath, _, err := resolveAIToolsGuidePathForWrite(relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(guidePath, []byte(content), 0o644)
}

func readWorkspaceTextFileRawForPath(relativePath string) (string, string, error) {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return "", "", errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	content, err := os.ReadFile(target)
	if err != nil {
		return "", "", err
	}
	return normalized, string(content), nil
}

func writeWorkspaceTextFileRawForPath(relativePath, content string) error {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

func isWorkspaceTextFilePath(filePath string) bool {
	return isWorkspaceDocsAIFilePath(filePath) || isAIToolsWorkspaceFilePath(filePath)
}

func isWorkspaceDocsAIFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, workspaceDocsAIDir+"/")
}

func isAIToolsWorkspaceFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		if path == candidate {
			return true
		}
	}
	return false
}

func resolveAIToolsGuidePathForRead() (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return "", "", err
	}
	for _, relativePath := range candidates {
		guidePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		info, statErr := os.Stat(guidePath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return "", "", statErr
		}
		if info.IsDir() {
			continue
		}
		return guidePath, relativePath, nil
	}
	return "", "", fmt.Errorf("%w: ai tools guide not found in %s", os.ErrNotExist, strings.Join(candidates, ", "))
}

func resolveAIToolsGuidePathForWrite(relativePath string) (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := strings.TrimSpace(relativePath)
	if target == "" {
		envPath, hasEnv, err := aiToolsGuidePathFromEnv()
		if err != nil {
			return "", "", err
		}
		if hasEnv {
			target = envPath
		} else {
			target = aiToolsGuideRelativePath
		}
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(target)
	if !ok {
		return "", "", errors.New("invalid ai tools guide path")
	}
	return filepath.Join(repoRoot, filepath.FromSlash(normalized)), normalized, nil
}

func aiToolsGuidePathCandidates() ([]string, error) {
	candidates := []string{}
	if envPath, hasEnv, err := aiToolsGuidePathFromEnv(); err != nil {
		return nil, err
	} else if hasEnv {
		candidates = append(candidates, envPath)
	}
	candidates = append(
		candidates,
		aiToolsGuideRelativePath,
		aiToolsGuideLegacyRelativePath,
		aiToolsGuideLegacyV0RelativePath,
	)

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique, nil
}

func aiToolsGuidePathFromEnv() (string, bool, error) {
	raw := strings.TrimSpace(os.Getenv(aiToolsGuidePathEnv))
	if raw == "" {
		return "", false, nil
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(raw)
	if !ok {
		return "", false, fmt.Errorf("%s must be a relative path without traversal", aiToolsGuidePathEnv)
	}
	return normalized, true, nil
}

func normalizeAIToolsGuideRelativePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return clean, true
}

func findRepoRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// Release bundles do not contain .git metadata. In that case, treat the
	// current working directory as the workspace root.
	return start, nil
}
