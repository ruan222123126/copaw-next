package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultQQAPIBase    = "https://api.sgroup.qq.com"
	defaultQQTokenURL   = "https://bots.qq.com/app/getAppAccessToken"
	defaultQQTimeout    = 8 * time.Second
	defaultQQExpiresIn  = 7200
	qqTokenRefreshAhead = 5 * time.Minute
	qqMessageSeqLimit   = 1000
	qqMessageSeqTrimTo  = 500
)

type QQChannel struct {
	mu           sync.Mutex
	token        string
	tokenExpire  time.Time
	tokenCacheID string
	messageSeq   map[string]int
}

func NewQQChannel() *QQChannel {
	return &QQChannel{
		messageSeq: map[string]int{},
	}
}

func (c *QQChannel) Name() string {
	return "qq"
}

func (c *QQChannel) SendText(ctx context.Context, userID, _ string, text string, cfg map[string]interface{}) error {
	appID := strings.TrimSpace(toString(cfg["app_id"]))
	if appID == "" {
		return fmt.Errorf("channel qq requires config.app_id")
	}
	clientSecret := strings.TrimSpace(toString(cfg["client_secret"]))
	if clientSecret == "" {
		return fmt.Errorf("channel qq requires config.client_secret")
	}

	content := strings.TrimSpace(text)
	if content == "" {
		return nil
	}
	if prefix := toString(cfg["bot_prefix"]); prefix != "" {
		content = prefix + content
	}

	targetType := normalizeQQTargetType(cfg["target_type"])
	targetID := strings.TrimSpace(toString(cfg["target_id"]))
	if targetID == "" && targetType == "c2c" {
		targetID = strings.TrimSpace(userID)
	}
	if targetID == "" {
		return fmt.Errorf("channel qq requires config.target_id for target_type %q", targetType)
	}

	timeout := toDurationSeconds(cfg["timeout_seconds"], defaultQQTimeout)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tokenURL := strings.TrimSpace(toString(cfg["token_url"]))
	if tokenURL == "" {
		tokenURL = defaultQQTokenURL
	}
	token, err := c.getAccessToken(requestCtx, appID, clientSecret, tokenURL)
	if err != nil {
		return err
	}

	msgID := strings.TrimSpace(toString(cfg["msg_id"]))
	body := map[string]interface{}{
		"content": content,
	}
	if msgID != "" {
		body["msg_id"] = msgID
	}

	path := ""
	switch targetType {
	case "group":
		body["msg_type"] = 0
		body["msg_seq"] = c.nextMessageSeq(targetType, targetID, msgID)
		path = "/v2/groups/" + targetID + "/messages"
	case "guild":
		path = "/channels/" + targetID + "/messages"
	default:
		body["msg_type"] = 0
		body["msg_seq"] = c.nextMessageSeq(targetType, targetID, msgID)
		path = "/v2/users/" + targetID + "/messages"
	}

	baseURL := strings.TrimRight(strings.TrimSpace(toString(cfg["api_base"])), "/")
	if baseURL == "" {
		baseURL = defaultQQAPIBase
	}

	if err := sendQQAPIRequest(requestCtx, token, baseURL+path, body); err != nil {
		return err
	}
	return nil
}

func normalizeQQTargetType(raw interface{}) string {
	switch strings.ToLower(strings.TrimSpace(toString(raw))) {
	case "group":
		return "group"
	case "guild", "channel":
		return "guild"
	default:
		return "c2c"
	}
}

func (c *QQChannel) nextMessageSeq(targetType, targetID, msgID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.messageSeq == nil {
		c.messageSeq = map[string]int{}
	}

	key := strings.TrimSpace(msgID)
	if key == "" {
		key = targetType + ":" + targetID
	}
	next := c.messageSeq[key] + 1
	c.messageSeq[key] = next

	if len(c.messageSeq) > qqMessageSeqLimit {
		for existing := range c.messageSeq {
			delete(c.messageSeq, existing)
			if len(c.messageSeq) <= qqMessageSeqTrimTo {
				break
			}
		}
	}
	return next
}

func (c *QQChannel) getAccessToken(ctx context.Context, appID, clientSecret, tokenURL string) (string, error) {
	cacheID := appID + "\n" + clientSecret + "\n" + tokenURL

	c.mu.Lock()
	if c.token != "" && c.tokenCacheID == cacheID && time.Now().Before(c.tokenExpire.Add(-qqTokenRefreshAhead)) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	body, err := json.Marshal(map[string]string{
		"appId":        appID,
		"clientSecret": clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("marshal qq token request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
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

	token := strings.TrimSpace(toString(payload["access_token"]))
	if token == "" {
		return "", fmt.Errorf("qq token response missing access_token")
	}
	expiresIn := parseQQExpiresIn(payload["expires_in"])
	expireAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	c.mu.Lock()
	c.token = token
	c.tokenCacheID = cacheID
	c.tokenExpire = expireAt
	c.mu.Unlock()

	return token, nil
}

func parseQQExpiresIn(raw interface{}) int {
	switch value := raw.(type) {
	case float64:
		if value > 0 {
			return int(value)
		}
	case int:
		if value > 0 {
			return value
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultQQExpiresIn
}

func sendQQAPIRequest(ctx context.Context, accessToken, endpoint string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal qq payload failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build qq request failed: %w", err)
	}
	req.Header.Set("Authorization", "QQBot "+strings.TrimSpace(accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send qq request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	bodyText := strings.TrimSpace(string(respBody))
	if bodyText == "" {
		return fmt.Errorf("qq api returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("qq api returned status %d: %s", resp.StatusCode, bodyText)
}
