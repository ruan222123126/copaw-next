# Smoke Tests

真实 e2e 场景：
- 启动 Gateway（临时端口 + 临时数据目录）
- 创建 chat
- 调用 `/agent/process`（`stream: true`）并解析 SSE
- 校验 `/chats` 与 `/chats/{chat_id}` 历史闭环

运行：

```bash
cd tests/smoke
pnpm test
```

夜间 live（真实 provider）：

```bash
NEXTAI_LIVE_OPENAI_API_KEY=... pnpm test -- --test-name-pattern "nightly live provider chain"
```
