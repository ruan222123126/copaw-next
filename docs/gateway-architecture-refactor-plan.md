# Gateway 架构级重构计划（基于 2026-02 文件级拆分）

## 1. 背景与目标

### 背景
当前我们已经完成 `server.go` 的文件级拆分：
- `apps/gateway/internal/app/server.go`（入口、路由、基础生命周期）
- `apps/gateway/internal/app/server_agent.go`（Agent/Tool/QQ 入站编排）
- `apps/gateway/internal/app/server_cron.go`（Cron 调度与执行）
- `apps/gateway/internal/app/server_admin.go`（Models/Skills/Workspace/Config）

文件体积风险下降了，但核心问题仍在：
- HTTP 处理、业务编排、仓储读写、插件调用耦合在同一层（`app`）。
- 缺乏稳定的服务边界，导致改一点牵一片。
- 测试以 HTTP 集成为主，单元级行为验证颗粒度不足。

### 本次目标（架构级）
在不改变对外 API 契约与行为的前提下，完成“分层 + 边界 + 可测试性”重构：
1. 把 HTTP 层与业务编排层解耦。
2. 把跨领域流程拆成可复用的 Application Service（Agent/Cron/Workspace/Model）。
3. 把存储与插件依赖通过接口下沉，减少 `app` 包横向耦合。
4. 保持 v1 边界，不引入动态插件、不过度抽象。

## 2. 非目标（明确不做）
- 不改 OpenAPI 路径/字段/错误模型（仍为 `{ error: { code, message, details } }`）。
- 不引入新业务能力（仅重构现有实现）。
- 不做跨进程任务系统重写（Cron 仍沿用当前机制）。
- 不在本次重构中替换现有 `repo.Store` 存储格式。

## 3. 目标架构（落地形态）

```text
Transport (HTTP)
  -> internal/app/http
     - router
     - agent_handlers
     - cron_handlers
     - admin_handlers

Application (Use Case / Orchestrator)
  -> internal/service
     - agent_service
     - cron_service
     - workspace_service
     - model_service

Domain & Infra Adapters
  -> internal/repo      (state persistence)
  -> internal/plugin    (channel/tool)
  -> internal/runner    (LLM turn)
```

### 关键边界约束
- Handler 只做：参数解析、鉴权上下文、调用 Service、统一响应映射。
- Service 只做：业务流程编排与领域规则，不直接操作 `http.*`。
- Repo/Plugin/Runner 通过接口注入给 Service，便于单测 mock。
- `server.go` 仅保留组合根（wiring）和生命周期管理。

## 4. 分阶段执行计划

## 阶段 0：基线冻结与防回归护栏
- 目标：先把行为边界钉死，再动结构。
- 操作：
  - 补全关键链路黑盒用例清单（agent/tool/cron/workspace/models）。
  - 固定错误码与状态码映射快照（尤其 `mapToolError` / `mapChannelError` / `mapRunnerError`）。
- 主要文件：
  - `apps/gateway/internal/app/server_test.go`
  - 新增：`apps/gateway/internal/app/contract_regression_test.go`（建议）
- 验证：`cd apps/gateway && go test ./internal/app ./...`
- 回滚：测试护栏属于增量，可直接保留，不影响运行时。

## 阶段 1：抽离 HTTP Transport 层
- 目标：让 handler 从 `Server` 大文件中独立为 transport 包。
- 操作：
  - 新建 `internal/app/http`，迁移路由注册与 handler 方法。
  - `server.go` 仅负责初始化 router 与依赖注入。
  - 引入统一 `Responder`（`writeJSON`/`writeErr`）组件，禁止散落在业务层。
- 主要文件（新增/迁移）：
  - `apps/gateway/internal/app/http/router.go`
  - `apps/gateway/internal/app/http/agent_handlers.go`
  - `apps/gateway/internal/app/http/cron_handlers.go`
  - `apps/gateway/internal/app/http/admin_handlers.go`
- 风险：路由绑定变更易引发 404/中间件遗漏。
- 验证：
  - 全量 `go test ./...`
  - 冒烟：`/healthz`、`/agent/process`、`/cron/jobs`、`/workspace/files`
- 回滚：保留旧 handler 分支，按 PR 级回滚。

## 阶段 2：抽离 Application Service（核心）
- 目标：把编排逻辑从 handler 中剥离。
- 操作：
  - 新建 `internal/service/agent`：承接 `processAgentWithBody` 相关流程。
  - 新建 `internal/service/cron`：承接 job 调度执行、workflow 计划与状态变更。
  - 新建 `internal/service/workspace`、`internal/service/model`：承接 admin 业务流程。
  - 定义输入/输出 DTO，禁止 service 暴露 `http` 类型。
- 主要文件（新增）：
  - `apps/gateway/internal/service/agent/service.go`
  - `apps/gateway/internal/service/cron/service.go`
  - `apps/gateway/internal/service/workspace/service.go`
  - `apps/gateway/internal/service/model/service.go`
- 风险：流程迁移时易出现字段遗漏（metadata、tool_call_id、cron state）。
- 验证：
  - 现有集成测试全过。
  - 新增 service 单测覆盖关键分支（成功/失败/超时/禁用）。
- 回滚：按服务维度分 PR，单服务可独立回退。

## 阶段 3：依赖反转（Repo/Runner/Plugin 接口化）
- 目标：降低 `service` 对具体实现的硬依赖。
- 操作：
  - 在 `internal/service/ports` 定义端口接口：
    - Chat/Cron/Workspace Repository Port
    - Tool/Channel Registry Port
    - LLM Runner Port
  - `repo.Store`、`runner.Runner`、`plugin` 注册表通过 adapter 接口实现。
- 主要文件（新增）：
  - `apps/gateway/internal/service/ports/*.go`
  - `apps/gateway/internal/service/adapters/*.go`
- 风险：接口颗粒度过粗会变成“伪抽象”；过细则增加样板代码。
- 验证：
  - Service 层单测使用 mock/stub，不依赖真实 store。
  - `go test ./...` 与现有 e2e 不回退。
- 回滚：接口层可保留，adapter 回切老实现。

## 阶段 4：配置与系统层策略收敛
- 目标：把系统 prompt 层、runtime-config、路径解析策略从 admin 混杂逻辑中收敛。
- 操作：
  - 新建 `internal/service/systemprompt`（层构建、token estimate、path policy）。
  - `agent` service 只依赖 `SystemPromptProvider` 接口。
- 主要文件：
  - `apps/gateway/internal/service/systemprompt/*.go`
- 风险：路径策略改动引入兼容问题（`docs/AI/AGENTS.md` 兼容路径）。
- 验证：
  - `TestFindRepoRoot...` 与 system layers 相关测试全过。
- 回滚：保持旧路径候选逻辑实现可回切。

## 阶段 5：收尾与治理
- 目标：清理历史遗留，建立长期约束。
- 操作：
  - 删除已废弃 helper 与重复映射逻辑。
  - 增加 lint 规则（限制 handler 直接访问 store/plugin）。
  - 更新开发文档与贡献指南（新增“层间依赖红线”）。
- 验证：
  - 代码审查清单通过。
  - CI `ci-fast` / `ci-full` 通过。

## 5. PR 切分策略（必须小步）
1. PR-1：阶段 0 测试护栏。
2. PR-2：阶段 1（仅 transport 拆分，不碰业务逻辑）。
3. PR-3：阶段 2A（agent service）。
4. PR-4：阶段 2B（cron service）。
5. PR-5：阶段 2C（workspace/model service）。
6. PR-6：阶段 3（ports + adapters）。
7. PR-7：阶段 4/5（systemprompt 收敛 + 清理文档）。

每个 PR 必须包含：
- 变更说明
- 测试说明
- 回滚说明

## 6. 测试与验收标准

### 测试矩阵
- 单元：service 层用 mock 覆盖核心分支。
- 集成：`internal/app` HTTP 行为回归。
- 端到端：核心闭环不退化。
  - 创建会话 -> 发消息 -> 收回复 -> 查历史
  - Cron 创建/执行/状态更新
  - Workspace 文件读写/导入导出

### 验收标准（DoD）
- [ ] `server.go` 保持组合根，不再承载业务编排。
- [ ] HTTP handler 文件中不出现对 `repo.Store` 的直接写操作。
- [ ] Agent/Cron/Workspace/Model 均有独立 service 与单测。
- [ ] 现有 API 契约与错误模型保持兼容。
- [ ] `go test ./...`、合同测试、关键 e2e 全通过。

## 7. 风险清单与缓解
- 风险：重构期间行为漂移（尤其流式事件顺序）。
  - 缓解：阶段 0 先固化事件序列回归测试。
- 风险：多 PR 长周期导致冲突。
  - 缓解：严格按模块切 PR，先落接口再迁逻辑。
- 风险：抽象过度影响迭代效率。
  - 缓解：只围绕已存在领域抽象，不为未来场景预埋复杂机制。

## 8. 执行顺序建议
1. 阶段 0（测试护栏）
2. 阶段 1（transport 拆分）
3. 阶段 2A（agent）
4. 阶段 2B（cron）
5. 阶段 2C（workspace/model）
6. 阶段 3（接口化）
7. 阶段 4/5（系统层收敛 + 治理）

## 9. 阶段 0 详细落地清单（本周必须完成）

### 9.1 必补回归用例（黑盒）
- Agent 基础对话：创建会话 -> 发消息 -> 收回复 -> 拉历史（非流式）。
- Agent 流式事件序列：至少覆盖 `step_started -> assistant_delta -> completed` 与 `[DONE]` 收尾。
- Tool 成功链路：`tool_call` 与 `tool_result` 成对出现，最终可得到 assistant 回复。
- Tool 失败链路：覆盖 `mapToolError` 的核心分支，校验 HTTP 状态码与 `error.code`。
- Runner 失败链路：覆盖 `provider_invalid_reply`、timeout、context cancel 的状态码与错误模型。
- Cron 执行链路：创建任务 -> 手动触发运行 -> `last_status/last_error` 更新正确。
- Workspace 链路：`/workspace/files` 列表、`/workspace/files/{path}` 读写、`/workspace/export|import` 正常。
- Models/Catalog 链路：active/default/provider 删除后的可见性与兜底行为保持现状。

### 9.2 必补映射快照（白盒）
- `mapToolError`：状态码、`error.code`、message 语义。
- `mapChannelError`：状态码、`error.code`、message 语义。
- `mapRunnerError`：状态码、`error.code`、message 语义。
- 要求：上述映射新增表驱动测试，禁止只断言 message 子串。

### 9.3 阶段 0 完成判定（DoD）
- 新增测试均稳定通过，且可在本地重复跑 3 次无随机失败。
- `go test ./internal/app -run "Agent|Cron|Workspace|Model|RuntimeConfig"` 通过。
- `go test ./...` 全绿。
- 不改 API 契约文件，不新增行为开关。

## 10. 每阶段输入/输出定义（避免“重构中跑偏”）

### 阶段 1（Transport）
- 输入：阶段 0 测试护栏已落地。
- 输出：`internal/app/http` 新包上线；`server.go` 仅保留 wiring/lifecycle；所有路由行为不变。
- 禁止项：迁移阶段不改业务分支，不调整错误语义。

### 阶段 2A/2B/2C（Service）
- 输入：handler 已独立。
- 输出：`agent/cron/workspace/model` service 独立可测，handler 仅做协议适配。
- 禁止项：service 直接依赖 `http.Request/ResponseWriter`。

### 阶段 3（Ports/Adapters）
- 输入：service 行为稳定。
- 输出：service 通过 ports 依赖 repo/plugin/runner；可通过 mock 完成主要单测。
- 禁止项：为“未来可能”引入未使用端口。

### 阶段 4/5（System + 治理）
- 输入：核心链路已迁移完毕。
- 输出：system prompt 组装职责收敛；冗余 helper 删除；文档和守护规则同步。
- 禁止项：在收尾阶段插入新功能需求。

## 11. PR 执行模板（每个 PR 都按这个交付）

### 11.1 变更说明
- 本 PR 所属阶段：
- 只改了哪些包：
- 明确不改哪些行为：

### 11.2 测试说明
- 新增测试：
- 回归测试：
- 执行命令与结果：

### 11.3 回滚说明
- 回滚粒度（整 PR/单模块）：
- 回滚后影响：
- 数据兼容性说明：

## 12. 推进看板（交接持续更新）

- [x] PR-1 阶段 0：测试护栏
- [x] PR-2 阶段 1：Transport 拆分
- [x] PR-3 阶段 2A：Agent Service
- [x] PR-4 阶段 2B：Cron Service
- [x] PR-5 阶段 2C：Workspace/Model Service
- [x] PR-6 阶段 3：Ports + Adapters
- [ ] PR-7 阶段 4/5：SystemPrompt 收敛 + 治理

> 维护规则：每完成一个 PR，必须同时更新本节勾选状态与 `docs/TODO.md` 的时间线记录。
