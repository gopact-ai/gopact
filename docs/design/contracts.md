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
| 运行记录 | `Event`、`EventPayload`、`ModelRoute`、`Usage` | 可观察、可评估、可导出的轨迹 |
| 对外展示 | `SurfaceMessage`、`SurfacePart`、`SurfaceAction` | 面向 TUI、Lark、Web、A2UI、AG-UI 的统一输出语义 |
| 恢复边界 | `CheckpointRecord`、`InterruptRecord`、`ResumeRequest` | checkpoint、interrupt、resume、cancel-safe point |
| 外部产物 | `Artifact`、`ArtifactRef` | 文件、图片、报告、大 payload、跨系统输出 |
| 治理决策 | `PolicyRequest`、`PolicyDecision` | provider、tool、sandbox、memory、MCP、A2A、artifact export 的授权结果 |

`Artifact` 和 `Policy` 是基础契约。它们有默认实现和可替换 adapter，但不归入 provider/tool/sandbox/memory/skill/MCP/A2A 这类业务运行时模块。

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

## ModelRequest 和 ModelRoute

`ModelRequest` 只表达模型调用意图，不表达具体 provider API。

```go
type ModelRequest struct {
	IDs            RuntimeIDs
	Messages       []Message
	Tools          []ToolSpec
	ResponseSchema JSONSchema
	RouteHint      string
	Capabilities   []Capability
	Budget         Budget
	Metadata       map[string]any
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
	CreatedAt   time.Time
}
```

规则：

- `SurfaceMessage` 从 event stream 投影得到，不直接改变 runtime state；
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
	Step          int
	Node          string
	State         []byte
	StateCodec    string
	Queue         []string
	Pending       *InterruptRecord
	ConfigVersion string
	CreatedAt     time.Time
}
```

详细恢复协议见 [checkpoint-resume.md](checkpoint-resume.md)。

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
- 外部 export 必须走 policy；
- 事件和 checkpoint 默认只保存 `ArtifactRef`，不保存完整 bytes；
- 文件路径不能作为长期稳定引用，必须转换成 store 管理的 ref。

## PolicyDecision

```go
type PolicyDecision struct {
	Allowed bool
	Reason  string
	Scope   string
	Redact  []RedactionRule
	RequireApproval bool
	Metadata map[string]any
}
```

规则：

- policy deny 必须是结构化错误；
- approval 走 interrupt/resume，不阻塞 middleware；
- policy request 和 decision 必须产生事件；
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
- `CheckpointRecord` 草案类型或等价接口；
- `ArtifactRef`；
- `PolicyDecision`；
- 不依赖真实模型的 fixture builder；
- event assertion helper 能断言 `RunID`、`ThreadID`、`CallID` 和事件顺序。
