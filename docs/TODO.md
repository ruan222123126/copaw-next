# CoPaw Next TODO

更新时间：2026-02-17 11:47:11 +0800

## 执行约定（强制）
- 每位接手 AI 开始前，必须先阅读本文件与 `/home/ruan/.codex/handoff/latest.md`。
- 执行顺序优先遵循交接文件“接手建议（按顺序）”，并与本文件未完成项对齐推进。
- 每次执行后必须更新本文件：勾选完成项、记录阻塞原因、刷新“更新时间”。

## 0. 目标范围（v1）
- 以 `copaw-local` 功能边界为准，遵循 `openclaw` 的工程方法（契约优先、分层测试、CI 分层、CLI/Gateway 分离），不扩展超出 v1 范围能力。

## 1. 基础工程与规范
- [x] Monorepo 结构、pnpm 包管理、核心文档（README/CONTRIBUTING/SECURITY）、`Makefile` 与 `.env.example` 已统一落地。

## 2. 核心实现（Gateway / CLI / Web）
- [x] Gateway 已完成 v1 核心 API、统一错误模型、请求追踪、关键安全防护（如上传路径穿越拦截）、模型 provider/catalog/alias/配置管理等能力。
- [x] CLI 已完成核心命令集、流式输出、错误分级提示、`--json` 机器输出、多语言与模型配置链路。
- [x] Web 控制台已完成聊天与关键管理面板（Models/Envs/Workspace/Cron），并具备统一错误提示与多语言支持。

## 3. Contracts（契约）
- [x] OpenAPI 契约与关键 schema 已补齐，契约 lint、契约测试与 SDK 生成流程已接入并可运行。

## 4. 测试与 CI/CD
- [x] 已建立 unit / integration / e2e / contract 的分层测试能力与覆盖率门禁，关键闭环（chat/cron/workspace/provider）具备自动化验证。
- [x] CI 已形成 `ci-fast` / `ci-full` / `nightly-live` 分层门禁，发布流水线支持 tag 到 artifact 与 release notes。

## 5. 文档与交付
- [x] `docs/v1-roadmap.md`、`docs/contracts.md`、本地开发文档、部署文档与发布模板已完成。

## 6. 实操验证（汇总）
- [x] Go/TS 全量关键检查通过：`go test ./...`、`make gateway-coverage`、`pnpm -r lint/test/build`。
- [x] 分模块验证通过：Web、CLI、Contracts、SDK 生成、Gateway provider 兼容性相关回归均已通过。
- [x] 2026-02-17 11:47 +0800 分批提交并推送完成：按 `CLI`、`Gateway+Contracts`、`Web+TODO` 三批提交到分支 `feat/agent-multi-step-events`，提交分别为 `c6fc522`、`8a5c139`、`ebcb162`。
- [x] 2026-02-17 11:47 +0800 推送验证通过：`git push origin feat/agent-multi-step-events` 成功（`7a20578..ebcb162`）。
- [x] 2026-02-17 11:44 +0800 Web 新增“QQ 渠道配置”面板：在配置页提供 `app_id/client_secret/target_type/target_id/api_base/token_url/timeout_seconds` 可视化编辑，直连 `/config/channels/qq` 读写。
- [x] 2026-02-17 11:44 +0800 验证通过：`pnpm -C apps/web test`、`pnpm -C apps/web build`。
- [x] 2026-02-17 11:43 +0800 Web 工具调用文案优化：`view` / `edit`（兼容 `exit`）调用摘要改为“查看（文件路径）/编辑（文件路径）”，不再显示“调用任务 'view' / 'edit'”；补充 e2e 回归覆盖流式展示与历史恢复。
- [x] 2026-02-17 11:43 +0800 验证通过：`pnpm -C apps/web test -- test/e2e/web-shell-tool-flow.test.ts`、`pnpm -C apps/web build`。
- [x] 2026-02-17 11:35 +0800 QQ 入站闭环落地：新增 `POST /channels/qq/inbound`，支持解析 QQ 入站事件（c2c/group/guild）并复用 `/agent/process` 主流程，按事件动态覆盖 `qq` 通道 `target_type/target_id/msg_id` 后回发。
- [x] 2026-02-17 11:35 +0800 验证通过：`cd apps/gateway && go test ./...`、`pnpm --filter @copaw-next/tests-contract run lint`、`pnpm --filter @copaw-next/tests-contract run test`、`pnpm --dir packages/sdk-ts run generate && pnpm --dir packages/sdk-ts run generate:check`。
- [x] 2026-02-17 11:34 +0800 聊天工具调用顺序渲染修复：Web 聊天消息改为按流式事件时间线渲染（支持“文本 -> 工具 -> 文本”交错），不再将工具调用统一压在消息底部。
- [x] 2026-02-17 11:34 +0800 验证通过：`pnpm -C apps/web test -- test/e2e/web-shell-tool-flow.test.ts`、`pnpm -C apps/web build`。
- [x] 2026-02-17 11:27 +0800 聊天工具调用持久化修复：Gateway 将 `tool_call` 原始事件与文本/工具顺序写入 assistant `metadata`（`tool_call_notices`/`text_order`/`tool_order`），Web 打开历史会话时从 metadata 恢复工具调用展示，刷新后不再丢失。
- [x] 2026-02-17 11:27 +0800 验证通过：`cd apps/gateway && go test ./...`、`pnpm -C apps/web test -- test/e2e/web-shell-tool-flow.test.ts`、`pnpm -C apps/web build`。
- [x] 2026-02-17 11:25 +0800 QQ 频道接入完成：Gateway 新增静态 `qq` channel 插件（token 获取与缓存、`c2c/group/guild` 发送、`bot_prefix` 支持），`/config/channels` 默认配置与契约文档同步补齐。
- [x] 2026-02-17 11:25 +0800 验证通过：`cd apps/gateway && go test ./...`、`pnpm --filter @copaw-next/tests-contract run lint`、`pnpm --filter @copaw-next/tests-contract run test`。
- [x] 2026-02-17 11:23 +0800 聊天消息渲染顺序修复：Web 聊天区改为按输出先后展示（谁先输出谁在上），不再固定工具块总在顶部；补充 e2e 覆盖“文本先到时文本在上、工具后到时工具在下”的顺序断言。
- [x] 2026-02-17 11:23 +0800 验证通过：`pnpm -C apps/web test -- test/e2e/web-shell-tool-flow.test.ts` 与 `pnpm -C apps/web build`。
- [x] 实际运行验证通过：Gateway 可启动并通过 `/healthz`、`/version`、`/chats`；CLI 可跑通 chat/cron；Web 静态服务可访问。
- [x] Provider 管理策略验证通过：支持新增自定义 provider、内置/自定义 provider 删除、可删空；删掉激活 provider 后 `active_llm` 置空并返回空字符串字段。
- [x] 2026-02-16 13:34 +0800 现场启动验证：执行 `make gateway` 成功，`/healthz` 返回 `{"ok":true}`，`/version` 返回 `{"version":"0.1.0"}`，`/chats` 返回 `[]`。
- [x] 2026-02-16 13:35 +0800 联合启动验证：Gateway 持续监听 `127.0.0.1:8088`；Web 通过 `python3 -m http.server 5173` 提供静态页面，`HEAD /` 返回 `200 OK`。
- [x] 2026-02-16 13:45 +0800 Web 样式修复验证：Provider 拟态弹窗从 Models 面板结构中剥离为全局层，去除拟态发光阴影；执行 `pnpm -C apps/web test -- --run test/smoke/shell.test.ts` 通过（12 tests）。
- [x] 2026-02-16 13:46 +0800 Web 构建验证：执行 `pnpm -C apps/web build` 通过。
- [x] 2026-02-16 13:53 +0800 Web 交互优化验证：Provider 配置中的 `headers` 与 `model_aliases` 改为可视化键值编辑（增删行）并接入提交流程；执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 13:55 +0800 Web 交互增强验证：编辑 Provider 时可从模型列表 `alias_of` 自动回填 `model_aliases` 可视化键值行；再次执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 14:03 +0800 模型配置扩展验证：新增 Provider 弹窗“自定义模型配置”可视化选项（模型 ID 增删行），提交时并入 `model_aliases`；后端补齐 custom provider alias 解析与模型展示；执行 `pnpm -C apps/web test`、`pnpm -C apps/web build`、`cd apps/gateway && go test ./internal/provider ./internal/app` 均通过。
- [x] 2026-02-16 14:08 +0800 Provider 策略调整验证：移除内置 `demo` 提供商（默认 state 与迁移加载均不再保留），`/agent/process` 在未配置激活模型时改为内部 demo 回声兜底；执行 `cd apps/gateway && go test ./internal/provider ./internal/repo ./internal/app`、`pnpm -C apps/web test`、`pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 14:16 +0800 Provider 拟态弹窗交互调整验证：`Provider ID` 改为 `Provider type` 下拉，仅可选择现有接口类型（`openai`、`openai Compatible`）；编辑模式保留原 provider_id 并锁定类型；执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 14:24 +0800 Provider 类型与删空回退修复验证：`/models/catalog` 新增 `provider_types`，Web 改为动态读取接口类型（不再硬编码）；补充“删除全部 provider 后 `/agent/process` 仍走 demo 回声兜底”回归测试；执行 `cd apps/gateway && go test ./internal/provider ./internal/app ./internal/repo`、`pnpm -C apps/web test`、`pnpm -C apps/web build`、`pnpm --filter @copaw-next/tests-contract run lint && pnpm --filter @copaw-next/tests-contract run test` 均通过。
- [x] 2026-02-16 14:36 +0800 Web Provider 自定义模型联动修复验证：`openai` 类型下禁用并隐藏“自定义模型配置”，保存时仅对非内置 provider 提交 custom model IDs，避免“输入但看似未保存”的误导；执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 14:39 +0800 服务重启验证：已重启 Gateway（`make gateway`）与 Web（`python3 -m http.server 5173`）；`/healthz`、`/version` 与 Web `HEAD /` 均返回 200/正常结果。
- [x] 2026-02-16 14:46 +0800 Web 模型与会话显示修复验证：Models 面板补回“激活模型”可视化入口（provider/model 下拉 + 手动覆盖 + `PUT /models/active`），并修复会话列表按钮布局为纵向分行；执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 14:49 +0800 服务重启验证：再次重启 Gateway（`make gateway`）与 Web（`python3 -m http.server 5173`）；`/healthz`、`/version` 与 Web `HEAD /` 均返回正常结果。
- [x] 2026-02-16 14:51 +0800 Web e2e 覆盖补齐：新增 `apps/web/test/e2e/web-active-model-chat-flow.test.ts`，使用 jsdom 真实驱动页面流程（Models 设 active -> Chat 发送消息）并断言助手回复不含 `Echo:`；执行 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 17:39 +0800 服务重启验证：再次重启 Gateway（`make gateway`）与 Web（`python3 -m http.server 5173`）；`/healthz`、`/version` 与 Web `HEAD /` 均返回正常结果。
- [x] 2026-02-16 17:43 +0800 Web 聊天自动激活模型修复验证：页面启动与模型刷新时若 `active_llm` 为空且存在“启用 + 已配置 API Key + 有模型”的 provider，则自动调用 `PUT /models/active` 设定激活模型，避免聊天误走 `Echo` 兜底；执行 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 18:10 +0800 安全审查复核：逐条复核 8 项网关安全/稳定性风险（鉴权默认放行、SSRF、workspace 资源耗尽、权限位、客户端鉴权头、channels 并发 map、JSON 体积上限、request-id 日志），确认均可由当前代码路径触发；按“体验影响优先”建议主清单保留 1/2/3/5/7，4/6/8 作为次级改进项跟踪。
- [x] 2026-02-16 19:07 +0800 安全整改落地：完成默认鉴权强制（`COPAW_API_KEY` 默认必填）、provider/webhook SSRF 防护、workspace 上传下载资源上限、JSON body 统一限流、CLI/Web `X-API-Key` 链路、store 权限位收紧、channels 并发 map 加锁、request-id 入日志；验证通过 `go test ./...`（apps/gateway）、`pnpm -C apps/cli test`、`pnpm -C apps/cli build`、`pnpm -C apps/web test`、`pnpm -C apps/web build`。
- [x] 2026-02-16 19:39 +0800 Web 顶栏设置抽屉改造：将 API 地址/API Key/用户 ID/渠道/语言/刷新会话收纳到右上角设置图标弹层，支持点击图标打开、点击空白或按 Esc 关闭；执行 `pnpm -C apps/web test` 与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 20:43 +0800 PR 分支回退操作：本地分支 `refactor/rename-copaw-to-nextai` 已回退到 `1c94b19`，并创建备份分支 `backup/pr5-before-rollback-20260216-1` 与 `stash@{0}` 保留现场；普通 `git push` 因非 fast-forward 被拒绝，强推命令（`--force-with-lease`/`+refspec`）受当前执行策略拦截，未能同步远端。
- [x] 2026-02-16 20:45 +0800 工作区安全清理：定位并备份 `apps/gateway/.data/workspace` 为 `apps/gateway/.data/workspace-backup-20260216-204506.tar.gz` 后删除并重建空目录；验证目录存在、权限为 `755`、当前为空。
- [x] 2026-02-16 20:47 +0800 流程规范更新：`AGENTS.md` 开发流程新增“每次完成任务后必须提交对应 PR，未提交视为未完成；特殊豁免需在 TODO 记录原因”。
- [x] 2026-02-16 20:47 +0800 服务重启验证：先停止端口占用进程（Gateway `pid=498972`、Web `pid=499049`），再启动 Gateway（`NEXTAI_ALLOW_INSECURE_NO_API_KEY=true make gateway`）与 Web（`python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/web/dist`）；`GET /healthz`、`GET /version`、Web `HEAD /` 均返回 `200`。
- [x] 2026-02-16 20:56 +0800 Web 技能区移除验证：删除前端 Skills 标签与面板、移除 `main.ts` 中技能状态/请求逻辑并同步 smoke 测试与 README；执行 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build` 均通过。
- [x] 2026-02-16 21:01 +0800 PR 提交记录：将“Web 技能区移除”提交为 `feat(web): remove skills panel from console`（`6bfd7b8`）并推送到分支 `refactor/rename-copaw-to-nextai`，已更新 GitHub PR `#5`。
- [x] 2026-02-16 21:30 +0800 工作区重构落地：移除 `/workspace/download` 与 `/workspace/upload`，改为 `/workspace/files` + `/workspace/files/{file_path}` + `/workspace/export` + `/workspace/import`；CLI 改为 `workspace ls/cat/put/rm/export/import`，Web 工作区面板改为文件列表 + JSON 编辑 + 导入导出，契约与 SDK 同步更新；验证通过 `cd apps/gateway && go test ./...`、`pnpm -C apps/cli test && pnpm -C apps/cli build`、`pnpm -C apps/web test && pnpm -C apps/web build`、`pnpm --filter @copaw-next/tests-contract run lint && pnpm --filter @copaw-next/tests-contract run test`、`pnpm --dir packages/sdk-ts run lint && pnpm --dir packages/sdk-ts run test`。
- [x] 2026-02-16 21:50 +0800 Web 工作区 404 诊断提示修复：命中 `404 page not found` 时不再直接回显原文，改为提示“当前 API 地址未提供 `/workspace/files`（请指向最新 Gateway）”；补充中英文 i18n 文案并仅在工作区相关操作生效；验证通过 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build`。
- [x] 2026-02-16 21:50 +0800 服务重启验证：采用持久会话重启 Gateway（`NEXTAI_ALLOW_INSECURE_NO_API_KEY=true make gateway`）与 Web（`python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/web/dist`）；`GET /healthz` 返回 `{"ok":true}`、`GET /version` 返回 `{"version":"0.1.0"}`、Web `HEAD /` 返回 `HTTP/1.0 200 OK`。
- [x] 2026-02-16 22:01 +0800 Web 工作区页面改名为配置页面：前端保留 `workspace` 逻辑与接口不变，仅将页面与 i18n 可见文案统一为“配置/Config”；验证通过 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build`。
- [x] 2026-02-16 22:02 +0800 Web 聊天区高度锁定修复：`apps/web/src/styles.css` 中 `.chat` 由 `min-height` 改为固定 `height: 72vh` + `max-height: 72vh`，保留 `.messages` 的滚动逻辑，避免内容撑高；验证通过 `pnpm -C apps/web test`（13 tests）与 `pnpm -C apps/web build`。
- [x] 2026-02-16 22:08 +0800 聊天电脑操作能力补齐：Gateway 新增 `shell` 工具插件并在 `/agent/process` 接入 `biz_params.tool` 调用链，Web 聊天支持通过 `/shell <command>` 自动构造工具请求，新增网关与 Web e2e 回归测试；验证通过 `cd apps/gateway && go test ./...`、`pnpm -C apps/web test`（14 tests）与 `pnpm -C apps/web build`。
- [x] 2026-02-16 23:37 +0800 CLI workspace export 默认输出补齐：`workspace export` 增加 `--out` 默认值 `workspace.json`，未显式传参时自动写入当前目录同名文件。
- [x] 2026-02-16 23:37 +0800 e2e 默认导出行为覆盖补齐：新增 `workspace export` 未传 `--out` 的 e2e 用例，断言默认输出文件名为 `workspace.json`。
- [x] 2026-02-16 23:40 +0800 e2e 默认导入行为覆盖补齐：新增 `workspace import` 输入缺省 `mode` 时默认补 `replace` 的 e2e 用例。
- [x] 2026-02-16 23:44 +0800 e2e 失败场景覆盖补齐：新增 `workspace put` 同时缺少 `--body` 与 `--file` 的失败场景覆盖。
- [x] 2026-02-17 11:29 +0800 服务重启验证：停止存量 Gateway/Web（含 `go run` 派生 `gateway` 进程）后重启 Gateway（`NEXTAI_ALLOW_INSECURE_NO_API_KEY=true make gateway`）与 Web（`python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/web/dist`）；`GET /healthz` 返回 `{"ok":true}`、`GET /version` 返回 `{"version":"0.1.0"}`、Web `HEAD /` 返回 `HTTP/1.0 200 OK`。
- [x] 2026-02-17 11:37 +0800 服务重启验证：再次停止存量 Gateway/Web（含 `go run` 派生 `gateway` 进程）后重启 Gateway（`NEXTAI_ALLOW_INSECURE_NO_API_KEY=true make gateway`）与 Web（`python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/web/dist`）；`GET /healthz` 返回 `{"ok":true}`、`GET /version` 返回 `{"version":"0.1.0"}`、Web `HEAD /` 返回 `HTTP/1.0 200 OK`。
- [x] 2026-02-17 11:44 +0800 服务重启验证：再次停止存量 Gateway/Web（含 `go run` 派生 `gateway` 进程）后重启 Gateway（`NEXTAI_ALLOW_INSECURE_NO_API_KEY=true make gateway`）与 Web（`python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/web/dist`）；`GET /healthz` 返回 `{"ok":true}`、`GET /version` 返回 `{"version":"0.1.0"}`、Web `HEAD /` 返回 `HTTP/1.0 200 OK`。

## 7. 当前未完成项与阻塞（2026-02-17 11:47:11 +0800）
- [x] 设计并实现 provider 可删除方案（含内置 provider），并完成 catalog/active/default 语义调整：删除后从 `/models/catalog` 消失；删掉激活 provider 后 `active_llm` 置空。
- [x] 风险已消除：删除全部 provider 后，`/agent/process` 在 `active_llm` 为空时走内部 demo 回声兜底；并有回归测试覆盖（`apps/gateway/internal/app/server_test.go`）。
- [ ] 阻塞：无法将 PR 分支远端回退到 `1c94b19`。原因：当前环境策略禁止强推（`git push --force-with-lease` 与 `git push origin +ref` 均被 policy 拦截）；仅普通 `git push` 可执行但因 non-fast-forward 被拒绝。
- [x] 待办已关闭：`/workspace` 重构改动已提交为 `eca30a5` 与 `74c31f9`，并已推送分支 `refactor/rename-copaw-to-nextai`（见最近提交记录）。
- [x] 待办已关闭：Web 工作区 404 诊断提示修复已提交为 `603906c`。

- [x] 2026-02-16 23:42 +0800 Agent 多步自治链路落地：`/agent/process` 支持模型 `tool_calls` 循环与结构化事件流（`step_started`/`tool_call`/`tool_result`/`assistant_delta`/`completed`）；Web 聊天区新增 Agent 事件流展示并保留 `delta + [DONE]` 兼容；验证通过 `cd apps/gateway && go test ./...`、`pnpm -C apps/web test`、`pnpm -C apps/web build`、`pnpm --filter @copaw-next/tests-contract run lint && pnpm --filter @copaw-next/tests-contract run test`。
