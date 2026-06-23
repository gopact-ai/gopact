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
| run | `RunStarted`、`RunCompleted`、`RunFailed`、`RunCanceled` |
| turn loop | `TurnInputReceived`、`TurnPreemptRequested`、`TurnInputMerged` |
| graph/node | `NodeStarted`、`NodeCompleted`、`NodeFailed`、`NodeSkipped` |
| model | `ModelRoutePlanned`、`ModelRequested`、`ModelStreamDelta`、`ModelResponded`、`ModelFailed` |
| provider routing | `ModelProviderAttemptStarted`、`ModelProviderRetryDecided`、`ModelProviderFallbackStarted` |
| tool | `ToolVisibleListed`、`ToolCalled`、`ToolReturned`、`ToolFailed`、`ToolPromoted` |
| checkpoint | `CheckpointWriting`、`CheckpointWritten`、`CheckpointFailed`、`CheckpointLoaded` |
| interrupt/resume | `InterruptRaised`、`RunInterrupted`、`ResumeReceived`、`NodeResumed` |
| sandbox | `SandboxCreated`、`SandboxExecStarted`、`SandboxExecCompleted`、`SandboxClosed` |
| memory | `MemoryPut`、`MemorySearched`、`MemoryDeleted` |
| skill | `SkillActivated`、`SkillLoaded`、`SkillResourceRead`、`SkillScriptCompleted` |
| MCP | `MCPServerConnected`、`MCPToolsListed`、`MCPToolCalled`、`MCPResourceRead` |
| A2A | `A2ATaskStarted`、`A2ATaskStatusUpdated`、`A2AArtifactUpdated`、`A2ATaskCompleted` |
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

规则：

- `RunStarted` 必须是某个 `RunID` 的第一条运行事件；
- `RunCompleted`、`RunFailed`、`RunCanceled` 是 terminal events；
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

## OTel / LangSmith 映射

| gopact 事件 | OTel / LangSmith |
| --- | --- |
| `RunStarted` / terminal event | root span |
| `NodeStarted` / `NodeCompleted` | node span |
| `ModelRequested` / `ModelResponded` | LLM span |
| `ToolCalled` / `ToolReturned` | tool span |
| `CheckpointWritten` | span event |
| `InterruptRaised` / `ResumeReceived` | span event |
| `RunFailed` / `NodeFailed` | span status + error event |

大 payload 用 artifact attachment 或 hash，不塞进 span attribute。

## Channel / transfer 映射

Channel 层只能消费事件和 `SurfaceMessage`：

- `ModelStreamDelta` -> `SurfaceMessage{text_delta}` -> TUI line / Lark text update / A2UI data update；
- `ToolCalled` / `ToolReturned` -> `SurfaceMessage{tool_call/tool_result}` -> tool chip/card/status；
- `InterruptRaised` -> `SurfaceMessage{approval/selection}` -> approval prompt/card/action component；
- `ArtifactPut` -> `SurfaceMessage{artifact}` -> file/image/report preview；
- terminal events -> `SurfaceMessage{status}`；
- `ResumeReceived` -> `SurfaceMessage{status}` 或 action acknowledgement。

A2UI、AG-UI、SSE、WebSocket、TUI、Lark bot、飞书卡片都必须遵守同一个事件输入和 `SurfaceMessage` 边界。

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
- 断言 checkpoint event 在 interrupt 前出现。
