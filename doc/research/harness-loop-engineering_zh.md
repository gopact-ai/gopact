# Harness Engineering 与 Loop Engineering 调研

<!-- gopact:doc-language: zh -->

[英文文档](./harness-loop-engineering.md)

## 中文

日期：2026-06-23

范围：AI Harness Engineering、Agentic Harness Engineering、HarnessX、Loop Engineering、turn-control、MCP overthinking loop、long-horizon iterative coding degradation。目标是判断这些概念对 `gopact` 的 SDK 设计有什么实际启发，而不是追随术语热度。

## 摘要

Harness engineering 的核心观点是：agent 能力不是模型单独提供的，而是 `model + harness + environment` 组合产生的。这里的 harness 不是测试 harness，而是模型外部的运行时基座，包含任务规格、上下文选择、工具访问、记忆、任务状态、可观测性、失败归因、验证、权限、熵审计和人工介入记录。

Loop engineering 的核心观点是：用户不应该反复手写单轮 prompt，而应该设计可重复运行、可观测、可停止、可复盘的 agent 循环。一个 loop 能够调度 agent、工具、技能、插件、子 agent 和外部系统，但它也会带来成本失控、循环诱导、质量退化和安全边界扩大的风险。

对 `gopact` 的直接结论：

1. `gopact` 不应该提供唯一 harness，而应该提供构建 harness 所需的原子过程能力。
2. Harness engineering、context engineering、loop engineering、prompt engineering 是业务层和 template 层的方法论，不应该下沉为 core 原子能力。
3. Core 应沉淀更底层的 step、event、checkpoint、artifact、policy、export/import、resume 契约。
4. 每次 run 应该能产出可审计的 run export：输入、事件、step exports、checkpoint、artifact、权限决策和最终结果。
5. Prompt-only 优化价值有限。长期收益更可能来自工具、middleware、memory、policy、event、verification、artifact 等结构化组件，但这些组件仍要以原子契约暴露。
6. MCP、A2A、skill 和 plugin 都会扩大 agent 的行动空间。必须把 tool-call 图、预算、重复模式和权限边界作为 runtime 资源管理。

## 概念边界

### Harness engineering

`AI Harness Engineering: A Runtime Substrate for Foundation-Model Software Agents` 把 harness 定义为围绕 foundation model 的运行时基座，负责模型如何观察项目、采取动作、接收反馈并证明任务完成。它列出的 11 个职责非常适合转成 `gopact` 的 runtime checklist：

| Harness 职责 | gopact 对应位置 |
| --- | --- |
| task specification | `Runner.Run` 输入、template task contract、interrupt/resume |
| context selection | memory、skill、deferred tool search、template context loader |
| tool access | `tools.Registry`、MCP bridge、A2A bridge、sandbox |
| project memory | `memory.Store`、skill references、developer guide artifact |
| task state | graph state、checkpoint、TurnLoop pending input |
| observability | event stream、OTel/LangSmith plugin、record/replay |
| failure attribution | structured error、failure decision、verification report |
| verification | trajectory tests、deterministic checks、template verification node |
| permissions | policy、approval interrupt、sandbox boundary |
| entropy auditing | diff review、dependency churn、stale docs、residue detection |
| intervention recording | HITL event、resume payload、approval record |

这说明 `gopact` 当前模块方向是对的，但文档需要更明确地说：这些模块是 SDK 原子能力，真正的 harness 由使用者或 template 根据业务场景组装。

### Agentic harness engineering

`Agentic Harness Engineering` 更进一步：不仅手写 harness，还让另一个 agent 根据运行轨迹改进 harness。它的关键不是“让 agent 自己乱改自己”，而是三层 observability：

- component observability：把可编辑 harness component 明确分成文件级或类型级单元，能定位、diff、rollback；
- experience observability：把海量 trajectory 压缩成分层证据，让后续 agent 能 drill down；
- decision observability：每个 harness 改动都带预测，下一轮用结果验证，失败就能回滚。

对 `gopact` 的启发是：即使第一版不做自演化，也应该从 Day 1 让 middleware、plugin、template、tool、skill、memory policy 具备可观测、可替换、可比较的边界。这里沉淀到 core 的不是 harness evolution 这个业务概念，而是 step export/import、事件、checkpoint、policy 和 artifact 这些过程证据。

### HarnessX

HarnessX 把 harness 建模为 typed primitives，通过 lifecycle hook 组合，随后用 trace-driven adaptation engine 持续改进。它对 `gopact` 最有价值的点是：harness component 要可组合、可替换、可隔离，而不是在一个 runner 内部靠 if/else 拼起来。

这强化了已有设计：

- `Runner` 只组合接口，不拥有模块实现；
- middleware 挂在明确的 node/model/tool/event 边界；
- plugin 注册 middleware 和订阅事件，不直接改 graph；
- template 是 graph 组合，不定义另一套 runtime；
- adapter 翻译外部系统，不污染 core contract。

## Loop engineering

行业里说的 loop engineering，通常是让 agent 在一个目标下持续工作，而不是用户一轮轮发 prompt。最近的公开讨论把 loop 拆成 automations、worktrees、skills、plugins/connectors、sub-agents 等要素。这个说法有现实意义，但对 SDK 设计来说还不够精确。

`gopact` 应该把 loop 看成受控运行时过程：

```text
input
  -> plan / context selection
  -> action decision
  -> model/tool/agent step
  -> observation
  -> attribution
  -> verification
  -> continuation decision
  -> terminal / interrupt / resume / retry / escalate
```

一个合格 loop 至少要回答：

- 为什么继续？
- 为什么停止？
- 为什么调用这个工具？
- 为什么切换 provider/model？
- 为什么把工具提升为 visible？
- 为什么需要人工介入？
- 为什么当前结果已经验证完成？
- 当前 loop 消耗了多少 token、时间、步骤、工具调用和人工注意力？

因此 `TurnLoop` 只负责多轮输入、抢占、取消和恢复还不够。ReAct、Dev Agent、Agent-as-Tool 等 template 内部也需要自己的过程决策记录。两者不要混在一起：

| 概念 | 作用 |
| --- | --- |
| `TurnLoop` | 用户输入层的多轮控制，处理 preempt、cancel、pending input、resume |
| template loop | 任务内部的模型、工具、验证循环，由 template 或应用层决定 |
| template decision | 对继续、停止、升级、压缩、fallback、interrupt 的业务层记录 |
| `RunExport` | 一次 run 的通用过程包，包含事件、step exports、checkpoint、artifact 和 policy decision |

## 风险

### 成本和 turn-control

`More with Less` 的 turn-control 研究说明，coding agent 的成本和 turn 数高度相关，固定 turn limit 能显著降成本，动态扩展比固定限制更好。对 `gopact` 来说，`MaxSteps` 只是最低配，还应该支持：

- step budget；
- token budget；
- wall-clock budget；
- tool-call budget；
- per-tool budget；
- dynamic extension decision；
- budget exhausted 的结构化 terminal event。

预算不是 provider adapter 的内部细节，而是 template 过程控制的一部分。

### MCP overthinking loop

`Overthinking Loops in Agents` 说明，恶意或不良 MCP tool 可以通过看似合理的工具描述和返回消息诱导 agent 进入重复调用、反复 refine 或注意力转移，造成 token/latency 放大。只限制输出长度解决不了这个问题，因为问题在 tool-call 结构。

对 `gopact` 的要求：

- tool registry 要记录 tool-call 图和重复模式；
- deferred tools 不能自动进入 visible set；
- MCP tool 返回的建议不能自动执行；
- tool middleware 可以拦截重复调用、循环依赖和异常 fan-out；
- policy 要能基于 tool sequence 做 deny 或 interrupt；
- event assertion helper 要支持断言“不发生重复工具链”。

### 长周期质量退化

`SlopCodeBench` 的结果提醒我们：agent 在多 checkpoint 自我扩展代码时，即使局部通过测试，也会出现结构侵蚀和冗余膨胀。更强 prompt 可以改善初始质量，但不能阻止长期退化。

对 `gopact` 自举设计的要求：

- Dev Agent 不能只看测试通过，还要看 diff 质量；
- episode package 要包含 entropy audit；
- 低风险写入也要记录文件残留、依赖变更、测试弱化、复杂度集中、重复代码；
- 自举 milestone 不能因为“能 apply patch”就升级到日常开发，必须等 trajectory test、review 和 entropy audit 成熟。

## 对当前设计的影响

### 1. 把 gopact 明确为 harness substrate

建议在 `philosophy.md` 和 `index.md` 中使用这个表述：`gopact` 是 Go-first agent SDK，提供构建 agent harness 所需的原子过程能力。Agent template 是一种可选组装，provider adapter 是外部接入，channel 是展示输出。

这能避免项目走向两个误区：

- 只做 graph 执行器，忽略工具、记忆、权限、验证和可观测；
- 只做 agent template 集合，把关键运行时能力藏进 prompt 和 while-loop。

### 2. 增加 RunExport 和 StepExport 作为 evaluation/replay 边界

`Event` 是流，`RunExport` 是一次执行结束后的过程包，`StepExport` 是单个稳定 step 的迁移包。当前代码已经有 `RunExport` / `RunRecorder` / `ReplayRunExport` 第一片，也已经把 task/input/intervention/failure process records、entropy audit records 和 run-level verification reports 接入 `RunExport`；`RunRecorder` 的 `RunFailed` 派生归因已能消费 `EventMetadataFailureKind`、policy signal 和标准 model/tool/sandbox node 信号，failed verification report 仍优先升级为 `FailureVerification`；root `RecordModelCallCheck` 已能把已观察模型请求/响应/错误转成不含 raw prompt/response text、但含 request/response metadata key 摘要的 model call evidence，root `RecordToolCallCheck` 已能把已观察工具请求/结果/错误转成不含 raw args/result content、但含 result metadata key 摘要的 tool call evidence，root `RecordChannelEventCheck` 已能把已观察 channel action/message/cancel 转成不含 raw text/payload content 的 channel event evidence，root `RecordFailureAttributionCheck` 已能把已观察 failure attribution 转成含补充 metadata key 摘要的 failed verification evidence，`checkpoint.RecordVerificationCheck` 已支持 checkpoint record evidence bridge，`gopacttest` 已具备 trajectory、command、file snapshot、diff 和含补充 metadata key 摘要的 review decision 基础 evidence bridge；`templates/react` 的 verifier 候选 export 已能自动填充 template task、run input、resume input 和 resolved intervention process records。更多 template / Dev Agent process record 覆盖、业务侧 verification evidence、failure attribution 跨组件证据深化、具体 entropy audit 采集器和 Dev Agent gate 仍属于 M5 template 与评测证据层。

建议结构：

```go
type RunExport struct {
	IDs              RuntimeIDs
	Tasks            []TaskRecord
	Inputs           []InputRecord
	Events           []Event
	Steps            []StepExport
	Checkpoints      []CheckpointRecord
	Artifacts        []ArtifactRef
	VerificationReports []VerificationReport
	Failures         []FailureAttribution
	PolicyDecisions  []PolicyDecision
	Interventions    []InterventionRecord
	EntropyAudits    []EntropyAudit
	Outcome          EpisodeOutcome
}
```

它服务于：

- record/replay plugin；
- evaluation plugin；
- LangSmith/OTel export；
- 自举 agent 的 review gate；
- harness evolution 的证据输入。

### 3. Template 层可以定义业务决策

Loop 里的“继续还是停止”不能只藏在 template 代码里。建议在 template 层记录业务决策，但不要把 `LoopDecision` 做成 core contract。

```go
type TemplateDecision struct {
	Kind         TemplateDecisionKind
	Reason       string
	Step         int
	Budget       LoopBudgetSnapshot
	NextNode     string
	Interrupt    *InterruptRecord
	Fallback     *provider.FailoverDecision
	Verification []VerificationRef
}
```

常见 kind：

- `continue`;
- `final`;
- `interrupt`;
- `retry`;
- `fallback`;
- `compress_context`;
- `promote_tool`;
- `fail_budget_exceeded`;
- `fail_unverified`;
- `fail_policy_denied`.

每个 decision 可以作为通用 node/step/model/tool/policy 事件的 payload 进入事件流，不需要新增 core event category。

### 4. 让 verification 成为 template 的一等节点

ReAct template 不应该只是 `call_model -> maybe_call_tools -> decide_next`。Dev Agent 这类高风险 template 至少需要 `verify` 和 `attribute_failure` 节点：

```text
Start
  -> load_context
  -> select_tools
  -> call_model
  -> maybe_call_tools
  -> attribute_failure
  -> verify
  -> decide_next
  -> End
```

普通 ReAct 可以把它们作为可选节点；Dev Agent 必须启用。

### 5. 自举 milestone 要增加 harness quality gate

当前 milestone 已经把 M3 定义为 Level 1 自举、M5 定义为正式自举。调研后建议增加两个 gate：

- M3 只允许只读分析和受限测试，必须记录 tool-call 图和 step export；
- M5 正式自举前，必须有 run export、verification report、entropy audit 和 reviewer plugin 的最小实现。

这样自举不是“能让模型改仓库”这么粗糙，而是“能产出可验证、可归因、可回滚的变更证据”。

## 设计检查清单

新增任何 agent template、runtime module、middleware 或 plugin 时，增加这些问题：

1. 它是 harness component、adapter、plugin 还是 template？
2. 它的输入、输出、错误和权限决策是否结构化？
3. 它是否产生事件？
4. 它是否影响 template 的继续/停止？如果影响，是否能映射到底层 step/event/checkpoint？
5. 它是否扩大工具或外部动作空间？如果扩大，policy 如何介入？
6. 它的失败能否归因到 context、tool、feedback、verification、recovery、entropy、model 或 unknown？
7. 它能否被 run export 和 step export 复盘？
8. 它是否会增加长期维护熵？如何审计？
9. 它能否在 fake provider / fake tool / memory store 下测试？
10. 它是否需要进入 core，还是应该是 adapter/plugin/template？

## 资料来源

- AI Harness Engineering: A Runtime Substrate for Foundation-Model Software Agents：https://arxiv.org/abs/2605.13357
- Agentic Harness Engineering: Observability-Driven Automatic Evolution of Coding-Agent Harnesses：https://arxiv.org/html/2604.25850
- HarnessX: A Composable, Adaptive, and Evolvable Agent Harness Foundry：https://arxiv.org/abs/2606.14249
- More with Less: An Empirical Study of Turn-Control Strategies for Efficient Coding Agents：https://arxiv.org/abs/2510.16786
- Overthinking Loops in Agents: A Structural Risk via MCP Tools：https://arxiv.org/abs/2602.14798
- SlopCodeBench: Benchmarking How Coding Agents Degrade Over Long-Horizon Iterative Tasks：https://arxiv.org/html/2603.24755
- Business Insider, Forget prompt engineering: "Loop engineering" is all the rage now：https://www.businessinsider.com/what-are-loops-ai-engineering-tips-2026-6
