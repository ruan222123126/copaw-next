# NextAI

NextAI 是一个个人 AI 助手控制平面项目，采用 Monorepo 结构，包含 Gateway、CLI、Web 和 TUI。

## 核心能力（v1）

- Go Gateway：统一 API、会话/历史、Cron、渠道、配置与运行时能力。
- TypeScript CLI：通过 Gateway API 完成聊天、模型、环境、渠道、工作区等操作。
- Terminal UI（TUI）：`nextai tui` 提供终端内会话交互与流式回复。
- Web Console：浏览器端控制台，覆盖聊天、模型、渠道、配置与 Cron 操作。
- Contract-First：以 OpenAPI 为单一契约来源，配套契约测试与 SDK 生成。
- 目前只支持qq机器人链接，因为我只用qq，之后再写其他渠道，目前只支持发文字，其他之后在搞
- 支持定时任务，支持openai接口和兼容接口，小龙虾超简陋版（也可以说轻量吧）
- qq发送/new即可刷新对话

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

## 使用发布版（Release）

适用场景：不想在目标机安装 Go/pnpm，只想直接运行构建产物。

### 方式一：下载总包（推荐）

1. 下载并解压（示例版本：`v0.1.0-rc.3`）

```bash
export NEXTAI_VERSION=v0.1.0-rc.3
mkdir -p /opt/nextai && cd /opt/nextai
curl -fL -o nextai-release-linux-amd64.tar.gz \
  "https://github.com/ruan222123126/NextAI/releases/download/${NEXTAI_VERSION}/nextai-release-linux-amd64.tar.gz"
tar -xzf nextai-release-linux-amd64.tar.gz
```

2. 准备配置（复制模板后直接改值）

```bash
cd /opt/nextai
cp .env.example .env
```

`.env` 至少建议配置：

```bash
NEXTAI_HOST=0.0.0.0
NEXTAI_PORT=8088
NEXTAI_DATA_DIR=/opt/nextai/.data
NEXTAI_API_KEY=replace-with-your-key

NEXTAI_ENABLE_SEARCH_TOOL=true
NEXTAI_SEARCH_DEFAULT_PROVIDER=
# 三选一或多选都行（至少配置一个 key）
NEXTAI_SEARCH_SERPAPI_KEY=
NEXTAI_SEARCH_TAVILY_KEY=
NEXTAI_SEARCH_BRAVE_KEY=

NEXTAI_ENABLE_BROWSER_TOOL=true
NEXTAI_BROWSER_AGENT_DIR=/opt/nextai/browser-agent-poc
```

`search` 工具可用 API：

- `serpapi`：`NEXTAI_SEARCH_SERPAPI_KEY`（可选 `NEXTAI_SEARCH_SERPAPI_BASE_URL`）
- `tavily`：`NEXTAI_SEARCH_TAVILY_KEY`（可选 `NEXTAI_SEARCH_TAVILY_BASE_URL`）
- `brave`：`NEXTAI_SEARCH_BRAVE_KEY`（可选 `NEXTAI_SEARCH_BRAVE_BASE_URL`）
- `NEXTAI_SEARCH_DEFAULT_PROVIDER` 可设为 `serpapi|tavily|brave`，留空会从已配置 key 的 provider 自动选择
- 工具调用时也可在 `items[].provider` 显式指定 provider

`browser` 工具说明：

- 若无须使用 AI 操作浏览器，可跳过本节所有步骤，并保持 `NEXTAI_ENABLE_BROWSER_TOOL=false`
- 需要单独部署 `browser-agent-poc` 目录（原因：Gateway 只负责调度，实际浏览器动作由 `node agent.js` 执行）
- `NEXTAI_BROWSER_AGENT_DIR` 必须指向该目录；目录里必须有 `agent.js`、`node_modules`、`.env`

`browser-agent-poc` 部署步骤（发布后）：

1. 准备目录（示例）

```bash
mkdir -p /opt/nextai
cd /opt/nextai
# 把仓库里的 browser-agent-poc 整个目录拷过来
```

2. 安装依赖与浏览器运行时

```bash
cd /opt/nextai/browser-agent-poc
cp .env.example .env
pnpm install --ignore-workspace
pnpm exec playwright install chromium
```

3. 配置 `browser-agent-poc/.env`（必填）

```bash
MODEL_API_KEY=你的模型API密钥
MODEL_BASE_URL=https://你的模型网关/v1
MODEL_NAME=模型ID
```

字段解释：

- `MODEL_API_KEY`：给浏览器代理调用模型时使用的密钥（例如 OpenAI-compatible key）
- `MODEL_BASE_URL`：模型 API 基地址，必须是 OpenAI-compatible 接口地址（通常以 `/v1` 结尾）
- `MODEL_NAME`：实际调用的模型标识（例如 `gpt-4o-mini`）

限制说明（重要）：

- `browser-agent-poc` 这里只支持 OpenAI-compatible API 接口
- 非 OpenAI-compatible 协议的模型服务不能直接接入，必须先通过兼容层/网关转换后再使用

4. 与 Gateway 联动

```bash
# /opt/nextai/.env
NEXTAI_ENABLE_BROWSER_TOOL=true
NEXTAI_BROWSER_AGENT_DIR=/opt/nextai/browser-agent-poc
```

5. 快速自检

```bash
cd /opt/nextai/browser-agent-poc
node agent.js "打开 https://example.com，读取标题"
```

如果这一步能返回文本或明确错误（比如权限/风控确认），说明浏览器代理链路已连通。

3. 启动 Gateway（会自动加载当前目录 `.env`）

```bash
cd /opt/nextai
chmod +x gateway-linux-amd64
./gateway-linux-amd64
```

也可以指定自定义配置路径：

```bash
NEXTAI_ENV_FILE=/etc/nextai/gateway.env ./gateway-linux-amd64
```

4. 启动 CLI（需要 Node.js `22+`）

```bash
cd /opt/nextai
node cli/index.js --help
node cli/index.js chats list --api-base http://127.0.0.1:8088
```

5. 启动 Web（静态文件）（web页面中配置你的模型和qq渠道）

```bash
cd /opt/nextai
python3 -m http.server 5173 --bind 0.0.0.0 --directory web
```

### 方式二：按单独产物下载

- `gateway-linux-amd64`：Gateway 可执行文件（Linux amd64）
- `cli-dist.tar.gz`：CLI/TUI 构建产物（解压后用 `node index.js ...` 运行）
- `web-dist.tar.gz`：Web 静态产物（解压后用 Nginx/Caddy/`http.server` 托管）

这些产物都在 GitHub Release 附件中。

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
