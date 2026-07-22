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
- `gopacttest`：跨仓可复用的 Model、minimal Agent 与 Workflow-backed Agent 协议 conformance helper。

官方 provider、concrete Agent 和 SQLite adapter 位于 `gopact-ext`，可运行示例位于 `gopact-examples`。

最小 `agent.Agent` interface 仍可由用户直接实现。只有 Workflow-backed Agent 获得其所配置 Workflow 的 checkpoint、恢复、控制与历史语义；直接实现不会自动获得这些保证。

共享的 direct/Workflow-backed Agent 协议使用 `gopacttest.RequireAgentConformance`；实现承诺 Workflow lifecycle、lineage 与 run-extension 语义时，使用 `gopacttest.RequireWorkflowAgentConformance`。response callback 只是针对给定 fixture 的确定性测试断言；任务质量评估与 release acceptance 仍由 application 负责，不是 runtime API。

`SessionID` 是关联多个 Run 的 runtime metadata，不是 Session 容器，也不是认证、授权或租户隔离凭据。应用必须在查询前完成授权，并通过独立 Store、数据库 namespace 或外层 query wrapper 隔离数据。Agent Context 是由业务或具体 Agent 的 Workflow 逻辑显式构造的最终 `gopact.ModelRequest`；core 不隐式注入会话或 semantic Memory 状态。

## 要求

需要 Go 1.27 或更新版本。本仓库按 Go 1.27+ 设计，Agent 与 workflow 的实现者 API 都使用泛型对象方法。

## 可选模型能力

`Model` 与 `StreamingModel` 仍是最小文本生成协议。Provider 可以独立实现 `Embedder` 和 `ModelCatalog`，让应用无需依赖具体 provider 包即可发现可用模型与生成 embedding：

```go
catalog, ok := model.(gopact.ModelCatalog)
if ok {
	models, err := catalog.ListModels(ctx)
	// 展示 models.Models，不再要求用户手填不透明的模型 ID。
}

embedder, ok := model.(gopact.Embedder)
if ok {
	result, err := embedder.Embed(ctx, gopact.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"gopact"},
	})
}
```

这些能力有意保持可选：如果某个 provider 只公开生成能力，没有公开 embedding 或模型目录 API，就不需要模拟一套。账号配额、订阅限制、媒体生成、文件上传等 provider-specific runtime 操作仍留在 provider adapter，不进入 core 协议。

## 快速检查

```bash
go test ./...
go test -race ./...
go vet ./...
```

项目使用聚焦的原生门禁进行验证：`gofmt`、`go mod tidy -diff`、`go test`、`go test -race`、`go vet` 与 `govulncheck`，不依赖聚合式第三方 lint 工具。

## 发布状态

Go 1.27 stable 发布前，RC 只能称为 production evaluation candidate。stable tag 必须先通过 Go 1.27 stable toolchain 门禁、core → ext → examples 协调源码 E2E、immutable tag clean-consumer 验证和 RC burn-in。当前源码 checkout 不代表 post-tag 验证已经通过。

## 生产执行

未配置持久化选项时，Workflow 会在同一个内存 Store 中保留 checkpoint 和 RunLog 事件。该默认值适合测试和短生命周期本地程序；长驻服务应配置带明确保留策略的持久化 Store：

```go
wf := workflow.New[Input, Output](
    "agent",
    workflow.WithStore(store),
    workflow.WithCheckpointLease(3*time.Minute, time.Minute),
)
```

Workflow 默认使用 Go 标准库 UUID 生成 Session、Run 与租约 owner ID。服务可以在构建阶段按 identity kind 分别替换，单次调用还可以再次覆盖：

```go
wf := workflow.New[Input, Output]("agent",
    workflow.WithIDGenerator(gopact.IDKindSession, newSessionID),
)
out, err := wf.Invoke(ctx, input,
    gopact.WithIDGenerator(gopact.IDKindRun, newRunID),
)
```

优先级为：显式 ID > RunOption generator > Workflow generator > UUID 默认值。生成结果必须非空、是合法 UTF-8、最多 191 字节、不含 NUL 且不能以空格结尾，否则运行会被拒绝。generator 可能被并发调用，并由实现者保证全局唯一性。

部署边界是明确的：`workflow.MemoryStore` 仅用于测试和短生命周期本地进程；`stores/sqlite` 适用于单机，或安全共享同一个本地数据库文件的多进程。多主机部署必须使用支持原子 Claim 与 fencing 的分布式数据库 Store。

配置后的 `workflow.Store` 是唯一的权威持久化边界，所有写入都 fail closed。同一个实例同时提供 checkpoint 持久化与历史、RunLog 追加与查询，以及原子 ownership fencing；runtime 不接受彼此分离的 checkpoint 和 journal authority。Claim 与续租必须原子执行，fenced append 必须在 journal 写入所使用的同一把锁或同一个数据库事务中校验当前 owner 和 claim。runtime 会传递租约时长，让分布式 Store 使用数据库时钟生成到期时间，而不是信任主机墙钟。续租或权威写入失败会终止本次调用，lease loss 会用 `workflow.ErrCheckpointLeaseLost` 取消节点 Context。

Observer telemetry 与 durable authority 相互独立。应用的 `EventSink` 按自身配置的 delivery policy 接收已接受的生命周期事件，但不会替代或参与 Store 的 checkpoint、history、journal 与 fencing contract。Sink 也可以额外实现可选的 `ModelEventSink` 或 `ToolEventSink`，接收节点主动发出的实时、非持久化 component observation。领域日志、指标和 trace 从 Event/View 数据投影；基础设施 telemetry 包装应用持有的 Store 或 adapter。core 不引入 OpenTelemetry 依赖。Plugin 只在 Compile 时注册扩展，不拥有资源；资源由创建它的应用或 adapter 关闭。节点实现必须在 Context 被取消后及时停止。

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
