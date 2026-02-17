# Browser Agent PoC

单文件脚本模式：`Node.js + Playwright + Tool Calling`。

## 1. 安装

```bash
cd browser-agent-poc
cp .env.example .env
pnpm install --ignore-workspace
pnpm exec playwright install chromium
```

## 2. 配置

编辑 `.env`：

- `MODEL_API_KEY`：模型 API Key
- `MODEL_BASE_URL`：兼容 OpenAI API 的 base URL（例如官方 `https://api.openai.com/v1`）
- `MODEL_NAME`：模型名
- `BLOCKED_HOSTS`：域名黑名单，逗号分隔（留空表示不拦截）
- `MODEL_TIMEOUT_MS`：单次模型调用超时（毫秒）

## 3. 运行

```bash
pnpm start -- "打开 https://example.com，读取 h1 文本并截图"
```

不带任务参数时，会进入命令行交互输入。

## 4. 默认能力

- `open_url`
- `click`
- `type`
- `extract_text`
- `screenshot`
- `scroll`

## 5. 安全策略

- 域名黑名单拦截
- 高风险动作人工确认（匹配 submit/pay/delete/购买/支付/删除 等关键词）
- 执行步数和超时限制
- 失败自动截图并记录日志

## 6. 输出产物

- 日志：`logs/<run_id>.jsonl`
- 截图：`shots/*.png`
