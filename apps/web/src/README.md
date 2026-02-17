# Web Control Console

Current scope:
- Chat tab
  - Session list (`GET /chats`)
  - Chat history (`GET /chats/{chat_id}`)
  - Streaming send (`POST /agent/process` with SSE parsing)
  - Tool call raw payload in chat message (`tool_call` -> 原始 SSE JSON 文本)
- Models tab
  - Providers catalog (`GET /models/catalog`, fallback to `GET /models`)
  - Active model view (`GET /models/active`)
  - Set active model (`PUT /models/active`)
- Channels tab
  - QQ channel config (`GET/PUT /config/channels/qq`)
- Config tab
  - File list (`GET /workspace/files`)
  - File read/edit/save/delete (`GET/PUT/DELETE /workspace/files/{file_path}`)
  - JSON import (`POST /workspace/import`)
- Cron tab
  - Jobs list (`GET /cron/jobs` + optional `GET /cron/jobs/{id}/state` for `next_run_at`)
  - Create interval text job (`POST /cron/jobs`)
  - Manual run (`POST /cron/jobs/{id}/run`)

Notes:
- One shared status area handles all panel feedback.
- Error parsing prefers `{ error: { code, message } }`.
- API Base / User ID / Channel controls are shared by all tabs.
- Locale supports `zh-CN` and `en-US` via top-bar selector and persists with `localStorage` key `nextai.web.locale`.

Error handling convention:
- All non-stream requests go through `requestJSON` in `main.ts`.
- Error parsing is centralized in `src/api-utils.ts` via `parseErrorMessage(raw, status)`.
- Error display priority:
  1) `error.code + error.message`
  2) `error.message`
  3) raw response text
  4) fallback `request failed (<status>)`
- UI feedback is centralized through `setStatus(message, tone)` and global `#status-line`.

Tests:
- Unit: `test/unit/api-utils.test.ts` (error parsing and env map parsing)
- Smoke: `test/smoke/shell.test.ts` (critical tabs/panel roots present)

Build output:
- `dist/index.html`
- `dist/styles.css`
- `dist/main.js`
