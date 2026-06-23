# gopact Channel 与 Transfer 设计

日期：2026-06-23

设计入口：[index.md](index.md)

Channel 层负责把 agent 的统一输出投递到具体交互位置，例如 TUI、Lark bot、Web、A2UI renderer、AG-UI client、Slack、Discord 或企业内部 IM。它不改变 agent runtime，只负责展示、投递和把用户动作带回 TurnLoop。

## 核心观点

Agent 对外只输出统一的运行时事件和 surface message。不同平台通过 transfer 转换为目标格式：

```text
Event Stream
  -> Surface Projector
  -> SurfaceMessage
  -> Transfer
  -> Channel Adapter
  -> TUI / Lark Bot / Web / A2UI / AG-UI / ...
```

这样做的好处：

- agent 不感知 Lark、TUI、Web、A2UI 等展示平台；
- tool call、artifact、interrupt、approval、streaming text 的外部格式统一；
- 新接入一个展示位置时，只新增 transfer/channel plugin；
- 同一段运行轨迹可以 fan-out 到多个 channel；
- 用户动作统一回到 TurnLoop 的 input/resume/action 边界。

## 概念分层

| 概念 | 负责什么 | 示例 |
| --- | --- | --- |
| Event | runtime 事实记录 | `ModelStreamDelta`、`ToolCalled`、`InterruptRaised` |
| SurfaceMessage | 面向用户展示的统一语义 | 文本增量、工具卡片、审批请求、artifact preview |
| Transfer | 把 `SurfaceMessage` 转成目标 payload | Lark card、TUI line、A2UI message、SSE event |
| Channel Adapter | 投递 payload 并接收用户动作 | Lark bot、terminal TUI、WebSocket/SSE server |
| Channel Plugin | 注册 transfer、channel、事件订阅和生命周期 | `plugins/channel/larkbot`、`plugins/channel/tui` |

`SurfaceMessage` 是 gopact 自己的统一输出格式。A2UI、AG-UI、Lark card、TUI rendering 都是它的下游格式，不是 core runtime 的内部格式。

## SurfaceMessage

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

基础类型：

| 类型 | 含义 |
| --- | --- |
| `text_delta` | 模型流式文本 |
| `message` | 完整 assistant/user/system 展示消息 |
| `tool_call` | 工具调用状态 |
| `tool_result` | 工具结果摘要 |
| `artifact` | 文件、图片、报告等产物预览 |
| `approval` | 需要人工批准 |
| `selection` | 需要用户选择 |
| `status` | run/node/task 状态 |
| `error` | 可展示错误 |

规则：

- `SurfaceMessage` 只包含展示语义，不包含平台 payload；
- 大内容必须通过 `ArtifactRef`；
- action 必须带 `RunID`、`ThreadID`、`CallID` 或 `InterruptID`；
- `SourceEvent` 记录来源事件，便于 replay 和调试；
- 同一 `SurfaceMessage` 可以被多个 transfer 转换。

## Transfer

```go
type Transfer interface {
	Name() string
	Supports(target ChannelTarget) bool
	Convert(ctx context.Context, msg SurfaceMessage) (ChannelPayload, error)
}
```

Transfer 规则：

- transfer 是纯转换层，默认不做网络投递；
- transfer 不能读取 Runner 内部状态；
- transfer 可以使用 artifact store 生成预览或下载链接，但必须经过 policy；
- transfer 失败不改变业务运行结果，但要产生 channel 诊断事件；
- transfer 应该有 snapshot tests，保证同一 `SurfaceMessage` 转换稳定。

示例映射：

| SurfaceMessage | TUI transfer | Lark transfer | A2UI transfer |
| --- | --- | --- | --- |
| `text_delta` | append line/token | update message/card text | `updateDataModel` |
| `tool_call` | compact status line | card module | component update |
| `artifact` | path/link preview | file/image card | surface component |
| `approval` | prompt + keyboard action | interactive card button | action component |
| `error` | stderr style line | error card | error surface |

## Channel Adapter

```go
type Channel interface {
	Name() string
	Send(ctx context.Context, payload ChannelPayload) error
	Events(ctx context.Context) iter.Seq2[ChannelEvent, error]
	Close(ctx context.Context) error
}
```

Channel 规则：

- channel 负责投递、更新、删除和接收用户动作；
- channel 不解释 graph state；
- inbound action 必须转换成 `TurnLoop.Push`、`ResumeRequest` 或受控 `ActionRequest`；
- channel 必须处理目标平台的 rate limit、message id、thread id、retry；
- channel 失败默认不改变 agent run 结果，但 strict 模式可以让投递失败返回错误。

## Plugin 接入

Channel 推荐通过 plugin 接入：

```text
channel plugin setup
  -> register transfer
  -> register channel
  -> subscribe event stream
  -> project events into SurfaceMessage
  -> transfer SurfaceMessage into ChannelPayload
  -> send payload
  -> receive channel action
  -> push/resume TurnLoop
```

插件边界：

- 可以订阅事件；
- 可以注册 event middleware 做 redaction/enrichment；
- 可以管理 channel 连接生命周期；
- 不能修改 graph 分支；
- 不能绕过 policy；
- 不能把平台 action 直接变成 tool call。

## Lark Bot

Lark bot 是一个 channel adapter，不是 agent runtime 模块。

职责：

- 把 `SurfaceMessage` 转成 Lark 文本消息、交互卡片、文件消息或图片消息；
- 把 Lark 用户点击、回复、审批动作转成 `ChannelEvent`；
- 把 `ChannelEvent` 转成 TurnLoop input/resume/action；
- 维护 Lark message id、chat id、open id 和 run/thread/session 的映射。

安全规则：

- Lark 用户身份必须映射到 `UserID`；
- chat id 可映射到 `SessionID`；
- Lark card action 必须携带签名或 nonce；
- artifact export 到 Lark 前必须经过 policy；
- Lark 消息内容进入 memory 前必须经过 scope 和 redaction。

## TUI

TUI 是本地 channel adapter。

职责：

- 把 `SurfaceMessage` 转成 terminal line、status bar、progress、picker、approval prompt；
- 支持键盘输入、选择、取消和 resume；
- 在本地开发时可作为默认调试 channel。

规则：

- TUI 不需要网络；
- TUI action 仍然必须走 TurnLoop input/resume/action；
- TUI 展示不等同于 event source of truth，event stream 才是事实记录。

## A2UI / AG-UI / Web

A2UI、AG-UI、SSE、WebSocket 属于 channel/transfer 组合：

- A2UI transfer：`SurfaceMessage` -> A2UI JSON messages；
- AG-UI transfer：`SurfaceMessage` -> AG-UI events；
- SSE channel：投递 JSONL/SSE；
- WebSocket channel：双向投递和 action 接收。

A2UI 的声明式 UI 安全边界仍然保留：agent 只输出数据和组件意图，不输出可执行前端代码。

## 事件

Channel 层至少产生：

- `SurfaceMessageProjected`
- `ChannelTransferStarted`
- `ChannelTransferCompleted`
- `ChannelTransferFailed`
- `ChannelSendStarted`
- `ChannelSendCompleted`
- `ChannelSendFailed`
- `ChannelActionReceived`
- `ChannelActionRejected`

这些事件必须包含 `RunID`、`ThreadID`、`SessionID`、`Channel`、`Transfer`、目标平台 message id 或 action id。

## 注入示例

```go
runner, err := gopact.NewRunner(
	graph,
	gopact.WithTransfers(
		tui.NewTransfer(),
		lark.NewCardTransfer(),
		a2ui.NewTransfer(),
	),
	gopact.WithChannels(
		tui.NewChannel(),
		lark.NewBotChannel(lark.BotOptions{
			AppID:     appConfig.LarkAppID,
			AppSecret: appSecrets.LarkAppSecret,
		}),
	),
)
```

## 测试要求

- 同一个 `SurfaceMessage` 可以转换成 TUI、Lark、A2UI payload；
- transfer snapshot 稳定；
- Lark action 转成 `ResumeRequest`；
- TUI cancel 转成 TurnLoop cancel；
- channel send failure 产生事件；
- artifact export 到 channel 前经过 policy；
- redaction 早于 transfer；
- channel plugin close 时释放连接。
