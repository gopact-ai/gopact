# gopact Channel 与 Transfer 设计

<!-- gopact:doc-language: zh -->

[英文文档](./channels.md)

## 中文

日期：2026-06-23

设计入口：[index.md](index.md)

Channel 层负责把 agent 的统一输出投递到具体交互位置，例如 TUI、Lark bot、Web、A2UI host/renderer、AG-UI client、Slack、Discord 或企业内部 IM。它不改变 agent runtime，只负责展示、投递和把用户动作带回 TurnLoop。

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

## 当前实现状态

当前 root package 已完成第一片基础契约：

- `SurfaceMessage`、`SurfacePart`、`SurfaceAction`、`ChannelPayload`、`ChannelEvent`；
- `ProjectSurfaceMessages(event)`：把 runtime event 机械投影成一个或多个 platform-neutral surface message；
- `Transfer` / `TransferFunc`：把 `SurfaceMessage` 纯转换成目标 payload；
- `Channel` / `ChannelFunc`：投递 `ChannelPayload`，接收 `ChannelEvent`，并提供 close 生命周期；
- `PolicyChannel`：对 outbound send 和 inbound channel event 做 `PolicyBoundaryChannel` 检查，支持 deny/review、policy events 和 approval interrupt；
- channel 诊断事件常量：`surface_message_projected`、`channel_transfer_*`、`channel_send_*`、`channel_action_*`；
- `ChannelEvent.ResumeRequest()`：把 channel action 转回 root `ResumeRequest`。

当前 `adapters/channel/tui` 已提供 writer-based TUI transfer/channel 第一片，可把 `SurfaceMessage` 转成 TUI payload 并写入任意 `io.Writer`。`adapters/channel/sse` 已提供 HTTP SSE transfer/channel 第一片：把 `SurfaceMessage` 转成 SSE payload，通过 `http.Handler` 对外广播，并把 HTTP action POST 转成 `ChannelEvent`。`github.com/gopact-ai/gopact-adapters-channel/lark` 已提供 host-injected Lark transfer/channel/callback source 第一片：把 `SurfaceMessage` 转成 Lark text/interactive payload，通过宿主注入的 sender 投递，处理 URL verification 和卡片 action callback，并把 Lark action value 转回 `ChannelEvent` / `ResumeRequest`；raw callback 签名和 action 防伪通过 `CallbackVerifier` / `ActionVerifier` 由宿主注入。`github.com/gopact-ai/gopact-adapters-channel/a2ui` 已提供 A2UI v0.9 JSON message transfer/JSONL channel/history replay/schema catalog validation、复用 root `ValidateJSONSchemaValue` 且可通过 `ValidatorConfig.SchemaValidator` 注入完整 engine 的 component JSON Schema validation、client-supported catalog negotiation 和 in-memory reference renderer 第一片：输出 `createSurface`、`updateComponents`、`updateDataModel` 消息，把 message 逐条写成 JSONL，记录成功投递的 message snapshot 供重连 replay，用本地 catalog registry 校验 surface catalog、组件名和 child 引用，可按 host 注入的 registry 与 renderer supported catalog ID 顺序选择首个匹配 catalog，并在协商未命中时回退到显式配置或默认 catalog，可把消息物化成 surface snapshot，并把 renderer action context 转回 `ChannelEvent` / `ResumeRequest`。`github.com/gopact-ai/gopact-adapters-channel/agui` 已提供 AG-UI event transfer 与 HTTP SSE channel 第一片：把 `SurfaceMessage` 转成 `RUN_STARTED`、`TEXT_MESSAGE_*`、`CUSTOM`、`RUN_ERROR`、`RUN_FINISHED` 等 AG-UI event sequence，通过 `StreamHandler` 逐帧广播，并通过 `ActionHandler` 把 HTTP action POST 转回 `ChannelEvent` / `ResumeRequest`。`github.com/gopact-ai/gopact-templates-devagent/channelreview` 已复用同一 `SurfaceMessage` / `Transfer` / `ChannelEvent` 边界，可先投递 Dev Agent approval prompt，再把 Lark/TUI/SSE/CI 等外部 approve/reject action 转成 Dev Agent `ReviewDecision`，供 release gate 消费。尚未完成的是完整前端 renderer、完整 JSON Schema engine adapter/plugin 与 conformance 深化、更完整 catalog negotiation 深化、AG-UI WebSocket/plugin 等生产级 adapter，以及 Lark 真实 client/OAuth/plugin host、nonce 防重放存储、用户身份映射、redaction、artifact export 的完整生产级接入。

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
	Metadata    map[string]any
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

| SurfaceMessage | TUI transfer | Lark transfer | A2UI transfer | AG-UI transfer |
| --- | --- | --- | --- | --- |
| `text_delta` | append line/token | update message/card text | `updateDataModel` | `TEXT_MESSAGE_CONTENT` |
| `message` | print line/block | text/card message | `createSurface` / `updateComponents` | `TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` / `TEXT_MESSAGE_END` |
| `tool_call` | compact status line | card module | component update | `CUSTOM`，后续深化为 `TOOL_CALL_*` |
| `artifact` | path/link preview | file/image card | surface component | `CUSTOM` |
| `approval` | prompt + keyboard action | interactive card button | action component | `CUSTOM` |
| `error` | stderr style line | error card | error surface | `RUN_ERROR` |

## Channel Adapter

```go
type Channel interface {
	Name() string
	Send(ctx context.Context, payload ChannelPayload) error
	Events(ctx context.Context) iter.Seq2[ChannelEvent, error]
	Close(ctx context.Context) error
}
```

`ChannelFunc` 是 root 包提供的轻量适配器，用于测试、示例和宿主应用快速注入 channel 行为。真实平台 adapter 应该在 `adapters/channel/*` 或 plugin package 内实现 `Channel`。

Channel 规则：

- channel 负责投递、更新、删除和接收用户动作；
- channel 不解释 graph state；
- inbound action 必须转换成 `TurnLoop.Push`、`ResumeRequest` 或受控 `ActionRequest`；
- inbound action 进入 runtime 前必须先经过 channel policy；
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

当前第一片实现位于 `github.com/gopact-ai/gopact-adapters-channel/lark`：

- `lark.Transfer`：把 `SurfaceMessage` 转成 text 或 interactive card payload；
- `lark.Channel`：只依赖宿主注入的 `Sender`，不创建 Lark client、不读取配置文件、不管理 OAuth；
- `lark.ActionValue` / `ChannelEventFromActionValue`：把 card action value 转回 `ChannelEvent`，再由 `ChannelEvent.ResumeRequest()` 回到 TurnLoop；
- `lark.CallbackSource`：作为 `http.Handler` 处理 URL verification 和卡片 action callback，并通过 `Events(ctx)` 暴露统一 `ChannelEvent`；
- `WithActionSigner`：允许宿主给 outbound action value 写入签名或 nonce；
- `WithCallbackVerifier` / `WithActionVerifier`：允许宿主接入 raw callback 签名校验、nonce 防重放和 action 防伪校验；SDK 不持有密钥和配置。

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

- A2UI transfer/channel：当前 `github.com/gopact-ai/gopact-adapters-channel/a2ui` 已支持 `SurfaceMessage` -> A2UI v0.9 JSON messages，包含 surface 创建、组件更新、data model 更新、JSONL 投递、history replay、local catalog registry、structural validation、复用 root `ValidateJSONSchemaValue` 且可通过 `ValidatorConfig.SchemaValidator` 注入完整 engine 的 component JSON Schema validation、client-supported catalog negotiation、in-memory reference renderer 和 action decode；
- AG-UI transfer/channel：当前 `github.com/gopact-ai/gopact-adapters-channel/agui` 已支持 `SurfaceMessage` -> AG-UI event sequence，覆盖 run lifecycle、完整 message、text delta、error 和 custom surface event，并提供 HTTP SSE event stream 与 action POST 回流第一片；
- SSE channel：当前 `adapters/channel/sse` 已提供第一片，负责 HTTP SSE 广播和 action POST 回流；
- WebSocket channel：双向投递和 action 接收。

A2UI 的声明式 UI 安全边界仍然保留：agent 只输出数据和组件意图，不输出可执行前端代码。当前 A2UI 第一片负责 transfer、JSONL channel、history replay、local catalog registry、structural validation、复用 root `ValidateJSONSchemaValue` 且可通过 `ValidatorConfig.SchemaValidator` 注入完整 engine 的 component JSON Schema validation、client-supported catalog negotiation、in-memory reference renderer 与 action decode；完整 JSON Schema engine adapter/plugin 与 conformance 深化、真实前端 renderer、更完整 catalog negotiation 深化、A2A transport 封装或 v1.0 `actionResponse` 语义仍是后续项。

当前 root portable JSON Schema subset 覆盖 SDK 边界常见结构约束：`type`、`required`、`properties`、`additionalProperties`、`const`、`enum`、`items`、`minLength`、`maxLength`、`minimum`、`maximum`、`exclusiveMinimum`、`exclusiveMaximum`、`multipleOf`、`minItems`、`maxItems` 和 `pattern`。A2UI component catalog validation 默认复用这套 root 原语约束 component payload；需要完整 JSON Schema 2020-12 时，宿主通过 `gopact.JSONSchemaValidator` 注入 adapter/plugin 实现，core 不绑定重依赖；外部 adapter 可复用 `gopacttest` 的 portable JSON Schema validator conformance helper 做兼容性测试。

## SSE

SSE 是 Web/调试场景的轻量 channel adapter。

职责：

- 把 `SurfaceMessage` 转成稳定 JSON payload，并以 Server-Sent Events frame 广播给订阅者；
- 暴露 action POST handler，把浏览器或宿主 UI 的按钮/输入动作转成 `ChannelEvent`；
- 保持与 TurnLoop 的边界一致：action 只回到 input/resume/action 入口，不直接调用 graph/node；
- 作为后续 WebSocket、A2UI 真实 renderer 和 AG-UI plugin 化接入的 HTTP 参考。

当前第一片不包含认证、session store、跨进程 fan-out、历史消息 replay 或目标平台 message id 持久化；这些属于生产级 channel plugin 的职责。

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
tuiChannel, err := tui.NewChannel(os.Stdout)
if err != nil {
	return err
}

runner, err := gopact.NewRunner(
	graph,
	gopact.WithTransfers(
		tui.NewTransfer(),
		lark.NewTransfer(),
		a2ui.NewTransfer(),
	),
	gopact.WithChannels(
		tuiChannel,
		larkChannel,
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
