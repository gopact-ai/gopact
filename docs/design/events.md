# gopact 事件流设计

日期：2026-06-23

设计入口：[index.md](index.md)

事件流是 `gopact` 的调试界面、观测边界和 trajectory test 输入。事件不是日志；日志可以从事件派生，但事件必须保留结构化语义。

## 事件模型

```go
type Event struct {
	ID           string
	Type         EventType
	IDs          RuntimeIDs
	Sequence     int64
	ParentID     string
	CallID       string
	CheckpointID string
	Payload      EventPayload
	Redaction    RedactionState
	CreatedAt    time.Time
}
```

规则：

- `Sequence` 在单个 `RunID` 内单调递增；
- `ID` 在单个 event stream 内唯一；
- `ParentID` 用于表达 node -> model/tool/sandbox/MCP/A2A 的嵌套关系；
- `CallID` 标识当前执行边界，必须能和 `RuntimeIDs.CallID` 对齐；
- `Payload` 必须是结构化对象，大内容使用 `ArtifactRef`；
- 所有外部 sink 看到的事件必须已经过 redaction。

## 事件分类

| 分类 | 事件 |
| --- | --- |
| run | `RunStarted`、`RunCompleted`、`RunFailed`、`RunCanceled`、`RunInterrupted` |
| turn loop | `TurnInputReceived`、`TurnInputMerged`、`TurnResumed`、`TurnPreempted`、`TurnInterrupted`、`TurnCanceled` |
| graph/node/step | `NodeStarted`、`NodeCompleted`、`NodeFailed`、`NodeSkipped`、`StepSnapshotCreated`、`StepExported`、`StepImported` |
| model | `ModelRoutePlanned`、`ModelRequested`、`ModelStreamDelta`、`ModelResponded`、`ModelFailed` |
| provider routing | `ModelProviderAttemptStarted`、`ModelProviderRetryDecided`、`ModelProviderFallbackStarted` |
| tool | `ToolVisibleListed`、`ToolCalled`、`ToolReturned`、`ToolFailed`、`ToolPromoted` |
| checkpoint | `CheckpointWriting`、`CheckpointWritten`、`CheckpointFailed`、`CheckpointLoaded` |
| interrupt/resume | `InterruptRaised`、`RunInterrupted`、`ResumeReceived`、`NodeResumed` |
| sandbox | `SandboxCreated`、`SandboxExecStarted`、`SandboxExecCompleted`、`SandboxClosed` |
| memory | `MemoryPut`、`MemorySearched`、`MemoryDeleted` |
| skill | `SkillActivated`、`SkillLoaded`、`SkillResourceRead`、`SkillScriptCompleted` |
| MCP | `MCPServerConnected`、`MCPToolsListed`、`MCPToolCalled`、`MCPResourceRead` |
| A2A | `A2AAgentRegistered`、`A2AAgentCardFetched`、`A2ATaskSent`、`A2AMessageReceived`、`A2AArtifactUpdated`、`A2ATaskStatusUpdated`、`A2ATaskCompleted`、`A2ATaskFailed`、`A2ATaskCanceled` |
| artifact | `ArtifactPut`、`ArtifactRead`、`ArtifactExported` |
| policy | `PolicyRequested`、`PolicyDecided` |
| channel | `SurfaceMessageProjected`、`ChannelTransferCompleted`、`ChannelSendCompleted`、`ChannelActionReceived` |

## 顺序规则

一次正常 run 的最小事件序列：

```text
RunStarted
NodeStarted
NodeCompleted
CheckpointWriting
CheckpointWritten
RunCompleted
```

一次失败 node：

```text
RunStarted
NodeStarted
NodeFailed
RunFailed
```

一次 interrupt：

```text
RunStarted
NodeStarted
InterruptRaised
CheckpointWriting
CheckpointWritten
RunInterrupted
ResumeReceived
NodeResumed
NodeCompleted
RunCompleted
```

当前 graph 第一片已经实现 `EventInterrupted`、`EventRunInterrupted`、`EventStepImported`、`EventCheckpointLoaded`、`EventResumeReceived` 和 `EventNodeResumed`，并保证 interrupt 不会降级为 `RunFailed`。checkpoint store 的 latest 恢复路径会在 `RunStarted` 后产生 `CheckpointLoaded`；如果恢复 interrupted checkpoint 并携带 `ResumeRequest`，随后会产生 `ResumeReceived`，第一个实际执行节点会产生 `NodeResumed`。带 effects 的 `StepImported` / `CheckpointLoaded` 会在 metadata 的 `effect_replay_plan` 字段暴露 `EffectReplayPlan`，调用方优先使用 root `EventEffectReplayPlan(event)` 类型化读取；`CheckpointLoaded` 会透传 checkpoint metadata，因此显式允许的 config drift 会出现在 `checkpoint_config_drift` 字段。TurnLoop 第一片已经实现 `TurnInputReceived`、`TurnInputMerged`、`TurnResumed`、`TurnPreempted`、`TurnInterrupted` 和 `TurnCanceled`；通过 `TurnLoop.Cancel(reason)` 触发的 `TurnCanceled` 会在 metadata 中保留 `reason`，便于审计人工或上层控制面取消原因。`CheckpointWriting`、`CheckpointWritten` 和 `CheckpointFailed` 仍属于后续持久化事件细化。

规则：

- `RunStarted` 必须是某个 `RunID` 的第一条运行事件；
- `RunCompleted`、`RunFailed`、`RunCanceled`、`RunInterrupted` 是 terminal events；
- terminal event 后不能再出现同一 `RunID` 的执行事件，但异步 exporter 可以产生诊断日志；
- `CheckpointWritten` 必须出现在依赖它的 `RunInterrupted` 之前；
- model/tool/sandbox/MCP/A2A 的子事件必须有 parent node event；
- streaming delta 可以多次出现，但必须在对应 started/responded 或 failed 边界内。

## Stream API

目标 API 形态：

```go
func (r *Runner) Run(ctx context.Context, input Input, opts ...RunOption) iter.Seq2[Event, error]
func (g *Runnable[S]) Run(ctx context.Context, state S, opts ...RunOption) iter.Seq2[Event, error]
```

语义：

- event stream 返回事件和运行错误；
- terminal event 不等于 iterator error；
- iterator error 表达事件流自身无法继续，例如 checkpoint store 失败、context cancel、panic recovery；
- 如果业务失败可表达为 `RunFailed`，iterator 仍可以正常结束；
- 如果 checkpoint 失败，iterator 返回 error，因为恢复语义不可靠。

## Redaction

Redaction 必须早于外部 sink：

```text
runtime event
  -> core redaction
  -> event middleware
  -> subscriber/exporter/transfer/channel adapter
```

规则：

- prompt、tool args、tool result、memory content、sandbox stdout 默认可脱敏；
- 大 payload 只输出 hash、size、schema、artifact ref；
- redaction 状态写入 `Event.Redaction`；
- strict exporter 只能接收 redacted event；
- replay 测试可以使用未脱敏 fixture，但不得默认写入外部 trace。

## Sink 失败策略

| sink 类型 | 默认策略 |
| --- | --- |
| checkpoint writer | 失败即终止 run |
| in-process event collector | 失败即返回 error |
| trace/log exporter | 记录诊断事件，不阻断主执行 |
| channel adapter | 不改变业务结果，错误返回给调用方显示并产生 channel 诊断事件 |
| evaluation recorder | 可配置；默认不阻断主执行 |

strict 模式允许外部 sink 失败时终止 run，但必须在配置和事件里显式标注。

当前 root 第一片提供 `EventSinkMiddleware` 和 `AsyncEventSink`：

- 默认 `strict`：sink 返回错误时停止 event chain，Runner 会产生 `RunFailed`；
- `WithEventSinkFallback()`：sink 返回错误时继续发出事件，并在 event metadata 写入 `event_sink_error`；
- `AsyncEventSink`：把 event sink 包成 bounded background queue，默认 `block` 背压；`WithAsyncEventSinkDropNewest()` 会在队列满时丢弃当前事件，并写入 `async_event_sink_dropped` metadata；`Close(ctx)` 会停止接收、drain 队列并返回 sink errors；
- plugin subscriber 的 `Publish` 路径默认保持严格失败语义；`NewPluginHost(WithPluginFailureFallback())` 会记录 `plugin_subscriber_errors` metadata 并继续发布，非关键 exporter 可通过 fallback event middleware 或 `AsyncEventSink` 接入。

## OTel / LangSmith 映射

| gopact 事件 | OTel / LangSmith |
| --- | --- |
| `RunStarted` / terminal event | root span |
| `NodeStarted` / `NodeCompleted` | node span |
| `ModelRequested` / `ModelResponded` | LLM span |
| `ToolCalled` / `ToolReturned` | tool span |
| `CheckpointWritten` | span event |
| `InterruptRaised` / `ResumeReceived` | span event |
| `RunFailed` / `RunInterrupted` / `NodeFailed` | span status + error event |

大 payload 用 artifact attachment 或 hash，不塞进 span attribute。

当前 `adapters/observability/trace` 已提供 provider-neutral trace plugin 第一片：插件通过 `PluginHost` event subscriber 消费 runtime event stream，输出 `SpanRecord`，并把 `RuntimeIDs`、node、step、event type、status、provider/model/tool/policy 等低基数字段保留下来。`trace.HTTPExporter` 已提供 HTTP/JSON exporter 第一片，可把 span record POST 到宿主控制的 collector 或 vendor 网关；`trace.OTLPHTTPExporter` 已提供 OTLP/HTTP JSON exporter 第一片，可把 span record 转成 `ExportTraceServiceRequest` 发往 OpenTelemetry collector；`trace.LangSmithHTTPExporter` 已提供 LangSmith-compatible run payload HTTP exporter 第一片，可把 span record 映射成 project/trace/run/thread 语义并携带 `session_id` / `thread_id` metadata；`trace.PolicyExporter` 可在 `ExportSpan` 前用 `PolicyBoundaryExporter` / `PolicyActionExport` 做外发治理，支持 deny/review、policy events 和 approval interrupt。它不直接依赖 OTel SDK 或 LangSmith SDK；真实 LangSmith Go SDK、dataset/evaluation/feedback/run query 和更细 redaction/policy 深化后续继续通过 `trace.Exporter` 接入。

## Channel / transfer 映射

Channel 层只能消费事件和 `SurfaceMessage`：

- `ModelStreamDelta` -> `SurfaceMessage{text_delta}` -> TUI line / Lark text update / A2UI data update；
- `ToolCalled` / `ToolReturned` -> `SurfaceMessage{tool_call/tool_result}` -> tool chip/card/status；
- `InterruptRaised` -> `SurfaceMessage{approval/selection}` -> approval prompt/card/action component；
- `ArtifactPut` -> `SurfaceMessage{artifact}` -> file/image/report preview；
- terminal events -> `SurfaceMessage{status}`；
- `ResumeReceived` -> `SurfaceMessage{status}` 或 action acknowledgement。

A2UI、AG-UI、SSE、WebSocket、TUI、Lark bot、飞书卡片都必须遵守同一个事件输入和 `SurfaceMessage` 边界。

当前 root package 已提供 `ProjectSurfaceMessages(event)` 和 channel 诊断事件常量第一片，`adapters/channel/tui` 已提供 writer-based TUI transfer/channel 第一片，`adapters/channel/sse` 已提供 HTTP SSE transfer/channel 第一片，`adapters/channel/lark` 已提供 host-injected Lark transfer/channel/callback source 第一片，可把 `SurfaceMessage` 转成 text/interactive payload，处理 URL verification 和卡片 action callback，并把 action value 转回 `ChannelEvent`；`adapters/channel/a2ui` 已提供 A2UI v0.9 JSON message transfer/JSONL channel/history replay/schema catalog validation/component JSON Schema validator 注入/client-supported catalog negotiation/in-memory reference renderer/action decode 第一片，可输出 `createSurface`、`updateComponents`、`updateDataModel`、逐条写出 JSONL、记录可 replay 的 message history、用本地 catalog registry 校验组件和 child 引用，按 renderer supported catalog ID 顺序选择首个匹配 catalog，并把 renderer action context 转回 `ChannelEvent`；`adapters/channel/agui` 已提供 AG-UI event transfer/HTTP SSE channel 第一片，可把 `SurfaceMessage` 转成 run lifecycle、完整 message、text delta、error 和 custom surface events，再通过 SSE 逐帧广播，并把 HTTP action POST 转回 `ChannelEvent`。完整前端 A2UI renderer、完整 JSON Schema engine adapter/plugin 与 conformance 深化、更完整 catalog negotiation 深化、AG-UI WebSocket/plugin 等 transfer/channel adapter，以及 Lark 真实 client/OAuth/plugin、生产级 nonce/replay store、用户映射、目标平台 message id 和 policy/redaction/artifact export 深化仍需要在 adapter 或 plugin package 中补齐。

Channel 事件规则：

- transfer 成功或失败必须产生事件；
- channel 投递成功或失败必须产生事件；
- inbound action 必须产生 `ChannelActionReceived`；
- action 被 policy 拒绝时产生 `ChannelActionRejected`；
- channel 事件必须带 `Channel`、`Transfer`、目标平台 message id 或 action id。

## Event assertion helper

测试 helper 至少支持：

- 按类型断言事件顺序；
- 按 `RunID`、`ThreadID`、`CallID` 过滤；
- 断言某个事件 payload 字段；
- 断言没有未脱敏字段；
- 断言 terminal event；
- 断言 checkpoint event 在 interrupt 前出现；
- 断言 step snapshot/export/import 的顺序、schema version 和 config version；
- 断言没有异常重复 tool-call 图或 MCP 诱导循环。

## Run export

Event stream 是实时流，run export 是一次执行结束后的过程包。当前代码已落地 `RunExport` 验证契约、`RunExportJSONSchema()`、`RunRecorder` 和 `ReplayRunExport` 第一片：recorder 可消费事件、记录稳定 step snapshot，并显式记录 task/input/intervention/failure process records、entropy audits 和 verification reports；JSON Schema 只发布可移植结构契约，不绑定具体校验引擎；replay 只回放 recorded events，不执行 tool/model/sandbox 副作用。

```go
type RunExport struct {
	IDs             RuntimeIDs
	Events          []Event
	Steps           []StepSnapshot
	Tasks           []TaskRecord
	Inputs          []InputRecord
	Interventions   []InterventionRecord
	Failures        []FailureAttribution
	EntropyAudits   []EntropyAudit
	VerificationReports []VerificationReport
	Checkpoints     []CheckpointRecord
	Artifacts       []ArtifactRef
	PolicyDecisions []PolicyDecision
	Outcome         RunOutcome
}
```

当前 `VerificationReport` / `VerificationRecorder` 第一片已经落地：它把 `RunExport` 身份、run outcome、检查项、证据引用和 passed/failed/skipped 计数汇总成可持久化报告，但不负责执行命令或调用 model/tool backend；`RunRecorder` 现在可显式记录 verification report，也可从事件 metadata 的 `verification_report` 自动提取 report 并写入 `RunExport.VerificationReports`。`RecordRunExportCheck` 可以把宿主已观察到的 `RunExport`、outcome、runtime ids 和过程计数记录成 run export evidence；completed export 至少要包含一个 event 和一个稳定 step 才会记录为 passed evidence，否则会先记录 failed evidence 再返回 `ErrRunExportIncomplete`；failed export 会先记录 failed evidence 再返回 `ErrRunExportFailed`；interrupted/canceled export 会记录 skipped evidence，用于阻止 release gate 把未完成 run 当成已通过。

`RecordModelCallCheck` 可以把宿主已观察到的 `ModelRequest` / `ModelResponse` / error 转成 model call evidence，记录 runtime ids、route、usage、消息/工具/能力计数、输出 tool call 摘要和 request/response metadata key 摘要，不触发模型调用，也不保存 raw prompt 或 raw response text；失败调用会先记录 failed evidence 再返回 `ErrModelCallFailed`。`RecordToolCallCheck` 可以把宿主已观察到的 `ToolCall` / `ToolResult` / error 转成 tool call evidence，记录 runtime ids、工具名、参数/结果字节数、artifact/effect/event 计数和 result metadata key 摘要，不触发工具执行，也不保存 raw args 或 raw result content；失败调用会先记录 failed evidence 再返回 `ErrToolCallFailed`。`RecordChannelEventCheck` 可以把宿主已观察到的 `ChannelEvent` / error 转成 channel event evidence，记录 runtime ids、channel、event/action 类型、text/payload shape 和 metadata keys，不触发 channel receive，也不保存 raw text 或 raw payload；失败事件会先记录 failed evidence 再返回 `ErrChannelEventFailed`。`RecordFailureAttributionCheck` 可以把宿主已观察到的 `FailureAttribution` 转成 failure attribution evidence，记录 kind、runtime ids、node/step、error、自定义 metadata key 摘要和关联证据，不推理归因；它总是记录 failed check 并返回 `ErrFailureAttributionFailed`，用于避免 release gate 把带失败归因的过程误判为通过。`RecordPolicyDecisionCheck` 可以把宿主已观察到的 `PolicyRequest` / `PolicyDecision`、boundary/action、runtime ids、policy/request metadata 及其 key 摘要记录成 policy decision evidence，非 allow 决策会先记录 failed evidence 再返回 `ErrPolicyDecisionNotAllowed`。`RecordEntropyAuditCheck` 可以把宿主已观察到的 `EntropyAudit`、findings、max severity 和 runtime ids 记录成 entropy audit evidence，failed audit 会先记录 failed evidence 再返回 `ErrEntropyAuditFailed`。

`artifact.RecordVerifyRefs` 已提供 artifact integrity evidence 第一片，用于把 artifact refs 校验结果写成标准 verification check。`checkpoint.RecordVerificationCheck` 可以把宿主已观察到的 checkpoint record、runtime ids、step/node/phase、queue、pending interrupt、effect/artifact 计数或 checkpoint 采集错误记录成 checkpoint evidence，失败时先记录 failed evidence 再返回 `ErrVerificationCheckFailed`。`gopacttest` 已提供 compact trajectory frame 和 JSON golden fixture helper，可从 event stream 或 `RunExport` 提取 event type、node 和 step，支持后续 template-specific golden trajectory tests；`TemplateTrajectoryConformance` 的 required frame 还可要求原始 event metadata 子集（如 Dev Agent 的 action/mode），避免只匹配节点顺序而丢失关键过程语义；`RecordGoldenTrajectoryCheck` / `RecordRunExportGoldenTrajectoryCheck` 也能把 golden 轨迹比对结果写入 `VerificationRecorder`，失败时先记录 failed evidence 再返回 mismatch/load error；`RecordCommandCheck` 可以把宿主已观察到的命令结果和非保留补充 metadata key 摘要记录成 command verification evidence，失败时先记录 failed evidence 再返回 `ErrCommandFailed`；`ParseGitHubActionsCIRun` 可以把宿主已经取得的 GitHub Actions run/job/step JSON 转成 `CIRun`，再由 `RecordCIRunCheck` 记录为带非保留补充 metadata key 摘要的标准 `ci_gate` evidence；它只解析调用方提供的 JSON，不调用 GitHub、不读取 `gh` CLI、token 或配置，`GateNames` 只用于把 GitHub job/step 展示名映射/过滤为稳定 gate 名；`internal/extensionscaffold.RecordRemoteStatusCheck` 可以把 `-remote-status-json` 已观察到的 `RemoteStatusReport` 转成 `external_repository_readiness` evidence，失败时先记录 failed evidence 再返回 `ErrRemoteStatusNotReady`，不调用 GitHub、不读取 token 或配置；`internal/extensionscaffold.RecordRemoteCIRunSetCheck` 可以把同一报告中的最新 Actions run 映射成 `external-ci:gopact-ai` 跨仓 `ci_gate` evidence，并继承 `RecordCIRunSetCheck` 的 metadata key 摘要契约，失败时先记录 failed evidence 再返回 `ErrRemoteCINotReady`；`cmd/gopact-extscaffold -remote-status-evidence-json` 会直接输出 readiness verification check，`-remote-ci-evidence-json` 会直接输出跨仓 CI verification check，not-ready/CI failed 状态由 check status 表达，CLI 不把该状态转成非零退出码；`RecordFileSnapshotCheck` 可以把宿主已观察到的文件路径、hash、size、mtime、非保留补充 metadata key 摘要或读取错误记录成 file snapshot evidence，失败时先记录 failed evidence 再返回 `ErrFileSnapshotFailed`；`RecordDiffCheck` 可以把宿主已观察到的 patch/worktree diff、文件列表、增删统计和非保留补充 metadata key 摘要记录成 diff evidence，失败时先记录 failed evidence 再返回 `ErrDiffFailed`；`RecordReviewCheck` 可以把宿主已观察到的 human/model/CI/Lark 等 reviewer decision 记录成 review evidence，并把宿主传入的 reviewer metadata 与 metadata key 摘要复制到 check 与 evidence metadata，rejected 或采集错误会先记录 failed evidence 再返回 `ErrReviewFailed`。

`adapters/devagent/gitdiff` 已提供 git diff scanner adapter 第一片，可把 worktree/staged git diff 映射为 `DiffSnapshot` 和 `PatchProposal`，供 `RecordDiffCheck` 与 `BuildEntropyAudit` 使用。`adapters/devagent/channelreview` 已提供 channel reviewer adapter 第一片，可把 review prompt 作为 `SurfaceMessageApproval` 经 transfer/channel 投递，并把 `ChannelEvent` 中的 approve/reject action、payload 或 metadata 映射为 `ReviewDecision`，供 Lark/TUI/SSE/CI 等外部审批入口复用。`adapters/devagent/cireview` 已提供 CI reviewer adapter 第一片，只消费已观察 `VerificationReport`、required checks 和 `EntropyAudit`，把 CI 证据映射为 approved/rejected `ReviewDecision`，供 release gate 复用。`adapters/devagent/modelreview` 已提供 model reviewer adapter 第一片，通过宿主注入的 `gopact.ChatModel` 把 review input evidence 转成模型请求，只接受显式 JSON approve/reject decision，并可通过 `WithGovernance` 把 prompt/eval/policy metadata 写入请求和决策。`templates/devagent.BuildProcessRecords` 已支持把外部审批恢复动作记录为 `devagent.review_resume` / `InputResume`，并把同一个 `ResumeRequest` 挂到 review intervention 上；workflow process conformance 会拒绝 resume input 与同 action review intervention resume 任一方向孤立的导入记录；self-bootstrap resumed release golden trajectory 已覆盖导入 interrupted release step、接收 resume、恢复 release gate、写入 release workflow process records 并封存 release bundle resume boundary；self-bootstrap resumed apply release golden trajectory 已覆盖导入 interrupted apply step、接收 resume、恢复 apply policy/sandbox evidence、写入 apply workflow process records 并继续封存 release bundle。`templates/devagent.RecordReleaseGateCheck` 可以把已评估的 `GateResult` 记录成 release gate evidence，rejected gate 会先记录 failed evidence 再返回 `ErrReleaseGateRejected`。`sandbox.RecordExecCheck` 可以把 sandbox 已观察到的 `ExecRequest` / `ExecResult` / error 转成 command-like verification evidence，失败时先记录 failed evidence 再返回 `ErrExecCheckFailed`。

`templates/devagent.BuildWorkflowProcessRecords` 还会把任意 child action 的 resume/review 边界提升到 workflow parent action summary：`resume_input_id` 指向 `devagent.review_resume` / `InputResume`，`review_intervention_id` 指向同 action 的 approval review intervention。这样 UI、channel adapter 或外部 replay 系统可以从 workflow 父摘要直接定位 apply/release 等 step 的恢复边界，再按需拉取完整 process records。

`RunRecorder` 已支持显式 `RecordFailure` / `RecordEntropyAudit`，会从 `RunFailed` 终止事件派生最小 `FailureAttribution`，并在失败前已经观察到 failed verification report 时把归因升级为 `FailureVerification` 且附带 report evidence。`templates/react` 已能在 model/tool node completed 边界产生 `StepSnapshot`，也能在 tool policy review 时产生 `EventInterrupted` / `EventRunInterrupted` 和 pending approval `StepSnapshot`；使用 root `WithStepExport` / `WithResumeRequest` 导入 interrupted tool step 后，会产生 `StepImported`、`ResumeReceived`、`NodeResumed` 并继续原 tool call；导入 completed `call_model` 或 `call_tool` step 后，也会产生 `StepImported`，并以 `NodeResumed` 分别继续 pending tool calls 或下一次 model call；使用 `react.WithCheckpointStore` 注入 checkpoint store 后，completed model/tool checkpoint load 会产生 `CheckpointLoaded`，恢复后的首个实际 node 会产生 `NodeResumed`，tool approval interrupted checkpoint 携带匹配 `ResumeRequest` 时会产生 `CheckpointLoaded`、`ResumeReceived`、`NodeResumed` 并继续原 tool call；使用 `react.WithArtifactVerifier` 后，step/checkpoint import 会先校验 artifact refs，校验失败只产生 `RunFailed` 并阻止恢复执行；使用 `gopact.AdaptStreamingModel(router)` 注入 provider router 后，会把 route planned、provider attempt、fallback started 和 model message events 带入 ReAct `call_model` node/step；tool artifact refs 会出现在 `EventToolResult`、tool node `StepSnapshot` 和 `RunExport` step 中；显式配置 `WithMemoryExtractor` 后，默认同步模式会产生 `MemoryPut` 事件和已 applied 的 `memory_put` effect；配置 `WithMemoryWriteMode(MemoryWriteDeferred)` 后，template 只记录 pending `MemoryPut` event 和可重放 idempotent `memory_put` effect，不提前写入 store，宿主可通过 `memory.NewReplayHandler` 或自己的 effect executor 后台提交；配置 `WithMemoryExtractMode(MemoryExtractDeferred)` 后，template 不在当前 run 调用 extractor/store，也不产生 `MemoryPut`，只在 `call_model` completed step 记录带 final state 和 runtime ids 的 pending `memory_extract` effect，供宿主通过 `memory.NewExtractionReplayHandler` 在 worker、队列或 resume 流程中抽取并写入；显式配置 `react.WithVerifier` 后，template 会在 completed run commit 前运行 `verify` node，verifier 基于候选 `RunExport` 向 `VerificationRecorder` 写入 evidence，候选 export 会自动带上 template task、run input、resume input 和 resolved intervention process records，report 会通过 `verification_report` metadata 出现在 verify step 中，failed report 或 verifier error 会产生 failed verify step 与 `RunFailed`，完整事件流经 `RunRecorder` 后会把该 report 提升为 run-level verification report，并把失败归因为 `FailureVerification`。

因此 `RunRecorder` 可以记录 ReAct run export stable steps、completed/interrupted checkpoint resume 轨迹、interrupted tool step、provider fallback 轨迹、artifact 轨迹、memory write / extraction request 轨迹、failure attribution、entropy audit 和 verification gate 轨迹；可选 memory recall 会产生 `MemorySearched` 事件，并以 `gopact.memory` system context 注入模型请求。更完整 replay 仍属于后续 M5。后续 M5 仍需补齐：

- `RunRecorder` 的 `RunFailed` 派生归因已经具备第二片边界感知能力：优先消费 `EventMetadataFailureKind`，其次按 policy denial / policy request、标准 `call_model` / `call_tool` / verify / context / review / resume / entropy / sandbox / external 等 node 信号保守推断 failure kind；root taxonomy 覆盖 `runtime`、`unknown`、`context`、`model`、`tool`、`feedback`、`policy`、`verification`、`recovery`、`entropy`、`sandbox`、`external`，failed verification report 仍优先升级为 `FailureVerification`。
- tool/context/更广 verification trace；
- failure attribution 的跨组件证据深化；
- 更完整 entropy audit 采集器、git diff scanner 策略和 Dev Agent gate；
- 更广 template / Dev Agent 真实 workflow process record 覆盖；
- 更多具体 verification evidence 采集来源，以及 model review 真实评测治理深化、CI provider 拉取/重跑/secret 治理、Lark 真实 client/plugin、生产级 callback 治理和更完整 review gate event integration。

用途：

- trajectory test；
- record/replay plugin；
- OTel/LangSmith/exporter；
- 自举 agent review gate；
- 业务层 harness/template 评估的证据输入。
