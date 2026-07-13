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

## 生产执行

未配置持久化选项时，Workflow 会在内存中保留 checkpoint 和 RunLog 事件。该默认值适合测试和短生命周期本地程序；长驻服务应配置带明确保留策略的持久化 Store：

```go
wf := workflow.New[Input, Output](
    "agent",
    workflow.WithCheckpointer(store),
    workflow.WithJournal(store),
    workflow.WithCheckpointLease(3*time.Minute, time.Minute),
)
```

部署边界是明确的：`workflow.MemoryStore` 仅用于测试和短生命周期本地进程；`stores/sqlite` 适用于单机，或安全共享同一个本地数据库文件的多进程。多主机部署必须使用支持原子 Claim 与 fencing 的分布式数据库 Store。

配置后的 Store 是权威数据源，写入失败会直接终止 Run。Checkpointer 必须支持原子抢占和续租；续租失败时，runtime 会用 `workflow.ErrCheckpointLeaseLost` 取消节点 Context。节点实现必须在 Context 被取消后及时停止。

多实例持久化执行必须把同一个组合 Store 实例同时配置为 Checkpointer 和 Journal，并使用实现了 `runlog.FencedLog` 的 Store。这样 Store 可以在同一把锁或同一个数据库事务中校验当前 owner/claim 并追加 observed event，关闭 Claim 后旧 owner 继续物理写 journal 的窗口，同时避免每个 observed event 额外产生两份 checkpoint history。若 checkpoint 与 journal 是两个独立后端，runtime 会使用持久化 pending-event 进行恢复，但无法让跨后端的 owner 校验与物理 journal append 成为一个原子操作。

Workflow 恢复采用 at-least-once 语义，journal 到事件消费者的投递同样是 at-least-once；消费者应使用 `(RunID, Sequence)` 或 `RevisionID` 等稳定事件身份去重。Heartbeat 可以避免健康的长耗时节点仅因原租约过期而被接管，但 checkpoint 协议无法让任意外部 API 自动获得 exactly-once 语义。发送消息、扣款、修改库存或调用计费模型时，必须使用跨 Resume 稳定的幂等键，例如 `RunInfo.RunID + "/" + RunInfo.ActivationID`。

稳定 key 只有在两种模式下才能形成可靠保证：外部 API 原生按该 key 去重；或者业务在修改业务数据的同一数据库事务中，写入带唯一约束的 dedup/outbox 记录。`gopact` 不提供通用 outbox，也无法把自身 checkpoint 事务与任意远程 API 合并成一个原子事务。如果显式业务重试确实要再次产生副作用，必须为该操作生成新的 key，不能继续复用恢复幂等键。

高层历史投影都有读取边界。`ListSessionRuns` 与未显式设置 Limit 的 `Snapshot` 默认最多读取 10,000 条记录；checkpoint history 以及 Retry/Jump 使用每页 256 条的分页扫描，超过安全上限时返回 `workflow.ErrHistoryLimitExceeded`，不会静默返回不完整结果。Timeline 应通过 `SnapshotRequest.Limit` 显式分页；终态 Run 在超过控制历史上限前应完成归档或清理。

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
