# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化工作流执行，以及可恢复的运行时状态。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加模型适配器或完整的 ReAct 执行能力。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件和检查点都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。

总体设计入口见 [docs/design/index.md](docs/design/index.md)，研发计划见 [docs/design/development-plan.md](docs/design/development-plan.md)，milestone 状态清单见 [docs/design/milestone-readiness.json](docs/design/milestone-readiness.json)，主仓边界清单见 [docs/design/repository-boundary.json](docs/design/repository-boundary.json)，root public API 边界清单见 [docs/design/public-api-boundary.json](docs/design/public-api-boundary.json)，root public API 示例契约见 [docs/design/public-api-examples.json](docs/design/public-api-examples.json)，public API 废弃策略见 [docs/design/deprecation-policy.md](docs/design/deprecation-policy.md)，版本策略见 [docs/design/versioning-policy.md](docs/design/versioning-policy.md)，迁移指南见 [docs/design/migration-guide.md](docs/design/migration-guide.md)，template 指南见 [docs/design/template-guide.md](docs/design/template-guide.md)，生产集成外部路线见 [docs/design/external-integration-roadmap.json](docs/design/external-integration-roadmap.json)，外部仓库初始化清单见 [docs/design/external-repositories.json](docs/design/external-repositories.json)，外部仓库 scaffold 蓝图见 [docs/design/extension-scaffold-spec.json](docs/design/extension-scaffold-spec.json)，外部扩展兼容性契约见 [docs/design/extension-conformance.json](docs/design/extension-conformance.json)，扩展仓库 README 模板见 [docs/design/extension-repository-template.md](docs/design/extension-repository-template.md)，扩展仓库 CONFORMANCE 模板见 [docs/design/extension-conformance-template.md](docs/design/extension-conformance-template.md)。项目级原则见 [docs/design/philosophy.md](docs/design/philosophy.md)，API 调用体验设计见 [docs/design/api-ergonomics.md](docs/design/api-ergonomics.md)，核心契约设计见 [docs/design/contracts.md](docs/design/contracts.md)，事件流设计见 [docs/design/events.md](docs/design/events.md)，checkpoint/resume 设计见 [docs/design/checkpoint-resume.md](docs/design/checkpoint-resume.md)，SDK setup 与默认值设计见 [docs/design/sdk.md](docs/design/sdk.md)，配置注入设计见 [docs/design/config.md](docs/design/config.md)，安全设计见 [docs/design/security.md](docs/design/security.md)，channel/transfer 设计见 [docs/design/channels.md](docs/design/channels.md)，扩展性设计见 [docs/design/extensibility.md](docs/design/extensibility.md)，运行时模块设计见 [docs/design/modules.md](docs/design/modules.md)。

调研记录见 [docs/research/agent-sdk-landscape.md](docs/research/agent-sdk-landscape.md) 和 [docs/research/harness-loop-engineering.md](docs/research/harness-loop-engineering.md)。

`gopact` 从第一版运行时开始就要具备 model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的 core contract 和默认实现。`artifact`、`policy`、typed options/config snapshot 是基础契约和支撑能力，不归入业务运行时模块；生产后端通过 adapter 或 plugin 接入。

## 当前形态

- `gopact`：provider-neutral 的消息、模型请求、模型响应、streaming model adapter、工具规格、工具调用、tool retry decision contract / middleware、执行事件、surface message / transfer / channel root 契约、run option、step/resume import、root `JSONSchemaValidator` / `JSONSchemaValidatorFunc` 可插拔 schema validator 契约、root `ValidateJSONSchemaValue` portable schema subset 校验、root `ValidateResumePayload` resume payload schema gate（含 pattern、exclusive bounds、multipleOf 第二片）、run export、run export JSON schema、run export -> verification evidence 桥接、已观察 model call / tool call / channel event -> verification evidence 桥接、policy decision -> verification evidence 桥接、task/input/intervention/failure process record、细粒度 failure attribution taxonomy、failure attribution -> verification evidence 桥接、entropy audit、entropy audit -> verification evidence 桥接、run-level verification report 和 verification recorder 契约。
- `graph`：类型化 graph 执行，包含 `Start`、`End`、节点函数、编译期校验、event stream、checkpoint hook/loader/store、node middleware option、completed/interrupted/canceled `StepExport` resume 第一片，以及 checkpoint latest load、interrupted resume payload `ResumeSchema` 校验、`StepImported` / `CheckpointLoaded` / `ResumeReceived` / `NodeResumed` 恢复事件；导入 step/checkpoint 时可通过 `WithArtifactVerifier` 先校验 artifact refs，并会在事件 metadata 中附带 effect replay plan，root `EventEffectReplayPlan` 可类型化读取。
- `provider`：模型 provider registry、route set、router、model middleware、model rate limit middleware、fake provider、fallback/error classification。
- `adapters/model/openaicompatible`：OpenAI-compatible chat completion adapter，适合 OpenRouter、企业网关和兼容接口。
- `adapters/observability/trace`：provider-neutral trace plugin 第一片，把 runtime event stream 投影成 `SpanRecord`，提供 `MemoryExporter`、`ExporterFunc`、HTTP/JSON `HTTPExporter`、OTLP/HTTP JSON `OTLPHTTPExporter`、LangSmith-compatible `LangSmithHTTPExporter`、LangGraph-style `LangGraphHTTPExporter` 第一片、trace exporter policy wrapper 和 trace exporter conformance helper；后续真实 LangSmith Go SDK、dataset/evaluation/feedback/run query 和更深插件治理可接在该接口后面。
- `tools`：visible/deferred tool registry、tool search、promotion、direct invocation、model-visible invocation guard、tool middleware、scope metadata 进入 tool policy、tool-call `EffectRecord` 第一片、`ToolResult.Commit` 幂等提交契约第一片、tool-level retry policy contract / middleware、tool-call replay handler 第一片、effect graph/replay policy 契约、`PlanEffectReplay` 单步 runtime plan、`BuildRunEffectGraph` 跨 step effect graph、`PlanRunEffectReplay` run-level plan、`EffectReplayRegistry` handler 分发，`ExecuteEffectReplay` / `ExecuteRunEffectReplay` executor 第一片，以及 step-level/run-level effect replay verification evidence 桥接。
- `artifact`：进程内 artifact store，生成 `ArtifactRef` 的 size/hash 元数据，提供 `VerifyRef` / `VerifyRefs` payload integrity 校验原语，可接入 graph resume 边界的 `WithArtifactVerifier`，提供 `RecordVerifyRefs` verification evidence 桥接、policy store wrapper，以及 `artifact_write` replay verify handler 第一片。
- `sandbox`：本地 allowlist sandbox 和内存 sandbox，覆盖路径逃逸、命令 allowlist、timeout、输出限制，提供 sandbox exec -> `VerificationRecorder` evidence 桥接、policy manager/session wrapper，并提供 `sandbox_exec`、`sandbox_file_read`、`sandbox_file_write` replay handler 第一片。
- `memory`：长期记忆 store，支持 scope/type/text 搜索、内存实现、policy store wrapper、`memory_put` / `memory_delete` / `memory_search` replay handler 第一片、`memory_extract` replay handler 第一片，以及已观察 memory replay work -> verification evidence 桥接。
- `skill`：skill registry、搜索和激活记录，提供 filesystem registry loader、register/get/search/activate policy wrapper、resource read / script exec 原子接口、fake reader/runner、local `FileResourceReader`、sandbox-backed `SandboxScriptRunner`，以及 resource/script policy wrapper 第一片；remote install、signing registry 和生产级脚本 artifact/limits 策略仍是后续项。
- `mcp`：MCP-like server manager 和 client 契约，聚合 namespaced tools/resources/prompts，提供 newline JSON-RPC transport、newline interleaved server-to-client request handler、newline notification handler、Streamable HTTP POST + JSON/SSE response transport 第一片、POST request-scoped SSE resume、Streamable HTTP GET listen stream、HTTP/SSE interleaved server-to-client capability request dispatch、HTTP/SSE notification handler、URL-mode elicitation completion notification handler 第一片、continuous listen reconnect/retry、session/protocol header、DELETE session termination 与 404 session-expired 处理第一片、legacy initialize/initialized 兼容握手、JSON-RPC client 第一片、`ToolServer` server adapter 第一片、sampling/elicitation handler contract、policy wrapper 与 `CapabilityServer` JSON-RPC dispatch 第一片、manager connect/list policy wrapper，以及 client list/call/read/get policy wrapper 第一片。
- `a2a`：A2A agent registry、local runnable agent adapter、HTTP JSON/JSONL client/server wrapper、JSON-RPC 2.0 + SSE client/server wrapper、agent card discovery、auth context、task send/cancel，以及 streaming task message、artifact update 和 status 的最小契约。
- `TurnLoop`：root 包里的多轮控制面第一版，支持 turn event、cancel、preempt、`Push` pending input queue、`Resume` queue、`Interrupted` input + interrupt record 记录、默认 `TurnInputBatch` 合并包、`TurnInputMergeFunc` / `WithTurnInputMerge` 业务级输入合并策略第一片、resume request 透传到 root `RunOption` 第一片、TurnLoop 入口 `ResumeSchema` gate 第一片、`WithTurnJSONSchemaValidator` 自定义 schema validator 注入、TurnLoop resume policy gate/event 第一片、`Close(ctx)` 取消 active turn 并按 runner -> store 顺序关闭资源、成功资源幂等且失败资源可重试、关闭后 `Run` / `Push` 返回 `ErrTurnLoopClosed`、`TurnLoopStore` 持久化第一片、内存 store、本地 JSON file store、row backend store 端口、`adapters/turnloop/sqlstore` database/sql row/versioned CAS backend 第一片、`adapters/turnloop/httpstore` HTTP/JSON control-plane row/versioned CAS backend 第一片、`adapters/turnloop/redisstore` Redis GET/SET/EVAL row/versioned CAS backend 第一片、`adapters/turnloop/objectstore` conditional object versioned CAS backend 第一片、blob backend store 端口第一片、`adapters/storage/fileblob` filesystem blob backend 第一片、`adapters/storage/objectblob` 通用对象 client blob backend 第一片、`adapters/storage/s3blob` AWS SDK v2 S3 object/blob adapter 第一片、`adapters/storage/gcsblob` Google Cloud Storage object/blob adapter 第一片、`adapters/storage/r2blob` Cloudflare R2 S3-compatible object/blob adapter 第一片、`adapters/storage/ossblob` Alibaba Cloud OSS SDK v2 object/blob adapter 第一片、versioned CAS store 端口第一片，以及 `NewLeasedTurnLoopStore` worker ownership wrapper 第一片，并复用 root `ResumeRequest`。
- `LeaseBackend`：root worker ownership 租约契约第一片，支持 acquire/renew/release/get、owner+token 校验、TTL 过期转移和内存实现；`NewLeasedTurnLoopStore` 可把 lease 包到任意 `TurnLoopStore` 外层，已支持可选后台续约、`LeaseObserver` 争用/续约观测、`Release(ctx)` / `Close(ctx)` 释放，以及随 `TurnLoop.Close` 关闭；`adapters/lease/sqlstore` 已提供 database/sql lease backend 第一片，`adapters/lease/redisstore` 已提供 Redis GET/EVAL lease backend 第一片，`adapters/lease/httpstore` 已提供 HTTP/JSON control-plane lease backend 第一片，`adapters/lease/objectstore` 已提供 conditional object lease backend 第一片。
- `NodeContext` / `EventContext` / `PluginHost`：Gin 风格 `Next()` node middleware、event/model/tool middleware、`EventSinkMiddleware` strict/fallback、`AsyncEventSink` bounded queue/backpressure 第一片、插件注册、事件订阅、插件 subscriber strict/fallback、插件能力声明、插件生命周期状态机、幂等 close 和 close-while-running 等待；`graph.WithNodeMiddleware` 与 Runner/provider/tools middleware 已接入执行路径。
- `InterruptRecord` / `ResumeRequest`：root interrupt/resume 契约、结构化 interrupt error、root `WithStepExport` / `WithResumeRequest` / `WithJSONSchemaValidator` 运行选项、`ValidateJSONSchemaValue` / `ValidateJSONSchemaValueWith` / `ValidateResumePayload` / `ValidateResumePayloadWithValidator` / `ErrJSONSchemaValidationFailed` / `ErrResumePayloadInvalid` 校验原语，以及 graph interrupted resume 第一片。
- `Policy` / `TextRedactor` / `SecretProvider`：root policy contract、secret reference/provider/value 原子契约、model/tool/A2A send/channel/exporter 以及 memory/sandbox/artifact/skill/MCP wrapper policy boundary、policy requested/decided events、review-to-approval interrupt、`RedactModelRequest` / `RedactModelResponse` / `ModelIORedactionMiddleware`、`RedactToolResult` / `ToolResultRedactionMiddleware`、event redaction middleware、`Event.Redaction` 状态和结构化 policy denial error；`SecretValue` 的 string/fmt/JSON 输出固定 redacted，SDK 不读取 env/file/config。
- `SurfaceMessage` / `Transfer` / `Channel`：root 展示与交互边界第一片，支持把 runtime event 投影成平台无关的 `SurfaceMessage`，由 `Transfer` 转成目标 payload，再由 `Channel` 投递并接收 `ChannelEvent`；`PolicyChannel` 已提供 send/receive 治理第一片，`adapters/channel/tui` 已提供 writer-based TUI transfer/channel 第一片，`adapters/channel/sse` 已提供 HTTP SSE transfer/channel 第一片，`adapters/channel/lark` 已提供 host-injected Lark transfer/channel/callback source 第一片，`adapters/channel/a2ui` 已提供 A2UI v0.9 JSON message transfer、JSONL channel、action decode、history replay、local catalog registry、structural validation、复用 root `ValidateJSONSchemaValue` 且可通过 `ValidatorConfig.SchemaValidator` 注入完整 engine 的 component JSON Schema validation、client-supported catalog negotiation 和 in-memory reference renderer 第一片；`adapters/channel/agui` 已提供 AG-UI event transfer、HTTP SSE event stream channel 和 action POST 回流第一片；完整前端 renderer、完整 JSON Schema engine adapter/plugin、更完整 catalog negotiation 深化、AG-UI WebSocket/plugin adapter、Lark 真实 client/OAuth/plugin、生产级 nonce/replay/user mapping 仍是后续项。
- `checkpoint`：checkpoint record、`StateCodec`、默认 `JSONCodec`、统一 `WithCodec` option、state hash integrity 校验、record migration 第一片、config drift 默认拒绝/显式允许策略、内存 store、本地 JSON file store、数据库型 `RowBackend` / `RowStore` 端口、`adapters/checkpoint/sqlstore` database/sql backend 第一片、`adapters/checkpoint/redisstore` Redis atomic row/index backend 第一片、`adapters/checkpoint/objectstore` conditional object row/index backend 与索引 `VerifyIndex` / `RepairIndex` / `RecordIndexConsistencyCheck` 第一片、`adapters/checkpoint/s3store` AWS SDK v2 S3 conditional checkpoint backend 第一片、`adapters/checkpoint/gcsstore` Google Cloud Storage generation-CAS checkpoint backend 第一片、`adapters/checkpoint/r2store` Cloudflare R2 S3-compatible conditional checkpoint backend 第一片、`adapters/checkpoint/ossstore` Alibaba Cloud OSS SDK v2 conditional checkpoint backend 第一片、对象存储 `ObjectBackend` / `ObjectStore` 第一片、`adapters/storage/fileblob` filesystem object backend、`adapters/storage/objectblob` 通用对象 client backend 第一片、`adapters/storage/s3blob` AWS SDK v2 S3 object backend 第一片、`adapters/storage/gcsblob` Google Cloud Storage object backend 第一片、`adapters/storage/r2blob` Cloudflare R2 S3-compatible object backend 第一片、`adapters/storage/ossblob` Alibaba Cloud OSS SDK v2 object backend 第一片，以及已观察 checkpoint record -> checkpoint evidence 桥接，支持写入、按 id get、按 thread list/latest load。
- `gopacttest`：event stream 收集、event type 断言、compact trajectory frame helper、golden trajectory fixture helper、template trajectory conformance helper、trajectory golden -> `VerificationRecorder` evidence 桥接、portable JSON Schema validator conformance helper、extension scaffold conformance helper、turnloop store conformance helper、channel/transfer conformance helper、verification evidence conformance helper（可要求 report valid、指定 check id、指定 evidence type 和具体 CI gate 已 passed）、已观察命令结果 -> command evidence 桥接、`RecordCIGateSuiteCheck` 已观察 CI gate suite -> `ci_gate` evidence 聚合桥接、已观察文件快照 -> file snapshot evidence 桥接、已观察 diff -> diff evidence 桥接，以及已观察 reviewer decision -> review evidence 桥接；review evidence metadata 会保留 reviewer source/status 和宿主传入的 prompt/eval/policy governance metadata；`gopacttest/providerconformance` 提供 provider conformance helper，`gopacttest/checkpointconformance` 提供 checkpoint store conformance helper，`gopacttest/reactconformance` 提供 ReAct deferred memory work queue conformance helper。
- `templates/react`：最小 ReAct template 第一片，支持 model final、model tool call、tool result 回灌、可选 memory recall 注入、显式 `WithMemoryExtractor` 同步写入、`WithMemoryMerge` 宿主注入 memory 压缩/合并策略、`WithMemoryWriteMode(MemoryWriteDeferred)` 记录可重放后台 memory write effect、`WithMemoryExtractMode(MemoryExtractDeferred)` 记录可由 `memory.NewExtractionReplayHandler` 消费的 host-managed `memory_extract` effect、`PlanDeferredMemoryWork` / `ExecuteDeferredMemoryWork` / `RunDeferredMemoryWork` 从 `RunExport` 调度和执行 pending memory effects、`NewMemoryDeferredMemoryWorkQueue` 本地内存队列、`NewDeferredMemoryWorkRetryDecider` 默认有界 retry/backoff 调度决策器、`DeferredMemoryWorkWorker.RunOnce` 消费 `DeferredMemoryWorkQueue` 并执行 complete / retry / stop / dead-letter 队列转换，且 worker 未显式配置 decider 时默认接入该 retry/backoff decider、`WithDeferredMemoryWorkLease` 可用宿主注入 `LeaseBackend` 对每次 `RunOnce` 做 acquire / transition check / release ownership gate，`WithDeferredMemoryWorkLeaseRenewalInterval` 可在单次 pass 内续租并在续租丢失时阻止 queue transition、`DeferredMemoryWorkWorker.Drain` 以显式 limit 批量消费到队列空、达到上限或终端错误并返回汇总、`RecordDeferredMemoryWorkCheck` 把单次 worker pass 记录为 `memory_replay` evidence、`RecordDeferredMemoryWorkScheduleCheck` 把宿主 retry / stop / dead-letter 调度决策记录为 `memory_work_schedule` evidence、completed step snapshot、completed `call_model` / `call_tool` `StepExport` 恢复继续、`WithCheckpointStore` 持久化 completed model/tool checkpoint 并按 `ThreadID` resume、interrupted tool approval checkpoint + `ResumeRequest` 恢复继续、`WithArtifactVerifier` 校验 step/checkpoint artifact refs 后再恢复、tool approval interrupt/resume snapshot、多 tool 批次 resume 跳过已完成工具、通过 `gopact.AdaptStreamingModel(router)` 消费 provider fallback events、tool artifact refs 进入 events/run export、run export 记录、可选 `WithVerifier` verify node 生成 `VerificationReport` gate，并由 `RunRecorder` 把 verify event metadata 提升为 run-level verification report，以及 direct final / tool-then-final / multi-tool-then-final / multi-tool-error / memory recall / memory write / memory merge / memory deferred write / memory deferred extract / provider fallback / approval interrupt / policy deny / approval step resume / multi-tool pending resume / interrupted checkpoint resume golden trajectory tests。
- `templates/agenttool`：Agent-as-Tool template 第一片，支持 A2A agent card 与 tool spec 互转，把本地 `gopact.Runnable` 或远程 `a2a.Agent` 包装成 `gopact.Tool`，并通过 `RuntimeIDs` context 保留 parent/child call chain、child events、A2A task events、send/cancel policy events、timeout failure、cancel event、streaming message/artifact/status events、discovered card spec、sanitized auth context 和 artifact refs。
- `templates/devagent`：Dev Agent template 的 mode/action gate、entropy audit collector、reviewer plugin slot 和 release gate 第一片，区分 analyze/plan/write mode，约束 patch proposal、patch apply、review 和 release 所需证据；write apply 必须带 policy allow decision、sandbox event、observed diff 和 observed checkpoint ref；release gate 默认消费 verification/entropy/reviewer decision，并可通过 `RequireCheckIDs` 要求指定 verification checks 存在且 passed，通过 `RequireEvidenceTypes` 要求 run_export、model_call、tool_call、channel_event、effect_replay、run_effect_replay、memory_replay、memory_work_schedule、policy_decision、ci_gate、diff、file_snapshot、checkpoint、checkpoint_objectstore_index、trajectory、failure_attribution、entropy_audit 等已观察证据类型，通过 `RequireCIGates` 要求 `ci_gate` evidence 中指定 gate 已 passed；root `RecordRunExportCheck` 可把已观察 `RunExport` 转成标准 run export evidence，调用方 metadata 不能覆盖 outcome/count/runtime ids 等 canonical run export 字段；`RecordModelCallCheck` 可把已观察 `ModelRequest` / `ModelResponse` / error 的路由、usage 和结构摘要转成标准 model call evidence 且不保存 raw prompt/response text，`RecordToolCallCheck` 可把已观察 `ToolCall` / `ToolResult` / error 的工具名、运行身份、payload/result shape 和 artifact/effect/event 计数转成标准 tool call evidence 且不保存 raw args/result content，`RecordChannelEventCheck` 可把已观察 `ChannelEvent` / error 的 channel、action、runtime ids、payload/text shape 和 metadata keys 转成标准 channel event evidence 且不保存 raw text/payload，`RecordPolicyDecisionCheck` 可把已观察 policy request/decision 转成标准 policy evidence；`BuildEntropyAudit` 从已观察 patch metadata、unified diff header 和 `VerificationReport` 生成 `EntropyAudit`，root `RecordEntropyAuditCheck` 可把该 audit 转成标准 entropy audit evidence；这些 root/replay/checkpoint/gopacttest/devagent/objectstore evidence bridge 的调用方 metadata 只能补充非保留字段，不能覆盖 ref、计数字段、runtime ids、状态、错误和 shape 摘要等 canonical evidence 字段；`BuildProcessRecords` / `RecordProcessRecords` 可把已观察 action、sanitized patch summary、release gate 和 reviewer decision 转成 `RunRecorder` 的 task/input/intervention process records，release gate input 会记录 gate status/mode/report/review/max entropy 摘要，且不保存 raw diff；`BuildWorkflowProcessRecords` / `RecordWorkflowProcessRecords` 可把一组已观察 action boundary 汇总成 workflow 父 task、子 action task、input 和 intervention records，父 task output 会带稳定 child action summary，并通过 `workflow_id` / `workflow_action_index` / `workflow_action_count` 保留稳定顺序，同时子 action 会继承 workflow runtime identity 且拒绝显式冲突的 run/user/session/thread/agent/app/call/trace 等字段；`BuildReleaseBundle` / `ReleaseBundle` 可把已观察 `RunExport`、`VerificationReport`、entropy audits、approved review decision、passed `GateResult`、write-mode release action 和 sanitized process records（可通过 `ReleaseBundleInput.Process` 显式传入已观察 workflow child process records）打包成 release-ready evidence bundle，并校验 run/report/gate/process/required evidence/required CI gates 对齐，要求 gate 内的 report/review status 与 max entropy severity 摘要存在且匹配 bundle 中的 report/review/entropy audits，要求 process task metadata/input/output 与 release action 对齐、release gate input value 与 gate 摘要对齐、review intervention metadata 与 reviewer/status 对齐，要求 run export 与 process records 匹配 bundle 已知的 session/user/thread/agent/call/trace 等 runtime identity，且 verification report / entropy audit 只要携带这些细分身份也必须匹配；如果 run export 内已经携带 verification reports / task/input/intervention process records，则必须分别包含 bundle 顶层 verification report 和 bundle process records 的同一语义快照；同时拒绝 failed entropy audit 和仍包含 `FailureAttribution` 的 run export；bundle 会防御性拷贝 `RunExport`，避免宿主后续 mutation 污染已封存证据；`RecordReleaseBundleCheck` 可把校验通过的 bundle 写入 `VerificationRecorder`，生成标准 `release_bundle` evidence，并在 metadata 中保留 reviewer、gate report/review/max entropy 摘要、required CI gates 以及 process task / release gate input / review intervention id；调用方 metadata 只能补充非保留字段，不能覆盖这些 canonical release evidence 字段；`Review` / `ReviewerFunc` / `StaticReviewer` 只消费已观察证据并返回显式 reviewer decision，`gopacttest.RecordReviewCheck` 可把该 decision 转成标准 review evidence，并在 evidence metadata 中保留 reviewer decision metadata；`RecordReleaseGateCheck` 可把已评估的 `GateResult` 转成标准 release gate evidence，调用方 metadata 不能覆盖 gate status/mode/report/review/entropy/reasons 等 canonical release gate 字段；不执行命令、不扫描工作区、不 apply patch。
- Dev Agent action metadata 会从 `EvaluateAction` 防御性复制到 `ActionResult`，并贯穿 process/workflow child task、input、intervention metadata，保留 prompt/eval/policy governance ref；SDK canonical 字段仍覆盖冲突键。
- `adapters/devagent/gitdiff`：Dev Agent git diff scanner adapter 第一片，支持 worktree/staged diff，调用 git diff 捕获 unified diff 与 numstat，并输出 `gopacttest.DiffSnapshot` 和 `devagent.PatchProposal`，供 verification evidence 和 entropy audit 使用。
- `adapters/devagent/channelreview`：Dev Agent channel reviewer adapter 第一片，可选通过 `Transfer`/`Channel` 先投递 `SurfaceMessageApproval` review prompt，再消费 `gopact.ChannelEvent` 中的 approve/reject action 或 payload，转换成 `devagent.ReviewDecision`；适合把 Lark/TUI/SSE/CI 等外部审批入口通过统一 channel 边界接入 release gate。
- `adapters/devagent/cireview`：Dev Agent CI reviewer adapter 第一片，只消费宿主已观察到的 `VerificationReport`、required checks 和 `EntropyAudit`，不执行命令、不连接 CI 系统，把通过/失败/缺失检查和 entropy 风险转换成显式 `devagent.ReviewDecision`。
- `adapters/devagent/modelreview`：Dev Agent model reviewer adapter 第一片，通过宿主注入的 `gopact.ChatModel` 把已观察 review input 序列化成模型请求，只接受显式 JSON approve/reject decision；`WithGovernance` 可把 prompt id/version、eval id/version、policy ref 和宿主 metadata 注入模型请求与 reviewer decision，形成可审计的 prompt/eval governance 边界；不执行命令、不扫描工作区、不替代 release gate。
- `docs/design/index.md`：总体设计入口、架构图、组件交互和 milestone。
- `docs/design/milestone-readiness.json`：M1-M6 的状态、证据文档、未完成项和自举级别清单，用于判断路线图是否真的完成。
- `docs/design/public-api-boundary.json`：root 包顶层导出符号的分类、稳定性、来源文件和导出方法 receiver 继承策略清单，防止新增 public API 绕过 SDK 边界审查。
- `docs/design/public-api-examples.json`：root 包关键 SDK 入口的可执行 Example 覆盖契约，防止调用体验退化。
- `docs/design/deprecation-policy.md`：root public API 的稳定性、废弃标记、移除窗口和兼容性审查规则。
- `docs/design/versioning-policy.md`：core SDK、schema 和外部 extension 的 semver、release gates 和兼容性策略。
- `docs/design/migration-guide.md`：v1 前后的 public API、adapter split、checkpoint/resume 和 verification 迁移文档要求。
- `docs/design/template-guide.md`：外部 graph template 的边界、step export/resume、events/verification、memory/side effect 和 conformance 要求。
- `docs/design/core-ci-gates.json`：core repo 自身的 CI gate 清单，覆盖 whitespace、test、race、vet、lint、coverage、examples 和 security，并绑定 `.golangci.yml` 与 `coverage.out`。
- `docs/design/external-integration-roadmap.json`：OpenAI/Anthropic/Gemini/OpenRouter、Redis/SQL/S3/GCS/R2/OSS、A2UI/AG-UI/Lark/WebSocket、LangSmith/LangGraph、CI/model reviewer 等生产集成的外部 adapter/plugin/template 路线，并显式标注每条路线是否已可 scaffold。
- `docs/design/external-repositories.json`：gopact-ai 组织下外部 adapter/plugin/template 私有仓库初始化清单，绑定每个 roadmap target repo、extension target、必备 scaffold 文件和 CI 命令。
- `docs/design/extension-scaffold-spec.json`：外部仓库初始化蓝图，定义通用 scaffold 文件规则、每个 repo 的 module path 和 extension target 初始 package path。
- `internal/extensionscaffold`：本仓内部外部仓库 scaffold materializer，`LoadRepositoriesFromDesign` 可从 `docs/design/external-repositories.json`、`docs/design/extension-conformance.json` 和 `docs/design/extension-scaffold-spec.json` 读取仓库计划，`WriteRepositoriesFromDesign` 可把所有计划批量写成 `<output>/<repo-name>/go.mod`、README、CONFORMANCE、CI 和 `examples/minimal_test.go`，并在 CONFORMANCE 中记录已知 gopacttest helper reference、由 minimal test 做机器检查，`RenderSyncPlanFromDesign` 可生成包含私有仓库创建命令、文件清单、CI 命令和本地验证命令的机器可读远端同步计划，`RenderSyncScriptFromDesign` 可生成可审查的 GitHub 私有仓库同步脚本，并在本地 git 初始化后逐仓库执行 manifest required CI commands；它不属于 root SDK API，也不读取 SDK 运行时配置文件。
- `cmd/gopact-extscaffold`：本仓维护工具入口，可运行 `go run ./cmd/gopact-extscaffold -root . -out /tmp/gopact-ext -verify` 批量生成外部仓库 scaffold workspace、本地 `go.work`、`sync-plan.json`、`sync-repos.sh`、`sync-secrets.sh` 并逐仓库运行 manifest required CI commands（默认 `git diff --check`、`go test -count=1 ./...`、`go vet ./...`），用 `-dry-run` 查看将生成的仓库和文件数，用 `-plan-json` 输出远端私有仓库 bootstrap/sync 的 JSON 计划，用 `-plan-sh` 输出 shell 同步脚本，用 `-plan-secrets-sh` 输出显式配置 `GOPACT_GITHUB_TOKEN` repo secret 的脚本，或用 `-remote-status-json` 只读审计 gopact-ai 远端仓库是否存在、是否私有、CI workflow 是否存在、`GOPACT_GITHUB_TOKEN` secret 是否存在以及最新 Actions run 是否通过；`go.work` 只用于把生成的外部模块绑定到当前 SDK root 进行本地 conformance，`sync-plan.json` / `sync-repos.sh` / `sync-secrets.sh` 是给后续 GitHub 初始化/同步流程消费的计划文件，三者都不是外部仓库发布文件；`sync-repos.sh` 会在推送前用 `GOWORK=off go mod tidy` 生成远端可用的 `go.sum`，外部仓库 CI 在主仓仍私有时需要 `GOPACT_GITHUB_TOKEN` 或具备跨仓读权限的 `github.token`。
- `docs/design/extension-conformance.json`：外部 adapter/plugin/template 仓库的兼容性目标、必跑 conformance suite、scaffold 文件、CI 命令和 examples 要求。
- `docs/design/extension-repository-template.md`：外部扩展仓库 README 模板，要求显式说明兼容矩阵、安装、用法、conformance、examples 和安全边界。
- `docs/design/extension-conformance-template.md`：外部扩展仓库 CONFORMANCE 模板，要求记录 target、required suites、CI commands、integration tags 和安全边界。
- `docs/design/extension-ci-workflow.yml`：外部扩展仓库的默认 GitHub Actions CI 模板，覆盖 whitespace、test 和 vet gate。
- `docs/design/api-ergonomics.md`：调用体验、命名、option 分层、示例和 exported API review 清单。
- `docs/design/contracts.md`：`Message`、`ContentPart`、`RuntimeIDs`、`Event`、`SurfaceMessage`、`Transfer`、`ChannelEvent`、`StepSnapshot`、`StepExport`、`CheckpointRecord`、`InterruptRecord`、`ResumeRequest`、`ArtifactRef`、`PolicyRequest`、`PolicyDecision`、`VerificationReport` / `VerificationRecorder` 等基础契约。
- `docs/design/events.md`：事件顺序、event stream API、redaction、sink 失败策略和 channel/OTel 映射。
- `docs/design/checkpoint-resume.md`：checkpoint、interrupt、resume、cancel-safe point 和副作用幂等设计。
- `docs/design/sdk.md`：SDK setup 入口、默认 logger、全局默认值、option 优先级和测试约束。
- `docs/design/config.md`：runner、模块、adapter、plugin、transfer、channel 的 typed option 注入、热替换和 secret provider 设计。
- `docs/design/security.md`：信任边界、policy、redaction、sandbox、MCP/A2A、skill 和 channel 安全模型。
- `docs/design/channels.md`：`SurfaceMessage`、transfer、channel adapter 和 Lark/TUI/A2UI 等展示接入设计。
- `docs/design/templates.md`：ReAct、Agent-as-Tool、Dev Agent 等 graph template 的边界和测试要求。
- `docs/design/extensibility.md`：hook、middleware、plugin 的扩展性设计。
- `docs/design/modules.md`：model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的运行时模块设计。
- `docs/design/development-plan.md`：M4 closure、M5 template、自举门槛和生产化的研发计划。
- `docs/research/agent-sdk-landscape.md`：对 LangGraph、LangChain、Eino、Google ADK、OpenRouter、CC Switch、oh-my-pi 的调研记录。
- `docs/research/harness-loop-engineering.md`：对业务层 harness/loop/context/prompt engineering、turn-control、MCP loop 风险和长周期代码退化的调研记录。
- `docs/superpowers/plans/2026-06-23-gopact-runtime-spine.md`：M1 Runtime Spine 的研发计划。

## 示例

```go
package main

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

type State struct {
	Trace []string
}

func main() {
	ctx := context.Background()
	g := graph.New[State]()

	g.AddNode("plan", func(ctx context.Context, state State) (State, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddNode("act", func(ctx context.Context, state State) (State, error) {
		state.Trace = append(state.Trace, "act")
		return state, nil
	})
	g.AddEdge(graph.Start, "plan")
	g.AddEdge("plan", "act")
	g.AddEdge("act", graph.End)

	run, err := g.Compile()
	if err != nil {
		panic(err)
	}

	store := checkpoint.NewMemory[State]()
	var result State
	for event, err := range run.Run(
		ctx,
		State{},
		graph.WithRuntimeIDs(gopact.RuntimeIDs{RunID: "demo-run", ThreadID: "demo-thread"}),
		graph.WithCheckpointStore(store),
	) {
		if err != nil {
			panic(err)
		}
		if event.Type == gopact.EventNodeCompleted {
			result = event.StepSnapshot.Output.(State)
		}
	}

	fmt.Println(result.Trace)
}
```

## 开发

```bash
make fmt
make test
make vet
```

当前模块路径是 `github.com/gopact-ai/gopact`。如果最终 GitHub owner 不同，请在第一次公开发布前替换。
