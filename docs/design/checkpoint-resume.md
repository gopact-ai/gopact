# gopact Checkpoint 与 Resume 设计

日期：2026-06-23

设计入口：[index.md](index.md)

Checkpoint 是恢复语义，不只是状态持久化。它必须能解释一个 run 停在什么位置、为什么停、恢复时该继续什么，以及哪些副作用不能重复执行。

## 状态边界

| 名称 | 生命周期 | 用途 |
| --- | --- | --- |
| run state | 单次 run 内 | graph node 间传递的业务状态 |
| checkpoint | thread-scoped | resume、time travel、HITL、cancel-safe point |
| memory | cross-thread/session | 长期召回知识 |
| artifact | 可跨系统 | 文件、图片、报告、大 payload |
| telemetry | 执行观察 | trace、metrics、debug、evaluation |

Memory 不能替代 checkpoint。Artifact 不能替代 checkpoint。Telemetry 不能作为恢复来源。

## CheckpointRecord

```go
type CheckpointRecord struct {
	ID             string
	SchemaVersion  string
	IDs            RuntimeIDs
	Step           int
	Node           string
	State          []byte
	StateCodec     string
	Queue          []string
	Pending        *InterruptRecord
	Effects        []EffectRecord
	ConfigVersion  string
	CreatedAt      time.Time
}
```

字段规则：

- `ID` 在 checkpoint store 内唯一；
- `StateCodec` 标识 state 序列化方式，例如 `json`、`gob`、custom codec；
- `Queue` 保存后续待执行 node 或 TurnLoop 待处理输入摘要；
- `Pending` 只在 interrupt / approval / elicitation 等暂停点存在；
- `Effects` 记录已经完成且不能重复执行的外部副作用；
- `ConfigVersion` 用于重放时定位 provider/tool/sandbox/policy 配置。

## 写入时机

必须写 checkpoint：

- node 成功完成后；
- interrupt 返回给 caller 前；
- cancel 到达 safe point 时；
- tool approval 进入 pending 状态前；
- A2A task 或 sandbox job 产生可恢复 artifact 后。

不应该写 checkpoint：

- node 执行中间的任意临时状态；
- model streaming delta 的每个 token；
- policy deny 后没有状态变化的失败；
- event exporter 失败。

## InterruptRecord

```go
type InterruptRecord struct {
	ID          string
	Type        InterruptType
	Reason      string
	Prompt      Message
	RequiredBy  string
	ResumeSchema JSONSchema
	CreatedAt   time.Time
}
```

常见类型：

- `approval`：需要人工批准；
- `input`：需要用户补充信息；
- `selection`：需要用户从候选项选择；
- `external_wait`：等待外部系统回调。

Interrupt 不是 error。它是 run 的暂停状态。

## ResumeRequest

```go
type ResumeRequest struct {
	CheckpointID string
	InterruptID  string
	IDs          RuntimeIDs
	Payload      []byte
	PayloadCodec string
	CreatedAt    time.Time
}
```

规则：

- `CheckpointID` 和 `InterruptID` 必须匹配；
- resume payload 必须通过 `ResumeSchema` 校验；
- resume 后必须产生 `ResumeReceived` 和 `NodeResumed`；
- resume 不能绕过 policy；
- resume 后如果配置版本变化，事件必须记录 replay/config drift。

## Cancel 与 interrupt

Cancel 是外部控制面要求停止当前 run。Interrupt 是业务流程主动暂停等待恢复输入。

Cancel 规则：

- 通过 `context.Context` 和 runner control plane 传播；
- 到 safe point 后写 checkpoint；
- 超时可以升级为 hard cancel；
- hard cancel 不能保证 checkpoint 完整，必须产生诊断事件。

Interrupt 规则：

- 不使用 context cancel 表达；
- 必须有 pending record；
- 必须能通过 resume payload 继续；
- 不应该被当成 `RunFailed`。

## 副作用和幂等

外部副作用包括：

- tool 执行；
- sandbox 文件写入；
- MCP tool call；
- A2A task send/cancel；
- artifact export；
- memory put/delete。

规则：

- 副作用执行前必须有 `CallID`；
- 非幂等副作用完成后必须记录 `EffectRecord`；
- resume 时不得重复执行已完成的非幂等副作用；
- tool 可以声明 idempotency key；
- provider retry 不能导致 tool 副作用重放。

## TurnLoop 输入队列

TurnLoop 必须区分三类输入：

| 输入 | 含义 |
| --- | --- |
| interrupted input | 触发当前 interrupt 的原始输入 |
| pending input | run 中断或取消期间收到、尚未处理的输入 |
| resume/new input | 恢复动作本身或恢复后的新输入 |

preempt 语义：

- 高优先级输入可以 cancel 当前 run；
- cancel 到 safe point 后，TurnLoop 合并 pending input；
- 合并规则必须产生事件；
- Runner 不维护长期输入队列。

## Store contract

```go
type Store interface {
	Put(ctx context.Context, record CheckpointRecord) error
	Get(ctx context.Context, id string) (CheckpointRecord, error)
	Latest(ctx context.Context, threadID string) (CheckpointRecord, bool, error)
	List(ctx context.Context, threadID string) ([]CheckpointRecord, error)
}
```

规则：

- store 必须按 `ThreadID` 查询；
- store 必须保留 `RunID`，方便区分多次尝试；
- store 写入失败默认终止 run；
- store adapter 可以加密 state，但不能改变 record 语义；
- checkpoint schema 变更需要 migration 或显式拒绝恢复。

## 测试要求

- node 成功后写 checkpoint；
- interrupt 前写 checkpoint；
- checkpoint 失败导致 run error；
- resume payload schema 不匹配时拒绝恢复；
- cancel safe point 写 checkpoint；
- 非幂等 tool resume 不重复执行；
- config drift 产生事件。
