# gopact 设计哲学

<!-- gopact:doc-language: zh,en -->

## 中文

日期：2026-06-23

设计入口：[index.md](index.md)

`gopact` 是一个围绕显式运行时契约构建的 Go-first agent SDK。项目名是字面意义上的：系统里每一个重要边界都应该是一份可以检查、测试、回放和替换的 pact。

这份文档是项目的决策指南。当实现选择不明确时，优先选择更符合这些原则的方案。

## 使命

`gopact` 要让 Go 里的生产级 agent 系统变得可理解。

SDK 的目标不是让一行 demo 看起来惊艳，而是在 agent 拥有工具、长任务、人类审批、模型不确定性和多个运行时集成时，让它的行为可观察、可恢复、可测试。

换句话说，`gopact` 不替用户定义唯一 harness。SDK 提供的是构建 harness 所需的原子能力和过程契约：任务如何进入、每一步如何执行、状态如何导出、任意中断后如何恢复、事件如何复盘、权限如何裁决。Code agent、问答机器人、客服 agent、数据分析 agent 应该用同一套原语组装出不同 harness。

## 核心信念

### 契约就是产品

SDK 稳定的中心不是 provider adapter，也不是某个预置 agent，而是应用、运行时、模型、工具、状态和观察者之间的一组契约。

核心契约包括：

- `Message`：provider-neutral 的对话数据，必须能承载结构化 content parts。
- `ContentPart`：文本、推理内容、工具调用、工具结果、服务端工具、MCP 工具、多模态内容等消息片段。
- `ToolSpec`：暴露给模型的可调用能力 schema。
- `Tool`：暴露给应用的可执行能力。
- `ModelRequest`：provider-neutral 的模型调用输入。
- `ModelRoute`：模型调用在 provider、model、fallback 链路上的选择记录。
- `Event`：可观察的运行时记录。
- `StepSnapshot`：稳定 step 边界上的过程快照，用于 export、import、debug、replay 和 resume。
- `StepExport`：可迁移的 step 包，包含 snapshot、artifact refs 和 integrity 信息。
- `Checkpoint`：可持久化的执行快照。
- `RuntimeIDs`：贯穿 run、thread、session、user、agent、call 的身份契约。
- `Artifact`：可引用、可审计、可跨系统传递的输出。
- `PolicyDecision`：外部动作、权限、审批和脱敏的统一决策记录。
- `ConfigVersion`：配置快照版本，用于 replay、热加载和线上排障。

适配器负责翻译契约，但不能拥有契约。

### 运行时先于 agent 模式

ReAct、plan-execute、supervisor、reflection、多 agent delegation、deep-agent 等模式，都应该是运行时之上的组合。

运行时必须知道如何：

- 执行类型化节点；
- 流式输出执行事件；
- 持久化 checkpoint；
- 在 step 级别导出和导入执行快照；
- 中断和恢复；
- 为测试和评估暴露 trajectory 数据。

运行时不应该关心一个节点到底是模型、工具、retriever、planner，还是普通 Go 函数。

Template 内部的模型-工具循环必须建立在 step、event、checkpoint、policy 这些原子契约上。Loop engineering、context engineering、prompt engineering 是业务层和 template 层的组装方法，不应该下沉成 core 原子概念。

### 过程优先于结果

Agent 开发最重要的不是最终答案本身，而是产生结果的过程是否可以检查、打断、迁移和恢复。

`gopact` 的 workflow/graph 运行时必须做到：

- 每个 step 有稳定身份、输入、输出、错误、事件和副作用记录；
- step 完成后可以导出快照；
- 外部系统可以把 step 快照导入另一个进程、机器或之后的时间点继续执行；
- 任意中断都能落到最近的稳定 step 边界；
- resume 不能重复已经完成的非幂等副作用；
- replay 和测试可以断言中间过程，而不是只断言最终结果。

### 事件流是调试界面

Agent 系统的问题通常发生在执行中间，而不只发生在最终答案上。运行时的主要输出应该是一条有序事件流，用来说明发生了什么。

事件应该服务于：

- CLI 输出；
- Web UI 更新；
- trace 和 metrics；
- 人类审核；
- replay；
- trajectory 测试；
- 事后分析。

如果一个行为无法被事件表达，这个行为很可能隐藏得太深。

### 状态按生命周期拆分

不要用一个“memory”抽象承载所有状态问题。

`gopact` 应该拆分这些生命周期：

- run state：一次执行期间需要的数据；
- thread state：通过 checkpoint 持久化的单个对话或工作流状态；
- memory：跨 thread 的可搜索知识；
- artifacts：带名称和版本的二进制或文件类输出；
- telemetry：关于执行过程的观察数据，不是业务状态。

这吸收了 LangGraph checkpoint/store 分离，以及 Google ADK session/state/memory/artifact 分离中有价值的部分。

身份作用域也必须拆分，不能用一个 id 承载全部语义。`UserID` 表达最终用户，`SessionID` 表达用户会话，`ThreadID` 表达可恢复工作流，`RunID` 表达一次执行尝试，`AgentID` 表达当前 agent，`CallID` 表达当前 node/model/tool 调用。简单应用可以让部分 id 相同，但 SDK 契约不能假设它们天然相同。

### Go 应该保持 Go 的样子

SDK 应该像一个 Go library，而不是翻译成 Go 的 Python framework。

这意味着：

- 调用处先于抽象，先写用户代码再定 public API；
- 小接口；
- 尽量返回具体类型；
- 显式构造函数；
- 不使用隐藏全局 registry；
- 全局 `Setup` 只调整 SDK 默认值，不承担依赖查找或配置文件加载；
- core 不使用重反射的依赖注入；
- 可能阻塞的操作接收 `context.Context`；
- 用 generics 表达类型化 graph state；
- 普通函数可以自然成为节点和工具；
- 测试读起来像可执行示例。

## Core 边界

### 属于 core 的内容

Core package 可以定义：

- message、tool、model request、event、checkpoint；
- graph/runtime 原语；
- turn loop / runner root facade；
- checkpoint 接口和简单内存实现；
- step snapshot、step export/import 的基础契约；
- event、checkpoint、artifact、policy、config 的基础契约；
- provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的业务运行时模块 contract；
- hook point 明确后的 middleware 和 runner contract；
- trajectory 和 event assertion 的测试 helper。

### 不属于 core 的内容

Core package 不应该 import 或拥有：

- OpenAI、Gemini、Claude、Ark、Ollama 或其他 provider client；
- OpenRouter、CC Switch、企业网关等具体模型代理的专有配置模型；
- vector database；
- SQL/Redis/S3/GCS 等生产存储实现；
- Docker、Kubernetes、Firecracker 等生产 sandbox 后端；
- 具体 MCP server registry；
- 具体 A2A gateway；
- UI framework；
- tracing exporter；
- 应用特定的 agent template；
- harness engineering、context engineering、loop engineering、prompt engineering 的业务策略；
- prompt library。

这些应该放在 adapter package 或独立 module 里。Core 的职责是让 adapter 容易编写，而不是自己塞满所有 adapter。

## 插件与中间件

完整扩展性设计见 `doc/design/extensibility.md`。该文档定义了 hook、middleware、plugin 的分层、生命周期、执行顺序、错误语义和未来 API 形态。

运行时模块设计见 `doc/design/modules.md`。model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 是业务运行时模块，不是后置 plugin。`artifact`、`policy`、`config`、`event`、`checkpoint` 是基础契约和支撑能力。它们都应该有 core contract 和默认内存/本地实现，生产后端再通过 adapter 或 plugin 接入。

核心分层是：

- hook：运行时暴露的生命周期时点；
- middleware：围绕 node/model/tool/event 执行的链式包装；node middleware 采用类似 Gin 的 `c.Next()` 模型，让用户明确控制前置逻辑、后置逻辑、中止和短路；
- plugin：runner 级别的横切扩展，用来接入 telemetry、全局 policy、record/replay、评估、缓存、外部控制面等。

当前代码已经具备第一批扩展能力：`graph.Run` event stream、root `Runner` facade、`TurnLoop`、TurnLoop pending/resume queue、TurnLoop input merge strategy 第一片、`TurnLoopStore` 持久化第一片、内存/本地 file/row/blob/versioned CAS TurnLoop store、TurnLoop database/sql row/versioned CAS backend、TurnLoop HTTP/JSON control-plane row/versioned CAS backend、TurnLoop Redis GET/SET/EVAL row/versioned CAS backend、TurnLoop conditional object versioned CAS backend、leased TurnLoop wrapper、database/sql lease backend、Redis lease backend、HTTP/JSON control-plane lease backend、conditional object lease backend、`NodeContext`、`EventContext`、`PluginHost`、`PluginDescriptor` / `PluginCapability` 能力声明、插件 subscriber strict/fallback failure policy、`AsyncEventSink` bounded queue/backpressure 第一片、`SurfaceMessage` / `Transfer` / `Channel` / `ChannelEvent` root 契约第一片、writer-based TUI transfer/channel adapter 第一片、HTTP SSE transfer/channel adapter 第一片、host-injected Lark transfer/channel/callback source adapter 第一片、A2UI v0.9 JSON message transfer/JSONL channel/history replay/schema catalog validation/component JSON Schema validator 注入/client-supported catalog negotiation/in-memory reference renderer/action decode 第一片、AG-UI event transfer/HTTP SSE channel 第一片、`PolicyChannel` 第一片、skill registry/resource/script policy wrapper 第一片、trace HTTP/JSON exporter、trace OTLP/HTTP JSON exporter、LangSmith-compatible HTTP run exporter、LangGraph-style HTTP event exporter、trace exporter policy wrapper 第一片、MCP newline JSON-RPC client/transport 第一片、MCP newline interleaved server-to-client request handler、MCP newline notification handler、MCP Streamable HTTP POST + JSON/SSE response transport 第一片、MCP POST request-scoped SSE resume、MCP Streamable HTTP GET listen stream、MCP HTTP/SSE interleaved server-to-client capability request dispatch、MCP HTTP/SSE notification handler、MCP URL-mode elicitation completion notification handler 第一片、continuous listen reconnect/retry、session/protocol header、DELETE session termination 与 404 session-expired 处理第一片、MCP sampling/elicitation handler contract、policy wrapper 与 `CapabilityServer` JSON-RPC dispatch 第一片、MCP `ToolServer` server adapter 第一片、MCP manager/client policy wrapper 第一片，以及通过 `graph.WithNodeMiddleware` 接入节点执行路径的 Gin 风格 `Next()` node middleware、通过 Runner 接入的 event middleware、通过 provider router 接入的 model middleware、通过 tools registry 接入的 tool middleware、model/tool/channel policy middleware、model I/O redaction middleware、model rate limit middleware、tool result redaction middleware、memory/sandbox/artifact policy wrapper 和 event redaction middleware。这个切片足以验证 node 前置/后置逻辑、event 改写/drop/redaction、model/tool request-result 改写、model call 前限流 gate、TurnLoop 业务级输入合并、surface projection、transfer/channel contract、TUI writer channel 投递、SSE HTTP 投递与 action 回流、Lark text/interactive payload 投递、URL verification、action callback 与 action value decode、A2UI createSurface/updateComponents/updateDataModel JSONL 投递、history replay、local catalog registry 结构校验、component JSON Schema validator 注入、client-supported catalog negotiation、in-memory reference renderer 与 action decode、AG-UI event sequence transfer、HTTP SSE event stream 和 action POST 回流、skill register/search/activate/read/exec policy、trace span export policy、LangSmith-style project/trace/run/thread 映射、LangGraph-style thread/run/attempt event 映射、MCP connect/list/call/read/get/listen/session/sampling/elicitation policy、policy 阻断、短路、错误事件、plugin 注册链路、插件能力发现和非关键 subscriber/exporter 失败降级。

尚未完成的是 skill remote install、signing registry、script artifact capture 和更完整的 limits/workdir 策略、真实 LangSmith Go SDK / dataset/evaluation/feedback/run query、LangGraph 专项 exporter 的 policy/redaction 深化、完整前端 A2UI renderer、完整 JSON Schema engine adapter/plugin 与 conformance 深化、更完整 catalog negotiation 深化、AG-UI WebSocket/plugin 等生产级 channel adapter、Lark 真实 client/OAuth/plugin 接入、更多对象 checkpoint provider-specific 云 SDK 绑定和生产巡检审计深化。因此当前状态应该表述为“node/event/model/tool middleware 第一版、event sink strict/fallback 第一片、AsyncEventSink 第一片、policy/redaction/approval interrupt 第一片、memory/sandbox/artifact/channel policy wrapper 第一片、skill filesystem registry loader 第一片、skill registry/resource/script policy wrapper 第一片、skill local resource reader 和 sandbox script runner 第一片、trace HTTP/JSON exporter、trace OTLP/HTTP JSON exporter、LangSmith-compatible HTTP run exporter、LangGraph-style HTTP event exporter、trace exporter policy wrapper 第一片、MCP newline JSON-RPC client/transport 第一片、MCP newline interleaved server-to-client request handler、MCP newline notification handler、MCP Streamable HTTP POST + JSON/SSE response transport 第一片、MCP POST request-scoped SSE resume、MCP Streamable HTTP GET listen stream、MCP HTTP/SSE interleaved server-to-client capability request dispatch、MCP HTTP/SSE notification handler、MCP URL-mode elicitation completion notification handler 第一片、continuous listen reconnect/retry、session/protocol header、DELETE session termination 与 404 session-expired 处理第一片、MCP sampling/elicitation handler contract、policy wrapper 与 `CapabilityServer` JSON-RPC dispatch 第一片、MCP `ToolServer` server adapter 第一片、MCP manager/client policy wrapper 第一片、SurfaceMessage/Transfer/Channel root 契约第一片、writer-based TUI transfer/channel adapter 第一片、HTTP SSE transfer/channel adapter 第一片、host-injected Lark transfer/channel/callback source adapter 第一片、A2UI v0.9 transfer/JSONL channel/history replay/schema catalog validation/component JSON Schema validator 注入/client-supported catalog negotiation/in-memory reference renderer/action decode 第一片、AG-UI event transfer/HTTP SSE channel 第一片、插件宿主、能力声明、subscriber failure policy 和 lifecycle 状态机第一片、TurnLoop queue/store/input merge 第一片、FileTurnLoopStore 第一片、RowTurnLoopStore 端口第一片、TurnLoop database/sql row/versioned CAS backend 第一片、TurnLoop HTTP/JSON control-plane row/versioned CAS backend 第一片、TurnLoop Redis GET/SET/EVAL row/versioned CAS backend 第一片、TurnLoop conditional object versioned CAS backend 第一片、BlobTurnLoopStore 端口第一片、filesystem blob backend 第一片、通用对象 client blob backend 第一片、AWS SDK v2 S3 object/blob adapter 第一片、Google Cloud Storage object/blob adapter 第一片、Cloudflare R2 S3-compatible object/blob adapter 第一片、Alibaba Cloud OSS SDK v2 object/blob adapter 第一片、root lease/memory backend 第一片、leased TurnLoop wrapper、后台续约/锁争用观测第一片、database/sql lease backend 第一片、Redis lease backend 第一片、HTTP/JSON control-plane lease backend 第一片、conditional object lease backend 第一片、VersionedTurnLoopStore CAS 端口第一片、RowStore checkpoint 端口第一片、checkpoint database/sql backend 第一片、checkpoint Redis atomic row/index backend 第一片、checkpoint conditional object row/index backend 与索引 Verify/Repair 第一片、checkpoint AWS SDK v2 S3 conditional backend 第一片、checkpoint Google Cloud Storage generation-CAS backend 第一片、checkpoint Cloudflare R2 S3-compatible conditional backend 第一片、checkpoint Alibaba Cloud OSS SDK v2 conditional backend 第一片、ObjectStore checkpoint 端口第一片、filesystem object backend 第一片、通用对象 client checkpoint backend 第一片和 Runner/TurnLoop close 第一版已实现，横切扩展体系未完全闭环”。

落地顺序应该是：

1. 已补 event streaming，让运行时有稳定观察面。
2. 已引入 runner skeleton、`RuntimeIDs` 和 block-based message contract。
3. 已落地 artifact、provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的模块 contract 和默认内存/本地实现雏形。
4. 已先落地 node/event/model/tool middleware、policy requested/decided events、review-to-approval interrupt、redaction state 以及 memory/sandbox/artifact/channel/skill registry/resource/script/trace exporter/MCP manager/client policy wrapper 第一片，并补了 skill filesystem registry loader、local resource reader、sandbox script runner、MCP newline JSON-RPC client/transport、newline interleaved server-to-client request handler、newline notification handler、Streamable HTTP POST + JSON/SSE response transport、POST request-scoped SSE resume、GET listen stream、HTTP/SSE interleaved server-to-client capability request dispatch、HTTP/SSE notification handler、URL-mode elicitation completion notification handler、continuous listen reconnect/retry、session/protocol header、DELETE session termination、404 session-expired 处理、sampling/elicitation handler policy、`CapabilityServer` JSON-RPC dispatch 和 `ToolServer` server adapter 第一片；下一步补 approval UI/channel 和 event sink 策略。
5. 已有 `PluginHost` lifecycle 状态机、能力声明、subscriber failure policy、`AsyncEventSink`、幂等 close、close-while-running 等待、Runner/TurnLoop close、trace HTTP/JSON exporter、OTLP/HTTP JSON exporter、LangSmith-compatible HTTP run exporter 和 LangGraph-style HTTP event exporter 雏形；下一步补真实 LangSmith SDK、LangGraph-style exporter 的 policy/redaction 深化和剩余生产级 adapter。

原因很简单：没有事件流和 runner，plugin 只能变成全局回调集合，既难测试，也容易把 core 污染成 service locator。

## API 决策规则

评审新的 exported API 时使用这些规则。

1. Public type 应该命名一个稳定概念，而不是实现细节。
2. Public interface 应该小，通常一到三个方法。
3. 需要观察的行为应该产生事件。
4. 可能跨进程、跨机器或跨时间恢复的行为应该有 step export/import 和 checkpoint 方案。
5. 依赖模型 provider 的行为应该依赖 core contract，而不是 provider-specific type。
6. 如果一个 API 不连真实模型就无法测试，它耦合太深。
7. 隐藏状态或执行顺序的 shortcut 不应该进入 core。
8. Generic 必须带来真实类型安全，不要为了显得现代而使用 generics。
9. 依赖项应该比它解决的问题更小。
10. 如果一个概念只对一个示例有用，就放在示例里。
11. 全局默认值必须可被实例级 option 覆盖，并且不能反向修改已经创建的 Runner。
12. 新的 public API 必须先通过调用体验审查，见 `doc/design/api-ergonomics.md`。

## 运行时形态

目标运行时方向是：

```text
application input
  -> optional turn loop
  -> runner
  -> foundational contracts: event/checkpoint/artifact/policy/config
  -> runtime modules: provider routing/tool registry/sandbox/memory/skill/MCP/A2A
  -> graph/runtime
  -> node execution
  -> step snapshot/export/import
  -> model/tool/function calls
  -> event stream
  -> checkpoint/store/artifact/telemetry adapters
  -> surface message / transfer / channel adapters
```

当前的 `graph.Invoke` API 是过渡形态。目标运行时 API 应该支持事件迭代，很可能采用 Go 的 `iter.Seq2[Event, error]` 风格，因为 agent run 天然就是“值 + 可能错误”的流。`Runner` 只负责一次 run；多轮输入合并、抢占、取消和恢复应由 `TurnLoop` 负责。

Channel 在展示和交互边界有用，但不应该成为 core runtime 执行模块。Runtime 输出 event stream 和统一 `SurfaceMessage`；root package 只提供 `Transfer`、`Channel`、`ChannelEvent` 这些边界契约，TUI、Lark bot、Web、A2UI、AG-UI 等通过 transfer/channel adapter 接入。

## Go 版本策略

项目当前目标是 Go 1.25.11。最初的 Go 1.24 基线已经不能满足 2026-06 的安全和工具链要求：当前 golangci-lint v2 / govulncheck 线需要 Go 1.25+，且 Go 1.24 标准库安全修复不再覆盖我们需要的 gate。

当新 Go 特性能澄清运行时设计时再采用：

- 当 API 准备好时，用 `iter.Seq2` 表达事件流和模型流。
- 当测试取消逻辑因此更清晰时，使用 Go 1.24+ 的 testing context API。
- Go 1.25.11 是当前最低 patch 基线；只有当 Go 1.26+ 的标准库特性能实质改善正确性、安全性或开发体验时，才继续升级。

不要为了对齐 Google ADK 而升级 Go 版本。只有项目收益明确时才升级。

## 设计张力

### 类型化 graph 与动态 agent state

类型化 graph state 让 Go 用户更安全、测试更清楚。动态 state 在模型/工具边界有价值。用户工作流内部优先使用类型化 state，只有 provider 或 schema 边界才显式使用动态 map。

### 简单 core 与丰富能力

有用的 SDK 需要 adapter 和 template，但把所有东西放进 core 会让 core 不稳定。Core 保持小，覆盖面通过 adapter module 扩展。

### Human-in-the-loop 与全自动 agent

HITL 不是 UI 问题，而是运行时状态转移：执行暂停、状态持久化、决策进入、执行恢复或分支。它应该被视为 core runtime 能力。

### Cancel 与 interrupt

Cancel 是外部控制面终止当前 run，interrupt 是业务流程主动暂停等待恢复输入。二者都需要事件和 checkpoint，但不能复用同一个错误类型或状态枚举。Cancel 应该有安全点、递归取消和超时升级语义；interrupt 应该有明确的 resume payload 和恢复点。

### 最终答案测试与 trajectory 测试

最终答案不足以验证 agent 系统。项目应该让用户容易断言中间事件、工具选择、checkpoint 和中断行为。

## 近期路线

下一步设计动作应该是：

1. 用 event streaming 替换或补充 `graph.Invoke`。
2. 增加 node start、node end、step snapshot、checkpoint、error、interrupt 等运行时事件。
3. 增加 runner root facade、`RuntimeIDs`、block-based message contract、artifact/policy/config contract。
4. 增加 provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的业务运行时模块 contract 和默认实现。
5. 在完整 HITL flow 前先增加 cancel、interrupt/resume 数据结构。
6. 增加 step export/import 和 TurnLoop，用于任意中断后的导入、抢占、取消和恢复。
7. 增加 trajectory test helper。
8. 基于运行时契约构建一个小的 ReAct graph template。
9. 增加围绕 node、model、tool 执行的 middleware hook。
10. 在 runner 成型后增加 plugin manager 和插件生命周期。

## 非目标

现阶段 `gopact` 不应该尝试：

- 成为完整 LangChain 替代品；
- 包装所有模型 provider 或内置托管模型代理；
- 提供可视化 workflow editor；
- 内置重量级 vector memory 或托管 memory 产品；
- 决定 prompt engineering 策略；
- 决定 harness/context/loop engineering 策略；
- 用不透明 agent class 隐藏模型/工具执行；
- 让 demo 优先于运行时正确性。

## 评审清单

接受一个设计或实现变更前，先问：

- 它引入或修改了什么契约？
- 它能否通过事件被观察？
- 如果它是长任务，能否 checkpoint 或 resume？
- 它属于 core，还是 adapter/template？
- 它是否让 Go 用户写出更清晰的代码？
- 它是否可以不依赖真实模型测试？
- 它是否保持 provider-neutral？
- 它是否让状态生命周期保持显式？

## English

Design philosophy for gopact. It records the evidence-first, Go-first, provider-neutral principles used to decide what belongs in core.
