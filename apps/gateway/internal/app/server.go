package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	cronv3 "github.com/robfig/cron/v3"

	"copaw-next/apps/gateway/internal/channel"
	"copaw-next/apps/gateway/internal/config"
	"copaw-next/apps/gateway/internal/domain"
	"copaw-next/apps/gateway/internal/observability"
	"copaw-next/apps/gateway/internal/plugin"
	"copaw-next/apps/gateway/internal/provider"
	"copaw-next/apps/gateway/internal/repo"
	"copaw-next/apps/gateway/internal/runner"
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
)

var errCronJobNotFound = errors.New("cron_job_not_found")
var errCronMaxConcurrencyReached = errors.New("cron_max_concurrency_reached")

type Server struct {
	cfg      config.Config
	store    *repo.Store
	runner   *runner.Runner
	channels map[string]plugin.ChannelPlugin

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
		cronStop: make(chan struct{}),
		cronDone: make(chan struct{}),
	}
	srv.registerChannelPlugin(channel.NewConsoleChannel())
	srv.registerChannelPlugin(channel.NewWebhookChannel())
	srv.startCronScheduler()
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

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(observability.RequestID)
	r.Use(observability.Logging)
	r.Use(observability.APIKey(s.cfg.APIKey))
	r.Use(cors)

	r.Get("/version", s.handleVersion)
	r.Get("/healthz", s.handleHealthz)

	r.Route("/chats", func(r chi.Router) {
		r.Get("/", s.listChats)
		r.Post("/", s.createChat)
		r.Post("/batch-delete", s.batchDeleteChats)
		r.Get("/{chat_id}", s.getChat)
		r.Put("/{chat_id}", s.updateChat)
		r.Delete("/{chat_id}", s.deleteChat)
	})

	r.Post("/agent/process", s.processAgent)

	r.Route("/cron", func(r chi.Router) {
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

	r.Route("/models", func(r chi.Router) {
		r.Get("/", s.listProviders)
		r.Get("/catalog", s.getModelCatalog)
		r.Put("/{provider_id}/config", s.configureProvider)
		r.Get("/active", s.getActiveModels)
		r.Put("/active", s.setActiveModels)
	})

	r.Route("/envs", func(r chi.Router) {
		r.Get("/", s.listEnvs)
		r.Put("/", s.putEnvs)
		r.Delete("/{key}", s.deleteEnv)
	})

	r.Route("/skills", func(r chi.Router) {
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

	r.Route("/workspace", func(r chi.Router) {
		r.Get("/download", s.downloadWorkspace)
		r.Post("/upload", s.uploadWorkspace)
	})

	r.Route("/config", func(r chi.Router) {
		r.Get("/channels", s.listChannels)
		r.Get("/channels/types", s.listChannelTypes)
		r.Put("/channels", s.putChannels)
		r.Get("/channels/{channel_name}", s.getChannel)
		r.Put("/channels/{channel_name}", s.putChannel)
	})

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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Request-Id")
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
	var req domain.AgentProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if req.SessionID == "" || req.UserID == "" || req.Channel == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "session_id, user_id, channel are required", nil)
		return
	}
	channelPlugin, channelCfg, channelName, err := s.resolveChannel(req.Channel)
	if err != nil {
		status, code, message := mapChannelError(err)
		writeErr(w, status, code, message, nil)
		return
	}
	req.Channel = channelName

	chatID := ""
	activeLLM := domain.ModelSlotConfig{}
	providerSetting := repo.ProviderSetting{}
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
		for _, input := range req.Input {
			state.Histories[chatID] = append(state.Histories[chatID], domain.RuntimeMessage{
				ID:      newID("msg"),
				Role:    input.Role,
				Type:    input.Type,
				Content: toRuntimeContents(input.Content),
			})
		}
		activeLLM = state.ActiveLLM
		activeLLM.ProviderID = normalizeProviderID(activeLLM.ProviderID)
		providerSetting = getProviderSettingByID(state, activeLLM.ProviderID)
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !providerEnabled(providerSetting) {
		writeErr(w, http.StatusBadRequest, "provider_disabled", "active provider is disabled", nil)
		return
	}
	resolvedModel, ok := provider.ResolveModelID(activeLLM.ProviderID, activeLLM.Model, providerSetting.ModelAliases)
	if !ok {
		writeErr(w, http.StatusBadRequest, "model_not_found", "active model is not available for provider", nil)
		return
	}
	activeLLM.Model = resolvedModel

	reply, err := s.runner.GenerateReply(r.Context(), req, runner.GenerateConfig{
		ProviderID: activeLLM.ProviderID,
		Model:      activeLLM.Model,
		APIKey:     resolveProviderAPIKey(activeLLM.ProviderID, providerSetting),
		BaseURL:    resolveProviderBaseURL(activeLLM.ProviderID, providerSetting),
		AdapterID:  provider.ResolveAdapter(activeLLM.ProviderID),
		Headers:    sanitizeStringMap(providerSetting.Headers),
		TimeoutMS:  providerSetting.TimeoutMS,
	})
	if err != nil {
		status, code, message := mapRunnerError(err)
		writeErr(w, status, code, message, nil)
		return
	}
	assistant := domain.RuntimeMessage{
		ID:      newID("msg"),
		Role:    "assistant",
		Type:    "message",
		Content: []domain.RuntimeContent{{Type: "text", Text: reply}},
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

	if err := channelPlugin.SendText(r.Context(), req.UserID, req.SessionID, reply, channelCfg); err != nil {
		status, code, message := mapChannelError(&channelError{
			Code:    "channel_dispatch_failed",
			Message: fmt.Sprintf("failed to dispatch message to channel %q", channelName),
			Err:     err,
		})
		writeErr(w, status, code, message, nil)
		return
	}

	if !req.Stream {
		writeJSON(w, http.StatusOK, map[string]string{"reply": reply})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "stream_not_supported", "streaming not supported", nil)
		return
	}

	for _, chunk := range splitReplyChunks(reply, 12) {
		payload, _ := json.Marshal(map[string]string{"delta": chunk})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func toRuntimeContents(in []domain.RuntimeContent) []domain.RuntimeContent {
	if in == nil {
		return []domain.RuntimeContent{}
	}
	return in
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

type channelError struct {
	Code    string
	Message string
	Err     error
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

	return s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return nil
		}
		state := st.CronStates[id]
		state.LastStatus = &finalStatus
		state.LastError = finalErr
		st.CronStates[id] = state
		return nil
	})
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
		channelName := resolveCronDispatchChannel(job)
		channelPlugin, channelCfg, _, err := s.resolveChannel(channelName)
		if err != nil {
			return err
		}
		if err := channelPlugin.SendText(ctx, job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, job.Text, channelCfg); err != nil {
			return &channelError{
				Code:    "channel_dispatch_failed",
				Message: fmt.Sprintf("failed to dispatch cron job to channel %q", channelName),
				Err:     err,
			}
		}
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
	writeJSON(w, http.StatusOK, domain.ModelCatalogInfo{
		Providers: providers,
		Defaults:  defaults,
		ActiveLLM: active,
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

func (s *Server) downloadWorkspace(w http.ResponseWriter, _ *http.Request) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	base := s.store.WorkspaceDir()
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == base {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			_, err = zw.Create(strings.Trim(filepath.ToSlash(rel), "/") + "/")
			return err
		}
		f, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	_ = zw.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=workspace.zip")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) uploadWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_multipart", "invalid multipart form", nil)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing_file", "missing file field", nil)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read_file_error", err.Error(), nil)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_zip", "uploaded file is not valid zip", nil)
		return
	}
	for _, zf := range zr.File {
		clean := filepath.Clean(zf.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			writeErr(w, http.StatusBadRequest, "unsafe_zip_path", "zip contains unsafe path", map[string]string{"path": zf.Name})
			return
		}
	}
	workspace := s.store.WorkspaceDir()
	entries, _ := os.ReadDir(workspace)
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(workspace, e.Name()))
	}
	for _, zf := range zr.File {
		target := filepath.Join(workspace, filepath.Clean(zf.Name))
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				writeErr(w, http.StatusInternalServerError, "extract_error", err.Error(), nil)
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "extract_error", err.Error(), nil)
			return
		}
		src, err := zf.Open()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "extract_error", err.Error(), nil)
			return
		}
		payload, err := io.ReadAll(src)
		_ = src.Close()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "extract_error", err.Error(), nil)
			return
		}
		if err := os.WriteFile(target, payload, 0o644); err != nil {
			writeErr(w, http.StatusInternalServerError, "extract_error", err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
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

func (s *Server) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig) {
	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.store.Read(func(st *repo.State) {
		active = st.ActiveLLM
		ids := map[string]struct{}{}
		settingsByID := map[string]repo.ProviderSetting{}

		for _, id := range provider.ListBuiltinProviderIDs() {
			ids[id] = struct{}{}
		}
		for rawID, setting := range st.Providers {
			id := normalizeProviderID(rawID)
			if id == "" {
				continue
			}
			normalizeProviderSetting(&setting)
			settingsByID[id] = setting
			ids[id] = struct{}{}
		}

		ordered := make([]string, 0, len(ids))
		for id := range ids {
			ordered = append(ordered, id)
		}
		sort.Strings(ordered)

		for _, id := range ordered {
			setting := settingsByID[id]
			normalizeProviderSetting(&setting)
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
