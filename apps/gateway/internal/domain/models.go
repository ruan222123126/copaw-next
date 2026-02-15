package domain

type APIErrorBody struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type ChatSpec struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Channel   string                 `json:"channel"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
	Meta      map[string]interface{} `json:"meta"`
}

type RuntimeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type RuntimeMessage struct {
	ID       string                 `json:"id,omitempty"`
	Role     string                 `json:"role,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Content  []RuntimeContent       `json:"content,omitempty"`
}

type ChatHistory struct {
	Messages []RuntimeMessage `json:"messages"`
}

type AgentInputMessage struct {
	Role    string           `json:"role"`
	Type    string           `json:"type"`
	Content []RuntimeContent `json:"content"`
}

type AgentProcessRequest struct {
	Input     []AgentInputMessage    `json:"input"`
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Channel   string                 `json:"channel"`
	Stream    bool                   `json:"stream"`
	BizParams map[string]interface{} `json:"biz_params,omitempty"`
}

type CronScheduleSpec struct {
	Type     string `json:"type"`
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

type CronDispatchTarget struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

type CronDispatchSpec struct {
	Type    string                 `json:"type"`
	Channel string                 `json:"channel"`
	Target  CronDispatchTarget     `json:"target"`
	Mode    string                 `json:"mode"`
	Meta    map[string]interface{} `json:"meta"`
}

type CronRuntimeSpec struct {
	MaxConcurrency      int `json:"max_concurrency"`
	TimeoutSeconds      int `json:"timeout_seconds"`
	MisfireGraceSeconds int `json:"misfire_grace_seconds"`
}

type CronJobSpec struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Enabled  bool                   `json:"enabled"`
	Schedule CronScheduleSpec       `json:"schedule"`
	TaskType string                 `json:"task_type"`
	Text     string                 `json:"text,omitempty"`
	Request  map[string]interface{} `json:"request,omitempty"`
	Dispatch CronDispatchSpec       `json:"dispatch"`
	Runtime  CronRuntimeSpec        `json:"runtime"`
	Meta     map[string]interface{} `json:"meta"`
}

type CronJobState struct {
	NextRunAt  *string `json:"next_run_at,omitempty"`
	LastRunAt  *string `json:"last_run_at,omitempty"`
	LastStatus *string `json:"last_status,omitempty"`
	LastError  *string `json:"last_error,omitempty"`
}

type CronJobView struct {
	Spec  CronJobSpec  `json:"spec"`
	State CronJobState `json:"state"`
}

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProviderInfo struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	APIKeyPrefix       string      `json:"api_key_prefix"`
	Models             []ModelInfo `json:"models"`
	AllowCustomBaseURL bool        `json:"allow_custom_base_url"`
	HasAPIKey          bool        `json:"has_api_key"`
	CurrentAPIKey      string      `json:"current_api_key"`
	CurrentBaseURL     string      `json:"current_base_url"`
}

type ModelSlotConfig struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type ActiveModelsInfo struct {
	ActiveLLM ModelSlotConfig `json:"active_llm"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SkillSpec struct {
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	Source     string                 `json:"source"`
	Path       string                 `json:"path"`
	References map[string]interface{} `json:"references"`
	Scripts    map[string]interface{} `json:"scripts"`
	Enabled    bool                   `json:"enabled"`
}

type ChannelConfigMap map[string]map[string]interface{}
