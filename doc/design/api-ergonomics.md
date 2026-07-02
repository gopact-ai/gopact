# gopact API 调用体验设计

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


日期：2026-06-23

设计入口：[index.md](index.md)

`gopact` 是 SDK，最终会被写进用户的代码里。API 不只要抽象正确，还要在调用处清爽、稳定、语义准确。读者应该能从一段调用代码里看出：谁在运行、用哪些能力、权限边界是什么、事件如何消费、错误如何处理。

## 核心原则

1. 先设计调用处，再设计类型。
2. 常见路径短，复杂路径显式。
3. 语义准确优先于名字短。
4. 调用代码必须经得起 `gofmt` 后阅读。
5. option 顺序应该自然分组，避免一坨无序 `WithX`。
6. 不用 `map[string]any` 承载核心语义。
7. 不用全局 registry 或隐式 magic 让调用代码“看不见依赖”。
8. 每个公开 API 都要有可编译 Example 或测试覆盖。

## 标准调用形态

应用层 setup：

```go
err := gopact.Setup(
	gopact.WithLogger(logger),
	gopact.WithLogLevel(gopact.LevelWarn),
)
if err != nil {
	return fmt.Errorf("setting up gopact: %w", err)
}
```

Runner 构造：

```go
runner, err := gopact.NewRunner(
	workflow,
	gopact.WithProviderRouter(router),
	gopact.WithTools(searchTool, repoTool),
	gopact.WithPolicy(policy),
	gopact.WithArtifactStore(artifacts),
)
if err != nil {
	return fmt.Errorf("creating runner: %w", err)
}
```

单次运行：

```go
events := runner.Run(
	ctx,
	input,
	gopact.WithUserID(userID),
	gopact.WithThreadID(threadID),
	gopact.WithRunLogger(runLogger),
)

for event, err := range events {
	if err != nil {
		return fmt.Errorf("running agent: %w", err)
	}
	handleEvent(event)
}
```

Graph 原语：

```go
workflow := graph.New[State]()

workflow.AddNode("plan", plan)
workflow.AddNode("act", act)
workflow.AddEdge(graph.Start, "plan")
workflow.AddEdge("plan", "act")
workflow.AddEdge("act", graph.End)

run, err := workflow.Compile()
if err != nil {
	return fmt.Errorf("compiling graph: %w", err)
}
```

这些示例是 API 设计的“视觉单元测试”。如果一个新 API 让这些调用变得别扭，应优先调整 API，而不是要求用户忍受复杂调用。

## Option 设计规则

`gopact` 使用 functional options，但不能滥用。

规则：

- 构造必需依赖用普通参数，非必需能力用 option；
- option 名称使用 `WithX`；
- 作用域不清时要显式命名，例如 `WithRunLogger` 而不是复用 `WithLogger`；
- 可能扩大权限的 option 只能出现在 Runner 构造层；
- Run 层 option 默认只能收窄权限、补充身份或调整当前 run 的观察行为；
- option 校验失败必须在构造或 run 开始前返回 error；
- 不提供 `WithConfig(map[string]any)`、`WithRawOptions(any)` 这类逃避类型系统的入口；
- 同一概念不要同时提供多个等价入口。

推荐分组顺序：

```go
runner, err := gopact.NewRunner(
	workflow,
	// runtime modules
	gopact.WithProviderRouter(router),
	gopact.WithToolRegistry(tools),
	gopact.WithMemory(memory),
	// safety and persistence
	gopact.WithPolicy(policy),
	gopact.WithCheckpointer(checkpoints),
	gopact.WithArtifactStore(artifacts),
	// cross-cutting
	gopact.WithLogger(logger),
	gopact.WithPlugins(plugins...),
)
```

如果 option 数量长期超过 8 个，说明需要更高层的 composition helper 或 profile type，但不能退回到配置文件 loader。

## Tool Commit 调用形态

工具的默认 `tool_call` effect 由 registry 生成。工具需要声明幂等提交时，不应该手写
`EffectRecord`，而是在 `ToolResult.Commit` 上给出 key：

```go
return gopact.ToolResult{
	Content: "patched",
	Commit: &gopact.ToolCommit{
		IdempotencyKey: patchKey,
		Metadata: map[string]any{
			"patch_id": patchID,
		},
	},
}, nil
```

registry 会把默认 `tool_call` effect 标记为 idempotent，并记录 replay 所需 args。`tools.ReplayHandler`
可以通过 `WithReplayCommitStore` 接入宿主提供的 `CommitStore`，先查已提交结果，未命中时再重放工具并写回
commit record；`NewMemoryCommitStore` 只作为测试和单进程参考实现。生产级 exactly-once ledger、
外部持久化、冲突处理和跨 worker 一致性仍由 adapter 或宿主实现，不进入 core SDK。外部 adapter 可以通过
`gopacttest/toolconformance.CheckCommitStoreConformance` 或 `RequireCommitStoreConformance` 复用 SDK 的最小
commit store 契约测试。

## 命名规则

命名必须服务调用处。

- root package 暴露跨模块常用 facade：`gopact.NewRunner`、`gopact.Setup`、`gopact.WithLogger`。
- 子包名使用单数名词：`graph`、`provider`、`tools`、`memory`、`sandbox`。
- 避免 stutter：调用处应写 `provider.NewRegistry()`，不要写 `provider.NewProviderRegistry()`。
- 单主类型包使用 `New()`；多主类型包使用 `NewTypeName()`。
- 布尔 option 使用自然语义：`WithStrictEvents()`、`WithDryRun()`；避免双重否定。
- 错误变量使用 `Err` 前缀；错误字符串小写。
- ID 字段统一使用 `UserID`、`SessionID`、`ThreadID`、`RunID`、`AgentID`、`CallID`，不混用 `UserId`。

## 返回值和错误

规则：

- 可能失败的构造函数返回 `(*T, error)`；
- 不隐藏 panic；runtime 边界可以 recover 并转成事件和 error；
- 错误向上返回时加上下文；
- SDK 内部不要 log-and-return；
- terminal event 不等于 iterator error；
- convenience API 可以返回最终结果，但必须能从 event stream 推导同样事实。

示例：

```go
result, err := runner.Invoke(ctx, input, opts...)
if err != nil {
	return fmt.Errorf("invoking agent: %w", err)
}
```

`Invoke` 可以作为 `Run` 的 convenience wrapper，但不能取代 `Run` 的事件流语义。

## 事件元数据的类型化 helper

事件可以携带 module-specific metadata，但调用方不应该在常见路径手写 `map[string]any`
类型断言。只要 metadata 表达的是 SDK 核心语义，就应该提供 root helper。

示例：

```go
for event, err := range runner.Run(ctx, input, opts...) {
	if err != nil {
		return err
	}
	plan, ok := gopact.EventEffectReplayPlan(event)
	if !ok {
		continue
	}
	results, replayErr := gopact.ExecuteEffectReplay(ctx, plan, executor)
	snapshot, ok := gopact.EffectReplaySnapshotFromEvent(event, results, replayErr)
	if !ok {
		continue
	}
	if err := gopact.RecordEffectReplayCheck(recorder, snapshot); err != nil {
		return err
	}
}
```

规则：

- helper 返回值必须是拷贝，不暴露事件内部可变结构；
- metadata key 仍可稳定序列化，但普通用户代码优先使用 helper；
- helper 要覆盖进程内 typed metadata 和 JSON export/import 后的结构化 metadata；
- helper 只提取或组装已经观察到的事实，不偷偷执行 tool、sandbox、model 或 replay；
- helper 缺失目标 metadata 时返回 `(zero, false)`，不 panic。

## 包边界与导入体验

用户常见代码应该只需要少量 import：

```go
import (
	"context"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/graph"
	"github.com/gopact-ai/gopact/provider"
)
```

规则：

- root package 承载稳定契约和 facade；
- adapter 放在 `adapters/...`，不污染 core import；
- plugin 放在 `plugins/...`，不让常规用户被迫导入；
- 测试 helper 可以放 `gopacttest` 或子包内 test helper，避免污染 root API；
- internal 实现可以后移到 `internal/`，但公开包不要过早拆碎。

## 示例和文档门槛

每个公开 API 合入前必须满足：

- 有 godoc 注释；
- 有至少一个可编译 `Example` 或调用片段测试；
- README 或设计文档里的示例能 `gofmt`；
- 示例不依赖真实 provider、真实网络或真实文件系统；
- 示例展示错误处理；
- 示例体现 context first；
- 示例不使用全局 mutable 变量。
- root package 新增顶层导出符号时，必须先更新 `public-api-boundary.json`，明确 category、stability、source file 和 rationale。
- root package 新增 exported method 时，receiver type 必须已经在 `public-api-boundary.json` 中登记；method 继承 receiver type 的 category、stability 和 deprecation policy。
- setup、runner、export/import/resume、verification 等关键 root 入口必须在 `public-api-examples.json` 中登记可执行 `ExampleXxx`，并由测试确认 example function 存在且引用的符号仍属于允许的 public API 类别。
- 废弃、移动或删除 public API 前必须遵守 [deprecation-policy.md](deprecation-policy.md)，包括 `Deprecated:` godoc 标记、迁移示例和移除窗口。

建议把关键 API 示例写成 `_test.go` 的 `ExampleXxx`，让 `go test ./...` 自动验证文档不会腐烂。

## API Review 清单

新增 exported API 前逐项检查：

- 调用处是否一眼能看懂依赖和权限？
- 参数是否超过 4 个？超过就考虑 option 或 typed struct。
- 是否引入了 `any`、`map[string]any` 或字符串枚举来承载核心语义？
- 是否能用 fake/noop 依赖单元测试？
- 是否会读取全局状态、环境变量或配置文件？
- 是否和现有 option 命名冲突？
- 是否已经在 `public-api-boundary.json` 中归入 `core-contract`、`runtime-facade`、`typed-option`、`middleware-plugin`、`export-import-resume`、`verification`、`reference-implementation` 或 `transitional`？
- 如果它是 exported method，receiver type 是否已经在 `public-api-boundary.json` 中登记？
- 如果它改变关键 root 调用形态，是否已经更新 `public-api-examples.json` 并补齐可运行 Example？
- 如果它废弃或替换旧入口，是否已经按 [deprecation-policy.md](deprecation-policy.md) 标记 `Deprecated:` 并记录迁移窗口？
- 是否能被 `gofmt` 排成稳定、清爽的形状？
- 是否有 Example 测试？
- 是否遵守错误只处理一次？
- 是否有安全默认值？

如果以上任一项答案含糊，先不要合入 API。SDK 的 public surface 一旦发布就是承诺。
