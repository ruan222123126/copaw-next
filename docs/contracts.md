# API / CLI 最小契约

## API
- /version, /healthz
- /chats, /chats/{chat_id}, /chats/batch-delete
- /agent/process
- /channels/qq/inbound
- /channels/qq/state
- /cron/jobs 系列
- /models 系列
- /envs 系列
- /skills 系列
- /workspace/files, /workspace/files/{file_path}
- /workspace/export, /workspace/import
- /config/channels 系列

### 渠道配置约定（/config/channels）
- 支持类型：`console`、`webhook`、`qq`
- `qq` 推荐字段：`enabled`、`app_id`、`client_secret`、`bot_prefix`、`target_type(c2c/group/guild)`、`target_id`、`api_base`、`token_url`、`timeout_seconds`

### QQ 入站约定（/channels/qq/inbound）
- 接受 QQ 入站事件（支持 `C2C_MESSAGE_CREATE`、`GROUP_AT_MESSAGE_CREATE`、`AT_MESSAGE_CREATE`、`DIRECT_MESSAGE_CREATE` 及兼容化 `message_type` 结构）。
- 网关会将入站文本转换为 `channel=qq` 的内部 `/agent/process` 请求并自动回发。
- 回发目标按事件动态覆盖 `target_type/target_id`，无需写死在全局配置里。

## CLI
- nextai app start
- nextai chats list/create/get/delete/send
- nextai cron list/create/update/delete/pause/resume/run/state
- nextai models list/config/active-get/active-set
- nextai env list/set/delete
- nextai skills list/create/enable/disable/delete
- nextai workspace ls/cat/put/rm/export/import
- nextai channels list/types/get/set
- nextai tui

## /agent/process 多步 Agent 协议

`POST /agent/process` 支持两种模式：

1. 常规对话（模型自治多步）
2. 显式工具调用（推荐顶层 `view/edit/shell`，兼容 `biz_params.tool`；三者的值均为对象数组，单次操作也需传 1 个元素）

特殊指令约定：

- 当用户文本输入为 `/new`（忽略前后空白）时，Gateway 不调用模型，直接清理当前 `session_id + user_id + channel` 对应会话历史，并返回确认回复（流式/非流式均适用）。
- `channel` 字段在 `/agent/process` 中为可选；若请求未显式传值则默认 `console`。QQ 入站路径固定使用 `channel=qq`。

工具启用策略：

- 默认注册工具可用。
- 通过环境变量 `NEXTAI_DISABLED_TOOLS`（逗号分隔，如 `shell,edit`）按名称禁用工具。
- 当调用被禁用工具时，返回 `403` 与错误码 `tool_disabled`。

请求示例：

```json
{
  "input": [
    {
      "role": "user",
      "type": "message",
      "content": [{ "type": "text", "text": "请读取配置并给出结论" }]
    }
  ],
  "session_id": "s1",
  "user_id": "u1",
  "channel": "console",
  "stream": true
}
```

`stream=false` 返回：

```json
{
  "reply": "最终回复文本",
  "events": [
    { "type": "step_started", "step": 1 },
    { "type": "tool_call", "step": 1, "tool_call": { "name": "shell" } },
    { "type": "tool_result", "step": 1, "tool_result": { "name": "shell", "ok": true, "summary": "..." } },
    { "type": "assistant_delta", "step": 2, "delta": "..." },
    { "type": "completed", "step": 2, "reply": "最终回复文本" }
  ]
}
```

`stream=true` 返回 SSE，`data` payload 与上面 `events` 同构；事件在执行过程中实时推送（每个事件写出后立即 flush），并以 `data: [DONE]` 结束。  
其中常规对话的 `assistant_delta` 在 OpenAI-compatible 适配器下透传上游原生 token/delta（不再由 Gateway 按字符二次切片模拟）。若流式处理中途失败，额外发送 `{"type":"error","meta":{"code","message"}}` 后结束。

事件类型：

- `step_started`
- `tool_call`
- `tool_result`
- `assistant_delta`
- `completed`
- `error`（仅流式失败场景）
