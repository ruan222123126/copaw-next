package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"nextai/apps/gateway/internal/repo"
)

const (
	qqInboundSupervisorInterval = 5 * time.Second
	qqInboundReconnectMinDelay  = 1 * time.Second
	qqInboundReconnectMaxDelay  = 30 * time.Second
	qqInboundReadTimeout        = 90 * time.Second
	qqInboundWriteTimeout       = 10 * time.Second

	qqInboundDefaultAPIBase  = "https://api.sgroup.qq.com"
	qqInboundDefaultTokenURL = "https://bots.qq.com/app/getAppAccessToken"

	qqGatewayOpDispatch       = 0
	qqGatewayOpHeartbeat      = 1
	qqGatewayOpIdentify       = 2
	qqGatewayOpReconnect      = 7
	qqGatewayOpInvalidSession = 9
	qqGatewayOpHello          = 10

	qqIntentPublicGuildMessages = 1 << 30
	qqIntentDirectMessage       = 1 << 12
	qqIntentGroupAndC2C         = 1 << 25
	qqIntentGuildMembers        = 1 << 1
	qqDefaultIntents            = qqIntentPublicGuildMessages | qqIntentDirectMessage | qqIntentGroupAndC2C | qqIntentGuildMembers
	qqFallbackIntents           = qqIntentPublicGuildMessages | qqIntentGuildMembers
)

type qqInboundConfig struct {
	AppID        string
	ClientSecret string
	APIBase      string
	TokenURL     string
	Intents      int
	IntentsSet   bool
}

type qqGatewayFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int            `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type qqInboundRuntimeState struct {
	Running         bool   `json:"running"`
	Connected       bool   `json:"connected"`
	ActiveSignature string `json:"-"`
	Intents         int    `json:"intents"`
	IntentsSource   string `json:"intents_source"`
	GatewayURL      string `json:"gateway_url,omitempty"`
	LastConnectedAt string `json:"last_connected_at,omitempty"`
	LastEventAt     string `json:"last_event_at,omitempty"`
	LastEventType   string `json:"last_event_type,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LastErrorAt     string `json:"last_error_at,omitempty"`
}

func (c qqInboundConfig) signature() string {
	return strings.Join([]string{
		c.AppID,
		c.ClientSecret,
		c.APIBase,
		c.TokenURL,
	}, "\x1f")
}

func (s *Server) mutateQQInboundState(apply func(*qqInboundRuntimeState)) {
	if s == nil || apply == nil {
		return
	}
	s.qqInboundMu.Lock()
	defer s.qqInboundMu.Unlock()
	apply(&s.qqInbound)
}

func (s *Server) snapshotQQInboundState() qqInboundRuntimeState {
	if s == nil {
		return qqInboundRuntimeState{}
	}
	s.qqInboundMu.RLock()
	defer s.qqInboundMu.RUnlock()
	return s.qqInbound
}

func (s *Server) getQQInboundState(w http.ResponseWriter, _ *http.Request) {
	runtime := s.snapshotQQInboundState()
	cfg, configured := s.loadQQInboundConfig()

	configInfo := map[string]interface{}{
		"enabled": configured,
	}
	if configured {
		configInfo["app_id"] = cfg.AppID
		configInfo["api_base"] = cfg.APIBase
		configInfo["token_url"] = cfg.TokenURL
		configInfo["intents"] = cfg.Intents
		if cfg.IntentsSet {
			configInfo["intents_source"] = "configured"
		} else {
			configInfo["intents_source"] = "default"
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured":        configured,
		"running":           runtime.Running,
		"connected":         runtime.Connected,
		"intents":           runtime.Intents,
		"intents_source":    runtime.IntentsSource,
		"gateway_url":       runtime.GatewayURL,
		"last_connected_at": runtime.LastConnectedAt,
		"last_event_at":     runtime.LastEventAt,
		"last_event_type":   runtime.LastEventType,
		"last_error":        runtime.LastError,
		"last_error_at":     runtime.LastErrorAt,
		"config":            configInfo,
	})
}

func (s *Server) startQQInboundSupervisor() {
	s.cronWG.Add(1)
	go func() {
		defer s.cronWG.Done()

		var workerCancel context.CancelFunc
		activeSignature := ""

		reconcile := func() {
			cfg, ok := s.loadQQInboundConfig()
			if !ok {
				if workerCancel != nil {
					workerCancel()
					workerCancel = nil
				}
				s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
					st.Running = false
					st.Connected = false
					st.ActiveSignature = ""
					st.Intents = 0
					st.IntentsSource = ""
					st.GatewayURL = ""
				})
				activeSignature = ""
				return
			}
			nextSignature := cfg.signature()
			if nextSignature == activeSignature {
				return
			}
			if workerCancel != nil {
				workerCancel()
			}

			runCtx, cancel := context.WithCancel(context.Background())
			workerCancel = cancel
			activeSignature = nextSignature
			s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
				st.Running = true
				st.Connected = false
				st.ActiveSignature = nextSignature
				st.Intents = cfg.Intents
				if cfg.IntentsSet {
					st.IntentsSource = "configured"
				} else {
					st.IntentsSource = "default"
				}
				st.GatewayURL = ""
			})

			s.cronWG.Add(1)
			go func(inboundCfg qqInboundConfig, signature string) {
				defer s.cronWG.Done()
				defer s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
					if st.ActiveSignature != signature {
						return
					}
					st.Running = false
					st.Connected = false
					st.GatewayURL = ""
				})
				s.runQQInboundLoop(runCtx, inboundCfg)
			}(cfg, nextSignature)
		}

		reconcile()
		ticker := time.NewTicker(qqInboundSupervisorInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				reconcile()
			case <-s.cronStop:
				if workerCancel != nil {
					workerCancel()
				}
				return
			}
		}
	}()
}

func (s *Server) loadQQInboundConfig() (qqInboundConfig, bool) {
	cfg := qqInboundConfig{}
	found := false

	s.store.Read(func(st *repo.State) {
		if st == nil {
			return
		}
		raw := cloneChannelConfig(st.Channels["qq"])
		if !parseBool(raw["enabled"]) {
			return
		}
		if inboundRaw, exists := raw["inbound_enabled"]; exists && !parseBool(inboundRaw) {
			return
		}
		appID := strings.TrimSpace(qqString(raw["app_id"]))
		clientSecret := strings.TrimSpace(qqString(raw["client_secret"]))
		if appID == "" || clientSecret == "" {
			return
		}

		apiBase := strings.TrimRight(strings.TrimSpace(qqString(raw["api_base"])), "/")
		if apiBase == "" {
			apiBase = qqInboundDefaultAPIBase
		}
		tokenURL := strings.TrimSpace(qqString(raw["token_url"]))
		if tokenURL == "" {
			tokenURL = qqInboundDefaultTokenURL
		}

		cfg = qqInboundConfig{
			AppID:        appID,
			ClientSecret: clientSecret,
			APIBase:      apiBase,
			TokenURL:     tokenURL,
			Intents:      qqDefaultIntents,
		}
		if parsed, ok := parseQQIntents(raw["inbound_intents"]); ok && parsed > 0 {
			cfg.Intents = parsed
			cfg.IntentsSet = true
		}
		found = true
	})

	return cfg, found
}

func (s *Server) runQQInboundLoop(ctx context.Context, cfg qqInboundConfig) {
	backoff := qqInboundReconnectMinDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := s.runQQInboundSession(ctx, cfg)
		if err != nil && ctx.Err() == nil {
			log.Printf("qq inbound session ended: %v", err)
			s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
				st.Connected = false
				st.GatewayURL = ""
				st.LastError = strings.TrimSpace(err.Error())
				st.LastErrorAt = nowISO()
			})
			if errors.Is(err, errQQInboundInvalidSession) && !cfg.IntentsSet && cfg.Intents != qqFallbackIntents {
				cfg.Intents = qqFallbackIntents
				backoff = qqInboundReconnectMinDelay
				log.Printf("qq inbound fallback intents applied: %d", cfg.Intents)
				s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
					st.Intents = cfg.Intents
					st.IntentsSource = "fallback"
				})
				continue
			}
		}
		if ctx.Err() != nil {
			return
		}

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
		if backoff < qqInboundReconnectMaxDelay {
			backoff *= 2
			if backoff > qqInboundReconnectMaxDelay {
				backoff = qqInboundReconnectMaxDelay
			}
		}
	}
}

func (s *Server) runQQInboundSession(ctx context.Context, cfg qqInboundConfig) error {
	token, err := fetchQQGatewayAccessToken(ctx, cfg)
	if err != nil {
		return err
	}
	gatewayURL, err := fetchQQGatewayURL(ctx, cfg, token)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: qqInboundWriteTimeout,
	}
	conn, _, err := dialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("dial qq gateway failed: %w", err)
	}
	defer conn.Close()

	log.Printf("qq inbound connected: %s", gatewayURL)
	s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
		st.Connected = true
		st.GatewayURL = gatewayURL
		st.LastConnectedAt = nowISO()
		st.LastError = ""
	})

	var writeMu sync.Mutex
	writeJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(qqInboundWriteTimeout)); err != nil {
			return err
		}
		return conn.WriteJSON(v)
	}

	var (
		seqMu           sync.RWMutex
		lastSeq         *int
		heartbeatCancel context.CancelFunc
	)
	setSeq := func(v int) {
		seqMu.Lock()
		defer seqMu.Unlock()
		next := v
		lastSeq = &next
	}
	getSeq := func() interface{} {
		seqMu.RLock()
		defer seqMu.RUnlock()
		if lastSeq == nil {
			return nil
		}
		return *lastSeq
	}
	stopHeartbeat := func() {
		if heartbeatCancel != nil {
			heartbeatCancel()
			heartbeatCancel = nil
		}
	}
	defer stopHeartbeat()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(qqInboundReadTimeout)); err != nil {
			return err
		}
		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var frame qqGatewayFrame
		if err := json.Unmarshal(message, &frame); err != nil {
			continue
		}
		if frame.S != nil {
			setSeq(*frame.S)
		}

		switch frame.Op {
		case qqGatewayOpHello:
			interval := parseQQHeartbeatInterval(frame.D)
			if err := writeJSON(map[string]interface{}{
				"op": qqGatewayOpIdentify,
				"d": map[string]interface{}{
					"token":   "QQBot " + token,
					"intents": cfg.Intents,
					"shard":   []int{0, 1},
				},
			}); err != nil {
				return fmt.Errorf("send qq identify failed: %w", err)
			}
			stopHeartbeat()
			heartbeatCtx, cancel := context.WithCancel(ctx)
			heartbeatCancel = cancel
			go runQQHeartbeatLoop(heartbeatCtx, interval, getSeq, writeJSON)
		case qqGatewayOpDispatch:
			if !isQQInboundDispatchEvent(frame.T) {
				continue
			}
			raw, err := json.Marshal(map[string]interface{}{
				"t": frame.T,
				"d": json.RawMessage(frame.D),
			})
			if err != nil {
				continue
			}
			if shouldIgnoreQQInboundEvent(raw) {
				continue
			}
			accepted, reason, err := s.dispatchQQInboundPayload(ctx, raw)
			if err != nil {
				log.Printf("qq inbound dispatch failed: event=%s err=%v", frame.T, err)
				s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
					st.LastError = fmt.Sprintf("dispatch %s failed: %v", frame.T, err)
					st.LastErrorAt = nowISO()
				})
				continue
			}
			if !accepted && reason != "" {
				log.Printf("qq inbound ignored: event=%s reason=%s", frame.T, reason)
				s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
					st.LastEventType = frame.T
					st.LastEventAt = nowISO()
				})
				continue
			}
			s.mutateQQInboundState(func(st *qqInboundRuntimeState) {
				st.LastEventType = frame.T
				st.LastEventAt = nowISO()
			})
		case qqGatewayOpReconnect:
			return fmt.Errorf("qq gateway requested reconnect")
		case qqGatewayOpInvalidSession:
			return errQQInboundInvalidSession
		}
	}
}

var errQQInboundInvalidSession = errors.New("qq gateway invalid session")

func runQQHeartbeatLoop(
	ctx context.Context,
	interval time.Duration,
	getSeq func() interface{},
	writeJSON func(interface{}) error,
) {
	if interval <= 0 {
		interval = 45 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writeJSON(map[string]interface{}{
				"op": qqGatewayOpHeartbeat,
				"d":  getSeq(),
			}); err != nil {
				log.Printf("qq heartbeat failed: %v", err)
				return
			}
		}
	}
}

func fetchQQGatewayAccessToken(ctx context.Context, cfg qqInboundConfig) (string, error) {
	body, err := json.Marshal(map[string]string{
		"appId":        cfg.AppID,
		"clientSecret": cfg.ClientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("marshal qq token request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build qq token request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request qq token failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read qq token response failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("qq token endpoint returned status %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", fmt.Errorf("decode qq token response failed: %w", err)
	}
	token := strings.TrimSpace(qqString(payload["access_token"]))
	if token == "" {
		return "", fmt.Errorf("qq token response missing access_token")
	}
	return token, nil
}

func fetchQQGatewayURL(ctx context.Context, cfg qqInboundConfig, token string) (string, error) {
	url := strings.TrimRight(cfg.APIBase, "/") + "/gateway"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build qq gateway request failed: %w", err)
	}
	req.Header.Set("Authorization", "QQBot "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request qq gateway url failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read qq gateway response failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("qq gateway endpoint returned status %d", resp.StatusCode)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", fmt.Errorf("decode qq gateway response failed: %w", err)
	}
	gatewayURL := strings.TrimSpace(qqString(payload["url"]))
	if gatewayURL == "" {
		return "", fmt.Errorf("qq gateway response missing url")
	}
	return gatewayURL, nil
}

func parseQQHeartbeatInterval(raw json.RawMessage) time.Duration {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 45 * time.Second
	}
	intervalMS := 45000
	switch v := payload["heartbeat_interval"].(type) {
	case float64:
		if v > 0 {
			intervalMS = int(v)
		}
	case int:
		if v > 0 {
			intervalMS = v
		}
	}
	return time.Duration(intervalMS) * time.Millisecond
}

func parseQQIntents(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return int(v), true
		}
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 {
			return int(v), true
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func isQQInboundDispatchEvent(event string) bool {
	switch strings.ToUpper(strings.TrimSpace(event)) {
	case "C2C_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE", "AT_MESSAGE_CREATE", "DIRECT_MESSAGE_CREATE":
		return true
	default:
		return false
	}
}

func shouldIgnoreQQInboundEvent(raw []byte) bool {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	d, ok := qqInboundMap(payload["d"])
	if !ok {
		return false
	}
	author, ok := qqInboundMap(d["author"])
	if !ok {
		return false
	}
	bot, _ := author["bot"].(bool)
	return bot
}

func qqInboundMap(raw interface{}) (map[string]interface{}, bool) {
	value, ok := raw.(map[string]interface{})
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

func (s *Server) dispatchQQInboundPayload(ctx context.Context, payload []byte) (accepted bool, reason string, err error) {
	req := httptest.NewRequest(http.MethodPost, "/channels/qq/inbound", bytes.NewReader(payload)).WithContext(ctx)
	rec := httptest.NewRecorder()
	s.processQQInbound(rec, req)
	if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
		return false, "", fmt.Errorf("qq inbound handler status=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return true, "", nil
	}
	rawAccepted, hasAccepted := body["accepted"]
	if !hasAccepted {
		return true, "", nil
	}
	if acceptedValue, ok := rawAccepted.(bool); ok {
		if !acceptedValue {
			return false, strings.TrimSpace(qqString(body["reason"])), nil
		}
		return true, "", nil
	}
	return true, "", nil
}
