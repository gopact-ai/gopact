# gopact 扩展性设计

日期：2026-06-23

设计入口：[index.md](index.md)

本文定义 `gopact` 的 hook、middleware、plugin 扩展模型。它是设计文档，不代表当前代码已经实现这些能力。

## 设计目标

扩展机制要解决三个问题：

1. 让用户能在不 fork core 的情况下增强运行时行为。
2. 让所有扩展行为都能通过事件流观察和测试。
3. 让扩展点有清晰边界，避免变成隐藏全局状态或 service locator。

扩展机制不应该解决这些问题：

- 不应该替代 graph 编排；
- 不应该替代 provider adapter；
- 不应该让插件直接修改任意内部结构；
- 不应该绕过 checkpoint、event、context 这些运行时契约。

## 分层定义

### Hook

Hook 是运行时暴露的生命周期时点。它回答“什么时候可以观察或介入”。

Hook 不应该是用户主要编程模型。Hook 是 middleware 和 plugin 的底层时点。

典型 hook：

- run start / run end / run error；
- node start / node end / node error；
- model request / model response / model error；
- tool call / tool result / tool error；
- cancel requested / cancel completed；
- checkpoint write；
- interrupt raised / resume received；
- event emitted。

### Middleware

Middleware 是围绕某类执行动作的链式包装。它回答“如何改变这一次动作”。

Middleware 作用域小，应该挂在明确的执行边界上：

- node middleware 包裹 node execution；
- model middleware 包裹 model invocation；
- tool middleware 包裹 tool invocation；
- event middleware 包裹 event emission。

Middleware 可以：

- 观察输入输出；
- 增加 timeout/retry/rate limit；
- 改写请求；
- 拒绝执行并返回错误；
- 短路并返回结果；
- 生成额外事件。

Middleware 不应该：

- 持有全局 registry；
- 直接访问 runner 内部结构；
- 私自写 checkpoint；
- 吞掉错误而不产生事件；
- 在没有 context 取消检查的情况下做长时间阻塞工作。

### Plugin

Plugin 是 runner 级别的模块化扩展。它回答“如何给整个运行时增加一组横切能力”。

Plugin 可以注册 middleware、订阅事件、管理生命周期，也可以持有自己的外部资源。

典型 plugin：

- logging plugin；
- OpenTelemetry plugin；
- LangSmith plugin；
- policy plugin；
- record/replay plugin；
- evaluation plugin；
- cache plugin；
- audit plugin；
- prompt-injection defense plugin；
- human approval plugin。
- A2UI / AG-UI transfer plugin；
- TUI / Lark bot channel plugin。

Plugin 不应该成为任意依赖查找器。它可以接入 runner 生命周期，但不能把 runner 变成全局 container。

## 执行层级

扩展机制的执行层级如下。它只描述 middleware/plugin 在一次执行里的包裹顺序，不是另一套 runtime 架构：

```text
Runner
  Plugin lifecycle
  Run middleware
    Graph runtime
      Node middleware
        Node function
          Model middleware
            ChatModel
          Tool middleware
            Tool
      Event middleware
        Event sink
      Checkpoint writer
```

Plugin 处在 runner 层，负责组合能力。Middleware 处在具体执行边界，负责改变动作。Hook 是 runtime 在关键时点发出的内部信号和扩展入口。

## API 草案

以下 API 形态用于约束设计方向，后续实现时可以调整命名，但不应改变分层语义。

### RunContext

运行时需要一个显式上下文对象，传递只读运行信息和受控能力。

```go
type RuntimeIDs struct {
	UserID       string
	SessionID    string
	ThreadID     string
	RunID        string
	AgentID      string
	AppID        string
	CallID       string
	ParentCallID string
	TraceID      string
}

type RunContext struct {
	IDs          RuntimeIDs
	Step         int
	Node         string
	Attempt      int
	CheckpointID string
	Metadata     map[string]any
}
```

原则：

- `context.Context` 仍用于取消、超时和值传递；
- `RunContext` 用于 SDK runtime metadata 和当前执行边界身份；
- `RuntimeIDs` 是只读身份，不应该被 middleware 原地修改；
- 不把所有内部对象塞进 `RunContext`；
- `Metadata` 只放扩展相关的轻量数据，不放业务主状态。

身份字段必须显式拆分，不能把所有场景压成一个 id：

| 字段 | 作用域 | 用途 |
| --- | --- | --- |
| `UserID` | 跨 session 的最终用户或账号 | 权限隔离、审计、长期记忆、个性化 |
| `SessionID` | 一次用户会话或前端对话容器 | UI 会话、临时上下文、用户可见的对话归属 |
| `ThreadID` | 可 checkpoint/resume 的工作流时间线 | 恢复、中断、time travel、thread-scoped state |
| `RunID` | 一次运行或重放尝试 | 事件流、trace、评估、幂等和排障 |
| `AgentID` | 当前 agent/persona/worker | 多 agent 隔离、agent 级 memory 和 policy |
| `AppID` | 应用、租户、部署面 | 多应用隔离、导出、治理和批量清理 |
| `CallID` | 当前 node/model/tool 调用 | 输入输出关联、tool call 对账、trace span |
| `ParentCallID` | 父调用 | 嵌套 tool/model 调用和多 agent delegation 关联 |
| `TraceID` | 分布式追踪链路 | 对接 OpenTelemetry 或外部 trace 系统 |

参考拆分：

- mem0 的 entity-scoped memory 使用 `user_id`、`agent_id`、`app_id`、`run_id` 隔离记忆空间，其中 `run_id` 适合短生命周期流程、ticket 或会话。
- LangGraph 把 thread-scoped checkpoint、cross-thread store、static runtime context 和 execution info 分开；execution info 中包含 `thread_id`、`run_id` 和 attempt 信息。
- `gopact` 不把 `SessionID`、`ThreadID`、`RunID` 合并。简单应用可以让它们取同一个值，但 SDK 契约必须允许它们不同。

### Node middleware

Node middleware 应该是最早落地的 middleware，因为当前已有 graph runtime。

Node middleware 参考 Gin 的 middleware chain 机制，特别是 `Context.Next()` 推进后续 handler、`Context.Abort()` 阻止 pending handler 的语义；但 `gopact` 要按 agent runtime 的可恢复、可观察要求做约束。用户通过 `c.Next()` 明确推进后续 middleware 和实际 node 执行；`c.Next()` 之前的代码是前置逻辑，`c.Next()` 之后的代码是后置逻辑。

```go
type NodeMiddleware[S any] func(c *NodeContext[S]) error

type NodeContext[S any] struct {
	// opaque fields
}

func (c *NodeContext[S]) Context() context.Context
func (c *NodeContext[S]) Run() RunContext
func (c *NodeContext[S]) IDs() RuntimeIDs
func (c *NodeContext[S]) Node() string
func (c *NodeContext[S]) Step() int
func (c *NodeContext[S]) Attempt() int

func (c *NodeContext[S]) Input() S
func (c *NodeContext[S]) SetInput(input S)
func (c *NodeContext[S]) Output() (S, bool)
func (c *NodeContext[S]) SetOutput(output S)

func (c *NodeContext[S]) Next() error
func (c *NodeContext[S]) Abort(err error)
func (c *NodeContext[S]) Return(output S)
func (c *NodeContext[S]) IsAborted() bool
func (c *NodeContext[S]) NodeRan() bool
func (c *NodeContext[S]) Err() error
```

执行模型：

```text
WithNodeMiddleware(a, b, c)

a before
  b before
    c before
      actual node
    c after
  b after
a after
```

示例：

```go
func Audit[S any](c *NodeContext[S]) error {
	start := time.Now()
	err := c.Next()
	// 这里一定发生在后续 middleware 和实际 node 之后。
	recordNodeLatency(c.Node(), time.Since(start), c.NodeRan(), err)
	return err
}

func Policy[S any](c *NodeContext[S]) error {
	if !allowed(c.Node(), c.Input()) {
		c.Abort(ErrPolicyDenied)
		return ErrPolicyDenied
	}
	return c.Next()
}

func Cache[S any](c *NodeContext[S]) error {
	if output, ok := lookupNodeCache[S](c.Node(), c.Input()); ok {
		c.Return(output)
		return nil
	}
	return c.Next()
}
```

`Next()` 的含义：

- 推进到下一个 node middleware；
- 如果没有更多 middleware，则执行实际 node；
- 返回后，当前 middleware 可以读取 `Output()`、`Err()`、`NodeRan()`；
- `Next()` 只能调用一次，第二次应返回明确错误，避免重入和重复执行副作用；
- `Next()` 必须尊重 `context.Context` 取消。

中止和短路语义：

- `Abort(err)`：停止后续 middleware 和实际 node，当前 node 失败，错误进入事件流；
- `Return(output)`：停止后续 middleware 和实际 node，当前 node 被视为成功完成，输出为指定 state；
- middleware 如果既不调用 `Next()`，也不调用 `Abort()` 或 `Return()`，runtime 应返回 `ErrMiddlewareDidNotContinue`，不能静默跳过 node；
- `Abort` 不应阻止当前 middleware 中已经开始的后置逻辑执行，但会阻止 pending middleware 和 actual node。

输入输出语义：

- `Input()` 是当前 node 的输入 state，必须包含进入 node 时所有可用输入；
- `SetInput()` 只影响后续 middleware 和 actual node 的输入；
- `Output()` 返回当前 node 的输出 state，以及输出是否已经产生；
- `SetOutput()` 只用于 `Next()` 后的后置改写，或与 `Return()` 语义一致的显式短路；
- `Return(output)` 必须同时设置输出，并标记 actual node 没有执行；
- 后置改写必须产生事件，便于 replay 和测试。

`NodeContext` 必须至少包含当前执行边界的全部输入、全部输出、全部身份字段和错误状态。实现上可以把业务 state 设计为泛型 `S`，也可以在更高层 runner 中把 model/tool 的原始 request、response 放进事件；但 node middleware 看到的 `Input()` / `Output()` 必须足以做 audit、cache、policy、replay 和测试断言。

设计要求：

- middleware 顺序必须稳定；
- 每个 middleware 都必须能拿到 `context.Context`；
- node error 必须保留原始错误链；
- middleware 产生的错误要进入 event stream；
- middleware 不应该直接决定 graph 下一条边，分支逻辑应属于 graph；
- 不调用 `Next()` 必须是显式控制行为，不能变成隐式跳过；
- `Abort`、`Return`、`Next` 的结果必须能通过事件流观察。

参考：Gin `Context.Next` / `Context.Abort` 源码语义见 https://github.com/gin-gonic/gin/blob/master/context.go。

### Model middleware

Model middleware 包裹 `ChatModel.Generate`。

```go
type ModelHandler func(ctx context.Context, request ModelRequest) (Message, error)

type ModelMiddleware interface {
	WrapModel(next ModelHandler) ModelHandler
}
```

适合能力：

- request logging；
- provider route observation；
- retry/backoff；
- retry/failover decision observation；
- response validation；
- structured output repair；
- prompt policy；
- token/cost accounting；
- model cache。

设计要求：

- 请求改写必须可观察；
- provider-specific metadata 只能放在 metadata，不污染核心 request；
- retry 必须尊重 `context.Context`；
- 如果短路返回缓存结果，必须产生可区分事件。

Provider routing 的状态机不应该只藏在 model middleware 里。多 provider 注册、能力目录、健康状态、fallback 链、预算和错误分类属于 `provider.Router` 的核心模块 contract；model middleware 可以包裹 router 入口，做 trace、redaction、cache、retry policy 装饰，但不能成为唯一的路由状态持有者。否则 typed snapshot 热替换、circuit breaker、session stickiness 和 replay 都会变成隐藏副作用。

Retry/failover 决策应属于 provider router 或其策略对象。Model middleware 可以观察、补充 metadata、做 redaction 或缓存，但如果它改写下一次请求、切换候选模型或复用上一次成功模型，必须通过 router 事件表达，不能只在 middleware 内部完成。

### Tool middleware

Tool middleware 包裹 `Tool.Invoke`。

```go
type ToolHandler func(ctx context.Context, spec ToolSpec, args json.RawMessage) (ToolResult, error)

type ToolMiddleware interface {
	WrapTool(next ToolHandler) ToolHandler
}
```

适合能力：

- 参数校验；
- visible/deferred tool filtering；
- allow/deny list；
- sandbox；
- human approval；
- timeout；
- result redaction；
- audit log；
- idempotency key。

设计要求：

- 工具调用前后都要有事件；
- 参数和结果需要支持脱敏；
- human approval 应该走 interrupt/resume，而不是阻塞等待；
- tool middleware 不应该修改工具 schema，schema 改写应通过 tool registry 或 adapter 完成。
- tool middleware 可以调整当前调用可见工具集合，但必须通过 `tools.Registry` 的 promotion/filter 语义完成，并产生事件。

### Event middleware

Event middleware 包裹事件发出过程。

```go
type EventHandler func(ctx context.Context, event Event) error

type EventMiddleware interface {
	WrapEvent(next EventHandler) EventHandler
}
```

适合能力：

- event enrichment；
- redaction；
- sampling；
- fan-out 到 trace/log/channel；
- record/replay；
- evaluation capture。

设计要求：

- event middleware 不应该改变业务执行结果；
- event sink 失败的策略必须可配置；
- 默认策略应该是关键事件失败返回错误，非关键外部 sink 失败只产生诊断事件；
- event redaction 要早于外部 sink。

### Plugin

Plugin 是组合扩展能力的 runner-level 模块。

```go
type Plugin interface {
	Name() string
	Setup(ctx context.Context, registry PluginRegistry) error
	Close(ctx context.Context) error
}

type PluginRegistry interface {
	UseNodeMiddleware(NodeMiddleware[...])
	UseModelMiddleware(ModelMiddleware)
	UseToolMiddleware(ToolMiddleware)
	UseEventMiddleware(EventMiddleware)
	Subscribe(EventType, EventSubscriber)
}
```

上面 `NodeMiddleware[...]` 需要在真实实现时解决泛型注册问题。可能方案有两种：

1. graph-level 注册 typed node middleware，不通过 plugin registry 统一注册；
2. plugin 只注册非泛型 runtime hook，typed middleware 仍由 graph/runner option 注入。

推荐方案：早期避免让 plugin 直接注册泛型 node middleware。先让 plugin 管理 model/tool/event middleware 和事件订阅，node middleware 通过 graph option 保持类型安全。

### Event subscriber

事件订阅用于只观察、不改写。

```go
type EventSubscriber interface {
	OnEvent(ctx context.Context, event Event) error
}
```

与 event middleware 的区别：

- event middleware 可以改写、过滤或路由事件；
- event subscriber 只消费事件；
- subscriber 不应影响主执行，除非配置为 strict。

### LangSmith / LangGraph 观测接入

LangGraph 事实上的观测体系是 LangSmith。`gopact` 可以通过 plugin 接入这套体系，但接入层应该放在 adapter/plugin package，而不是放进 core。

推荐分两层实现：

1. `otel` plugin：把 `gopact` event stream 和 model/tool/node middleware 转成 OpenTelemetry spans。
2. `langsmith` plugin：复用 `otel` plugin 的 span 输出，通过 OTLP endpoint 或 LangSmith Go SDK 写入 LangSmith。

这样做的原因：

- LangSmith 当前支持 OpenTelemetry trace ingestion，非 LangChain 应用也可以用标准 OTel client 发送 trace。
- LangSmith 已有 Go SDK，可以用于 run 查询、dataset、evaluation、experiment 和直接 API 集成。
- OTel 是更稳定的边界。用户可以把同一份 trace fan-out 到 LangSmith、Jaeger、Tempo、Datadog 或内部平台。
- core 不应该依赖 LangSmith API key、网络 endpoint 或 vendor-specific SDK。

事件到 trace 的映射建议：

| gopact 事件/边界 | Span 类型 | 关键属性 |
| --- | --- | --- |
| `RunStarted` / `RunCompleted` | root agent/run span | `run_id`、`thread_id`、`session_id`、`user_id`、`agent_id` |
| `NodeStarted` / `NodeCompleted` | node span | `node`、`step`、`attempt`、`checkpoint_id` |
| `ModelRequested` / `ModelResponded` | LLM span | provider、model、token usage、latency、cost metadata |
| `ToolCalled` / `ToolReturned` | tool span | tool name、call id、arguments hash、result status |
| `CheckpointWritten` | span event | checkpoint id、thread id、step |
| `InterruptRaised` / `ResumeReceived` | span event | interrupt id、reason、resume source |
| `RunFailed` / `NodeFailed` / provider error | span status + error event | wrapped error type、message、retry attempt |

LangSmith 属性建议：

- 用 `TraceID` / `CallID` 关联 OTel trace/span；
- 把 `UserID`、`SessionID`、`ThreadID`、`RunID`、`AgentID`、`AppID` 放入 metadata；
- model/tool 的输入输出默认先经过 redaction middleware；
- 大 payload 不直接塞进 span attribute，应该走 artifact/attachment 或只记录 hash、size、schema；
- tags 用于环境、版本、graph name、agent template，不承载高基数字段。

插件边界：

- `langsmith` plugin 只能订阅事件、注册 event/model/tool middleware、管理 exporter 生命周期；
- 不能改变 graph 分支、checkpoint 内容或业务结果；
- exporter 默认异步、带 bounded queue，网络失败不阻断主执行；
- strict 模式可以让关键 trace 写入失败返回错误，但默认不建议开启；
- redaction 必须早于 LangSmith exporter，避免把敏感 prompt、tool args 或用户数据发到外部系统。

这套接入的目标是让 `gopact` 运行结果能出现在 LangSmith 的 trace、monitor、feedback、dataset/evaluation 工作流中；不是复用 LangGraph 的 Python callback runtime。

## 生命周期

Runner 生命周期：

```text
NewRunner
  -> plugin Setup
  -> Run start
  -> event stream
  -> Run end / Run error
  -> runner Close
  -> plugin Close
```

Run 生命周期：

```text
RunStarted
  -> NodeStarted
  -> ModelRequested / ToolCalled / custom node work
  -> ModelResponded / ToolReturned
  -> NodeCompleted
  -> CheckpointWritten
  -> RunCompleted
```

错误路径：

```text
RunStarted
  -> NodeStarted
  -> NodeFailed
  -> RunFailed
```

中断路径：

```text
RunStarted
  -> NodeStarted
  -> InterruptRaised
  -> CheckpointWritten
  -> RunInterrupted
  -> ResumeReceived
  -> NodeResumed
  -> RunCompleted / RunFailed
```

取消路径：

```text
RunStarted
  -> NodeStarted
  -> CancelRequested
  -> safe point reached
  -> CheckpointWritten
  -> RunCanceled
```

Cancel 与 interrupt 的区别：

- cancel 来自外部控制面，用于终止当前 run；
- interrupt 来自业务流程，用于暂停并等待 resume payload；
- cancel 可以打断 pending middleware 或递归取消子调用；
- interrupt 不应该被当成 error，也不应该复用 cancel error；
- cancel 超时可以升级为 hard cancel，但必须产生事件。

## 顺序规则

Middleware 顺序必须满足：

1. 用户显式顺序优先；
2. plugin 注册顺序稳定；
3. core safety middleware 先于用户 middleware；
4. observability middleware 应尽量包在外层；
5. redaction middleware 必须早于外部 sink；
6. retry middleware 应包住 provider call，但不应重复执行非幂等 tool，除非 tool 声明可重试。

建议默认顺序：

```text
policy
  observability
    timeout
      retry
        cache
          user middleware
            actual call
```

Tool middleware 需要更保守：

```text
policy
  approval
    timeout
      audit
        actual tool
```

## 错误语义

错误分三类：

- user error：用户输入、工具参数、配置导致；
- runtime error：graph、checkpoint、middleware、plugin 生命周期导致；
- provider error：模型、外部工具、外部存储导致。

设计要求：

- 错误必须保留 `errors.Is` / `errors.As` 可用的 wrapping；
- middleware 不应吞掉错误；
- plugin lifecycle error 应阻止 runner 创建或关闭成功；
- event sink error 默认不应破坏业务执行，除非该 sink 被配置为 strict；
- checkpoint error 应默认终止执行，因为恢复语义已经不可靠；
- interrupt 不是 error，应有独立事件和状态。

## 状态修改权限

扩展点对状态的修改权限必须受限：

| 扩展点 | 可读 | 可写 | 说明 |
| --- | --- | --- | --- |
| Hook | runtime metadata | 不直接写 | 只暴露时点 |
| Node middleware | node 输入、输出、runtime 身份 | 通过 `SetInput`、`Return`、`SetOutput` 控制 state | 类型安全，`Next` 显式推进 |
| Model middleware | model request/response | 可改写 request/response | 必须产生事件 |
| Tool middleware | tool args/result | 可拒绝或改写 result | 参数需可审计 |
| Event middleware | event | 可改写 event | 不改业务结果 |
| Plugin | runner metadata/event | 通过注册扩展间接影响 | 不直接改内部结构 |

## 与 checkpoint 的关系

Checkpoint 是恢复语义的基础，不能被扩展机制绕开。

规则：

- checkpoint 写入应发生在 node 成功完成后；
- interrupt 前必须有 checkpoint；
- middleware 可以观察 checkpoint 事件，但不应私自写 checkpoint；
- plugin 可以提供 checkpoint backend adapter，但不应改变 checkpoint record 的核心字段；
- checkpoint error 默认终止运行。

## 与 HITL 的关系

Human-in-the-loop 应该由 tool/model/node middleware 触发，但通过 runtime interrupt/resume 表达。

错误做法：

- middleware 内部阻塞等待人工审批；
- tool middleware 返回普通 error 表示需要审批；
- plugin 在外部保存一份不可回放的 pending 状态。

正确方向：

```text
ToolMiddleware detects approval requirement
  -> runtime emits InterruptRaised
  -> checkpoint persists pending state
  -> caller receives RunInterrupted
  -> resume input carries approval result
  -> runtime continues or branches
```

## 与 adapter 的关系

Provider adapter 不应该实现独立扩展系统。它们应该接入 core 的 model/tool middleware。

例如：

- OpenAI adapter 接收 `ModelRequest`，返回 `Message`；
- Gemini adapter 接收同一契约；
- middleware 对两者都可用；
- provider-specific 细节放进 metadata 或 adapter package，不污染 core。

Channel adapter 同理不应该成为另一套 runtime。A2UI、AG-UI、SSE、TUI、Lark bot、飞书卡片等 adapter 只消费 event stream、`SurfaceMessage`、artifact refs 和 interrupt/resume 事件。Transfer 负责把统一 `SurfaceMessage` 转成目标 payload，channel adapter 负责投递和接收用户动作。用户动作必须通过 TurnLoop 的 input/resume/action 边界回到运行时，不能从 channel adapter 直接调用 graph/node。

## 包布局建议

未来实现可以按以下包演进：

```text
gopact
  content.go
  event.go
  ids.go
  model.go
  tool.go
  runner.go
  turnloop.go
  middleware.go
  plugin.go

graph
  graph.go
  stream.go
  middleware.go

checkpoint
  memory.go

provider
tools
artifact
policy
sandbox
memory
skill
mcp
a2a
```

早期不建议单独创建 `hook` 包。Hook 是 runtime 内部时点和扩展设计语言，不一定需要成为公开包。

`Runner` 和 `TurnLoop` 的公开入口由 root package 暴露。后续如果内部实现变大，可以拆 internal package，但不应先把用户 API 分散到公开 `runner` / `turnloop` package。

模型 provider 的生产实现建议放在独立 adapter package：

```text
adapters/model/openai
adapters/model/anthropic
adapters/model/gemini
adapters/model/openai-compatible
adapters/model/openrouter
adapters/transfer/a2ui
adapters/transfer/agui
adapters/transfer/lark
adapters/transfer/tui
adapters/channel/sse
adapters/channel/websocket
adapters/channel/lark
adapters/channel/tui
```

`openrouter` 可以复用 `openai-compatible` transport，但保留独立 adapter 或 config preset 有价值，因为它有 gateway 级 provider routing、model fallback 和 auto-router 参数。

## 最小落地切片

不写完整实现时，后续最小实现顺序应该是：

1. `graph.Run(ctx, state, opts...) iter.Seq2[Event, error]`
2. `EventType` 补齐 node/model/tool/checkpoint/interrupt 生命周期事件。
3. `NodeContext[S]`、`NodeMiddleware[S]` 和 `WithNodeMiddleware`，采用 `c.Next()` 显式推进链路。
4. `EventMiddleware` 和 event sink。
5. `ModelMiddleware`、`ToolMiddleware`。
6. root `Runner` facade。
7. root `TurnLoop` facade。
8. `Plugin`、`PluginRegistry`、plugin lifecycle。

这样做的原因：

- 先有事件流，扩展行为才可观察；
- 先有 typed node middleware，不破坏 graph 的 Go 类型安全；
- model/tool middleware 可以在 ReAct template 前落地；
- plugin 必须等 runner 成型，否则生命周期没有归属。

## 评审清单

设计或实现扩展能力前，先问：

- 这是 hook、middleware，还是 plugin？
- 它作用在 run、node、model、tool、event 的哪个边界？
- 它是否能通过 event stream 观察？
- 它失败时是否有明确错误语义？
- 它是否会破坏 checkpoint/resume？
- 它是否保留 Go 类型安全？
- 它是否引入隐藏全局状态？
- 它是否需要 provider-specific 类型？如果需要，是否应该放到 adapter？
