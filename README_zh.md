# 🧠 gopact

<!-- gopact:doc-language: zh -->

[英文文档](./README.md)

`gopact` 是 Agent-first、Workflow-native 的 Go ADK core。

> **仅支持 Go 1.27+。** 本项目围绕泛型方法构建，也借此庆祝我们眼中 Go 近十年来最具影响力的语言演进之一。Go 1.27 正式发布前，本项目需要开发版工具链，应视为预览而非稳定版本。

## 选择入口

| 你想要…… | 前往 |
| --- | --- |
| 构建类型安全的 Workflow 和 Agent runtime | 当前仓库 |
| 添加模型 adapter、Agent 组合或 Store | [gopact-ext](https://github.com/gopact-ai/gopact-ext) |
| 运行完整的 quickstart 和集成示例 | [gopact-examples](https://github.com/gopact-ai/gopact-examples) |

当前仓库只保留：

- root `gopact`：Model、Tool、Event、Invokable 等共享协议和事实；
- `agent`：Agent Identity/Request/Response、typed Observation、Tool contract 和 immutable Directory；
- `workflow`：唯一执行 runtime，提供 typed node/route/join、hook/middleware、guard、checkpoint、history 与同 Run control；
- `runlog`：append/query/sink contract 与内存实现；
- `provider`：provider registry/router/fallback 和 basic provider normalization；
- `gopacttest`：跨仓可复用的 Model 与 Agent conformance helper。

官方 provider、concrete Agent 和 SQLite adapter 位于 `gopact-ext`，可运行示例位于 `gopact-examples`。

`SessionID` 是关联多个 Run 的 runtime metadata，不是 Session 容器。Agent Context 是由业务或具体 Agent 的 Workflow 逻辑显式构造的最终 `gopact.ModelRequest`；core 不隐式注入会话或 semantic Memory 状态。

## 要求

需要 Go 1.27 或更新版本。本仓库按 Go 1.27+ 设计，Agent 与 workflow 的实现者 API 都使用泛型对象方法。

## 快速检查

```bash
go test ./...
go test -race ./...
go vet ./...
```

项目使用聚焦的原生门禁进行验证：`gofmt`、`go mod tidy -diff`、`go test`、`go test -race`、`go vet` 与 `govulncheck`，不依赖聚合式第三方 lint 工具。

## 最小 workflow

```go
wf := workflow.New[string, int]("length")
count := wf.Node("count", func(_ context.Context, input string) (int, error) {
    return len(input), nil
})
wf.Entry(count)
wf.Exit(count)

out, err := wf.Invoke(ctx, "gopact")
```
