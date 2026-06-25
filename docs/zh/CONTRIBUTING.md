# 贡献指南

感谢你帮助改进 `uptime`。

这个项目刻意保持小而清晰：一个 `net/http` 可用性历史中间件、共享存储后端、内置状态页和 JSON 状态 API。请让改动保持在这个边界内。

## 开发要求

- Go 1.22 或更新版本

提交 pull request 前，请运行：

```sh
test -z "$(gofmt -l .)"
go test ./...
go vet ./...
```

涉及并发、store 实现、告警状态或 probe 行为的改动，也请运行：

```sh
go test -race ./...
```

PostgreSQL 集成测试是可选的，需要提供 DSN：

```sh
UPTIME_POSTGRES_DSN='postgres://user:password@host:5432/postgres?sslmode=disable' go test ./store/postgres -v
```

## 设计约定

- 保持公共 API 小而易用。
- 优先使用 Go 标准库，只有依赖有明确价值时才引入。
- 认证和访问控制放在本包外部。
- 告警投递保持可选，并通过用户自有 hook 实现。
- 外部探活放在可选 `probe` 包中，不放进核心中间件。
- 默认不读取请求 body，也不捕获响应 body。
- 不引入前端构建工具或外部浏览器资源。
- 保持请求路径开销低。
- 为可复用的导出类型记录并发行为。
- 避免在本包里加入高基数指标或请求路由分析。

## Pull Request

请包含：

- 简短的改动说明
- 行为变更对应的测试
- 当公开行为或配置发生变化时，同步更新文档

请避免把无关重构和功能/修复混在同一个 PR 中。

## English

See [Contributing](../../CONTRIBUTING.md).
