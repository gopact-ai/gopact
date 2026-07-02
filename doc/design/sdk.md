# gopact SDK 入口与默认值设计

<!-- gopact:doc-language: zh,en -->

## 中文

日期：2026-06-23

设计入口：[index.md](index.md)

`gopact` 首先是 Go SDK。除了运行时模块本身，还必须给用户一个稳定、可预测、可测试的 SDK 使用面：默认 logger、默认观测、默认时钟、默认 id 生成器、默认 redaction 和默认错误处理策略都应该有明确行为。

这层不是配置文件系统。它只定义 SDK 在进程内如何接收全局默认值，以及 Runner、TurnLoop、Run 如何覆盖这些默认值。

API 调用体验的通用规则见 [api-ergonomics.md](api-ergonomics.md)。本文件只细化 SDK setup 和默认值。

## 核心原则

1. SDK 默认值必须安全、安静、可测试。
2. 全局 setup 只影响未来创建的 Runner，不反向修改已经创建的 Runner。
3. Runner option 覆盖全局默认值。
4. Run option 覆盖 Runner 默认值，但不能默认扩大 policy。
5. SDK 不调用 `slog.SetDefault`，不修改应用的全局 logger。
6. 全局默认值必须用不可变 snapshot 或 atomic pointer 管理，避免 data race。
7. 测试必须能恢复默认值，避免全局状态污染并行测试。

优先级从高到低：

```text
RunOption
  -> Runner Option
  -> gopact.Setup defaults
  -> SDK built-in defaults
```

## Setup 入口

建议 root package 暴露：

```go
func Setup(opts ...SetupOption) error
func Defaults() DefaultsSnapshot
```

测试辅助可以放在 `gopacttest` 或只在内部测试使用：

```go
func ResetDefaultsForTest(t testing.TB)
```

`Setup` 语义：

- 校验所有 option，失败返回 error；
- 生成新的 immutable defaults snapshot；
- 原子替换全局 defaults；
- 不启动 goroutine；
- 不连接外部服务；
- 不读取文件；
- 不读取环境变量；
- 不修改已创建 Runner；
- 不清空用户已注册到某个 Runner 的实例级依赖。

不建议暴露 mutable 全局变量。用户不应该能写：

```go
gopact.DefaultLogger = logger
```

应该写：

```go
err := gopact.Setup(
	gopact.WithLogger(logger),
	gopact.WithLogLevel(gopact.LevelWarn),
)
```

## Logger

SDK 必须依赖 logger interface，而不是绑定具体 logging framework。

接口形态建议保持小：

```go
type Logger interface {
	Debug(ctx context.Context, msg string, attrs ...Attr)
	Info(ctx context.Context, msg string, attrs ...Attr)
	Warn(ctx context.Context, msg string, attrs ...Attr)
	Error(ctx context.Context, msg string, attrs ...Attr)
}

type Attr struct {
	Key   string
	Value any
}
```

默认 logger：

- 使用标准库 `log/slog` 适配器实现；
- 输出到 `os.Stderr`；
- 默认 level 是 warn；
- 只输出 warn 和 error；
- 不输出 prompt、tool args、tool result、memory content、sandbox stdout 等敏感内容；
- 结构化字段名必须稳定，例如 `run_id`、`thread_id`、`call_id`、`event_type`、`component`、`error`；
- SDK 内部返回错误时不同时日志打印，遵守“log once or return”规则。

用户覆盖方式：

```go
runner, err := gopact.NewRunner(
	graph,
	gopact.WithLogger(appLogger),
)
```

全局默认方式：

```go
err := gopact.Setup(gopact.WithLogger(appLogger))
```

规则：

- Runner 创建时拷贝当前 defaults snapshot；
- 后续 `gopact.Setup(WithLogger(...))` 不影响既有 Runner；
- Run scoped logger 只影响当前 run；
- plugin 可以注册 event sink，但不能替换 Runner logger，除非用户显式传入 option。

## 其他 SDK 默认值

和 logger 类似，以下能力也需要 SDK 级默认值或接口注入：

| 能力 | 默认值 | 用户覆盖 | 设计理由 |
| --- | --- | --- | --- |
| Logger | warn level stderr logger | `WithLogger` / `Setup(WithLogger)` | SDK 默认安静，问题可见 |
| Clock | real clock | `WithClock` | 测试 checkpoint、timeout、事件时间 |
| IDGenerator | crypto/random 或 monotonic+random | `WithIDGenerator` | 测试可重复，生产唯一 |
| Redactor | conservative redactor | `WithRedactor` | 默认不泄漏敏感内容 |
| ErrorHandler | return errors, no duplicate log | `WithErrorHandler` | 统一 panic recovery 和错误包装边界 |
| PanicHandler | recover at goroutine/runtime boundary | `WithPanicHandler` | 防止 plugin/channel goroutine 静默崩溃 |
| EventSink | in-memory/noop bounded sink | `WithEventSink` | 没有外部依赖也可测试 |
| Tracer | noop tracer | `WithTracerProvider` | 不强依赖 OTel SDK |
| Metrics | noop meter | `WithMeterProvider` | 不强依赖 Prometheus/OTel |
| StateCodec | JSON codec | `WithStateCodec` | checkpoint state 可序列化 |
| ArtifactStore | memory store | `WithArtifactStore` | 测试和本地开发可用 |
| Policy | deny external, allow local safe defaults | `WithPolicy` | 默认最小权限 |
| HTTPClient | nil unless adapter asks for one | adapter option | core 不默认发网络请求 |
| UserAgent | `gopact/<version>` | adapter option | 外部 adapter 可审计 |

不是所有能力都必须进入 `Setup` 第一版。M1 至少需要 logger、clock、id generator、redactor、event sink 和 state codec 的接口位置，哪怕实现先保持简单。

## Option 分层

建议分三类 option：

```go
type SetupOption func(*defaultsBuilder) error
type RunnerOption func(*runnerBuilder) error
type RunOption func(*RunConfig)
```

命名规则：

- 全局默认：`gopact.Setup(gopact.WithLogger(logger))`
- Runner 默认：`gopact.NewRunner(graph, gopact.WithLogger(logger))`
- 单次 run：`runner.Run(ctx, input, gopact.WithRunLogger(logger))`

当同名 option 适用于多个层级时，必须在文档里明确作用域。容易扩大权限的 option 应只允许在 Runner 层设置，Run 层只能收窄。

跨包 template 如果需要读取单次 run 身份、导入 step 或消费 resume payload，应使用 root facade 解析 option：

```go
cfg := gopact.ResolveRunOptions(opts...)
ids := cfg.IDs
step := cfg.StepExport
resume := cfg.ResumeRequest
```

`ResolveRunOptions` 只暴露已经稳定的 `RunConfig` 字段，例如 `IDs`、`StepExport` 和 `ResumeRequest`。它不应该成为绕过 Runner 生命周期、插件或权限策略的后门。Template 恢复 tool approval 时仍必须把 resume payload 带入 policy request，由 policy 决定 allow/deny/review。

`Runner.Run` 会把全局默认、Runner 默认和单次 run option 合并后的 `RuntimeIDs` 继续传给底层 `Runnable`，也会保留单次 run 的 step/resume option。Graph adapter 会把 root `WithStepExport` / `WithResumeRequest` 映射成 graph invoke option；其他 template 可以直接读取 `RunConfig`。这让 template 在构造 model/tool/memory request 时能看到同一组身份，同时 Runner 仍会在 event emission 边界补齐和校正事件身份。

`TurnLoop` 的输入合并也遵循同一套 SDK option 思路。默认情况下，`TurnLoop.Run` 在存在 pending input 或 interrupted input 时会生成 `TurnInputBatch`；如果宿主业务需要压缩多条用户消息、合并 IM action、过滤重复 resume 或转换成自己的 request shape，可以在 `NewTurnLoop` 时注入 `WithTurnInputMerge(func(ctx, req) (TurnInputMergeResult, error))`。merge function 只接收 defensive copy，不允许直接改写内部队列；返回的 metadata 会进入 `TurnInputMerged` 事件，便于审计业务合并策略。`WithResume` 还会被透传为底层 runner 可读取的 root `WithResumeRequest`，让 graph/template 不必从业务 input 中反解析恢复请求。

完整模型运行时和最小 template 入口之间通过 adapter 连接：

```go
router, _ := provider.NewRouter(registry, routes)
agent, _ := react.New(gopact.AdaptStreamingModel(router), tools)
```

`AdaptResponseModel` 只把 `ModelResponse.Message` 适配成最小 `ChatModel`；`AdaptStreamingModel` 会额外保留 `StreamingModel` 能力。Template 如果检测到 `StreamingModel`，应该消费 stream 中的 route/fallback/model events，并补齐自己的 node/step 信息。

Memory 写入也必须显式：

```go
agent, _ := react.New(model, tools, react.WithMemory(
	store,
	react.WithMemoryExtractor(func(ctx context.Context, state react.State, ids gopact.RuntimeIDs) ([]memory.Memory, error) {
		return nil, nil
	}),
))
```

Extractor 是 template 层 hook，不是 core 的默认行为。用户不配置 extractor 时，`WithMemory` 只做 recall，不会自动写入长期记忆。

## 线程安全

全局 defaults 必须是 immutable snapshot：

```go
type DefaultsSnapshot struct {
	Logger      Logger
	Clock       Clock
	IDGenerator IDGenerator
	Redactor    Redactor
	EventSink   EventSink
}
```

实现要求：

- `Setup` 构造新 snapshot 后一次性 swap；
- snapshot 内的 map/slice 必须深拷贝；
- `Defaults()` 返回副本或不可变视图；
- Runner 构造时复制 snapshot；
- 并发 `Setup` 和 `NewRunner` 不产生 data race；
- 测试提供 reset helper。

## SDK 默认行为清单

第一版实现时至少要明确：

- logger 默认 warn level；
- 没有 logger 时不 panic；
- 没有 provider 时只能使用 fake/noop provider 或返回结构化错误；
- 没有 checkpoint store 时可以运行，但不能 resume；
- 没有 policy 时使用安全默认 policy；
- 没有 artifact store 时使用 memory store；
- 没有 event sink 时仍可从 run iterator 读取事件；
- 没有 tracer/metrics 时不产生外部副作用；
- 没有 secret provider 时需要 secret 的 adapter 初始化失败；
- 没有 sandbox 时高风险 tool 注册失败或 deferred。

## 测试要求

- 默认 logger 只输出 warn/error；
- `WithLogger` 覆盖默认 logger；
- `Setup(WithLogger)` 只影响之后创建的 Runner；
- 已创建 Runner 不受后续 `Setup` 影响；
- `ResetDefaultsForTest` 能恢复内置默认值；
- 并发 `Setup` / `NewRunner` / `Run` 无 data race；
- 默认 redactor 早于 logger/exporter/channel；
- Run option 不能扩大 Runner policy；
- SDK 不调用 `slog.SetDefault`；
- SDK 不读取 env 或配置文件。

## English

SDK entry and defaults design. It defines setup behavior, default logger/runtime IDs, option precedence, and testability expectations.
