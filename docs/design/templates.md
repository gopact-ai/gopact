# gopact Agent Template 设计

日期：2026-06-23

设计入口：[index.md](index.md)

Agent template 是基于 core runtime 的可复用 graph 组合。它不是特殊 runtime，也不是不透明 agent class。

## 原则

- template 使用 `Runner`、graph、provider router、tool registry、memory、artifact、policy；
- template 不直接依赖具体 provider adapter；
- template 的每一步必须产生事件；
- template state 应该可 checkpoint；
- template 可以提供默认 prompt，但 prompt library 不进入 core；
- template 的行为必须能用 trajectory test 断言。

## 第一批 template

| Template | 目标 | 阶段 |
| --- | --- | --- |
| ReAct | 模型-工具循环，完成单 agent 任务 | M5 |
| Plan-Execute | 先计划再执行，可插入人工审批 | M5 后 |
| Supervisor | 多 worker agent 路由和合并 | M6 后 |
| Agent-as-Tool | 把本地或 A2A agent 暴露为 tool | M5 |
| Dev Agent | 用于自举的仓库开发 agent | M5 后 |

M5 前可以先写 template 规格和测试 fixture，不需要公开稳定 API。

## ReAct State

```go
type ReActState struct {
	Messages      []gopact.Message
	VisibleTools  []tools.ToolInfo
	DeferredTools []tools.ToolInfo
	Artifacts     []artifact.Ref
	Steps         []ReActStep
	Final         *gopact.Message
}

type ReActStep struct {
	CallID     string
	ModelRoute gopact.ModelRoute
	ToolCalls  []gopact.ToolCall
	ToolResults []gopact.ToolResult
	Error      string
}
```

规则：

- `Messages` 是模型上下文，不是完整事件轨迹；
- 完整轨迹来自 event stream；
- 大工具结果用 artifact ref；
- memory recall 结果必须以可审计 part 或 context block 注入；
- deferred tools 不直接暴露给模型。

## ReAct Graph

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

节点说明：

- `load_context`：从 memory、checkpoint、resume payload 准备上下文；
- `select_tools`：从 tool registry 获取 visible tools，必要时搜索 deferred tools；
- `call_model`：通过 provider router 调用模型；
- `maybe_call_tools`：执行模型请求的工具调用；
- `attribute_failure`：当模型、工具、sandbox、MCP/A2A 或验证失败时，把失败归因到 context、tool、feedback、verification、recovery、entropy、model 或 unknown；
- `verify`：把任务要求映射到确定性检查、测试、artifact 审查或人工审批结果；
- `decide_next`：判断 final、继续、interrupt、失败或达到最大步数。

## 终止条件

ReAct 必须有显式终止条件：

- 模型返回 final message；
- 没有 tool call 且没有继续信号；
- 达到 template 配置的最大循环/步数；
- context budget 不足且无法压缩；
- policy deny；
- interrupt raised；
- unrecoverable provider/tool error。

达到最大循环/步数不是成功，必须产生结构化错误和事件。

## Template 过程决策

Template 可以定义自己的过程决策，例如继续、停止、fallback、压缩上下文或请求人工介入。但这些属于 template 层，不属于 core 原子能力。Core 只要求这些决策最终能映射到底层 step、event、checkpoint、policy 和 resume。

```go
type TemplateDecisionKind string

const (
	TemplateContinue       TemplateDecisionKind = "continue"
	TemplateFinal          TemplateDecisionKind = "final"
	TemplateInterrupt      TemplateDecisionKind = "interrupt"
	TemplateRetry          TemplateDecisionKind = "retry"
	TemplateFallback       TemplateDecisionKind = "fallback"
	TemplateCompress       TemplateDecisionKind = "compress_context"
	TemplatePromoteTool    TemplateDecisionKind = "promote_tool"
	TemplateBudgetExceeded TemplateDecisionKind = "fail_budget_exceeded"
	TemplateUnverified     TemplateDecisionKind = "fail_unverified"
	TemplatePolicyDenied   TemplateDecisionKind = "fail_policy_denied"
)
```

规则：

- 每次循环继续、停止、升级、fallback、压缩上下文或请求人工介入都要有原因；
- 最大循环/步数、token budget、wall-clock budget、tool-call budget 是 template 过程控制的一部分；
- template decision 可以作为事件 payload 进入事件流，但 event type 仍应使用通用的 node/step/model/tool/policy 事件；
- ReAct 可以通过可选 `WithVerifier` 在 completed run commit 前运行 `verify` node；verifier 只消费候选 `RunExport` 并向 `VerificationRecorder` 写入 evidence，不直接执行 template loop 决策；
- ReAct `WithVerifier` 的候选 `RunExport` 会自动补 template task、run input、resume input 和 resolved intervention process records；verify node 产生的 report 会进入 event metadata，并可由 `RunRecorder` 提升为 `RunExport.VerificationReports`；这些记录只描述过程，不驱动业务 harness；
- MCP/A2A/tool 返回的建议不能自动触发下一步动作，必须经过 decision 和 policy；
- Dev Agent 必须启用 verification evidence 采集来源、entropy audit 采集器和 release gate，不能只凭测试通过宣布完成。

## Tool loop 规则

- 工具调用前必须经过 tool middleware 和 policy；
- 工具结果必须产生事件；
- 非幂等工具不能被自动重试；
- tool result 不自动成为高优先级 instruction；
- 工具调用失败是否继续由 template policy 决定，并产生事件；
- visible tool set 是单次 model request 的快照。

## HITL

HITL 通过 interrupt/resume 表达：

- tool approval；
- 模型不确定时请求用户选择；
- 高风险 artifact export；
- sandbox 写入或命令执行；
- A2A delegation。

template 不能阻塞等待用户输入。

## Dev Agent 自举语义

Dev Agent 是用于维护 `gopact` 仓库的 template。

自举模式：

| 模式 | patch 生成 | patch apply |
| --- | --- | --- |
| plan mode | 可以生成 patch 建议 | 不 apply |
| write mode | 可以生成并 apply patch | 必须经过 sandbox、policy、event、diff、checkpoint |

Level 1 自举只读分析和计划；Level 2 可以受控写 docs/examples/tests/adapter 骨架；Level 3 可以处理低风险 core 变更，但 release、权限扩大和破坏性修改仍需人工 gate。

`templates/devagent` 当前已落地 mode/action gate、entropy audit collector、reviewer plugin slot 和 release gate 第一片：action gate 只判断 analyze/plan/write mode 下 patch proposal、patch apply、release 是否允许；write apply 必须带 policy allow decision、sandbox event、observed diff 和 observed checkpoint，root `RecordPolicyDecisionCheck` 可把该 observed policy decision 转成标准 policy_decision verification evidence，`gopacttest.RecordDiffCheck` 可把该 observed diff 转成标准 diff verification evidence，`checkpoint.RecordVerificationCheck` 可把该 observed checkpoint record 转成标准 checkpoint verification evidence。root `RecordRunExportCheck` 可把已观察 `RunExport` 转成标准 run_export verification evidence，使 release gate 能要求完整过程包已经落盘；调用方 metadata 不能覆盖 outcome/count/runtime ids 等 canonical run export 字段；completed export 必须至少有一个 event 和一个稳定 step 才会被记录为 passed evidence。root `RecordModelCallCheck` 可把已观察模型请求/响应/错误转成标准 model_call verification evidence，只记录 runtime ids、route、usage、计数和输出 tool call 摘要，不保存 raw prompt/response text。root `RecordToolCallCheck` 可把已观察工具请求/结果/错误转成标准 tool_call verification evidence，只记录 runtime ids、工具名、参数/结果字节数和 artifact/effect/event 计数，不保存 raw args/result content。root `RecordChannelEventCheck` 可把已观察 channel action/message/cancel 转成标准 channel_event verification evidence，只记录 runtime ids、channel、event/action 类型、text/payload shape 和 metadata keys，不保存 raw text/payload content。

`BuildEntropyAudit` 只消费已观察 patch metadata、显式 `PatchProposal.Files`、unified diff header 和 `VerificationReport`，把 dependency change、sensitive file、source-without-docs 等信号转换成标准 `EntropyAudit` finding；root `RecordEntropyAuditCheck` 可把该 audit 转成标准 entropy verification evidence。这些 root/replay/checkpoint/gopacttest/devagent/objectstore evidence bridge 的调用方 metadata 只能补充非保留字段，不能覆盖 ref、计数字段、runtime ids、状态、错误和 shape 摘要等 canonical evidence 字段。`BuildProcessRecords` / `RecordProcessRecords` 只消费已观察 `ActionResult`、sanitized `PatchProposal` 摘要、`GateResult` 和 `ReviewDecision`，生成或写入 `RunRecorder` 的 task/input/intervention process records；`EvaluateAction` 会把调用方传入的 action metadata 防御性复制到 `ActionResult`，process/workflow 边界会继续把这些 prompt/eval/policy governance metadata 写入 task/input/intervention metadata，同时由 SDK canonical 字段覆盖冲突键；patch input 只记录 id、summary、file_count、has_diff 和 file path/intent，release gate input 只记录 gate status/mode/report/review/max entropy 摘要，不保存 raw diff，也不执行命令、扫描工作区或驱动业务 harness。`BuildWorkflowProcessRecords` / `RecordWorkflowProcessRecords` 只消费一组已观察 `ProcessInput`，生成或写入 workflow 父 task、子 action task、input 和 intervention records，父 task output 带稳定 child action summary（index、task id、mode、action、status、action_status、reason_count、input/intervention count），并在 child task/input/intervention metadata 中保留 `workflow_id`、`workflow_action_index` 和 `workflow_action_count`，便于外部系统重排后仍能恢复过程顺序；如果同一 workflow 内同类 action 重复出现，child task id 会追加稳定 action index 消歧，让按 task id 恢复第二个 plan/apply step 不会撞到前一个同类 step；workflow process conformance 会拒绝导入/恢复记录中的重复 child task id，避免 step 级 export/import 按 task id resume 时出现歧义；同时拒绝子 action 显式冲突的 run/user/session/thread/agent/app/call/trace 等 runtime identity。它只描述 analyze/plan/write/release 过程边界，不调度步骤、不重新评估 action、不 apply patch。`WorkflowActionProcessRecords` 可按 1-based `workflow_action_index` 从 workflow records 提取单个 child action 的 task/input/intervention process records，只返回同 action index 的边界 defensive copy，供 release bundle 或外部 step export/import 精确封存单个 action。`BuildReleaseBundle` / `ReleaseBundle` 只消费已观察 `RunExport`、`VerificationReport`、entropy audits、approved `ReviewDecision`、passed `GateResult`、write-mode release `ActionResult` 和上述 sanitized process records（可通过 `WorkflowActionProcessRecords` 生成或通过 `ReleaseBundleInput.Process` 显式传入已观察 workflow child process records），校验 run/report/gate IDs、outcome、required checks、required evidence types、required CI gates 和 process records 对齐，要求 gate 内的 report/review status 与 max entropy severity 摘要存在且匹配 bundle 中的 report/review/entropy audits，要求 process task metadata/input/output 与 release action 对齐、release gate input value 与 gate status/mode/report/review/max entropy 摘要对齐、review intervention metadata 与 reviewer/status 对齐；当 process task 来自 workflow child 时，拒绝混入其他 `workflow_action_index` 的 input/intervention 边界；要求 run export 与 process records 匹配 bundle 已知的 session/user/thread/agent/call/trace 等 runtime identity，且 verification report / entropy audit 只要携带这些细分身份也必须匹配；如果 run export 内已经携带 verification reports / task/input/intervention process records，则必须分别包含 bundle 顶层 verification report 和 bundle process records 的同一语义快照；同时拒绝 failed entropy audit 和仍包含 `FailureAttribution` 的 run export，形成 release-ready evidence bundle；bundle 会防御性拷贝 `RunExport`，避免宿主后续 mutation 污染已封存证据；`RecordReleaseBundleCheck` 可把校验通过的 bundle 转成标准 `release_bundle` verification evidence，metadata 会保留 reviewer、gate report/review/max entropy 摘要、required CI gates 以及 process task / release gate input / review intervention id，供最终 release report 引用；`TestRecordReleaseBundleCheckCapturesObservedWorkflowRelease` 覆盖 analyze -> plan -> write release 的 self-bootstrap workflow process、run export、release bundle 和 release bundle evidence 端到端封存，并验证 raw diff 不进入 release evidence；`TestSelfBootstrapReleaseMatchesGoldenTrajectory` 进一步把 self-bootstrap release 的 run_started -> analyze -> plan -> release_gate -> run_completed 事件序列固定为 golden fixture，并把 `RecordRunExportGoldenTrajectoryCheck` 产出的 `trajectory_golden` evidence 纳入 release gate；`TestSelfBootstrapApplyReleaseMatchesGoldenTrajectory` 进一步把 self-bootstrap apply release 的 run_started -> analyze -> plan -> apply_patch -> release_gate -> run_completed 事件序列固定为 golden fixture，policy/sandbox 事件由 action gate 固定，diff、checkpoint 和 `trajectory_golden` evidence 一起进入 release gate；`TestSelfBootstrapResumedApplyReleaseMatchesGoldenTrajectory` 进一步把 interrupted apply step import + approval resume + resumed policy/sandbox evidence + release gate completion 的事件序列固定为 golden fixture，并要求 apply workflow process records 保留 `devagent.review_resume` / `InputResume` 边界且 workflow parent summary 可直接定位 apply resume/review boundary；`TestSelfBootstrapRejectedApplyMatchesGoldenTrajectory` 进一步把缺少 policy/sandbox/diff/checkpoint evidence 的 write apply 的 run_started -> analyze -> plan -> apply_patch failed -> run_failed 事件序列固定为 golden fixture，并要求 workflow process conformance 保留 failed child summary 与 `FailurePolicy` attribution；`TestSelfBootstrapRejectedReleaseMatchesGoldenTrajectory` 进一步把 rejected release 的 run_started -> analyze -> plan -> release_gate failed -> run_failed 事件序列固定为 golden fixture，要求 failed release gate check 进入 failed report 与 `FailureVerification` attribution，并要求 rejected release workflow process records 写入 `RunExport` 后可从 export 反取并通过 `RequireWorkflowProcessConformance`；调用方 metadata 只能补充非保留字段，不能覆盖这些 canonical release evidence 字段；它不重新执行验证、不调用 reviewer、不 apply patch。`adapters/devagent/gitdiff.Scanner` 可在 adapter 层捕获 worktree/staged git diff，并产出 `gopacttest.DiffSnapshot` 与 `devagent.PatchProposal`，供 diff evidence 和 entropy audit 使用。`Review` / `ReviewerFunc` / `StaticReviewer` 只消费已观察 patch/action/report/entropy/gate evidence，返回显式 approved/rejected reviewer decision，并对输入和返回 metadata 做 defensive copy；`adapters/devagent/channelreview.Reviewer` 可选通过 `Transfer` / `Channel` 投递 `SurfaceMessageApproval` review prompt，并从 `gopact.Channel` inbound action stream 中提取 approve/reject decision，把 Lark/TUI/SSE/CI callback 等外部审批入口接到同一 reviewer slot；`adapters/devagent/cireview.Reviewer` 只消费宿主已观察到的 `VerificationReport`、required checks 和 `EntropyAudit`，不执行命令、不读取工作区、不连接 CI 系统，把 CI 证据转换成显式 reviewer decision；`adapters/devagent/modelreview.Reviewer` 通过宿主注入的 `gopact.ChatModel` 把 review input evidence 转成模型请求，只接受显式 JSON approve/reject decision，并可通过 `WithGovernance` 把 prompt/eval/policy metadata 写入模型请求与 reviewer decision，不执行命令、不扫描工作区、不替代 release gate；`gopacttest.RecordReviewCheck` 可把该 reviewer decision 转成标准 review verification evidence，并在 check/evidence metadata 中保留 reviewer metadata。

`WorkflowRecordsFromRunExport` 可从已观察 `RunExport` 中恢复 Dev Agent workflow parent task、child tasks、input records 和 intervention records；默认按 `devagent:<runID>:workflow` 定位父 task，要求至少存在一个 child task，按 `workflow_action_index` 恢复 child/boundary 顺序，复用 workflow process conformance 拒绝父子链、重复 child task id、workflow metadata、I/O 摘要或 release boundary 对账不一致的 export，并返回 defensive copy。`WorkflowActionProcessRecords` 可在恢复后的 workflow records 上按 action index 提取单个 child action 的 process records，只包含同 `workflow_action_index` 的 input/intervention 边界，避免 release/apply/plan 等 step 在 export/import 时互相污染。`WorkflowActionProcessRecordsByAction` 可按唯一 `ActionKind` 提取同一份单 action process records；如果 workflow 中同类 action 出现多次，它会显式报歧义，要求宿主用 action index 或 child task id 消歧，避免 SDK 替业务 harness 选择“第几个 plan/apply/release”。`WorkflowActionProcessRecordsByTaskID` 可按 child task id 提取同一份单 action process records，适合外部系统只有 task/step id 而没有 action index 的恢复路径；如果导入记录里 child task id 重复，会在 workflow conformance 阶段先被拒绝。`ImportProcessRecords` / `ImportWorkflowRecords` 可把已恢复或外部导入的 process/workflow records 经校验与 defensive copy 后写回 `RunRecorder`，让 step 级 export/import 能形成恢复、提取、再封存闭环。它们只恢复、提取、导入和校验已落盘过程证据，不调度 workflow、不运行验证命令、不重新执行 action。

`WorkflowActionProcessRecordsFromRunExport` 是面向外部 template 的便利 facade：调用方传入 `RunExport`、可选 workflow id 和 1-based action index，即可获得单个 child action 的 `ProcessRecords` defensive copy。`WorkflowActionProcessRecordsFromRunExportByAction` / `WorkflowActionProcessRecordsFromRunExportByTaskID` 则在同一恢复路径上分别按唯一 action kind 或 child task id 提取。它们复用 `WorkflowRecordsFromRunExport` 和 `WorkflowActionProcessRecords` / `WorkflowActionProcessRecordsByAction` / `WorkflowActionProcessRecordsByTaskID`，因此仍然先做 workflow process conformance，再做 action boundary 过滤，不调度 workflow、不重新执行 action。

`BuildWorkflowProcessRecords` 的 workflow 父 task output 会保留 child action summary 中的 `action_status`；当 child action rejected/failed 时，父 task 会标记为 failed，并保留 `failed_action_count`、child `reason_count` 与 sanitized child summary。release child summary 会额外保留 `release_gate_input_id` 和 `review_intervention_id`，让 workflow 父摘要可以直接指向已封存的 gate/review 过程边界。patch input 仍只保存 id、summary、file_count、has_diff 和 file path/intent，不泄露 raw diff。`CheckWorkflowProcessConformance` / `RequireWorkflowProcessConformance` 可复用同一组 workflow process contract case，校验 workflow 父 task、workflow parent input action_count、child task id 非空且唯一、child task parent link、child task input/output 的 mode/action/status/reason_count 与 metadata/output 对齐、action 顺序、失败计数、workflow output input/intervention count 对齐、child summary 的 mode/action/action_status/reason_count 与 child task metadata/output 对齐、child summary input/intervention count 对齐、release child summary 的 gate/review boundary id 对齐、input/intervention workflow metadata、合法 action index、mode/action/action_status 与归属 child task 对齐、release action 必须能对账到同一 action index 的 `devagent.release_gate` input；如果 release gate 或 task metadata 携带 `review_status`，还必须能对账到同一 action index 的 review intervention，approved review 必须是 resolved intervention，rejected review 必须是 rejected intervention，并要求 workflow parent action summary 中的 `review_intervention_id` 与该 review boundary 对齐；patch input raw diff 不能泄漏；它只检查已观察过程记录，不调度 workflow、不重新评估 action、不 apply patch。

恢复边界不是 release 专属：任何 child action 只要带 `devagent.review_resume` / `InputResume`，workflow 父摘要都会保留 `resume_input_id`；任何 child action 只要带 approval review intervention，父摘要都会保留 `review_intervention_id`。conformance 会对账 summary、resume input 和同 action review intervention；release action 只是额外拥有 `release_gate_input_id`，用于定位 release gate input。

`CheckReleaseBundleConformance` / `RequireReleaseBundleConformance` 可复用同一组 release bundle contract case，校验 bundle 自身结构、required check ids、required evidence types、required CI gates，以及 `RecordReleaseBundleCheck` 产出的 `release_bundle` evidence 是否保留这些 required metadata；当 bundle process task 来自 workflow child 时，还会对账 run export 中的 workflow parent task、parent release summary、bundle process task、release gate input、review intervention 和 release action input/intervention count，避免 release bundle 与 workflow 父摘要漂移；`BuildReleaseBundle` 也会拒绝 process records 中混入其他 `workflow_action_index` 的 input/intervention 边界，确保单个 release action 的 evidence bundle 不被 plan/apply 阶段边界污染；它不执行 CI、不读取 evidence ref、不替代宿主 release gate，只固定已观察 release evidence 的可移植证明形态。

release gate 只消费已经产生的 `VerificationReport`、`EntropyAudit` 和 reviewer decision，不执行命令、不扫描工作区、不 apply patch；调用方可以通过 `RequireCheckIDs` 要求指定 verification checks 存在且 passed，通过 `RequireEvidenceTypes` 要求 run_export、model_call、tool_call、channel_event、effect_replay、run_effect_replay、memory_replay、memory_work_schedule、policy_decision、ci_gate、diff、file_snapshot、checkpoint、checkpoint_objectstore_index、trajectory、failure_attribution、entropy_audit 等已观察 evidence type，通过 `RequireCIGates` 要求 `ci_gate` evidence 中指定 gate 已 passed，从而把生产级 harness 策略留在宿主侧。`RecordReleaseGateCheck` 可把已评估的 `GateResult` 转成标准 release gate verification evidence，调用方 metadata 不能覆盖 gate status/mode/report/review/entropy/reasons 等 canonical release gate 字段；rejected gate 会先记录 failed evidence 再返回 `ErrReleaseGateRejected`。write mode 默认要求 verification report 通过、没有超过阈值的 entropy finding、review approved；analyze/plan mode 只返回 skipped。

## 测试要求

ReAct template 至少有这些 trajectory tests：

- `[done: first slice]` 一次模型直接 final；
- `[done: first slice]` 一次模型调用工具后 final；
- `[done: first slice]` ReAct event stream 可以导出包含 completed `StepSnapshot` 和 run-level verification report 的 `RunExport`；
- `[done: first slice]` memory recall 注入可观察；
- `[done: first slice]` 显式 `WithMemoryExtractor` 可同步写入 memory，产生 `MemoryPut` 事件和已 applied 的 `memory_put` effect；
- `[done: first slice]` `WithMemoryMerge` 可在 extractor 后、写入/effect 记录前运行宿主注入的 memory 压缩/合并策略，SDK 不内置总结算法；
- `[done: first slice]` `WithMemoryWriteMode(MemoryWriteDeferred)` 可记录 pending `MemoryPut` event 和可重放 idempotent `memory_put` effect，不提前写入 store，供宿主后台 executor 或队列提交；
- `[done: first slice]` `WithMemoryExtractMode(MemoryExtractDeferred)` 可记录 pending `memory_extract` effect，不调用 extractor、不提前写入 store；宿主可把该 effect 交给 `memory.NewExtractionReplayHandler`，并自行决定在 worker、队列或 resume 流程中触发；
- `[done: second slice]` `PlanDeferredMemoryWork` 可从 `RunExport` 中筛选未应用的 pending `memory_put` / `memory_extract` effects，`ExecuteDeferredMemoryWork` 通过宿主注入的 replay executor 执行；`RunDeferredMemoryWork` 返回单次 worker pass 的 status、plan、results、partial results 和 error summary；`NewMemoryDeferredMemoryWorkQueue` 提供本地内存队列，支持 pending、complete、retry requeue、stop、dead-letter 和 snapshot defensive copy；`NewMemoryDeferredMemoryWorkQueueWithVisibilityTimeout` 提供单进程 visibility timeout reference 行为，dequeue 后进入 in-flight 并设置 `DeferredMemoryWorkJob.DeliveryID` 作为单次 delivery receipt，超时未完成会在下一次 dequeue 或 terminal transition 前重新可见，stale delivery transition 会返回明确错误；本地 receipt 不写入业务 `Metadata`，也不进入 snapshot / transition records；`gopacttest/reactconformance.CheckDeferredMemoryWorkQueueConformance` / `RequireDeferredMemoryWorkQueueConformance` 可让外部 durable queue adapter 复用 empty dequeue、canceled context、dequeue、complete、retry requeue、retry 保留 job metadata 并合并 decision metadata、stop/dead-letter terminal transition、input immutability 和 concurrent dequeue 不重复分发同一 job 的基础 contract case；`gopacttest/reactconformance.CheckDeferredMemoryWorkQueueVisibilityConformance` / `RequireDeferredMemoryWorkQueueVisibilityConformance` 可让外部 durable queue adapter 复用同一批 delivery receipt、in-flight、timeout redelivery 和 stale transition contract case；`NewDeferredMemoryWorkRetryDecider` 提供默认有界 retry/backoff 调度决策，只返回 retry/dead-letter decision 和 delay，不 sleep、不持有队列；`DeferredMemoryWorkWorker.RunOnce` 可消费 `DeferredMemoryWorkQueue`，按 report/decider 调用 `Complete` / `Retry` / `Stop` / `DeadLetter`，且 worker 未显式配置 decider 时默认接入该 retry/backoff decider；`WithDeferredMemoryWorkReportRecorder` 可选把每次已 dequeue 的 worker pass 写成 `memory_replay` evidence，失败 report 的 failed evidence 不阻断后续 retry/stop/dead-letter 调度；`WithDeferredMemoryWorkLease` 可用宿主注入的 `LeaseBackend` 对每次 `RunOnce` 做 acquire / transition check / release ownership gate，竞争 lease 时不会 dequeue；`WithDeferredMemoryWorkLeaseRenewalInterval` 可在单次 worker pass 内按间隔续租，续租 token 更新与 transition 前 lease 校验互斥，续租丢失会阻止 complete/retry/stop/dead-letter queue transition，release cleanup 对已丢失 lease 幂等；`DeferredMemoryWorkWorker.Drain` 可在显式 limit 内重复调用 `RunOnce`，直到队列为空、达到上限或遇到终端错误，并返回 completed/retried/dead-lettered/stopped 汇总；`RecordDeferredMemoryWorkCheck` 可把 worker report 记录成 `memory_replay` evidence；`RecordDeferredMemoryWorkScheduleCheck` 可把宿主已观察 retry / stop / dead-letter 调度决策记录成 `memory_work_schedule` evidence，dead-letter 会成为 failed check；SDK 不内置生产持久队列、线程池、distributed queue leasing、sleep scheduler 或生产 DLQ storage；
- `[done: first slice]` tool approval interrupt 可以产生 policy events、`EventInterrupted` / `EventRunInterrupted` 和 pending approval `StepSnapshot`；
- `[done: first slice]` tool policy deny 可以保留 policy events，并产生 failed step snapshot 与 `RunFailed` outcome；
- `[done: first slice]` visible/deferred tool 隔离，deferred tool 只有 promote 后才进入 model-visible tool set，且未 promote 时即使命中名称也不能被 model-driven invocation 执行；未 promote 的 deferred tool 被模型猜名调用时有 golden trajectory fixture；
- `[done: first slice]` tool approval resume 可以从 interrupted tool `StepExport` + `ResumeRequest` 继续原 tool call，并把 resume payload 暴露给 tool policy；
- `[done: first slice]` 普通多 tool 批次 tool-then-final 和多 tool 批次中途 tool error 已有 golden trajectory fixture；
- `[done: first slice]` 复杂多 tool 批次 resume 不重复已完成工具；
- `[done: first slice]` completed `call_model` / `call_tool` `StepExport` 可以分别继续 pending tool calls / 下一次 model call；
- `[done: first slice]` completed `call_model` / `call_tool` `StepExport` resume 可以分别继续 pending tool calls / 下一次 model call，并有 golden trajectory fixture；
- `[done: first slice]` completed `call_model` / `call_tool` checkpoint 可以通过 `react.WithCheckpointStore` 按 `ThreadID` 恢复继续；
- `[done: first slice]` completed `call_model` checkpoint resume 可以继续 pending tool calls，并有 golden trajectory fixture；
- `[done: first slice]` completed `call_tool` checkpoint resume 不重复执行已完成 tool，并有 golden trajectory fixture；
- `[done: first slice]` tool approval interrupted checkpoint 可以通过 `react.WithCheckpointStore` + `ResumeRequest.CheckpointID` / `InterruptID` 恢复继续；
- `[done: first slice]` `react.WithArtifactVerifier` 可以在 step/checkpoint import 继续执行前校验 artifact refs，失败时阻止恢复；checkpoint artifact verifier failure 有 golden trajectory fixture；
- `[done: first slice]` verifier failed report / verifier error 会产生 failed verify step 与 `RunFailed` outcome，并有 golden trajectory fixture；
- `[done: first slice]` max iterations exceeded 产生结构化错误、`RunFailed` outcome 和 golden trajectory fixture；
- `[done: first slice]` missing tool registry 产生 failed tool step、`RunFailed` outcome 和 golden trajectory fixture；
- `[done: first slice]` tool runtime error 产生 failed tool step、`RunFailed` outcome、`FailureTool` attribution 和 golden trajectory fixture；
- `[done: first slice]` provider fallback event 可通过 `gopact.AdaptStreamingModel(router)` 进入 ReAct `call_model` 事件流；
- `[done: first slice]` artifact ref 出现在 tool result event、tool step snapshot 和 run export step 中；
- checkpoint 后 resume 不重复非幂等工具；
- 后续更高级 memory 压缩/合并策略、durable 生产队列 adapter、distributed queue leasing、concurrency orchestration、生产级调度策略和真实 retry/DLQ storage 行为可观察；本地内存队列、local visibility timeout、RunOnce lease gate / pass-local lease renewal 和有界 drain loop 只覆盖单进程 reference/test 或宿主已提供 lease backend 的 ownership gate 场景；

## Agent-as-Tool 第一片

`templates/agenttool` 把子 agent 暴露成普通 `gopact.Tool`，供 ReAct、Supervisor 或业务 template 调用。当前第一片覆盖本地 runner、direct `a2a.Agent` 适配、可选 HTTP JSON/JSONL wrapper 和可选 JSON-RPC 2.0 + SSE wrapper，并允许宿主注入 discovered card 与 sanitized auth context；它不绑定具体配置文件、OAuth 流程或完整 official A2A proto/schema：

- A2A `AgentCard` 可以转成 `ToolSpec`，`ToolSpec` 也可以转回最小 `AgentCard`；
- 默认 tool args 支持 `{"input":"..."}` 和 `{"messages":[...]}`；
- `tools.Registry` 会把 `Scope.IDs` 注入 tool context，agent tool 基于父 `CallID` 派生 child `CallID`，并保留 `ParentCallID`；
- child agent events 会进入 `ToolResult.Events`，artifact refs 会进入 `ToolResult.Artifacts`；
- `agenttool.NewA2A` 可以把远程 `a2a.Agent` 包装成 tool，调用时发送 `a2a.Task`，并把 `a2a.Result.Output` / artifact refs / metadata 映射回 `ToolResult`；
- remote A2A 调用会产生 `a2a_task_sent`、`a2a_task_completed` 或 `a2a_task_failed` 事件，task id 映射到 child `CallID`；
- remote A2A 调用可以通过 `agenttool.WithPolicy` 在 send 前走 `PolicyBoundaryA2A` / `PolicyActionSend`，policy deny 不会发送任务，policy review 会返回 approval interrupt；
- remote A2A 调用可以通过 `agenttool.WithTimeout` 设置 send timeout，超时会保留 `a2a_task_failed` 事件和错误；
- remote A2A task 可以通过 `agenttool.A2ATool.Cancel` 显式取消，cancel 会走 `PolicyBoundaryA2A` / `PolicyActionCancel`，成功时产生 `a2a_task_canceled` 事件；
- remote A2A task 可以通过 `agenttool.A2ATool.Stream` 消费 `a2a.StreamingAgent` 任务流，send policy 仍在任务离开进程前执行；stream 会产生 `a2a_task_sent`、`a2a_message_received`、`a2a_artifact_updated`、`a2a_task_status_updated`、`a2a_task_completed` / `a2a_task_failed` / `a2a_task_canceled` 事件，并保留 status message、metadata、artifact refs 和 parent/child call chain；
- 本地 child agent run 已有 run_started/model_message/tool_result/run_completed 事件顺序的 golden trajectory fixture，并通过 `gopacttest.RequireTemplateTrajectoryConformance` 固定 terminal event 与 required event sequence；本地 child failure 已有 run_started/run_failed 事件顺序 golden trajectory fixture；
- remote A2A task stream 已有 message/artifact/status/completed 事件顺序的 golden trajectory fixture；remote A2A send failure 和 send timeout 已有 sent/failed 事件顺序 golden trajectory fixture；remote A2A send policy deny、stream policy deny 和 cancel policy deny 已有 policy requested/decided 事件顺序 golden trajectory fixture；remote A2A cancel success 已有 canceled 事件 golden trajectory fixture；
- remote A2A tool 可以通过 `agenttool.WithCard` 使用 `a2a.Registry.Discover` 得到的 discovered card 作为 model-visible spec；discovery 会产生 `a2a_agent_card_fetched` event evidence；
- remote A2A tool 可以通过 `agenttool.WithAuth` 注入 `a2a.Authenticator`；auth 发生在 policy 和 remote send/stream/cancel 之前，只把 scheme、principal、credential ref 等 sanitized context 写入 task/context 和审计 metadata，不读取配置文件、不持有 secret 原文；
- child agent failure 通过 tool error 返回，child failure events 仍保留在 `ToolResult.Events`，由父 template 决定如何失败、重试或中断。

后续 remote A2A 能力需要继续补更完整 official task/message/artifact schema、production discovery registry、OAuth/advanced auth negotiation、resumable/production streaming adapter 深化和 artifact transfer。
