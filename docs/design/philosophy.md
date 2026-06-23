# gopact 设计哲学

日期：2026-06-23

设计入口：[index.md](index.md)

`gopact` 是一个围绕显式运行时契约构建的 Go-first agent SDK。项目名是字面意义上的：系统里每一个重要边界都应该是一份可以检查、测试、回放和替换的 pact。

这份文档是项目的决策指南。当实现选择不明确时，优先选择更符合这些原则的方案。

## 使命

`gopact` 要让 Go 里的生产级 agent 系统变得可理解。

SDK 的目标不是让一行 demo 看起来惊艳，而是在 agent 拥有工具、长任务、人类审批、模型不确定性和多个运行时集成时，让它的行为可观察、可恢复、可测试。

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
- 中断和恢复；
- 为测试和评估暴露 trajectory 数据。

运行时不应该关心一个节点到底是模型、工具、retriever、planner，还是普通 Go 函数。

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

- 小接口；
- 尽量返回具体类型；
- 显式构造函数；
- 不使用隐藏全局 registry；
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
- prompt library。

这些应该放在 adapter package 或独立 module 里。Core 的职责是让 adapter 容易编写，而不是自己塞满所有 adapter。

## 插件与中间件

当前实现还不具备真正的插件能力和中间件能力。

完整扩展性设计见 `docs/design/extensibility.md`。该文档定义了 hook、middleware、plugin 的分层、生命周期、执行顺序、错误语义和未来 API 形态。

运行时模块设计见 `docs/design/modules.md`。model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 是业务运行时模块，不是后置 plugin。`artifact`、`policy`、`config`、`event`、`checkpoint` 是基础契约和支撑能力。它们都应该有 core contract 和默认内存/本地实现，生产后端再通过 adapter 或 plugin 接入。

核心分层是：

- hook：运行时暴露的生命周期时点；
- middleware：围绕 node/model/tool/event 执行的链式包装；node middleware 采用类似 Gin 的 `c.Next()` 模型，让用户明确控制前置逻辑、后置逻辑、中止和短路；
- plugin：runner 级别的横切扩展，用来接入 telemetry、全局 policy、record/replay、评估、缓存、外部控制面等。

当前代码只有 `graph.Invoke`、checkpoint hook 和基础事件类型；还没有 runner、event streaming、middleware chain、plugin manager 或插件生命周期。因此现阶段是“设计完整，尚未实现”，不能说“能力已经具备”。

落地顺序应该是：

1. 先补 event streaming，让运行时有稳定观察面。
2. 引入 runner skeleton、`RuntimeIDs` 和 block-based message contract。
3. 落地 artifact、policy、provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的模块 contract 和默认实现。
4. 再加 node/model/tool/event 级 middleware。
5. 最后基于 runner 增加 plugin manager 和插件生命周期。

原因很简单：没有事件流和 runner，plugin 只能变成全局回调集合，既难测试，也容易把 core 污染成 service locator。

## API 决策规则

评审新的 exported API 时使用这些规则。

1. Public type 应该命名一个稳定概念，而不是实现细节。
2. Public interface 应该小，通常一到三个方法。
3. 需要观察的行为应该产生事件。
4. 可能跨进程运行的行为应该有 checkpoint 方案。
5. 依赖模型 provider 的行为应该依赖 core contract，而不是 provider-specific type。
6. 如果一个 API 不连真实模型就无法测试，它耦合太深。
7. 隐藏状态或执行顺序的 shortcut 不应该进入 core。
8. Generic 必须带来真实类型安全，不要为了显得现代而使用 generics。
9. 依赖项应该比它解决的问题更小。
10. 如果一个概念只对一个示例有用，就放在示例里。

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
  -> model/tool/function calls
  -> event stream
  -> checkpoint/store/artifact/telemetry adapters
  -> surface message / transfer / channel adapters
```

当前的 `graph.Invoke` API 是过渡形态。目标运行时 API 应该支持事件迭代，很可能采用 Go 的 `iter.Seq2[Event, error]` 风格，因为 agent run 天然就是“值 + 可能错误”的流。`Runner` 只负责一次 run；多轮输入合并、抢占、取消和恢复应由 `TurnLoop` 负责。

Channel 在展示和交互边界有用，但不应该成为 core runtime 抽象。Runtime 输出 event stream 和统一 `SurfaceMessage`；TUI、Lark bot、Web、A2UI、AG-UI 等通过 transfer/channel adapter 接入。

## Go 版本策略

项目当前目标是 Go 1.24。这是合理基线：它支持 Go 1.23 的 iterator model，同时不会在 SDK 还不需要时强行要求 Go 1.25。

当新 Go 特性能澄清运行时设计时再采用：

- 当 API 准备好时，用 `iter.Seq2` 表达事件流和模型流。
- 当测试取消逻辑因此更清晰时，在测试中使用 Go 1.24 的 testing context API。
- 只有当 Go 1.25 的标准库特性能实质改善正确性或开发体验时，才考虑升级。

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
2. 增加 node start、node end、checkpoint、error、interrupt 等运行时事件。
3. 增加 runner root facade、`RuntimeIDs`、block-based message contract、artifact/policy/config contract。
4. 增加 provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的业务运行时模块 contract 和默认实现。
5. 在完整 HITL flow 前先增加 cancel、interrupt/resume 数据结构。
6. 增加 TurnLoop，用于多轮输入、抢占、取消和恢复。
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
