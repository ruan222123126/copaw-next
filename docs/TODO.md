# CoPaw Next TODO

更新时间：2026-02-15

## 0. 目标范围（v1）
- 以 `copaw-local` 功能边界为目标。
- 以 `openclaw` 的工程方法为基线（测试分层、CI 分层、契约优先、CLI 与 Gateway 分离）。

## 1. 基础工程与规范
- [x] 创建 Monorepo 基础结构（`apps/`、`packages/`、`tests/`）。
- [x] 落地 `AGENTS.md` 规范文件。
- [x] 统一包管理为 pnpm（仅保留 `pnpm-lock.yaml`）。
- [x] 补充根目录 `README.md`、`Makefile`、`.env.example`。
- [ ] 增加 `CONTRIBUTING.md`（分支策略、PR 模板、提交规范）。
- [ ] 增加 `SECURITY.md`（漏洞提交流程、密钥管理策略）。

## 2. Gateway（Go）
- [x] 网关启动入口与配置加载（`COPAW_HOST`、`COPAW_PORT`、`COPAW_DATA_DIR`）。
- [x] 基础接口：`/healthz`、`/version`。
- [x] 聊天接口：`/chats`、`/chats/{chat_id}`、`/chats/batch-delete`。
- [x] Agent 接口：`/agent/process`（支持流式 SSE 输出）。
- [x] Cron 接口：`/cron/jobs*` 全套最小端点。
- [x] 模型接口：`/models`、`/models/{provider_id}/config`、`/models/active`。
- [x] 环境变量接口：`/envs`、`/envs/{key}`。
- [x] 技能接口：`/skills*`（含 enable/disable/batch/files）。
- [x] 工作区接口：`/workspace/download`、`/workspace/upload`。
- [x] 渠道配置接口：`/config/channels*`。
- [x] 统一错误模型：`{ error: { code, message, details } }`。
- [x] 请求日志与 `X-Request-Id` 中间件。
- [x] 工作区上传 zip 路径穿越拦截。
- [ ] 实现真实 LLM provider 适配（当前为 demo echo）。
- [ ] 实现真实 cron 调度器（当前为 API 驱动手动 run）。
- [ ] 实现多渠道插件（当前仅 console 可用）。
- [ ] 增加鉴权中间件（当前默认本地无鉴权）。

## 3. CLI（TypeScript）
- [x] CLI 入口与命令注册。
- [x] API Client 封装（禁止直连业务存储）。
- [x] 命令：`app/chats/cron/models/env/skills/workspace/channels` 最小集。
- [x] `chats send --stream` 流式输出能力。
- [ ] 优化错误输出（按 error.code 分类提示）。
- [ ] 增加 `--json` 全局开关与机器可读输出模式。
- [ ] 增加命令级 e2e（当前仅 smoke）。

## 4. Contracts（契约）
- [x] 建立 `packages/contracts/openapi/openapi.yaml`。
- [x] 建立契约测试（路径覆盖检查）。
- [x] 建立最小 TS/JSONSchema 占位定义。
- [ ] 为关键请求/响应补全 schema 细节（字段级 required/enum/format）。
- [ ] 增加 OpenAPI lint（spectral 或等价工具）。
- [ ] 增加基于 OpenAPI 的 SDK 自动生成流程。

## 5. 测试与质量
- [x] Go 单测：runner。
- [x] Go 集成测：chat 与 workspace 安全关键路径。
- [x] CLI 单测：api-client。
- [x] 契约测试：openapi 关键路径存在性。
- [x] 端到端烟测：gateway + cli（chat 与 cron 基本闭环）。
- [ ] 增加 Gateway e2e：SSE 边界、错误码一致性、并发行为。
- [ ] 增加 CLI e2e：所有主命令成功/失败场景。
- [ ] 增加覆盖率基线（Go/TS）。

## 6. CI / CD
- [x] `ci-fast.yml`（PR 快检模板）。
- [x] `ci-full.yml`（主分支全检模板）。
- [x] `nightly-live.yml`（夜间 live 占位模板）。
- [ ] 将 CI 改为真实分层门禁（非占位命令）。
- [ ] 增加缓存策略与失败重试策略。
- [ ] 增加发布流水线（tag -> artifact -> release notes）。

## 7. Web（控制台）
- [x] 建立 `apps/web` 占位工程。
- [ ] 实现 Chat 页面（会话列表 + 消息区 + 发送 + SSE 展示）。
- [ ] 实现 Models/Envs/Skills/Workspace/Cron 页面最小操作面板。
- [ ] 增加前端 API 错误处理与提示规范。
- [ ] 增加前端测试（单测 + 冒烟）。

## 8. 文档与交付
- [x] `docs/v1-roadmap.md`。
- [x] `docs/contracts.md`。
- [ ] 增加本地开发文档（Gateway/CLI/Web 并行开发流程）。
- [ ] 增加部署文档（systemd/docker）。
- [ ] 增加版本发布说明模板（`v0.1.0-rc.1`）。

## 9. 我已经完成的实操验证
- [x] `go test ./...`（Gateway 通过）。
- [x] `pnpm -r lint`（通过）。
- [x] `pnpm -r test`（通过）。
- [x] `pnpm -r build`（通过）。
- [x] 实际启动 `COPAW_PORT=18088 go run ./cmd/gateway` 并验证 `/healthz`、`/version`、`/chats`。
- [x] 使用 CLI 跑通 chat + cron 烟测闭环。

## 10. 下一位 AI 开始前的第一动作
- [ ] 先读 `docs/HANDOFF.md`，按“接手建议（按顺序）”推进。
