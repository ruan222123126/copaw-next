# API / CLI 鏈€灏忓绾?

## API
- /version, /healthz
- /runtime-config
- /chats, /chats/{chat_id}, /chats/batch-delete
- /agent/process
- /channels/qq/inbound
- /channels/qq/state
- /cron/jobs 绯诲垪
- /models 绯诲垪
- /envs 绯诲垪
- /skills 绯诲垪
- /workspace/files, /workspace/files/{file_path}
- /workspace/export, /workspace/import
- /config/channels 绯诲垪

### 娓犻亾閰嶇疆绾﹀畾锛?config/channels锛?
- 鏀寔绫诲瀷锛歚console`銆乣webhook`銆乣qq`
- `qq` 鎺ㄨ崘瀛楁锛歚enabled`銆乣app_id`銆乣client_secret`銆乣bot_prefix`銆乣target_type(c2c/group/guild)`銆乣target_id`銆乣api_base`銆乣token_url`銆乣timeout_seconds`

### QQ 鍏ョ珯绾﹀畾锛?channels/qq/inbound锛?
- 鎺ュ彈 QQ 鍏ョ珯浜嬩欢锛堟敮鎸?`C2C_MESSAGE_CREATE`銆乣GROUP_AT_MESSAGE_CREATE`銆乣AT_MESSAGE_CREATE`銆乣DIRECT_MESSAGE_CREATE` 鍙婂吋瀹瑰寲 `message_type` 缁撴瀯锛夈€?
- 缃戝叧浼氬皢鍏ョ珯鏂囨湰杞崲涓?`channel=qq` 鐨勫唴閮?`/agent/process` 璇锋眰骞惰嚜鍔ㄥ洖鍙戙€?
- 鍥炲彂鐩爣鎸変簨浠跺姩鎬佽鐩?`target_type/target_id`锛屾棤闇€鍐欐鍦ㄥ叏灞€閰嶇疆閲屻€?

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

## /agent/process 澶氭 Agent 鍗忚

`POST /agent/process` 鏀寔涓ょ妯″紡锛?

1. 甯歌瀵硅瘽锛堟ā鍨嬭嚜娌诲姝ワ級
2. 鏄惧紡宸ュ叿璋冪敤锛堟帹鑽愰《灞?`view/edit/shell/browser/search`锛屽吋瀹?`biz_params.tool`锛涗笂杩板伐鍏风殑鍊煎潎涓哄璞℃暟缁勶紝鍗曟鎿嶄綔涔熼渶浼?1 涓厓绱狅級

鐗规畩鎸囦护绾﹀畾锛?

- 褰撶敤鎴锋枃鏈緭鍏ヤ负 `/new`锛堝拷鐣ュ墠鍚庣┖鐧斤級鏃讹紝Gateway 涓嶈皟鐢ㄦā鍨嬶紝鐩存帴娓呯悊褰撳墠 `session_id + user_id + channel` 瀵瑰簲浼氳瘽鍘嗗彶锛屽苟杩斿洖纭鍥炲锛堟祦寮?闈炴祦寮忓潎閫傜敤锛夈€?
- `channel` 瀛楁鍦?`/agent/process` 涓负鍙€夛紱鑻ヨ姹傛湭鏄惧紡浼犲€煎垯榛樿 `console`銆俀Q 鍏ョ珯璺緞鍥哄畾浣跨敤 `channel=qq`銆?

宸ュ叿鍚敤绛栫暐锛?

- 榛樿娉ㄥ唽宸ュ叿鍙敤銆?
- 閫氳繃鐜鍙橀噺 `NEXTAI_DISABLED_TOOLS`锛堥€楀彿鍒嗛殧锛屽 `shell,edit`锛夋寜鍚嶇О绂佺敤宸ュ叿銆?
- 褰撹皟鐢ㄨ绂佺敤宸ュ叿鏃讹紝杩斿洖 `403` 涓庨敊璇爜 `tool_disabled`銆?
- 娴忚鍣ㄥ伐鍏烽粯璁ゅ叧闂紱闇€璁剧疆 `NEXTAI_ENABLE_BROWSER_TOOL=true`锛屽苟鎻愪緵 `NEXTAI_BROWSER_AGENT_DIR`锛堟寚鍚?`agent.js` 鎵€鍦ㄧ洰褰曪級鍚庢墠浼氭敞鍐屻€?
- 鎼滅储宸ュ叿榛樿鍏抽棴锛涢渶璁剧疆 `NEXTAI_ENABLE_SEARCH_TOOL=true`銆傛敮鎸佸 provider锛坄serpapi` / `tavily` / `brave`锛夛紝鍚?provider 閫氳繃鐜鍙橀噺閰嶇疆 key锛堝彲閫?base url锛夛細
  - `NEXTAI_SEARCH_SERPAPI_KEY` / `NEXTAI_SEARCH_SERPAPI_BASE_URL`
  - `NEXTAI_SEARCH_TAVILY_KEY` / `NEXTAI_SEARCH_TAVILY_BASE_URL`
  - `NEXTAI_SEARCH_BRAVE_KEY` / `NEXTAI_SEARCH_BRAVE_BASE_URL`

璇锋眰绀轰緥锛?

```json
{
  "input": [
    {
      "role": "user",
      "type": "message",
      "content": [{ "type": "text", "text": "璇疯鍙栭厤缃苟缁欏嚭缁撹" }]
    }
  ],
  "session_id": "s1",
  "user_id": "u1",
  "channel": "console",
  "stream": true
}
```

`stream=false` 杩斿洖锛?

```json
{
  "reply": "鏈€缁堝洖澶嶆枃鏈?,
  "events": [
    { "type": "step_started", "step": 1 },
    { "type": "tool_call", "step": 1, "tool_call": { "name": "shell" } },
    { "type": "tool_result", "step": 1, "tool_result": { "name": "shell", "ok": true, "summary": "..." } },
    { "type": "assistant_delta", "step": 2, "delta": "..." },
    { "type": "completed", "step": 2, "reply": "鏈€缁堝洖澶嶆枃鏈? }
  ]
}
```

`stream=true` 杩斿洖 SSE锛宍data` payload 涓庝笂闈?`events` 鍚屾瀯锛涗簨浠跺湪鎵ц杩囩▼涓疄鏃舵帹閫侊紙姣忎釜浜嬩欢鍐欏嚭鍚庣珛鍗?flush锛夛紝骞朵互 `data: [DONE]` 缁撴潫銆? 
鍏朵腑甯歌瀵硅瘽鐨?`assistant_delta` 鍦?OpenAI-compatible 閫傞厤鍣ㄤ笅閫忎紶涓婃父鍘熺敓 token/delta锛堜笉鍐嶇敱 Gateway 鎸夊瓧绗︿簩娆″垏鐗囨ā鎷燂級銆傝嫢娴佸紡澶勭悊涓€斿け璐ワ紝棰濆鍙戦€?`{"type":"error","meta":{"code","message"}}` 鍚庣粨鏉熴€?

浜嬩欢绫诲瀷锛?

- `step_started`
- `tool_call`
- `tool_result`
- `assistant_delta`
- `completed`
- `error`锛堜粎娴佸紡澶辫触鍦烘櫙锛?

## Chat Default Session Rule
- Gateway always keeps one protected default chat in state (`id=chat-default`).
- Default chat baseline fields: `session_id=session-default`, `user_id=demo-user`, `channel=console`.
- Default chat carries `meta.system_default=true`.
- `DELETE /chats/{chat_id}` and `POST /chats/batch-delete` reject deleting `chat-default` with `400 default_chat_protected`.

## Cron Default Job Rule
- Gateway always keeps one protected default cron job in state (`id=cron-default`).
- Default cron job baseline fields: `name=浣犲ソ鏂囨湰浠诲姟`, `task_type=text`, `text=浣犲ソ`, `enabled=false`.
- `DELETE /cron/jobs/{job_id}` rejects deleting `cron-default` with `400 default_cron_protected`.

## Prompt Layering And Template Rollout (2026-02)

### Phase 1: system layers (no external behavior change)
- Gateway keeps `/agent/process` request/response contract unchanged.
- Internal prompt injection changes from a single `system` message to ordered `system` layers:
  1. `base_system`
  2. `tool_guide_system`
  3. `workspace_policy_system`
  4. `session_policy_system`
- Injection position is unchanged (still prepended before model generation loop).

### Phase 2: `/prompts:<name>` command expansion (client side)
- Template source is `prompts/*.md`.
- Web and TUI expand `/prompts:<name>` before sending to `/agent/process`.
- Phase 2 only supports named args: `KEY=VALUE`.
- Expansion failure blocks sending and returns a client-side error.
- Existing UI slash commands (`/history`, `/new`, `/refresh`, `/settings`, `/exit`) keep current behavior.

### Phase 3: environment context and observability
- Gateway adds a structured `environment_context` as an independent `system` layer when feature flag is enabled.
- New read-only endpoint:
  - `GET /agent/system-layers`
  - Purpose: return effective injected layers and token estimate used for this runtime.

Sample response:

```json
{
  "version": "v1",
  "layers": [
    {
      "name": "base_system",
      "role": "system",
      "source": "docs/AI/AGENTS.md",
      "content_preview": "## docs/AI/AGENTS.md ...",
      "estimated_tokens": 12
    }
  ],
  "estimated_tokens_total": 12
}
```

- Error model remains unchanged:
  - `{ "error": { "code": "...", "message": "...", "details": ... } }`

### Feature flags
- `NEXTAI_ENABLE_PROMPT_TEMPLATES` (default: `false`).
- `NEXTAI_ENABLE_PROMPT_CONTEXT_INTROSPECT` (default: `false`).

## Runtime Config Endpoint (2026-02)
- Gateway 提供公开只读接口（不含敏感信息）：`GET /runtime-config`。
- 返回体：

```json
{
  "features": {
    "prompt_templates": false,
    "prompt_context_introspect": false
  }
}
```

- 字段来源：
  - `features.prompt_templates` <- `NEXTAI_ENABLE_PROMPT_TEMPLATES`
  - `features.prompt_context_introspect` <- `NEXTAI_ENABLE_PROMPT_CONTEXT_INTROSPECT`
- Web 侧特性开关优先级：`query > localStorage > runtime-config > false`。

## Prompt Mode（会话级，2026-02）
- `POST /agent/process` 支持可选字段：`biz_params.prompt_mode`。
- 枚举值：`default` | `codex`。
- 非法值（包含非字符串或不在枚举内）返回：
  - `400 invalid_request`
  - `message=invalid prompt_mode`

### 会话持久化规则
- 会话元数据新增：`chat.meta.prompt_mode`。
- 若请求显式携带 `biz_params.prompt_mode`，Gateway 会写回并持久化到该会话 `meta.prompt_mode`。
- 若请求未携带 `biz_params.prompt_mode`，执行时按以下优先级解析有效模式：
  1. 请求显式值
  2. 会话 `meta.prompt_mode`
  3. `default`

### 系统层注入规则
- `prompt_mode=default`：
  - 维持原行为（`AGENTS + ai-tools` 分层注入）。
- `prompt_mode=codex`：
  - 仅注入 `prompts/codex/codex-rs/core/prompt.md`。
  - 不再叠加默认 `AGENTS/ai-tools` 系统层。

### 错误语义
- `prompt_mode=codex` 且 codex base 文件缺失或为空时返回：
  - `500 codex_prompt_unavailable`
  - `message=codex prompt is unavailable`
- `prompt_mode=default` 继续沿用既有系统层错误语义（如 `ai_tool_guide_unavailable`）。
