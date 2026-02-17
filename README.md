# NextAI

NextAI 是一个个人 AI 助手控制平面项目，采用 Monorepo 结构，包含 Gateway、CLI、Web 和 TUI。

## 核心能力（v1）

- Go Gateway：统一 API、会话/历史、Cron、渠道、配置与运行时能力。
- TypeScript CLI：通过 Gateway API 完成聊天、模型、环境、渠道、工作区等操作。
- Terminal UI（TUI）：`nextai tui` 提供终端内会话交互与流式回复。
- Web Console：浏览器端控制台，覆盖聊天、模型、渠道、配置与 Cron 操作。
- Contract-First：以 OpenAPI 为单一契约来源，配套契约测试与 SDK 生成。

## 仓库结构

- `apps/gateway`：Go Gateway（API + 运行时 + 调度）。
- `apps/cli`：TypeScript CLI/TUI（仅调用 Gateway API）。
- `apps/web`：TypeScript Web 控制台。
- `packages/contracts`：OpenAPI 契约与 schema。
- `packages/sdk-ts`：基于契约生成的 TypeScript SDK。
- `tests/contract`：契约测试。
- `tests/smoke`：跨模块冒烟测试。

## 环境要求

- Go `1.22+`
- Node.js `22+`
- pnpm `10.23.0+`

## 快速开始

1. 安装依赖

```bash
pnpm install --recursive
```

2. 启动 Gateway（默认 `http://127.0.0.1:8088`）

```bash
make gateway
```

3. 启动 CLI（单次命令）

```bash
cd apps/cli
pnpm build
node dist/index.js --help
```

4. 启动 TUI（终端交互）

```bash
cd apps/cli
pnpm build
node dist/index.js tui
```

5. 启动 Web 控制台（静态文件）

```bash
cd apps/web
pnpm build
python3 -m http.server 5173 --bind 127.0.0.1 --directory dist
```

浏览器访问 `http://127.0.0.1:5173`。

## 配置说明

- `NEXTAI_HOST`：Gateway 监听地址（默认 `127.0.0.1`）
- `NEXTAI_PORT`：Gateway 端口（默认 `8088`）
- `NEXTAI_DATA_DIR`：数据目录（默认 `.data`）
- `NEXTAI_API_KEY`：可选，设置后启用 API Key 鉴权

当启用 `NEXTAI_API_KEY` 后，客户端可通过 `X-API-Key` 或 `Authorization: Bearer <key>` 访问 Gateway。

## 常用验证命令

```bash
# Gateway
cd apps/gateway && go test ./...

# CLI
cd apps/cli && pnpm test && pnpm build

# Web
cd apps/web && pnpm test && pnpm build

# Contracts
pnpm --filter @nextai/tests-contract run lint
pnpm --filter @nextai/tests-contract run test

# Smoke
cd tests/smoke && pnpm test
```

## 相关文档

- `docs/development.md`：本地开发指南
- `docs/contracts.md`：API/CLI 契约说明
- `docs/deployment.md`：部署指南
- `CONTRIBUTING.md`：贡献流程
