# gopact Agent SDK 生态调研

日期：2026-06-23

范围：LangGraph、LangChain、CloudWeGo Eino、Google ADK、A2UI、OpenRouter、CC Switch、oh-my-pi。目标不是复制任何一个框架，而是为名为 `gopact` 的 Go SDK 提炼可长期使用的设计原则。

项目级设计哲学已经沉淀到 `docs/design/philosophy.md`。本文解释这些原则从哪里来；哲学文档是后续 API 和运行时工作的决策指南。

## 摘要

这些框架最强的共同模式，是把 agent 意图和运行时机制分离。

LangChain 更适合理解为 agent harness：它围绕模型、工具、prompt、结构化输出和 middleware 组织模型-工具循环。它的优势是集成面广，以及高度可配置的 `create_agent` 入口。

LangGraph 是更高级 agent 底下的运行时层：类型化状态、节点、边、super-step 执行、checkpoint 持久化、中断、恢复和 streaming。它的优势是让长时间运行、需要人类审核的 agent 变得可靠。

Eino 是最直接相关的 Go 参考。它使用类型化组件接口、graph/chain/workflow 编排、基于 graph 组合的 ReAct agent、事件流、runner 式执行、checkpoint/resume 和多 agent 组合。它的优势是用 Go generics 和显式接口完成符合 Go 习惯的组合。

Google ADK 是最强的生产运行时参考：event loop、不可变 event、session、state、memory service、artifact、callback/plugin、runner 级扩展，以及 trajectory evaluation。

对 `gopact` 来说，核心方向应该是：

1. 契约优先：message、tool、event、model request、checkpoint record 应该是稳定、provider-neutral 的 Go 契约。
2. 运行时优先：graph execution、event streaming、checkpoint、interrupt 应该是一等能力，而不是后续挂到 agent loop 上的补丁。
3. Go-native API：小接口、类型化 graph state、构造函数注入、无隐藏全局状态、core 不使用重反射 DI。
4. 最小 core，可选 adapter：模型 provider、vector store、observability、存储后端应尽量放在 core 之外。
5. Provider routing 是运行时核心模块：多 provider 不能只停留在手动切换，要支持配置驱动的能力匹配、错误分类、健康状态、预算和自动 fallback。

## 框架发现

### LangChain

LangChain v1 以 `create_agent` 为中心，可以理解为围绕 model、prompt、tool 和 middleware 的可配置 harness。Agent 本身是在循环里让模型调用工具直到完成。真正值得提炼的不是循环本身，而是循环周围的 middleware 层。

值得吸收的想法：

- 把 agent 看成 `Model + Harness`，不要看成单个不透明 class。
- 工具可以是普通 callable，schema 从类型提示和 docstring 推导。
- 结构化输出是一等响应模式。LangChain 可以在 provider 支持时使用 provider-native structured output，否则退化为 tool-based structured output。
- middleware 承担生产关注点：retry、tool filtering、context editing、human review、dynamic prompt、guardrail。
- 生态价值来自大量 integration 使用一致契约。

对 `gopact` 的含义：现在先定义小的 model/tool/message 契约，但不要硬编码 provider 行为。Middleware hook 应该围绕模型调用、工具调用和运行事件。

### LangGraph

LangGraph 是关键的编排参考。它把 workflow 建模为 state + node + edge。Node 负责工作，edge 决定下一步运行什么。Graph 在执行前 compile，这让结构校验、checkpointer、breakpoint 等运行时选项成为可能。

值得吸收的想法：

- Graph state 是显式的，并由 node 更新。
- 执行按 graph step 推进，而不是隐藏在 while-loop 里。
- 持久化有两层：thread-scoped graph snapshot 的 checkpointer，以及跨 thread 长期记忆的 store。
- Interrupt 是动态且条件化的。Node 可以暂停执行、持久化状态，并随后从同一逻辑点恢复。
- Streaming 暴露执行进展，模式包括 updates、values、messages、checkpoints、tasks、debug data。

对 `gopact` 的含义：先做类型化 graph execution 和 checkpoint hook，再构建完整 ReAct agent。可靠 agent 应该是 graph pattern，而不是特殊 runtime。

### Eino

Eino 是最接近的实现参考，因为它是 Go-first。公开文档和内部用法都强调组件接口、graph/chain/workflow 组合、ADK agent、runner 式执行。

值得吸收的想法：

- 组件是标准能力契约：`ChatModel`、`Tool`、`Retriever`、`ChatTemplate`、`Lambda` 和相关 node。
- Graph/chain/workflow 都是同一 compose engine 之上的编排形态。
- Graph 支持一般拓扑，包括 loop。Workflow 强调 DAG 式字段映射，并分离控制流和数据流。
- Eino ADK 围绕一个返回异步事件输出的 `Agent` 接口。
- Runner/TurnLoop 提供执行入口、事件流、checkpoint/resume 和多轮生命周期。
- 多 agent 组合优先使用 `AgentAsTool` 和 workflow composition。
- HITL 与 interrupt/resume 是架构级能力，不是 UI-only 特性。

对 `gopact` 的含义：graph 层保持 Go generics，但 root SDK contract 保持简单。尽早暴露事件流和 checkpoint store。未来的 ReAct、plan-execute、supervisor、deep-agent 模式都应该是同一运行时上的库级组合。

Eino v0.9 的 `agentic-runtime` 更新对 `gopact` 有直接参考价值：

- `AgenticMessage` 使用 content block 模型，能表达文本、推理内容、工具调用、工具结果、服务端工具、MCP 工具和多模态内容。`gopact.Message` 也应该升级为 block-friendly contract，而不是长期停留在单字符串 content。
- Model retry/failover 变成 decision function，可以读取失败 attempt 的输入、输出、错误和 attempt 序号，再决定是否重试、改写输入或切换模型。`gopact.provider.Router` 应采用同类决策点。
- Middleware 增强里出现 `ToolInfos` 和 `DeferredToolInfos`，说明工具暴露应该拆成当前可见工具和延迟发现工具。`tools.Registry` 应显式建模 visible/deferred tools。
- `TurnLoop` 把单次 agent run 提升为多轮运行时，支持输入合并、抢占、取消和声明式 checkpoint/resume。`gopact` 应把 `TurnLoop` 放在 `Runner` 之上，避免 Runner 变成长期在线状态机。
- Cancel 和 interrupt 需要区分：cancel 是外部控制面终止当前 run，interrupt 是业务流程暂停等待恢复输入。两者都要进事件流，但不能复用同一个语义。

### A2UI

A2UI 是独立协议，不是 Eino 自己定义的 UI 协议。官方站点把它定义为 agent-driven interface protocol：agent 发送声明式 UI 描述，client 使用自己的原生组件渲染，不执行任意代码。

值得吸收的想法：

- UI 是数据，不是代码。Agent 只能使用 client catalog 中预先批准的组件。
- UI 更新是流式 JSON message 序列，适合边生成边渲染。
- 组件结构、数据模型和 data binding 分离，便于增量更新。
- A2UI 是 transport-agnostic，可以通过 A2A、AG-UI 或自定义 JSON transport 传输。
- 用户交互通过 action 回到 agent；renderer 侧 local function 和发给 agent 的 event 要分开建模。
- 当前生产版本是 v0.9.1，v1.0 candidate 增加 `actionResponse`，支持 client-to-server action response。

对 `gopact` 的含义：

- A2UI 不应该进入 core runtime，也不应该成为默认 UI 抽象。
- `gopact.Event` 必须足够结构化，便于投影成 `SurfaceMessage`，再转成 A2UI、AG-UI、SSE、WebSocket、TUI、Lark card 等目标格式。
- 可以后置 `adapters/transfer/a2ui`：把 `SurfaceMessage` 转成 A2UI messages。
- UI/channel action 应作为 TurnLoop input、resume payload 或受控 action 进入运行时，不能让 channel adapter 直接调用 graph/node。
- A2UI 的安全边界值得借鉴：只输出声明式组件和 data binding，不让模型发可执行 UI 代码。

进一步看，A2UI 只是 channel/transfer 生态中的一种目标格式。`gopact` 应该先定义统一 `SurfaceMessage`，再通过 transfer 转成 A2UI、AG-UI、TUI line、Lark card 或 SSE/WebSocket payload。这样 Lark bot、TUI、Web 都能复用同一套 agent 输出语义。

### Google ADK

Google ADK 适合作为生产运行时参考。它的 runtime 是一个 event loop，协调 agent、tool、callback、LLM call、state change 和 storage。Event 是信息流的基础单位。

值得吸收的想法：

- Runtime 是 engine：用户定义 agent 和 tool，runtime 负责编排执行。
- Event 是不可变记录，表达用户输入、agent 输出、工具调用、工具结果、状态变化、控制信号和错误。
- Session、State、Memory 是不同概念：session 是会话 thread，state 是 session-scoped scratchpad，memory 是跨 session 的可搜索知识。
- Artifact 管理 session 或 user 作用域下的版本化二进制数据。
- Callback 和 plugin 是两个扩展层级：callback 可以挂到具体 agent/tool，plugin 作用于 runner 全局。
- Evaluation 包括 trajectory/tool-use evaluation，而不只是最终答案评估。

对 `gopact` 的含义：event 设计要包含足够 metadata，以服务 debugging 和 evaluation。State、memory、artifact、evaluation 应该是分离的 package 或 interface，而不是一个过载的“memory”抽象。

### OpenRouter、CC Switch、oh-my-pi

多 provider 能力需要拆成三层看：

- API gateway routing：OpenRouter 的 `models` 参数支持按优先级自动 fallback；provider routing 还能指定 provider 顺序、禁用 fallback、按价格或 BYOK 偏好排序；Auto Router 进一步把 prompt 路由到候选模型池，并提供 allowed models 和 cost/quality tradeoff。
- 配置和代理层：CC Switch 的核心价值是把 Claude Code、Codex、Gemini CLI 等工具的 provider 配置统一管理，并提供 local proxy、hot-switching、auto-failover、circuit breaker、provider health monitoring。
- 本地模型目录和兼容性层：oh-my-pi 的 `models.yml` 覆盖 provider、model override、认证解析、role alias、enabled model pattern、gateway routing、provider/model compat 和 context promotion；它有 context overflow 时自动 promotion 到更大上下文模型的机制，但这更像特定错误上的 recovery，不等同于完整的条件路由系统。

对 `gopact` 的含义：

- 要区分 model routing 和 provider routing。换 provider 不一定换 model，换 model 也不一定换 provider。
- Router 必须理解硬能力：tool calling、forced tool choice、JSON schema、vision、streaming、long context、reasoning params。
- 自动 fallback 必须由稳定错误类型触发，不能靠 provider error 字符串匹配。
- 外部配置层需要能组装候选列表、选择条件、fallback 错误、预算、区域和 degradation policy，并以 typed `RouteSet` 注入 SDK。
- 同一 `ThreadID` 默认应该保持 session stickiness；切换模型或 provider 必须产生事件，并清理 provider session cache。
- OpenRouter 或企业网关可以作为 adapter，但 `gopact` core 不应该绑定某个 gateway 的配置模型。

## 对比矩阵

| 维度 | LangChain | LangGraph | Eino | Google ADK | gopact 方向 |
| --- | --- | --- | --- | --- | --- |
| 主抽象 | Agent harness | Stateful graph runtime | Go components + ADK | Runtime + agents + services | Contracts + typed runtime |
| 编排 | Agent loop | Nodes, edges, state | Graph, chain, workflow, agents | Runner event loop | Typed graph first |
| 工具 | Callable/schema tools | Tool nodes through graph | Standard and enhanced tools | Function tools and tool context | JSON-schema tool contracts |
| 持久化 | 通过 LangGraph checkpointer | Checkpoints + stores | Runner/compose 的 CheckPointStore | SessionService, MemoryService | Checkpointer first, store later |
| HITL | Middleware + interrupts | Dynamic interrupts/resume | ADK/compose interrupt/resume | Event/control flow patterns | Interrupt event + checkpoint |
| Streaming | Agent stream | Runtime stream modes | Stream reader 和 event iterator | Event streams | Event iterator |
| 扩展性 | Middleware | Runtime config, nodes | Callbacks, middleware, components | Callbacks and plugins | Middleware + runner plugins |
| Go 相关性 | 低 | 概念相关 | 高 | ADK Go 参考价值高 | 使用 Go-native 子集 |

## 设计哲学快照

完整项目哲学见 `docs/design/philosophy.md`。简版如下：

- 契约就是产品；
- 运行时先于 agent 模式；
- 事件流是调试界面；
- 状态按生命周期拆分；
- core 保持 provider-neutral；
- Go API 保持显式、类型化、可测试；
- adapter 和 template 应在 core 之外生长，除非它们定义稳定契约。

这些原则把调研转化为评审规则。一个新功能提案应该说明它添加了哪个 core contract、如何体现在事件里、是否需要 checkpoint/resume，以及为什么它属于 core 而不是 adapter 或 template。

## 上下文和身份拆分

`NodeContext` 不应该只是 `context.Context + state` 的薄包装。它需要承载当前执行边界的全部输入、全部输出、错误状态和身份字段，否则 middleware、audit、cache、policy、memory、trajectory test 都无法稳定对账。

参考结论：

- mem0 把记忆按 `user_id`、`agent_id`、`app_id`、`run_id` 分区。它的核心价值不是字段名本身，而是把“人”“agent persona”“应用面”“短生命周期流程”拆成不同查询和治理维度。
- LangGraph 把 static runtime context、dynamic state、cross-thread store、execution info 拆开。`thread_id` 归 checkpoint/resume，`run_id` 归一次执行，attempt/task/checkpoint id 归执行诊断。
- `gopact` 应该显式保留 `UserID`、`SessionID`、`ThreadID`、`RunID`、`AgentID`、`AppID`、`CallID`、`ParentCallID`、`TraceID`。简单场景可以只填其中几个字段，但事件、checkpoint、middleware 和插件都应该使用同一组身份字段。

对 API 的含义：`NodeContext[S]` 应该暴露 `Input()`、`Output()`、`IDs()`、`Run()`、`Next()`、`Abort()`、`Return()`，而不是让用户从任意 metadata map 里猜字段。metadata 只承载扩展数据，不承载核心身份。

## 当前骨架

第一版骨架建立了：

- Root package `gopact`：message、tool contract、model request contract、event contract。
- Package `graph`：类型化 graph execution，包含 `Start`、`End`、compile validation、node execution、thread id 和 checkpointer hook。
- Package `checkpoint`：内存 checkpoint store，包含 list/latest 操作。
- 测试：tool adaptation、graph execution order、graph compile validation、checkpoint persistence。

它故意比 LangChain、LangGraph、Eino、ADK 小。下一步应该从已测试契约增长：

1. 增加核心契约：`ContentPart`、`RuntimeIDs`、`ArtifactRef`、`PolicyDecision`、`ConfigVersion`。
2. 增加 graph event streaming，优先考虑 Go 的 `iter.Seq2[Event, error]` 风格。
3. 增加 checkpoint/resume record，包括 cancel-safe point、interrupt/resume 和副作用幂等。
4. 增加 provider routing contract 和 typed `RouteSet`。
5. 增加 visible/deferred tool registry。
6. 增加 TurnLoop，用于多轮输入、抢占、取消和恢复。
7. 增加 trajectory test helper。
8. 增加 model-tool ReAct graph template。
9. 增加围绕 node/model/tool 执行的 middleware hook。
10. 在 runner 成型后增加 plugin manager 和插件生命周期。

## 插件与中间件当前状态

当前实现尚不具备插件能力或中间件能力。

完整扩展性设计见 `docs/design/extensibility.md`。该设计已经明确：

- hook 是运行时生命周期时点；
- middleware 应该是围绕 node/model/tool 执行的链式 hook，其中 node middleware 采用类似 Gin 的 `c.Next()` 模型，让用户明确控制 node 执行前后的逻辑；
- plugin 应该是 runner 级横切扩展；
- 扩展行为必须通过事件流可观察，并且不能依赖隐藏全局 registry。

当前代码里还没有 runner、event streaming、middleware chain 或 plugin manager。已有的 checkpoint hook 只是运行时扩展能力的最小雏形，不能等同于 middleware 或 plugin。

## LangSmith 可观测接入

LangGraph 的观测标杆主要落在 LangSmith 上。LangGraph 文档把 trace 定义为从输入到输出的一系列 execution steps，每个 step 是一个 run；LangSmith 用这些 trace 做本地调试、性能评估和生产监控。

`gopact` 应该支持接入 LangSmith，但不应该把 LangSmith SDK 放进 core。更好的边界是：

- core 负责稳定事件流、运行身份、输入输出、错误和 token/cost metadata；
- `otel` plugin 把事件和 middleware 边界转成 OpenTelemetry spans；
- `langsmith` plugin 通过 LangSmith 的 OTLP endpoint 或 LangSmith Go SDK 把 trace 写入 LangSmith；
- redaction/sampling/export failure policy 都属于 plugin 配置，不属于 graph 语义。

这个设计能进入 LangSmith 的 trace、monitor、feedback、dataset/evaluation 工作流，也保留了对 Jaeger、Tempo、Datadog、内部 OTel collector 的可移植性。

## 运行时模块结论

`gopact` 的第一版运行时不能只包含 graph、model、tool 和 checkpoint。生产 agent 一开始就会遇到这些基础边界：

- provider routing：多 provider、多模型、多 fallback 的配置驱动选择和自动切换；
- tool registry：当前可见工具、延迟发现工具和工具搜索；
- sandbox：执行脚本、命令和文件操作的安全边界；
- memory：checkpoint 之外的长期记忆和跨 thread 召回；
- skill：通过 `SKILL.md`、脚本、references、assets 封装过程知识；
- MCP：连接外部 tools、resources、prompts 的标准协议；
- A2A：远程 agent 发现、任务委派、状态流和 artifact 交换。

Provider routing 是模型调用边界，和 model middleware 配合但不等于 middleware。MCP 和 A2A 的边界必须清晰：MCP 是 agent-to-tool/data/prompt，A2A 是 agent-to-agent task/artifact。Skill 是本地或组织级能力包，sandbox 是 skill script、tool、本地 MCP server 的执行安全边界，memory 是所有 agent 模式共享的长期状态层。

Artifact、policy、config、event、checkpoint 是基础契约或支撑能力，不归入业务运行时模块清单，但必须在第一版运行时中存在。

完整设计见 `docs/design/modules.md`。

## 资料来源

- LangGraph / LangSmith observability：https://docs.langchain.com/oss/python/langgraph/observability
- LangSmith observability：https://docs.langchain.com/langsmith/observability
- LangSmith OpenTelemetry tracing：https://docs.langchain.com/langsmith/trace-with-opentelemetry
- LangSmith Go SDK：https://github.com/langchain-ai/langsmith-go
- MCP overview：https://modelcontextprotocol.io/docs/getting-started/intro
- MCP architecture：https://modelcontextprotocol.io/docs/learn/architecture
- A2A specification：https://github.com/a2aproject/A2A/blob/main/docs/specification.md
- Google A2A announcement：https://developers.googleblog.com/en/a2a-a-new-era-of-agent-interoperability/
- Agent Skills overview：https://agentskills.io/home
- Anthropic Agent Skills article：https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
- LangGraph memory overview：https://docs.langchain.com/oss/python/concepts/memory
- LangGraph 概览：https://docs.langchain.com/oss/python/langgraph/overview
- LangGraph graph API：https://docs.langchain.com/oss/python/langgraph/graph-api
- LangGraph 持久化：https://docs.langchain.com/oss/python/langgraph/persistence
- LangGraph 中断：https://docs.langchain.com/oss/python/langgraph/interrupts
- LangGraph runtime：https://docs.langchain.com/oss/python/langchain/runtime
- LangGraph context overview：https://docs.langchain.com/oss/python/concepts/context
- LangGraph fault tolerance：https://docs.langchain.com/oss/python/langgraph/fault-tolerance
- LangChain agents：https://docs.langchain.com/oss/python/langchain/agents
- LangChain 结构化输出：https://docs.langchain.com/oss/python/langchain/structured-output
- LangChain middleware：https://docs.langchain.com/oss/python/langchain/middleware/built-in
- mem0 entity-scoped memory：https://docs.mem0.ai/platform/features/entity-scoped-memory
- OpenRouter model fallbacks：https://openrouter.ai/docs/guides/routing/model-fallbacks
- OpenRouter provider routing：https://openrouter.ai/docs/guides/routing/provider-selection
- OpenRouter auto router：https://openrouter.ai/docs/guides/routing/routers/auto-router
- CC Switch：https://github.com/farion1231/cc-switch
- oh-my-pi model configuration：https://github.com/can1357/oh-my-pi/blob/main/docs/models.md
- Eino ADK 概览：https://www.cloudwego.io/docs/eino/core_modules/eino_adk/
- Eino v0.9 agentic-runtime：https://www.cloudwego.io/zh/docs/eino/release_notes_and_migration/eino_v0.9._agentic-runtime/
- Eino ADK agent 概览：https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_preview/
- Eino ADK quickstart：https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_quickstart/
- Eino components：https://www.cloudwego.io/docs/eino/core_modules/components/
- Eino graph 介绍：https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/chain_graph_introduction/
- Eino workflow：https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/workflow_orchestration_framework/
- Eino tools：https://www.cloudwego.io/docs/eino/core_modules/components/tools_node_guide/
- A2UI official site：https://a2ui.org/
- A2UI concepts：https://a2ui.org/concepts/overview/
- A2UI transports：https://a2ui.org/concepts/transports/
- A2UI actions：https://a2ui.org/concepts/actions/
- Google ADK event loop：https://adk.dev/runtime/event-loop/
- Google ADK events：https://adk.dev/events/
- Google ADK sessions/state/memory：https://adk.dev/sessions/
- Google ADK artifacts：https://adk.dev/artifacts/
- Google ADK callbacks：https://adk.dev/callbacks/types-of-callbacks/
- Google ADK plugins：https://adk.dev/plugins/
- Google ADK evaluation：https://adk.dev/evaluate/
- Google ADK Go 仓库：https://github.com/google/adk-go
