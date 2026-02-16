# CoPaw Next TODO

更新时间：2026-02-16 10:37:57 +0800

## 执行约定（强制）
- 每位接手 AI 开始前，必须先阅读本文件与 `/home/ruan/.codex/handoff/latest.md`。
- 执行顺序优先遵循交接文件“接手建议（按顺序）”，并与本文件未完成项对齐推进。
- 每次执行后必须更新本文件：勾选完成项、记录阻塞原因、刷新“更新时间”。

## 0. 目标范围（v1）
- 以 `copaw-local` 功能边界为目标。
- 以 `openclaw` 的工程方法为基线（测试分层、CI 分层、契约优先、CLI 与 Gateway 分离）。

## 1. 基础工程与规范
- [x] 创建 Monorepo 基础结构（`apps/`、`packages/`、`tests/`）。
- [x] 落地 `AGENTS.md` 规范文件。
- [x] 统一包管理为 pnpm（仅保留 `pnpm-lock.yaml`）。
- [x] 补充根目录 `README.md`、`Makefile`、`.env.example`。
- [x] 增加 `CONTRIBUTING.md`（分支策略、PR 模板、提交规范）。
- [x] 增加 `SECURITY.md`（漏洞提交流程、密钥管理策略）。

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
- [x] 实现真实 LLM provider 适配（支持 OpenAI，默认 demo 兜底）。
- [x] 实现真实 cron 调度器（常驻调度 + 重启恢复，interval 模式）。
- [x] 实现多渠道插件（已支持 `console` + `webhook`，含渠道配置与派发）。
- [x] 增加鉴权中间件（当前默认本地无鉴权）。
- [x] 模型提供能力升级：新增 provider catalog、alias 解析、provider 启用开关、openai-compatible adapter 抽象（含 `/models/catalog`）。

## 3. CLI（TypeScript）
- [x] CLI 入口与命令注册。
- [x] API Client 封装（禁止直连业务存储）。
- [x] 命令：`app/chats/cron/models/env/skills/workspace/channels` 最小集。
- [x] `chats send --stream` 流式输出能力。
- [x] 优化错误输出（按 error.code 分类提示）。
- [x] 增加 `--json` 全局开关与机器可读输出模式。
- [x] 增加命令级 e2e（当前仅 smoke）。
- [x] 增加 CLI 多语言支持（`zh-CN`/`en-US`，支持 `--locale` 与 `COPAW_LOCALE`）。
- [x] 扩展 `models config` 参数（`enabled/timeout_ms/headers/model_aliases`）并补齐 alias/custom provider e2e 链路。

## 4. Contracts（契约）
- [x] 建立 `packages/contracts/openapi/openapi.yaml`。
- [x] 建立契约测试（路径覆盖检查）。
- [x] 建立最小 TS/JSONSchema 占位定义。
- [x] 为关键请求/响应补全 schema 细节（字段级 required/enum/format）。
- [x] 增加 OpenAPI lint（等价 lint 工具已接入 `tests/contract/openapi.lint.mjs`）。
- [x] 增加基于 OpenAPI 的 SDK 自动生成流程。
- [x] 补齐 models 系列接口 schema（`/models`、`/models/catalog`、`/models/{provider_id}/config`、`/models/active`）。

## 5. 测试与质量
- [x] Go 单测：runner。
- [x] Go 集成测：chat 与 workspace 安全关键路径。
- [x] CLI 单测：api-client。
- [x] 契约测试：openapi 关键路径存在性。
- [x] 端到端烟测：gateway + cli（chat 与 cron 基本闭环）。
- [x] 增加 Gateway e2e：SSE 边界、错误码一致性、并发行为。
- [x] 增加 CLI e2e：所有主命令成功/失败场景。
- [x] 增加覆盖率基线（Go/TS）。

## 6. CI / CD
- [x] `ci-fast.yml`（PR 快检模板）。
- [x] `ci-full.yml`（主分支全检模板）。
- [x] `nightly-live.yml`（夜间 live 占位模板）。
- [x] 将 CI 改为真实分层门禁（非占位命令）。
- [x] 增加缓存策略与失败重试策略。
- [x] 增加发布流水线（tag -> artifact -> release notes）。

## 7. Web（控制台）
- [x] 建立 `apps/web` 占位工程。
- [x] 实现 Chat 页面（会话列表 + 消息区 + 发送 + SSE 展示）。
- [x] 实现 Models/Envs/Skills/Workspace/Cron 页面最小操作面板。
- [x] 增加前端 API 错误处理与提示规范。
- [x] 增加前端测试（单测 + 冒烟）。
- [x] 完成 Web 控制台中文化（界面文案、错误提示、时间/日期展示统一为中文语境）。
- [x] 增加 Web 控制台多语言支持（`zh-CN`/`en-US`，界面切换与本地持久化）。
- [x] Models 面板接入 `/models/catalog`（含 `/models` 回退），展示 default/alias/capabilities 关键信息。

## 8. 文档与交付
- [x] `docs/v1-roadmap.md`。
- [x] `docs/contracts.md`。
- [x] 增加本地开发文档（Gateway/CLI/Web 并行开发流程）。
- [x] 增加部署文档（systemd/docker）。
- [x] 增加版本发布说明模板（`v0.1.0-rc.1`）。

## 9. 我已经完成的实操验证
- [x] `go test ./...`（Gateway 通过）。
- [x] `make gateway-coverage`（Go 覆盖率门禁通过，当前总覆盖约 49.9%，阈值 45%）。
- [x] `cd apps/cli && pnpm run test:coverage`（CLI 覆盖率门禁通过，阈值：statements/lines 55%、functions 50%、branches 40%）。
- [x] `pnpm -r lint`（通过）。
- [x] `pnpm --filter @copaw-next/web run lint`（通过）。
- [x] `pnpm --filter @copaw-next/web run test`（通过，单测+冒烟共 7 个用例）。
- [x] `pnpm --filter @copaw-next/web run test`（通过，单测+冒烟共 12 个用例，含 i18n）。
- [x] `pnpm -r test`（通过）。
- [x] `pnpm -r build`（通过）。
- [x] `pnpm -r test && pnpm -r build`（通过，2026-02-16 10:37:57 +0800 复跑）。
- [x] `pnpm --filter @copaw-next/web run build`（通过）。
- [x] `pnpm --filter @copaw-next/cli run lint && pnpm --filter @copaw-next/cli run test && pnpm --filter @copaw-next/cli run build`（通过，含 i18n 用例）。
- [x] 实际启动 `COPAW_PORT=18088 go run ./cmd/gateway` 并验证 `/healthz`、`/version`、`/chats`。
- [x] 使用 CLI 跑通 chat + cron 烟测闭环。
- [x] 修复 `apps/gateway/.data/state.json` 空文件导致的启动失败，并成功启动 `go run ./cmd/gateway`（`127.0.0.1:8088`），验证 `/healthz` 与 `/version`。
- [x] 启动 Web 控制台静态服务（`python3 -m http.server 18080 --directory apps/web/dist`），验证 `http://127.0.0.1:18080/` 可访问。
- [x] `cd apps/gateway && go test ./...`（通过，含 provider catalog / alias / custom provider 链路新增测试）。
- [x] `pnpm --filter @copaw-next/tests-contract run lint && pnpm --filter @copaw-next/tests-contract run test`（通过）。
- [x] `pnpm --filter @copaw-next/web run lint && pnpm --filter @copaw-next/web run test && pnpm --filter @copaw-next/web run build`（通过）。
- [x] `pnpm --filter @copaw-next/cli run lint && pnpm --filter @copaw-next/cli run test && pnpm --filter @copaw-next/cli run build`（通过；CLI e2e 现为 4 文件 9 用例）。
- [x] 手工冒烟：启动 mock OpenAI + Gateway，验证 `custom-openai` 的 `models config -> models active-set -> /agent/process` 全链路（通过，回复 `mock reply from fast`）。

## 10. 当前未完成项与阻塞（2026-02-16）
- 当前无阻塞未完成项。
