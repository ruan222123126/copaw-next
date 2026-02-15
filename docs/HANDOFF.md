## 交接摘要
- 任务目标：在 `copaw-next` 落地 v1 可开工骨架，覆盖目录蓝图、最小 API/CLI 契约、4 周路线与第一批测试/CI 模板，并形成规范文件与可执行交接。
- 当前状态：核心骨架已完成并验证可运行；Web 与 live 测试仍为占位；项目目录尚未初始化 git 仓库（`git status` 不可用）。

## 已完成
1. 完成 Monorepo 初始化：`apps/gateway`、`apps/cli`、`apps/web`、`packages/contracts`、`tests/contract`、`.github/workflows`。
2. 完成规范文件 `AGENTS.md`，并补充基础文档 `README.md`、`docs/v1-roadmap.md`、`docs/contracts.md`。
3. 完成 Gateway（Go）最小实现，覆盖：
   `/version`、`/healthz`、`/chats*`、`/agent/process`、`/cron/jobs*`、`/models*`、`/envs*`、`/skills*`、`/workspace*`、`/config/channels*`。
4. 完成统一状态存储（`.data/state.json` 逻辑）与 workspace 存储目录管理。
5. 完成 CLI（TS）最小命令集（`app/chats/cron/models/env/skills/workspace/channels`）。
6. 完成 OpenAPI 契约首版：`packages/contracts/openapi/openapi.yaml`。
7. 完成第一批测试：Go 单测/集成测、CLI 单测、契约测试。
8. 完成 CI 模板：`ci-fast.yml`、`ci-full.yml`、`nightly-live.yml`。
9. 完成实机烟测：起网关 + 通过 CLI 完整验证 chat 与 cron 主流程。
10. 已统一包管理为 pnpm，并移除 `package-lock.json`（仅保留 `pnpm-lock.yaml`）。

## 未完成
1. `apps/web` 仍为占位，尚未实现实际页面与交互。
2. `nightly-live.yml` 仍为占位，未接真实模型验证。
3. Gateway 当前 `runner` 为 echo demo，未接真实模型 provider。
4. Cron 当前为“可管理 + 手动触发”最小能力，未接长期驻留调度器。
5. OpenAPI 仍是最小覆盖版本，尚未完成字段级严格约束与 lint。
6. 项目尚未 `git init`，无法输出 commit/diff 型交接信息。

## 关键改动文件
- `apps/gateway/internal/app/server.go:50`
- `apps/gateway/internal/repo/store.go:37`
- `apps/gateway/internal/runner/runner.go:7`
- `apps/gateway/internal/app/server_test.go:1`
- `apps/cli/src/index.ts:1`
- `apps/cli/src/client/api-client.ts:1`
- `apps/cli/src/commands/chats.ts:1`
- `packages/contracts/openapi/openapi.yaml:1`
- `.github/workflows/ci-fast.yml:1`
- `docs/TODO.md:1`

## 验证命令与结果
1. `cd apps/gateway && go test ./...`
   - 结果：通过（`internal/app`、`internal/runner` 测试通过）。
2. `cd /mnt/Files/copaw-next && pnpm -r lint`
   - 结果：通过。
3. `cd /mnt/Files/copaw-next && pnpm -r test`
   - 结果：通过（CLI + contracts + 占位包）。
4. `cd /mnt/Files/copaw-next && pnpm -r build`
   - 结果：通过。
5. 网关运行冒烟：
   - 启动：`cd apps/gateway && COPAW_PORT=18088 go run ./cmd/gateway`
   - 验证：`/healthz`、`/version`、`POST /chats`、`GET /chats`
   - 结果：通过。
6. CLI 端到端烟测：
   - `node dist/index.js app start`
   - `node dist/index.js chats create ...`
   - `node dist/index.js chats send ...`
   - `node dist/index.js cron create/run/state ...`
   - 结果：通过。
7. handoff 技能脚本：
   - `bash /home/ruan/.codex/skills/handoff/scripts/generate_handoff.sh --verify`
   - 结果：脚本执行成功；因非 git 仓库，git 相关收集项为 N/A/失败信息。

## 风险与注意事项
1. 当前目录不是 git 仓库，后续协作前建议先 `git init` 并首提基线。
2. 默认端口 `8088` 可能与本机已有 `copaw-local` 冲突；建议本地统一使用 `18088`。
3. 运行命令必须单行执行：`go run` 不能被断行拆成 `go` 与 `run` 两条命令。
4. CI workflow 使用 pnpm，请确保 CI 环境启用 corepack 或固定 pnpm 版本。
5. `nightly-live` 目前只是模板，不能代表真实线上可用性。

## 接手建议（按顺序）
1. 初始化 git 仓库并提交当前基线（方便后续 diff、review、handoff 自动化）。
2. 优先实现 Week2 的 Web Chat 页面（会话列表、消息区、SSE 渲染）。
3. 把 `tests/smoke` 补成真实 e2e，纳入 `ci-fast` 非占位门禁。
4. 将 `runner` 从 echo 替换为真实 provider 适配，并补失败场景测试。
5. 为 OpenAPI 增加字段级约束与 lint，避免契约与实现继续漂移。
6. 在 `nightly-live.yml` 接入真实 live checks（模型/网络/重试策略）。
