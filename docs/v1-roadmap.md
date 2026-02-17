# NextAI v1 路线图

## Week 1：骨架与契约
- Monorepo 初始化
- OpenAPI 首版
- AGENTS.md 规范落地

验收：
- Gateway 可启动
- 契约测试通过

## Week 2：聊天闭环
- chats CRUD
- /agent/process SSE
- CLI chats send/list/create

验收：
- 创建会话 -> 发送消息 -> 查询历史通过

## Week 3：管理能力
- models/envs/skills/workspace/channels API + CLI
- 插件接口静态注册

验收：
- 管理端点可用
- workspace 安全校验通过

## Week 4：Cron 与质量门禁
- cron 全链路
- CI 分层（fast/full/nightly）
- 发布文档

验收：
- cron run 可触发
- ci-fast/ci-full 可运行
