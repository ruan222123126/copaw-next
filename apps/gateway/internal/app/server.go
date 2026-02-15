package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"copaw-next/apps/gateway/internal/channel"
	"copaw-next/apps/gateway/internal/config"
	"copaw-next/apps/gateway/internal/domain"
	"copaw-next/apps/gateway/internal/observability"
	"copaw-next/apps/gateway/internal/repo"
	"copaw-next/apps/gateway/internal/runner"
)

const version = "0.1.0"

type Server struct {
	cfg     config.Config
	store   *repo.Store
	runner  *runner.Runner
	console *channel.ConsoleChannel
}

func NewServer(cfg config.Config) (*Server, error) {
	store, err := repo.NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:     cfg,
		store:   store,
		runner:  runner.New(),
		console: channel.NewConsoleChannel(),
	}, nil
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(observability.RequestID)
	r.Use(observability.Logging)
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
	chatID := ""
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
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}

	reply := s.runner.GenerateReply(req)
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

	s.console.SendText(req.UserID, req.SessionID, reply)

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

	for _, token := range strings.Split(reply, " ") {
		payload, _ := json.Marshal(map[string]string{"delta": token + " "})
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
	if err := s.store.Write(func(state *repo.State) error {
		state.CronJobs[req.ID] = req
		if _, ok := state.CronStates[req.ID]; !ok {
			state.CronStates[req.ID] = domain.CronJobState{}
		}
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
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errors.New("not_found")
		}
		st.CronJobs[id] = req
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
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), "paused")
}

func (s *Server) resumeCronJob(w http.ResponseWriter, r *http.Request) {
	s.updateCronStatus(w, chi.URLParam(r, "job_id"), "resumed")
}

func (s *Server) runCronJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	now := nowISO()
	status := "running"
	if err := s.store.Write(func(st *repo.State) error {
		job, ok := st.CronJobs[id]
		if !ok {
			return errors.New("not_found")
		}
		st.CronStates[id] = domain.CronJobState{LastRunAt: &now, LastStatus: &status}
		if job.TaskType == "text" && job.Text != "" {
			s.console.SendText(job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, job.Text)
		}
		return nil
	}); err != nil {
		if err.Error() == "not_found" {
			writeErr(w, http.StatusNotFound, "not_found", "cron job not found", nil)
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
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[id]; !ok {
			return errors.New("not_found")
		}
		state := st.CronStates[id]
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
	if status == "resumed" {
		key = "resumed"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) listProviders(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.ProviderInfo, 0)
	s.store.Read(func(st *repo.State) {
		for id, setting := range st.Providers {
			has := setting.APIKey != "" || setting.BaseURL != ""
			out = append(out, domain.ProviderInfo{
				ID: id, Name: strings.ToUpper(id), APIKeyPrefix: strings.ToUpper(id) + "_API_KEY",
				Models:             []domain.ModelInfo{{ID: "demo-chat", Name: "Demo Chat"}},
				AllowCustomBaseURL: true,
				HasAPIKey:          has,
				CurrentAPIKey:      maskKey(setting.APIKey),
				CurrentBaseURL:     setting.BaseURL,
			})
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) configureProvider(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider_id")
	var body struct {
		APIKey  *string `json:"api_key"`
		BaseURL *string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	var out domain.ProviderInfo
	if err := s.store.Write(func(st *repo.State) error {
		setting := st.Providers[providerID]
		if body.APIKey != nil {
			setting.APIKey = *body.APIKey
		}
		if body.BaseURL != nil {
			setting.BaseURL = *body.BaseURL
		}
		st.Providers[providerID] = setting
		has := setting.APIKey != "" || setting.BaseURL != ""
		out = domain.ProviderInfo{
			ID: providerID, Name: strings.ToUpper(providerID), APIKeyPrefix: strings.ToUpper(providerID) + "_API_KEY",
			Models:             []domain.ModelInfo{{ID: "demo-chat", Name: "Demo Chat"}},
			AllowCustomBaseURL: true, HasAPIKey: has, CurrentAPIKey: maskKey(setting.APIKey), CurrentBaseURL: setting.BaseURL,
		}
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
	if body.ProviderID == "" || body.Model == "" {
		writeErr(w, http.StatusBadRequest, "invalid_model_slot", "provider_id and model are required", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Providers[body.ProviderID]; !ok {
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
	writeJSON(w, http.StatusOK, domain.ActiveModelsInfo{ActiveLLM: body})
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
	out := make([]string, 0)
	s.store.Read(func(st *repo.State) {
		for k := range st.Channels {
			out = append(out, k)
		}
	})
	sort.Strings(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannels(w http.ResponseWriter, r *http.Request) {
	var body domain.ChannelConfigMap
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Channels = body
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	found := false
	var out map[string]interface{}
	s.store.Read(func(st *repo.State) {
		out, found = st.Channels[name]
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "channel not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		if st.Channels == nil {
			st.Channels = domain.ChannelConfigMap{}
		}
		st.Channels[name] = body
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
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
