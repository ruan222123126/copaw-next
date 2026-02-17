# NextAI

NextAI 是一个以 `nextai-local` 功能范围为目标、以 `openclaw` 工程方法为标杆的全新开源项目。

## 目录

- `apps/gateway`: Go Gateway（API + 运行时 + 调度）
- `apps/cli`: TypeScript CLI（仅调用 Gateway API）
- `apps/web`: 控制台前端（占位）
- `packages/contracts`: OpenAPI 与契约文件
- `tests/contract`: 契约测试

## 快速开始

```bash
cd apps/gateway
go run ./cmd/gateway
```

默认地址 `http://127.0.0.1:8088`。
