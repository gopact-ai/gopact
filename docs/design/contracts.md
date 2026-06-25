# gopact 核心契约设计

日期：2026-06-23

设计入口：[index.md](index.md)

本文定义 `gopact` 在 M1 必须稳定下来的基础契约。基础契约不是运行时模块，也不是 adapter；它们是模块、graph、runner、middleware、plugin 和外部系统之间共同使用的数据边界。

## 目标

核心契约要满足：

1. provider-neutral：不暴露 OpenAI、Gemini、Anthropic、OpenRouter 等专有类型；
2. replay-friendly：一次运行可以通过事件、checkpoint、artifact 引用和配置版本重放；
3. redaction-ready：敏感字段可以在进入外部 sink 前统一脱敏；
4. versioned：序列化格式有版本字段，允许后续兼容演进；
5. testable：不接真实模型、MCP server、A2A agent 或 sandbox 后端也能构造测试 fixture。

## 契约分层

| 层级 | 契约 | 说明 |
| --- | --- | --- |
| 输入输出 | `Message`、`ContentPart`、`ToolSpec`、`ToolResult`、`ModelRequest` | agent 与模型、工具之间的数据结构 |
| 运行身份 | `RuntimeIDs`、`RunContext`、`CallContext` | run/thread/session/user/agent/call 的身份边界 |
| 运行记录 | `Event`、`RunExport`、`TaskRecord`、`InputRecord`、`InterventionRecord`、`FailureAttribution`、`EntropyAudit`、`ModelRoute`、`Usage` | 可观察、可评估、可导出的轨迹和过程包 |
| 对外展示 | `SurfaceMessage`、`SurfacePart`、`SurfaceAction`、`Transfer`、`Channel`、`ChannelEvent` | 面向 TUI、Lark、Web、A2UI、AG-UI 的统一输出与交互语义 |
| 恢复边界 | `StepSnapshot`、`StepExport`、`CheckpointRecord`、`InterruptRecord`、`ResumeRequest` | step 级 export/import、checkpoint、interrupt、resume、cancel-safe point |
| 并发所有权 | `LeaseRecord`、`LeaseRequest`、`LeaseBackend` | 分布式 worker ownership、续约、释放和过期转移 |
| 外部产物 | `Artifact`、`ArtifactRef` | 文件、图片、报告、大 payload、跨系统输出 |
| 敏感引用 | `SecretRef`、`SecretProvider`、`SecretValue` | secret 只能由宿主通过 reference 和 provider 注入；resolved value 的 string/fmt/JSON 表示固定 redacted |
| 治理决策 | `PolicyRequest`、`PolicyDecision`、`ChannelPolicyInput`、`mcp.PolicyInput`、`skill.PolicyInput`、`trace.PolicyInput` | provider、tool、sandbox、memory、MCP、skill、A2A、channel、trace export、artifact export 的授权结果 |

`Artifact`、`Policy`、`LeaseBackend` 和 `SecretProvider` 是基础契约。它们有默认实现或可替换 adapter，但不归入 provider/tool/sandbox/memory/skill/MCP/A2A 这类业务运行时模块。`SecretProvider` 只定义注入和解析边界，SDK 不读取 env、文件或配置系统。

## RuntimeIDs

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
```

规则：

- `ThreadID` 归 checkpoint/resume；
- `RunID` 归一次运行尝试；
- `CallID` 归当前 node/model/tool/sandbox/memory/MCP/A2A 调用；
- `TraceID` 用于 OpenTelemetry、LangSmith 或内部 trace 系统；
- 简单应用可以让多个字段取同一个值，但 SDK 不能假设它们相同；
- 事件、checkpoint、artifact、policy request 必须携带同一组身份字段或其稳定子集。
- `ContextWithRuntimeIDs` / `RuntimeIDsFromContext` 只用于 tool、adapter、middleware 这类下游边界读取 request-scoped runtime identity；业务配置、长期状态和可恢复数据仍必须通过显式参数、事件、checkpoint 或 export 表达。

## Message 和 ContentPart

`Message.Content` 字符串只适合早期兼容。目标契约必须是 block-friendly：

```go
type Message struct {
	Role       Role
	Name       string
	Parts      []ContentPart
	Metadata   map[string]any
}

type ContentPart struct {
	Type       ContentPartType
	Text       string
	Reasoning *ReasoningPart
	ToolCall  *ToolCall
	ToolResult *ToolResult
	Media      *MediaPart
	Metadata   map[string]any
}
```

基础 part 类型：

| 类型 | 用途 |
| --- | --- |
| `text` | 普通文本 |
| `reasoning` | 模型推理摘要或 reasoning metadata，不默认展示给终端用户 |
| `tool_call` | 模型请求调用工具 |
| `tool_result` | 工具执行结果 |
| `server_tool_call` | provider-side tool 或 server-side tool |
| `mcp_tool_call` | MCP 工具调用边界 |
| `media` | 图片、音频、视频、文件引用 |

规则：

- 大二进制内容必须走 `ArtifactRef`，不能直接塞进 `Message`；
- `ContentPart.Metadata` 只能承载 adapter metadata，不承载核心字段；
- provider adapter 可以丢弃不支持的 part，但必须通过事件记录降级原因；
- tool call/result 必须能和 `CallID`、provider tool call id、artifact refs 对账。

## ModelRequest、ModelResponse 和 ModelRoute

`ModelRequest` 只表达模型调用意图，不表达具体 provider API。

```go
type ChatModel interface {
	Generate(ctx context.Context, request ModelRequest) (Message, error)
}

type ResponseModel interface {
	Generate(ctx context.Context, request ModelRequest) (ModelResponse, error)
}

type StreamingModel interface {
	Stream(ctx context.Context, request ModelRequest) iter.Seq2[Event, error]
}

type ModelRequest struct {
	IDs            RuntimeIDs
	Model          string
	Messages       []Message
	Tools          []ToolSpec
	ResponseSchema JSONSchema
	RouteHint      string
	Capabilities   []Capability
	Budget         Budget
	Metadata       map[string]any
}

type ModelResponse struct {
	Message  Message
	Route    ModelRoute
	Usage    Usage
	Events   []Event
	Metadata map[string]any
}
```

`ModelRoute` 记录一次模型调用如何被路由：

```go
type ModelRoute struct {
	RouteName     string
	Provider      string
	Model         string
	Endpoint      string
	Attempt       int
	ConfigVersion string
	Reason        string
}
```

规则：

- provider-specific 参数只能进入 adapter metadata 或 typed route policy；
- route decision、retry decision、failover decision 都必须进入事件流；
- `ChatModel` 是 template 的最小依赖；provider router 这类完整模型运行时返回 `ModelResponse` 并通过 `StreamingModel` 暴露 route/fallback attempt events；
- SDK 提供 `AdaptResponseModel` / `AdaptStreamingModel` 把完整模型运行时接入最小 `ChatModel` 入口；template 如果检测到 `StreamingModel`，应消费它的事件流，而不是吞掉 provider route/fallback events；
- 使用 token、cost、latency、cache hit、rate limit metadata 时，字段名要稳定。

## Event

`Event` 是运行时事实记录，不是日志字符串。

```go
type Event struct {
	ID          string
	Type        EventType
	IDs         RuntimeIDs
	Sequence    int64
	ParentID    string
	CheckpointID string
	Payload     EventPayload
	Redaction   RedactionState
	CreatedAt   time.Time
}
```

事件 payload 必须是结构化数据。大 payload 通过 `ArtifactRef` 引用。详细语义见 [events.md](events.md)。

## SurfaceMessage

`SurfaceMessage` 是 agent 对外展示的统一语义。它不是 Lark card、TUI line、A2UI message 或 SSE event；这些目标格式由 transfer 转换生成。

```go
type SurfaceMessage struct {
	ID          string
	IDs         RuntimeIDs
	Type        SurfaceMessageType
	Target      SurfaceTarget
	Parts       []SurfacePart
	Actions     []SurfaceAction
	Artifacts   []ArtifactRef
	SourceEvent string
	Metadata    map[string]any
	CreatedAt   time.Time
}
```

规则：

- `SurfaceMessage` 从 event stream 投影得到，不直接改变 runtime state；
- 当前 root package 已提供 `ProjectSurfaceMessages`、`Transfer` / `TransferFunc`、`Channel` / `ChannelFunc`、`ChannelPayload`、`ChannelEvent` 和 `ChannelEvent.ResumeRequest()` 第一片；
- 大内容必须使用 `ArtifactRef`；
- action 必须携带 run/thread/session/call/interrupt 关联信息；
- transfer 可以把同一 `SurfaceMessage` 转成 TUI、Lark、A2UI、AG-UI、SSE 等 payload；
- inbound action 必须通过 TurnLoop input/resume/action 边界回到 runtime。

## CheckpointRecord

`CheckpointRecord` 是恢复契约，不只是 graph state 的序列化。

```go
type CheckpointRecord struct {
	ID            string
	SchemaVersion string
	IDs           RuntimeIDs
	ThreadID      string
	Step          int
	Node          string
	Phase         StepPhase
	State         []byte
	StateCodec    string
	StateHash     string
	Queue         []string
	Pending       *InterruptRecord
	Effects       []EffectRecord
	ConfigVersion string
	CreatedAt     time.Time
	Metadata      map[string]any
}
```

当前代码第一片落在 `checkpoint.Record`：`StateCodec` / `JSONCodec` 负责稳定 state payload，`WithCodec` 统一注入 store codec，`StateHash` 用于 import/load integrity 校验，`WithRecordMigration` 处理 record schema migration 第一片，`WithConfigVersion` / `WithCurrentConfigVersion` / `WithConfigDriftPolicy` 处理 config drift 第一片，`checkpoint.Memory` 内部保存 encoded record 并在 `Get` / `Latest` 时解码；`checkpoint.FileStore` 提供本地 JSON 文件持久化、原子写入和跨实例恢复第一片；`checkpoint.RowStore` 提供数据库型行存储端口第一片，通过 `RowBackend` 注入真实 SQL/KV/内部表；`adapters/checkpoint/sqlstore` 提供 database/sql row backend 第一片，`adapters/checkpoint/redisstore` 提供 Redis atomic row/index backend 第一片，`adapters/checkpoint/objectstore` 提供 conditional object row/index backend 第一片，通过 `IfAbsent` / `IfVersion` CAS 维护 thread index，并提供 `VerifyIndex` / `RepairIndex` / `RecordIndexConsistencyCheck` 第一片；`adapters/checkpoint/s3store` 提供 AWS SDK v2 S3 conditional checkpoint backend 第一片，把 ETag 映射为 CAS version；`adapters/checkpoint/gcsstore` 提供 Google Cloud Storage conditional checkpoint backend 第一片，把 object generation 映射为 CAS version；`adapters/checkpoint/r2store` 提供 Cloudflare R2 S3-compatible conditional checkpoint backend 第一片，复用 S3-compatible ETag 条件写；`adapters/checkpoint/ossstore` 提供 Alibaba Cloud OSS SDK v2 conditional checkpoint backend 第一片，把 OSS ETag 映射为 CAS version，并把 `IfAbsent` 映射到 `x-oss-forbid-overwrite`、把 `IfVersion` 映射到 SDK v2 `RequestCommon.Headers["If-Match"]`；这些 adapter 都不接管连接池、配置文件或 migration；`checkpoint.ObjectStore` 提供对象存储端口第一片，按 record 独立写入对象并通过 `ObjectBackend` 注入真实后端；`adapters/storage/fileblob` 提供 filesystem object/blob backend 第一片，`adapters/storage/objectblob` 提供通用对象 client object/blob backend 第一片，`adapters/storage/s3blob` 提供 AWS SDK v2 S3 object/blob adapter 第一片，`adapters/storage/gcsblob` 提供 Google Cloud Storage object/blob adapter 第一片，`adapters/storage/r2blob` 提供 Cloudflare R2 S3-compatible object/blob adapter 第一片，`adapters/storage/ossblob` 提供 Alibaba Cloud OSS SDK v2 object/blob adapter 第一片，可同时服务 checkpoint object 与 TurnLoop blob。详细恢复协议见 [checkpoint-resume.md](checkpoint-resume.md)。

## StepSnapshot 和 StepExport

`StepSnapshot` 是 workflow/graph 的过程快照。它比 checkpoint 更接近执行模型：每个稳定 step 边界都可以产生 snapshot，用于 export、import、debug、replay 和 resume。

```go
type StepSnapshot struct {
	ID          string
	Step        int
	Node        string
	Phase       StepPhase
	IDs         RuntimeIDs
	Input       any
	Output      any
	Queue       []string
	Pending     *InterruptRecord
	Error       string
	Effects     []EffectRecord
	Artifacts   []ArtifactRef
	StartedAt   time.Time
	CompletedAt time.Time
	Metadata    map[string]any
}

type StepExport struct {
	Version  int
	Step     StepSnapshot
	Metadata map[string]any
}

type EffectReplayPolicy string

const (
	EffectReplayRecordOnly EffectReplayPolicy = "record_only"
	EffectReplayIdempotent EffectReplayPolicy = "idempotent"
	EffectReplaySkip       EffectReplayPolicy = "skip"
)

type EffectRecord struct {
	ID             string
	Type           string
	Target         string
	Applied        bool
	ReplayPolicy   EffectReplayPolicy
	IdempotencyKey string
	DependsOn      []string
	Artifacts      []ArtifactRef
	Sandbox        *SandboxEffect
	Metadata       map[string]any
}

type SandboxEffect struct {
	SessionID string
	Operation string
	Path      string
	Command   []string
	ExitCode  int
	Metadata  map[string]any
}

type EffectReplayAction string

const (
	EffectReplayActionRecordOnly EffectReplayAction = "record_only"
	EffectReplayActionReplay     EffectReplayAction = "replay"
	EffectReplayActionSkip       EffectReplayAction = "skip"
)

type EffectReplayPlan struct {
	StepID          string
	Step            int
	Node            string
	Decisions       []EffectReplayDecision
	ReplayCount     int
	SkipCount       int
	RecordOnlyCount int
}

type EffectReplayDecision struct {
	Effect         EffectRecord
	Action         EffectReplayAction
	ReplayPolicy   EffectReplayPolicy
	IdempotencyKey string
	Reason         string
}

type EffectReplayResult struct {
	EffectID     string
	Action       EffectReplayAction
	ReplayPolicy EffectReplayPolicy
	Effect       EffectRecord
	Metadata     map[string]any
}

type RunEffectReplayPlan struct {
	RunID           string
	ThreadID        string
	Decisions       []RunEffectReplayDecision
	ReplayCount     int
	SkipCount       int
	RecordOnlyCount int
}

type RunEffectReplayDecision struct {
	StepID   string
	Step     int
	Node     string
	Index    int
	Decision EffectReplayDecision
}

type RunEffectReplayResult struct {
	StepID string
	Step   int
	Node   string
	Index  int
	Result EffectReplayResult
}

type EffectReplayExecutor interface {
	ReplayEffect(ctx context.Context, decision EffectReplayDecision) (EffectReplayResult, error)
}

type EffectReplayRegistry struct{}

func NewEffectReplayRegistry() *EffectReplayRegistry
func (r *EffectReplayRegistry) Register(effectType string, handler EffectReplayExecutor) error
func (r *EffectReplayRegistry) RegisterTarget(effectType, target string, handler EffectReplayExecutor) error
func (r *EffectReplayRegistry) RegisterFallback(handler EffectReplayExecutor) error

// package graph
type ArtifactVerifier interface {
	VerifyRefs(ctx context.Context, refs []gopact.ArtifactRef) error
}

type ArtifactVerifierFunc func(context.Context, []gopact.ArtifactRef) error

func WithArtifactVerifier(verifier ArtifactVerifier) InvokeOption

type RunExport struct {
	Version       int
	IDs           RuntimeIDs
	Outcome       RunOutcome
	Events        []Event
	Steps         []StepSnapshot
	Tasks         []TaskRecord
	Inputs        []InputRecord
	Interventions []InterventionRecord
	Failures      []FailureAttribution
	EntropyAudits []EntropyAudit
	VerificationReports []VerificationReport
	CreatedAt     time.Time
	Metadata      map[string]any
}

type TaskRecord struct {
	ID          string
	ParentID    string
	Name        string
	Status      TaskStatus
	IDs         RuntimeIDs
	Input       any
	Output      any
	Error       string
	Artifacts   []ArtifactRef
	CreatedAt   time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	Metadata    map[string]any
}

type InputRecord struct {
	ID        string
	Kind      InputKind
	IDs       RuntimeIDs
	Source    string
	Value     any
	Resume    *ResumeRequest
	CreatedAt time.Time
	Metadata  map[string]any
}

type InterventionRecord struct {
	ID         string
	Type       InterruptType
	Status     InterventionStatus
	IDs        RuntimeIDs
	Request    *InterruptRecord
	Resume     *ResumeRequest
	CreatedAt  time.Time
	ResolvedAt time.Time
	Metadata   map[string]any
}

type FailureAttribution struct {
	ID        string
	Kind      FailureKind
	IDs       RuntimeIDs
	Node      string
	Step      int
	Summary   string
	Error     string
	Evidence  []VerificationEvidence
	CreatedAt time.Time
	Metadata  map[string]any
}

type EntropyAudit struct {
	ID        string
	Status    VerificationStatus
	IDs       RuntimeIDs
	Findings  []EntropyFinding
	CreatedAt time.Time
	Metadata  map[string]any
}

type EntropyFinding struct {
	ID        string
	Category  EntropyCategory
	Severity  EntropySeverity
	Summary   string
	Evidence  []VerificationEvidence
	CreatedAt time.Time
	Metadata  map[string]any
}

type VerificationStatus string

const (
	EventMetadataVerificationReport = "verification_report"

	VerificationStatusPassed  VerificationStatus = "passed"
	VerificationStatusFailed  VerificationStatus = "failed"
	VerificationStatusSkipped VerificationStatus = "skipped"
	VerificationStatusPartial VerificationStatus = "partial"
)

type VerificationEvidence struct {
	Type     string
	Ref      string
	Summary  string
	Metadata map[string]any
}

type VerificationCheck struct {
	ID       string
	Name     string
	Status   VerificationStatus
	Summary  string
	Evidence []VerificationEvidence
	Metadata map[string]any
}

type VerificationReport struct {
	Version      int
	IDs          RuntimeIDs
	Outcome      RunOutcome
	Status       VerificationStatus
	Checks       []VerificationCheck
	PassedCount  int
	FailedCount  int
	SkippedCount int
	CreatedAt    time.Time
	Metadata     map[string]any
}

type VerificationRecorder struct{}

func NewVerificationRecorder() *VerificationRecorder
func (r *VerificationRecorder) Record(check VerificationCheck) error
func (r *VerificationRecorder) Checks() []VerificationCheck
func (r *VerificationRecorder) Report(export RunExport) (VerificationReport, error)
```

规则：

- `StepSnapshot` 只在稳定 step 边界产生，不能捕获 node 内部任意半执行状态；
- `TaskRecord`、`InputRecord`、`InterventionRecord`、`FailureAttribution`、`EntropyAudit`、`VerificationReport` 是 run export 的过程记录，不执行任务、不合并输入、不自动恢复、不扫描文件或 diff；template、plugin 或宿主显式调用 recorder 写入；`RunRecorder` 也会从 `RunFailed` 终止事件派生最小 failure attribution，优先使用事件 metadata 的 `EventMetadataFailureKind` 显式归因，其次按 policy denial / policy request、标准 `call_model` / `call_tool` / verify / context / review / resume / entropy / sandbox / external 等 node 信号做保守分类；当前 root taxonomy 覆盖 `runtime`、`unknown`、`context`、`model`、`tool`、`feedback`、`policy`、`verification`、`recovery`、`entropy`、`sandbox`、`external`；`RunRecorder` 会从事件 metadata 的 `verification_report` 提取 run-level verification report，若失败前已经观察到 failed verification report，派生归因会升级为 `FailureVerification` 并附带 report evidence；
- 当前 root contract 先保留 Go 原生 `Input` / `Output`；跨进程稳定导出后续再补更完整的 schema version；
- `graph.WithArtifactVerifier` 是 import/load 边界的 artifact integrity 消费接口，当前会在继续执行前校验 `StepSnapshot.Artifacts` 和每个 `EffectRecord.Artifacts`；
- 大输入、输出和状态可以只保存 `ArtifactRef`，但 import 时必须能解析；
- `Effects` 记录已经完成或已观察到的外部副作用，包含 dependency edge、artifact refs、sandbox 操作摘要和 replay policy；
- `EffectReplayRecordOnly` 是默认策略，表示只记录过程证据，不能自动 replay 或 skip；
- `EffectReplayIdempotent` 必须带 `IdempotencyKey`，表示宿主可按幂等键安全重试或重放；
- `EffectReplaySkip` 表示恢复时可以复用已记录结果跳过重复执行，通常用于 artifact 引用或纯缓存结果；
- `ToolResult.Commit` 是默认 `tool_call` effect 的幂等提交声明；工具返回 `ToolCommit{IdempotencyKey: ...}` 后，tools registry 会把默认 tool-call effect 标记为 `EffectReplayIdempotent`，写入 idempotency key，并在 effect metadata 中保存可重放的 `tool_args`；显式 `EffectReplayIdempotent` 但缺少 key 会在 registry 边界拒绝；
- `ToolRetryPolicy` / `ToolRetryRequest` / `ToolRetryDecision` 是 tool retry decision contract；`DecideToolRetry` 只为已经失败的 tool attempt 生成 retry/stop 决策，不调用 tool、不执行业务 loop；`ToolRetryMiddleware` 只在 tool middleware 边界按显式 decider 重新运行下游 handler，默认不重试没有 idempotency key 的 tool，除非宿主通过 `RetryNonIdempotent` 显式放开；
- `EffectRecord.DependsOn` 可以引用同一 step 内的 effect，也可以在 `RunExport` 范围内引用前序 step 的 effect；空 dependency id 永远非法；
- `PlanEffectReplay` 会把同一 step 内的 effects 按依赖顺序转成 `EffectReplayPlan`，并在 `StepImported` / `CheckpointLoaded` 事件 metadata 的 `effect_replay_plan` 下暴露；调用方优先通过 root `EventEffectReplayPlan(event)` 类型化读取深拷贝，该 helper 同时支持进程内 typed metadata 和 JSON export/import 后的结构化 metadata；通过 `EffectReplaySnapshotFromEvent(event, results, err)` 把事件身份与已观察 replay 结果组装成 verification recorder input；二者都不执行 replay；
- `BuildRunEffectGraph` 会从 `RunExport` 的稳定 steps 构建 run-scope effect graph，拒绝重复 effect id、缺失 dependency、未来 dependency 和 cycle；
- `PlanRunEffectReplay` 会把 run-scope effect graph 转成带 step 身份的 `RunEffectReplayPlan`，但不执行任何 tool、sandbox、model 或 artifact 动作；
- `ExecuteEffectReplay` 和 `ExecuteRunEffectReplay` 只对 `replay` 决策调用外部 executor；`skip` 和 `record_only` 只生成结果，不触发外部动作；
- `EffectReplayRegistry` 是 backend replay adapter 的第一片注册点，按 `EffectRecord.Type + Target`、`Type`、fallback 的顺序分发；`tools.NewReplayHandler` 已提供 `tool_call` backend handler 第一片，读取 `tool_args` metadata 后重新调用 tool registry，registry 生成 idempotent `tool_call` effect 时会自动写入这份 args；`sandbox.NewReplayHandler` 已提供 `sandbox_exec`、`sandbox_file_read`、`sandbox_file_write` backend handler 第一片，读取 `EffectRecord.Sandbox.Command`、`EffectRecord.Sandbox.Path` 和 `sandbox_file_content` / `sandbox_file_mime_type` metadata 后通过 sandbox manager 执行；`memory.NewReplayHandler` 已提供 `memory_put`、`memory_delete`、`memory_search` backend handler 第一片，分别读取 `memory`、`memory_id`、`memory_query` metadata 后调用 memory store；`memory.NewExtractionReplayHandler` 已提供 `memory_extract` backend handler 第一片，读取 `memory_extract_state` / `memory_extract_ids` metadata 后调用宿主 extractor 并写入 memory store；`artifact.NewReplayHandler` 已提供 `artifact_write` backend verify handler 第一片，读取 `EffectRecord.Artifacts` 并校验 payload integrity；
- `RunExportJSONSchema()` 返回 `RunExport` v1 的可移植 JSON Schema map，覆盖 version、runtime ids、outcome、events、steps、entropy audits 和 verification reports 的核心结构；它不绑定具体 JSON Schema engine，调用方、adapter、CI 或 collector 可以自行选择校验器；
- `VerificationReport` / `VerificationRecorder` 只汇总已经发生的验证证据，不运行 command、不调用 model/tool；template 层可以通过 verify node 调用 verifier，把 report 放入事件 metadata 的 `verification_report`；`RunRecorder.RecordVerificationReport` 可显式记录 report，`RunRecorder.Record(Event)` 也会自动从 `verification_report` metadata 提取 report 写入 `RunExport.VerificationReports`；`RecordRunExportCheck` 是 run export evidence 第一片，会把宿主已观察到的 `RunExport`、outcome、runtime ids 和过程计数记录成标准 `run-export:<run_id>` check；completed export 至少要包含一个 event 和一个稳定 step 才能通过，否则先记录 failed check 再返回 `ErrRunExportIncomplete`；`RecordModelCallCheck` 是 model call evidence 第一片，会把宿主已观察到的 `ModelRequest` / `ModelResponse` / error 的 runtime ids、路由、usage、消息/工具/能力计数和输出 tool call 摘要记录成标准 `model-call:<ref>` check；它不触发模型调用，不保存 raw prompt 或 raw response text，失败调用会先记录 failed evidence 再返回 `ErrModelCallFailed`；`RecordToolCallCheck` 是 tool call evidence 第一片，会把宿主已观察到的 `ToolCall` / `ToolResult` / error 的 runtime ids、工具名、参数字节数、结果字节数、artifact/effect/event 计数和 result metadata 记录成标准 `tool-call:<ref>` check；它不触发工具执行，不保存 raw args 或 raw result content，失败调用会先记录 failed evidence 再返回 `ErrToolCallFailed`；`RecordChannelEventCheck` 是 channel event evidence 第一片，会把宿主已观察到的 `ChannelEvent` / error 的 runtime ids、channel、event/action 类型、text/payload shape 和 metadata keys 记录成标准 `channel-event:<ref>` check；它不触发 channel receive，不保存 raw text 或 raw payload，失败事件会先记录 failed evidence 再返回 `ErrChannelEventFailed`；`RecordFailureAttributionCheck` 是 failure attribution evidence 第一片，会把宿主已观察到的 `FailureAttribution`、kind、runtime ids、node/step、error、自定义 metadata 和关联证据记录成标准 `failure-attribution:<id>` failed check；它不推理归因，记录后返回 `ErrFailureAttributionFailed`，避免 release gate 把带失败归因的过程误判为通过；`RecordPolicyDecisionCheck` 是 policy decision evidence 第一片，会把宿主已观察到的 `PolicyRequest` / `PolicyDecision`、runtime ids、boundary/action 和 policy metadata 记录成标准 `policy-decision:<ref>` check，非 allow 决策会先记录 failed evidence 再返回 `ErrPolicyDecisionNotAllowed`；`RecordEntropyAuditCheck` 是 entropy audit evidence 第一片，会把宿主已观察到的 `EntropyAudit`、findings、max severity 和 runtime ids 记录成标准 `entropy-audit:<id>` check；`artifact.RecordVerifyRefs` 是 artifact integrity evidence 第一片，会把 refs 校验结果记录成标准 `artifact-integrity` check；`checkpoint.RecordVerificationCheck` 是 checkpoint record evidence 第一片，会把宿主已观察到的 checkpoint record、runtime ids、step/node/phase、queue、pending interrupt、effect/artifact 计数和错误记录成标准 `checkpoint` check；`gopacttest.RecordCommandCheck` 是 command result evidence 第一片，会把宿主已观察到的命令结果记录成标准 `command` check；`gopacttest.RecordCIGateSuiteCheck` 是 CI gate suite evidence 第一片，会把宿主已观察到的 gate 名称、命令、状态和聚合计数记录成标准 `ci_gate` check；`gopacttest.RecordCIRunCheck` 是远端 CI run evidence 第一片，会把宿主已观察到的 provider/repository/workflow/run/job/step 状态和 gate 聚合计数记录成同一标准 `ci_gate` check；`gopacttest.RecordFileSnapshotCheck` 是 file snapshot evidence 第一片，会把宿主已观察到的路径、hash、size、mtime 或读取错误记录成标准 `file-snapshot` check；`objectstore.RecordIndexConsistencyCheck` 是 checkpoint objectstore index evidence 第一片，会把宿主已观察到的 objectstore index report、thread 对齐、重复/缺失/错线程记录统计和错误记录成标准 `checkpoint-objectstore-index` check；`gopacttest.RecordDiffCheck` 是 observed diff evidence 第一片，会把宿主已观察到的 patch/worktree diff、文件列表和增删统计记录成标准 `diff` check；`gopacttest.RecordReviewCheck` 是 reviewer decision evidence 第一片，会把宿主已观察到的 human/model/CI/Lark 等 reviewer decision 记录成标准 `review` check；这些 root/replay/checkpoint/gopacttest/devagent/objectstore evidence bridge 的调用方 metadata 只能补充非保留字段，不能覆盖 ref、计数字段、runtime ids、状态、错误和 shape 摘要等 canonical evidence 字段；
- `StepExport` 必须能被另一个 runner 验证 integrity 后导入；
- step import 必须产生事件，并且不能绕过 policy、artifact integrity、config drift 检查和 schema version 检查。

## InterruptRecord 和 ResumeRequest

```go
type InterruptRecord struct {
	ID           string
	Type         InterruptType
	Reason       string
	Prompt       Message
	RequiredBy   string
	ResumeSchema JSONSchema
	CreatedAt    time.Time
	Metadata     map[string]any
}

type ResumeRequest struct {
	CheckpointID string
	StepID       string
	InterruptID  string
	IDs          RuntimeIDs
	Payload      any
	PayloadCodec string
	CreatedAt    time.Time
	Metadata     map[string]any
}
```

规则：

- node 可以返回 `Interrupt(record)`，graph 必须产生 `EventInterrupted` 和 `EventRunInterrupted`，不能降级成 `RunFailed`；
- interrupted step export 必须包含 `Pending`；
- `ResumeRequest.InterruptID` 必须匹配 pending interrupt；
- 当前 graph 第一片支持 interrupted `StepExport` + `ResumeRequest` 从 snapshot `Queue` 继续，也支持 interrupted checkpoint + `ResumeRequest.CheckpointID` 绑定恢复；root `WithStepExport` / `WithResumeRequest` 是跨 template 的单次 run 导入入口，graph runnable adapter 已把它们映射到 graph invoke option；root `ValidateResumePayload` 会按 pending `ResumeSchema` 校验 resume payload，并已接入 interrupted step/checkpoint resume 边界；checkpoint state codec、record migration 和 config drift 已有第一片。

## Artifact 和 ArtifactRef

```go
type ArtifactRef struct {
	ID        string
	URI       string
	MediaType string
	Name      string
	Size      int64
	SHA256    string
	Scope     ArtifactScope
}

type Artifact struct {
	Ref      ArtifactRef
	Bytes    []byte
	Metadata map[string]any
}
```

规则：

- artifact store 必须记录 media type、size、hash；
- 当前 `artifact.Memory` 会生成 size/hash，`artifact.VerifyRef` / `artifact.VerifyRefs` 已提供 payload integrity 校验第一片，`artifact.RecordVerifyRefs` 会把校验结果写入 `VerificationRecorder`，`artifact.NewReplayHandler` 会在 idempotent `artifact_write` replay 决策中验证已记录 artifact refs；
- 外部 export 必须走 policy；
- 事件和 checkpoint 默认只保存 `ArtifactRef`，不保存完整 bytes；
- 文件路径不能作为长期稳定引用，必须转换成 store 管理的 ref。

## LeaseRecord / LeaseBackend

`LeaseBackend` 是分布式 worker ownership 的原子契约，用来表达“谁现在有权继续处理某个 run/thread/turn key”。它不定义业务调度策略；root `NewLeasedTurnLoopStore` 是第一个组合消费方，用 lease 包住任意 `TurnLoopStore` 的 `Load` / `Save` 边界，并可通过 option 启用 SDK 级后台续约与 `LeaseObserver` 观测。更长周期的 worker 调度、业务级重试、Prometheus/OTel 指标导出和多租户控制面仍由宿主或生产 adapter 决定。

```go
type LeaseRecord struct {
	Key        string
	Owner      string
	Token      string
	AcquiredAt time.Time
	ExpiresAt  time.Time
	Metadata   map[string]any
}

type LeaseBackend interface {
	AcquireLease(ctx context.Context, request LeaseRequest) (LeaseRecord, error)
	RenewLease(ctx context.Context, request LeaseRenewRequest) (LeaseRecord, error)
	ReleaseLease(ctx context.Context, request LeaseReleaseRequest) error
	GetLease(ctx context.Context, key string) (LeaseRecord, bool, error)
}

type LeaseObserver interface {
	ObserveLease(ctx context.Context, event LeaseEvent)
}
```

规则：

- acquire 在 key 已被未过期 lease 持有时返回 `ErrLeaseConflict`；
- renew/release 必须同时校验 key、owner 和 token，失败返回 `ErrLeaseNotHeld`；
- token 是 ownership capability，不应由调用方伪造；
- 过期 lease 可以被新的 owner 获取；
- `Metadata` 只用于诊断和外部控制面索引，不参与 owner/token 判断；
- 当前代码已提供 `NewMemoryLeaseBackend` 第一片，覆盖 acquire/renew/release/get、TTL 过期、owner+token 校验和 metadata defensive copy；`NewLeasedTurnLoopStore` 已把 lease 原子契约接入 `TurnLoopStore` 的 `Load` / `Save` 边界，并提供 `WithLeasedTurnLoopRenewalInterval` 后台续约、`LeaseObserver` 续约/争用事件、显式 `Release(ctx)`、`Close(ctx)` 和随 `TurnLoop.Close` 关闭 store；`adapters/lease/sqlstore` 已提供 database/sql backend 第一片，接收 `*sql.DB`、`*sql.Tx` 或兼容 `DBTX`，支持默认 SQLite/Postgres/MySQL query 生成和宿主自定义 queries；`adapters/lease/redisstore` 已提供 Redis GET/EVAL backend 第一片，只依赖宿主注入的窄 Redis client，并用 Lua 原子完成 acquire/renew/release；`adapters/lease/httpstore` 已提供 HTTP/JSON control-plane backend 第一片，把内部控制面的 lease API 适配成 `LeaseBackend`；`adapters/lease/objectstore` 已提供 conditional object backend 第一片，要求宿主提供对象版本条件写/删能力。

database/sql lease backend 的最小 schema：

| 列 | 语义 |
| --- | --- |
| `lease_key` | lease key，必须唯一 |
| `owner` | 当前 owner id |
| `token` | 当前 ownership capability token |
| `acquired_at` | 获取时间，建议 RFC3339Nano UTC 字符串或等价 timestamp |
| `expires_at` | 过期时间，必须可被 acquire/renew/release query 比较 |
| `metadata_json` | 诊断和外部控制面索引 metadata |

## PolicyRequest / PolicyDecision

```go
type PolicyBoundary string

const (
	PolicyBoundaryNode     PolicyBoundary = "node"
	PolicyBoundaryModel    PolicyBoundary = "model"
	PolicyBoundaryTool     PolicyBoundary = "tool"
	PolicyBoundaryEvent    PolicyBoundary = "event"
	PolicyBoundaryMemory   PolicyBoundary = "memory"
	PolicyBoundarySandbox  PolicyBoundary = "sandbox"
	PolicyBoundaryArtifact PolicyBoundary = "artifact"
	PolicyBoundaryA2A      PolicyBoundary = "a2a"
	PolicyBoundaryChannel  PolicyBoundary = "channel"
	PolicyBoundaryMCP      PolicyBoundary = "mcp"
	PolicyBoundarySkill    PolicyBoundary = "skill"
	PolicyBoundaryExporter PolicyBoundary = "exporter"
)

type PolicyRequestAction string

const (
	PolicyActionRun      PolicyRequestAction = "run"
	PolicyActionGenerate PolicyRequestAction = "generate"
	PolicyActionInvoke   PolicyRequestAction = "invoke"
	PolicyActionEmit     PolicyRequestAction = "emit"
	PolicyActionSend     PolicyRequestAction = "send"
	PolicyActionReceive  PolicyRequestAction = "receive"
	PolicyActionConnect  PolicyRequestAction = "connect"
	PolicyActionActivate PolicyRequestAction = "activate"
	PolicyActionExport   PolicyRequestAction = "export"
	PolicyActionCreate   PolicyRequestAction = "create"
	PolicyActionExec     PolicyRequestAction = "exec"
	PolicyActionRead     PolicyRequestAction = "read"
	PolicyActionWrite    PolicyRequestAction = "write"
	PolicyActionPut      PolicyRequestAction = "put"
	PolicyActionGet      PolicyRequestAction = "get"
	PolicyActionSearch   PolicyRequestAction = "search"
	PolicyActionDelete   PolicyRequestAction = "delete"
	PolicyActionList     PolicyRequestAction = "list"
)

type PolicyRequest struct {
	IDs      RuntimeIDs
	Boundary PolicyBoundary
	Action   PolicyRequestAction
	Input    any
	Metadata map[string]any
}

type PolicyDecision struct {
	Action   PolicyAction
	Reason   string
	Metadata map[string]any
}

type Policy interface {
	Decide(ctx context.Context, req PolicyRequest) (PolicyDecision, error)
}
```

规则：

- policy deny 必须是结构化错误，`errors.Is(err, ErrPolicyDenied)` 可匹配，`errors.As` 可取得 decision 和 request；
- policy review 走 `InterruptApproval` + interrupt/resume，不阻塞 middleware；
- policy request 和 decision 必须产生 `PolicyRequested` / `PolicyDecided` 事件；当前第一片已覆盖 model/tool policy middleware、A2A send adapter、root `PolicyChannel`、MCP manager/client policy wrapper、skill registry/resource/script policy wrapper、trace exporter policy wrapper，以及 memory/sandbox/artifact 的 policy wrapper。model/tool/A2A 会通过 `ModelResponse.Events` / `ToolResult.Events` 向上层传递；memory/sandbox/artifact/skill/MCP/channel/exporter wrapper 支持可选 event sink，用于宿主把 policy events 接回统一 event stream。tool registry 在 policy review 这类错误路径上也必须保留 middleware 已产生的事件，便于 template 把审批原因投射到 event stream；
- model I/O 可以通过 `RedactModelRequest` / `RedactModelResponse` / `ModelIORedactionMiddleware` 在 model middleware 边界脱敏；tool result 可以通过 `RedactToolResult` / `ToolResultRedactionMiddleware` 在 tool middleware 边界脱敏；event redaction middleware 必须写入 `Event.Redaction`，外部 sink 可以据此拒收未脱敏事件；
- model rate limit 可以通过 `ModelRateLimiter` / `ModelRateLimitMiddleware` 在 provider call 前等待宿主注入 limiter；SDK 不内置令牌桶、队列或配置读取，生产算法和跨 provider 策略应由宿主或 adapter 提供；
- policy plugin 可以增强决策，但不能绕过基础 policy contract。

## 兼容性规则

- 所有可持久化契约必须带 schema version 或由外层 record 带 version。
- 新字段默认可选；删除字段需要 major version。
- `Metadata` 不用于核心语义判断。
- `Err` 不直接序列化，持久化时保存错误类型、message、wrapped chain 摘要和 provider raw code。
- JSON 是默认交换格式；Go 类型可以有更强类型约束，但不能破坏 JSON 表达能力。

## M1 验收

M1 完成时必须有：

- `RuntimeIDs`；
- block-friendly `Message` / `ContentPart`；
- 结构化 `Event`；
- `SurfaceMessage` 和基础 `SurfaceAction`；
- `Transfer`、`ChannelPayload`、`ChannelEvent` 的第一片 root 契约；
- `StepSnapshot` / `StepExport` 草案类型或等价接口；
- `CheckpointRecord` 草案类型或等价接口；
- `InterruptRecord` / `ResumeRequest` 草案类型或等价接口；
- `ArtifactRef`；
- `PolicyDecision`；
- 不依赖真实模型的 fixture builder；
- event assertion helper 能断言 `RunID`、`ThreadID`、`CallID` 和事件顺序。
