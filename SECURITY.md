# Security Policy



## 密钥管理策略

- 严禁在业务代码、测试快照、文档示例中硬编码密钥。
- 密钥仅可来自环境变量或 Secret Store。
- 推荐环境变量：
  - `NEXTAI_LIVE_OPENAI_API_KEY`
  - `OPENAI_API_KEY`
- 提交前应执行 secret scanning，并检查 `git diff` 中是否包含敏感值。

## 最小权限与默认关闭

- 危险能力默认关闭，按需开启。
- CI 令牌与运行权限应使用最小权限原则。
- 工作区上传必须做路径穿越校验（项目已启用）。

## 安全修复发布

- 安全修复优先走 `hotfix/*` 分支。
- 必须附带回归测试。
- 发布后补充 changelog 与影响范围说明。
