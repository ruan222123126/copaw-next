# AGENTS.md

## 1. 项目定位
- 项目名：NextAI
- 目标：个人 AI 助手控制平面（Gateway + CLI + Web + TUI）
- v1 功能边界：以 nextai-local 为准，不扩展多端重功能

## 2. 语言与沟通
- 默认中文沟通
- 代码标识符/命令/日志保持原文
- 讨论必须给出可验证结论，不允许拍脑袋

## 3. 架构硬约束
- Monorepo：apps + packages + tests
- Gateway 使用 Go；CLI/Web 使用 TypeScript
- CLI 只能调用 Gateway API，不得直接写业务存储
- OpenAPI 是 API 单一事实来源
- 先静态插件注册，v1 不做动态远程插件加载

## 4. 开发流程
- 先写/更新 contracts（docs/contracts)，再写实现
- 每个 PR 必须包含：变更说明、测试说明、回滚说明
- 小步提交：单 PR 聚焦单主题

## 5. 代码规范
- Go：`go test ./...` + `gofmt`
- TS：`tsc` + `vitest`
- 禁止在业务代码中硬编码密钥
- 统一错误模型：`{ error: { code, message, details } }`

## 6. 测试规范
- 分层：unit / integration / e2e / live
- PR 至少新增或更新一种测试
- 核心闭环必须有 e2e：创建会话->发消息->收回复->查历史

## 7. CI 规范
- PR 跑 ci-fast，main 跑 ci-full，nightly 跑 live
- CI 失败不得合并（除非明确豁免并记录原因）
- 变更 contracts 时必须跑 contract tests

## 8. 安全规范
- 密钥仅来自 env 或 secret store
- workspace zip 上传必须做路径穿越校验
- 默认最小权限；危险能力默认关闭
- 启用 secret scanning

## 9. 可观测性
- 每个请求必须带 request-id
- 关键链路记录结构化日志（chat/cron/skills/workspace）
- 错误日志必须包含 code 和上下文，不泄漏密钥

## 10. 版本与发布
- 语义化版本（`v1.0.0-rc.x`）
- 每次发布必须有 changelog
- 支持快速 hotfix，禁止未测试直接发版

## 11. 禁止项
- 未经评审引入超出 v1 范围的功能
- 为“未来可能”过度抽象
- 跳过契约直接改 CLI/Gateway 行为

## 12. 决策原则
- 优先用户闭环可用性
- 优先可验证正确性
- 优先可维护性，不追求炫技

## 13. AI 接手强制流程
- 每位新接手的 AI 必须先阅读 `docs/TODO.md` 与交接文件 `/home/ruan/.codex/handoff/latest.md`。
- 执行顺序必须以 TODO 未完成项与交接文件“接手建议（按顺序）”为准；冲突时以最新交接文件为准并在 TODO 记录原因。
- 每次任务结束后必须更新 `docs/TODO.md`：勾选已完成项、补充阻塞项、更新“更新时间”。
