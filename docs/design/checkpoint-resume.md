# gopact Checkpoint 与 Resume 设计

日期：2026-06-23

设计入口：[index.md](index.md)

Checkpoint 是恢复语义，不只是状态持久化。它必须能解释一个 run 停在什么位置、为什么停、恢复时该继续什么，以及哪些副作用不能重复执行。

`gopact` 的恢复能力以 step 为最小稳定边界。Workflow/graph 不承诺捕获 node 内部任意半执行状态，但必须保证每个完成或暂停的 step 都可以导出、导入和恢复。

## 当前实现状态

当前代码已经支持四段恢复切片：

- `graph.WithStepExport` 可以导入一个 `Phase=StepCompleted` 的 `StepExport`，用 snapshot `Output` 作为恢复 state，并从该 node 的后继节点继续执行。
- `graph.WithStepExport` + `graph.WithResumeRequest` 可以导入一个 `Phase=StepInterrupted` 的 `StepExport`，校验 pending interrupt id，用 snapshot `Output` 和 `Queue` 继续执行。如果没有 resume request，graph 会发出 `EventRunInterrupted`，不会把它当作 `RunFailed`。
- `graph.WithStepExport` 可以导入一个 `Phase=StepCanceled` 的 `StepExport`，用 snapshot `Output` 和 `Queue` 从 cancel safe point 继续执行。node 返回 `context.Canceled` 时，graph 会产生 `StepCanceled` snapshot 和 `EventRunCanceled`，不会降级为 `RunFailed`。
- `graph.WithCheckpointLoader` 可以按 `ThreadID` 加载 latest checkpoint，用 checkpoint 的 `State` 和 `Queue` 恢复执行；`graph.WithCheckpointStore` 则把写入和 latest load 组合成一个 SDK option。checkpoint store 恢复路径会产生 `EventCheckpointLoaded`；如果 interrupted checkpoint 携带匹配的 `ResumeRequest`，还会产生 `EventResumeReceived`。

这些路径都会保留 step 编号和 `RuntimeIDs`，因此恢复后的第一个节点会从下一个 step 开始。导入成功后会产生 `EventStepImported`；checkpoint load 成功后会产生 `EventCheckpointLoaded`；带 resume request 的 interrupted import/load 会产生 `EventResumeReceived`；恢复后的第一个实际 node 会产生 `EventNodeResumed`。

当前 graph checkpoint 写入已经包含 `Phase`、`RuntimeIDs`、`Queue`、`Pending` 和 `Effects` 第一版：node completed 后写入后续执行队列，interrupt 返回 caller 前写入 pending interrupt 和 queue，node 返回 `context.Canceled` 时写入 cancel safe point 和 queue。Root `RunOption` 已提供 `WithStepExport` / `WithResumeRequest`，graph runnable adapter 会把这些 root option 转成 graph invoke option。ReAct template 在 tool policy review 时也会把审批暂停映射成 `EventInterrupted` / `EventRunInterrupted` 和带 pending approval 的 interrupted `StepSnapshot`，并能从 interrupted tool `StepExport` + `ResumeRequest` 恢复原 tool call；如果同一批 tool calls 已经有部分 tool result 写入 interrupted snapshot output，恢复时会跳过已完成 call，只执行尚未完成的 call。ReAct 也已支持从 completed `call_model` `StepExport` 恢复 pending tool calls，以及从 completed `call_tool` `StepExport` 继续下一次 model call；`react.WithCheckpointStore` 复用 `graph.CheckpointStore[react.State]`，可以在 completed model/tool step 后写入 checkpoint，并在下一次 run 按 `ThreadID` load latest checkpoint 后产生 `CheckpointLoaded` 和 `NodeResumed`；对 tool approval 产生的 interrupted checkpoint，携带匹配 `ResumeRequest.CheckpointID` / `InterruptID` 后会产生 `CheckpointLoaded`、`ResumeReceived`、`NodeResumed`，并继续原 tool call。`react.WithArtifactVerifier` 已接入 step import 和 checkpoint load 边界，会在继续执行前校验 `StepSnapshot.Artifacts` 与 `EffectRecord.Artifacts`，失败时产生 `RunFailed` 并阻止恢复节点运行。更完整的生产级 checkpoint 策略仍属于后续 M5。tool 调用副作用也有第一片：tools registry 会在成功 invoke 后产生默认 `tool_call` effect，tool/node middleware 可以追加 effects，graph 会把 `NodeContext.Effects` 复制进 step snapshot，ReAct interrupted tool step 会保留中断前已完成 tool 的 artifacts/effects。`EffectRecord` 已支持 dependency edge、artifact refs、sandbox 操作摘要和 replay policy；`StepSnapshot.Validate` 会检查重复 effect id、非法 replay policy、空 dependency id 和 idempotent 缺 key。`PlanEffectReplay` 会在导入 step/checkpoint 时生成单步 replay/skip plan；`BuildRunEffectGraph` 会从 `RunExport` 的稳定 steps 构建跨 step effect graph；`PlanRunEffectReplay` 会把 run-level graph 转成带 step 身份的 replay/skip/record_only 决策；`EffectReplayRegistry` 会按 effect type/target 分发到宿主注册的 replay handler；`ExecuteEffectReplay` / `ExecuteRunEffectReplay` 会按 plan 调用 replay executor，并为 skip/record_only 产生结果。`tools.NewReplayHandler` 已提供 `tool_call` backend replay handler 第一片：对 idempotent replay 决策，它会读取 effect metadata 中的 `tool_args` 并重新调用 tool registry；如果配置 `tools.WithReplayCommitStore`，则先按 idempotency key 返回已提交结果，未命中才重放并写入 `CommitRecord`。`sandbox.NewReplayHandler` 已提供 sandbox backend replay handler 第一片：对 `sandbox_exec` idempotent replay 决策，它会读取 `EffectRecord.Sandbox.Command` 并通过 sandbox manager 重新执行命令；对 `sandbox_file_read` / `sandbox_file_write` 决策，它会读取 `EffectRecord.Sandbox.Path`，并从 effect metadata 中读取 `sandbox_file_content` / `sandbox_file_mime_type` 完成文件重读或写回；结果 metadata 携带 `sandbox_exec_result`、`sandbox_file` 和 `sandbox_session_id`。`memory.NewReplayHandler` 已提供 memory backend replay handler 第一片：对 `memory_put` 决策读取 `memory` metadata 并调用 memory store 写入，结果 metadata 携带 `memory_id`；对 `memory_delete` 决策读取 `memory_id` metadata 并删除 memory；对 `memory_search` 决策读取 `memory_query` metadata 并返回 `memory_search_result`。`artifact.NewReplayHandler` 已提供 `artifact_write` backend replay handler 第一片：对 idempotent replay 决策，它会读取 `EffectRecord.Artifacts`，校验 artifact payload integrity，并在结果 metadata 返回 `artifact_refs` / `artifact_count`。

checkpoint codec 现在也具备 migration/config drift 第一片：`checkpoint.Record` 和 `graph.Checkpoint` 会携带 `ConfigVersion`；`checkpoint.WithConfigVersion` 可用于 store 写入和读取；`graph.WithConfigVersion` 可用于任意自定义 checkpointer；`DecodeCheckpoint` 支持按旧 record schema 注册 `RecordMigrator`；当 stored/current config version 不一致时默认返回 `ErrConfigDrift`，显式 `ConfigDriftAllow` 时会在 metadata 写入 `checkpoint_config_drift`，并由 `EventCheckpointLoaded` 透传。

artifact payload integrity 也已有第一片：`artifact.Memory` 写入时生成 `ArtifactRef.Size` 和 `ArtifactRef.SHA256`，`artifact.VerifyRef` / `artifact.VerifyRefs` 可以按 ref 重新读取 payload 并校验 size/hash。`graph.WithArtifactVerifier` 和 `react.WithArtifactVerifier` 已把这类 verifier 接入 `StepExport` import 和 checkpoint load 边界：继续执行前会收集 `StepSnapshot.Artifacts` 和 `EffectRecord.Artifacts`，校验失败会产生 `EventRunFailed` 并阻止后续 node 执行。replay handler 侧也已有 `artifact.NewReplayHandler` 第一片；`artifact.RecordVerifyRefs` 会把 artifact refs 校验结果记录为标准 `VerificationCheck`，失败时也会先记录 failed evidence 再返回错误。ReAct template verification node、Dev Agent channel reviewer prompt/bridge adapter、CI reviewer adapter、远端 CI run -> `ci_gate` evidence 桥接、model reviewer adapter prompt/eval metadata governance 和 Lark callback source 已有第一片；更多证据来源、model review 真实评测治理深化、CI provider 拉取/重跑/secret 治理、Lark 真实 client/plugin 和生产级策略后续继续补齐。

尚未实现：

- 更完整的生产级 migration 策略，以及 template verification 节点中的更多证据采集来源、model review 真实评测治理深化、CI provider 拉取/重跑/secret 治理、Lark 真实 client/plugin 和生产 release gate 策略；
- 生产级 tool 幂等提交 ledger 持久化 / exactly-once adapter，以及跨 step 的完整 tool-call graph。

## 状态边界

| 名称 | 生命周期 | 用途 |
| --- | --- | --- |
| run state | 单次 run 内 | graph node 间传递的业务状态 |
| step snapshot | step-scoped | export/import、debug、replay、跨进程恢复 |
| checkpoint | thread-scoped | resume、time travel、HITL、cancel-safe point |
| memory | cross-thread/session | 长期召回知识 |
| artifact | 可跨系统 | 文件、图片、报告、大 payload |
| telemetry | 执行观察 | trace、metrics、debug、evaluation |

Memory 不能替代 checkpoint。Artifact 不能替代 checkpoint。Telemetry 不能作为恢复来源。

Step snapshot 和 checkpoint 的区别：

- step snapshot 是交换格式，强调某个稳定 step 的输入、输出、状态、事件引用和副作用；
- checkpoint 是存储格式，强调某条 thread 时间线的最新可恢复位置；
- checkpoint 可以由 step snapshot 持久化而来，但外部 import/export 不应该依赖具体 checkpoint store。

## StepSnapshot

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
```

`Phase` 至少需要表达：

- `pending`：step 已排队但未执行；
- `running`：node 正在执行；
- `completed`：node 已完成，输出和 state 可恢复；
- `interrupted`：业务主动暂停，等待 resume payload；
- `canceled`：外部取消已到达 safe point；
- `failed`：失败状态已记录，可用于 replay 和失败归因。

导出规则：

- step 完成后必须能导出 snapshot；
- interrupt 返回给 caller 前必须能导出 snapshot；
- cancel 到达 safe point 后必须能导出 snapshot；
- snapshot 的 `Effects` 必须覆盖已完成的非幂等副作用；
- 大 payload 使用 artifact ref，不能把任意大 bytes 塞进 snapshot；
- export 必须带 schema version、config version 和 integrity 信息。

导入规则：

- import 必须校验 schema version、state codec、artifact integrity 和 config drift；当前 graph 第一片已通过 `WithArtifactVerifier` 对 step/checkpoint artifact refs 提供可选校验门；
- import 后的 runner 必须从 snapshot 的 `Queue`、`Pending` 和 `Output` 恢复；后续如果引入 state codec，再把 `Output`/`State` 序列化为稳定格式；
- import 不能重复执行 `Effects` 中已经完成的非幂等副作用；
- step export import 必须产生 `StepImported` 和后续 `NodeResumed`；checkpoint store 恢复路径必须额外产生 `CheckpointLoaded`；
- import 不能绕过 policy，尤其是 artifact、sandbox、MCP、A2A 和 memory 副作用。

## CheckpointRecord

```go
type CheckpointRecord struct {
	ID             string
	SchemaVersion  string
	IDs            RuntimeIDs
	ThreadID       string
	Step           int
	Node           string
	Phase          StepPhase
	State          []byte
	StateCodec     string
	StateHash      string
	Queue          []string
	Pending        *InterruptRecord
	Effects        []EffectRecord
	ConfigVersion  string
	CreatedAt      time.Time
	Metadata       map[string]any
}
```

字段规则：

- `ID` 在 checkpoint store 内唯一；
- `StateCodec` 标识 state 序列化方式，例如 `json`、`gob`、custom codec；
- `StateHash` 是 state payload 的 integrity 摘要，当前内存实现使用 `sha256:<hex>`；
- `Queue` 保存后续待执行 node 或 TurnLoop 待处理输入摘要；
- `Pending` 只在 interrupt / approval / elicitation 等暂停点存在；
- `Effects` 记录已经完成或已观察到的外部副作用，包含 replay policy、dependency edge、artifact refs 和 sandbox 操作摘要；
- `ConfigVersion` 用于重放时定位 provider/tool/sandbox/policy 配置。

当前代码已落地 `checkpoint.Record`、`StateCodec`、默认 `JSONCodec`、统一 `WithCodec` option、`EncodeCheckpoint` 和 `DecodeCheckpoint`。`checkpoint.Memory` 内部存储 encoded record，读取 latest/get 时会先校验 state hash，再解码为 `graph.Checkpoint[S]`。`checkpoint.FileStore` 已提供本地 JSON 文件持久化第一片：单文件存储、原子 rename 写入、跨实例恢复、同 ID 覆盖和读时 integrity 校验。`checkpoint.RowBackend` / `RowStore` 已提供数据库型行存储端口第一片：SDK 只读写稳定 `Record`，真实 SQL/KV/内部表由用户实现 `UpsertRecord`、`GetRecord`、`ListRecords` 注入，`NewMemoryRowBackend` 用于测试和本地开发。`github.com/gopact-ai/gopact-adapters-checkpoint/sqlstore` 已提供 `database/sql` backend 第一片：接收 `*sql.DB`、`*sql.Tx` 或兼容 `DBTX`，提供 SQLite/Postgres/MySQL 默认 upsert/get/list 查询生成，也允许宿主通过 `WithQueries` 注入自己的 SQL。它不读取 SDK 配置文件、不创建表、不执行迁移，连接池和 migration 仍由宿主项目负责。`adapters/checkpoint/redisstore` 已提供 Redis row backend 第一片：只依赖宿主注入的 `GET` / `EVAL` 窄接口，用 Lua 原子写入 record 和 thread index，支持 provider-specific nil matcher，不创建 Redis client、不读取配置文件。`adapters/checkpoint/objectstore` 已提供 conditional object row backend 第一片：宿主注入暴露 `IfAbsent` / `IfVersion` 的对象 client，用 CAS 重试维护 thread index，支持 provider-specific not-found/precondition matcher，并提供 `VerifyIndex` / `RepairIndex` 第一片，用于发现和修复重复索引、缺失 record、串 thread record；已观察到的 `IndexConsistencyReport` 可通过 `RecordIndexConsistencyCheck` 进入标准 verification evidence；它不在普通 put/get/list object backend 上伪造 CAS 或索引一致性。`adapters/checkpoint/s3store` 已提供 AWS SDK v2 S3 conditional checkpoint backend 第一片，把 S3 ETag 映射为 checkpoint object version，并把 `IfNoneMatch` / `IfMatch` 映射为 objectstore CAS precondition。`adapters/checkpoint/gcsstore` 已提供 Google Cloud Storage conditional checkpoint backend 第一片，把 object generation 映射为 checkpoint object version，并把 `DoesNotExist` / `GenerationMatch` 映射为 objectstore CAS precondition。`adapters/checkpoint/r2store` 已提供 Cloudflare R2 S3-compatible conditional checkpoint backend 第一片，宿主注入已经配置 R2 endpoint/credentials/transport 的 S3-compatible client，SDK 只复用 ETag 和 `IfNoneMatch` / `IfMatch` 条件写。`adapters/checkpoint/ossstore` 已提供 Alibaba Cloud OSS SDK v2 conditional checkpoint backend 第一片，宿主注入已经配置 endpoint/credentials/transport 的 OSS v2 client 和 bucket，SDK 只复用 ETag、`x-oss-forbid-overwrite` 和通用 `If-Match` header，不读取 region、credentials 或配置文件。`checkpoint.ObjectBackend` / `ObjectStore` 已提供对象存储第一片：每个 checkpoint record 独立成对象，通过 `WithObjectPrefix` 做 key namespace，`NewMemoryObjectBackend` 用于测试和本地开发；`adapters/storage/fileblob` 已提供 filesystem object backend 第一片，可作为本地持久化和开发环境；`adapters/storage/objectblob` 已提供通用对象 client backend 第一片，支持 adapter prefix、paged list、provider-specific not-found matcher，并可同时服务 checkpoint object 与 TurnLoop blob；`adapters/storage/s3blob` 已提供 AWS SDK v2 S3 object/blob adapter 第一片，宿主注入 `*s3.Client` 兼容窄接口和 bucket，不读取 region、credentials 或配置文件；`adapters/storage/gcsblob` 已提供 Google Cloud Storage object/blob adapter 第一片，宿主注入 `*storage.Client` 兼容窄接口和 bucket，不读取 project、credentials 或配置文件；`adapters/storage/r2blob` 已提供 Cloudflare R2 S3-compatible object/blob adapter 第一片，宿主注入已经配置好的 S3-compatible client，不读取 account id、endpoint、credentials 或配置文件；`adapters/storage/ossblob` 已提供 Alibaba Cloud OSS SDK v2 object/blob adapter 第一片，宿主注入 OSS v2 client 和 bucket，不读取 endpoint、credentials 或配置文件。更严格的生产索引修复审计、list-after-write 风险治理仍由后续 provider adapter 或宿主注入完成，不沉入 SDK 配置。`WithRecordMigration`、`WithConfigVersion`、`WithCurrentConfigVersion` 和 `WithConfigDriftPolicy` 已提供 migration/config drift 第一片。后续生产 adapter 应继续补更多云对象存储 SDK 绑定、索引巡检审计事件、迁移清单治理和配置漂移审批策略。

### SQL backend schema 指南

`sqlstore` 默认查询假设一行对应一个 `checkpoint.Record`，最小列集如下：

| 列 | 语义 |
| --- | --- |
| `id` | checkpoint record 主键，和 `Record.ID` 一致 |
| `schema_version` | record schema version |
| `ids_json` | `RuntimeIDs` JSON |
| `thread_id` | thread 查询键，必须可索引 |
| `step` | step number，用于排序和诊断 |
| `node` | checkpoint 所在 node |
| `phase` | `StepPhase` 字符串 |
| `state` | codec 后的 state payload |
| `state_codec` | codec name |
| `state_hash` | state integrity hash |
| `queue_json` | 后续 node queue JSON |
| `pending_json` | interrupt/resume pending JSON |
| `effects_json` | 已观察副作用 JSON |
| `config_version` | runtime config version |
| `created_at` | RFC3339Nano UTC 字符串或等价可排序时间列 |
| `metadata_json` | record metadata JSON |

生产 schema 至少应保证 `id` 唯一、`thread_id + created_at + step + id` 可高效排序，并由宿主 migration 工具管理表结构。SDK adapter 只负责参数化读写，不把 schema lifecycle 沉入 gopact。

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

必须能导出 step snapshot：

- 每个 node 成功完成后；
- interrupt 或 approval pending 前；
- cancel safe point；
- checkpoint 写入失败前的诊断路径，如果 state 可安全序列化；
- import 后恢复执行前。

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
	Metadata    map[string]any
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

- `CheckpointID` 和 `InterruptID` 必须匹配；
- 如果通过 `StepExport` 恢复，snapshot id、thread id、run id 和 interrupt id 必须匹配恢复请求；
- resume payload 必须通过 `ResumeSchema` 校验；当前 root `JSONSchemaValidator` / `JSONSchemaValidatorFunc` 已提供可插拔 validator 契约，root `ValidateJSONSchemaValue` 已提供默认 portable JSON Schema subset 原语，`ValidateResumePayload` 基于它提供 resume payload gate，覆盖 type、required、properties、enum、const、items、string length、array length、numeric min/max、pattern、exclusive min/max、multipleOf 和 additionalProperties，并已接入 graph interrupted `StepExport` / interrupted checkpoint 两条恢复边界、root `WithJSONSchemaValidator` 运行选项，以及 TurnLoop `Resume` / `Run(WithResume)` 入口 gate 和 `WithTurnJSONSchemaValidator`；`gopacttest` 已提供 portable JSON Schema validator conformance helper，供外部完整 engine adapter 复用同一批 root boundary case；
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

当前已实现的 effect contract：

- `EffectReplayRecordOnly`：默认策略，只记录证据，不允许自动 replay/skip；
- `EffectReplayIdempotent`：必须提供 `IdempotencyKey`，后续 replay engine 可以用它安全重试；
- `EffectReplaySkip`：恢复时可复用已记录结果跳过重复动作，常用于 artifact/cache 结果；
- `DependsOn`：记录同一 step 内 effect dependency edge；
- `Artifacts`：记录 effect 产出的 artifact refs；
- `Sandbox`：记录 sandbox file/exec 操作摘要。

当前第一片 runtime 已实现 `PlanEffectReplay`：它会校验 `StepSnapshot`，按 `DependsOn` 做稳定拓扑排序，并把 effect 转成 `record_only`、`replay` 或 `skip` 决策。`graph.WithStepExport` 和 `graph.WithCheckpointLoader` 在产生 `StepImported` / `CheckpointLoaded` 事件时，会通过 `effect_replay_plan` metadata 暴露这份 plan；调用方应优先用 root `EventEffectReplayPlan(event)` 读取深拷贝，而不是手写 metadata 类型断言。该 helper 同时支持进程内 typed metadata 和 JSON export/import 后的结构化 metadata。

`BuildRunEffectGraph` 是跨 step effect graph 第一片：它会从 `RunExport.Steps` 收集所有稳定 step 的 effects，拒绝重复 effect id、缺失 dependency、未来 dependency 和 cycle，并输出 run-scope nodes、edges 和稳定拓扑顺序。`PlanRunEffectReplay` 在这个图之上生成 run-level replay plan，保留 step id、step number、node 和 effect index，供后续 backend adapter 按拓扑顺序消费。`ExecuteEffectReplay` / `ExecuteRunEffectReplay` 是 executor 第一片：它们只对 `replay` 决策调用外部 `EffectReplayExecutor`，对 `skip` 和 `record_only` 生成本地结果；run-level 结果会保留 step 身份，方便恢复报告、审计和后续 UI 展示。`RecordEffectReplayCheck` 可把已观察 step-level plan/results/error 转成标准 `effect_replay` verification evidence，`EffectReplaySnapshotFromEvent(event, results, err)` 可把 `StepImported` / `CheckpointLoaded` 事件身份与已观察 replay 结果组装成 recorder input；`RecordRunEffectReplayCheck` 可把已观察 run-level plan/results/error 转成标准 `run_effect_replay` verification evidence，metadata 会保留 planned/result step ids 以定位 run-level replay 覆盖的 steps；这些 helper 都不执行 replay。`ToolResult.Commit` 是 tool 幂等提交协议第一片：工具返回 `ToolCommit{IdempotencyKey: ...}` 后，tools registry 会把默认 `tool_call` effect 标记为 idempotent，并记录可重放的 `tool_args`；无参数 tool 会记录 `{}`，显式 idempotent 但缺 key 会被拒绝。`tools.CommitStore` / `CommitRecord` 是 replay commit ledger 插槽，`tools.NewMemoryCommitStore` 提供测试和单进程参考实现，`tools.NewReplayHandler(..., tools.WithReplayCommitStore(store))` 会先查已提交结果，未命中才重放工具并写回。`EffectReplayRegistry` 是 backend adapter 插槽第一片，按 target-specific、type-specific、fallback 顺序分发 replay 决策。`tools.NewReplayHandler` 已接入 `tool_call` backend handler 第一片，`sandbox.NewReplayHandler` 已接入 `sandbox_exec`、`sandbox_file_read`、`sandbox_file_write` backend handler 第一片，`memory.NewReplayHandler` 已接入 `memory_put`、`memory_delete`、`memory_search` backend handler 第一片，`memory.NewExtractionReplayHandler` 已接入 `memory_extract` backend handler 第一片，`artifact.NewReplayHandler` 已接入 `artifact_write` backend verify handler 第一片。生产级 exactly-once 持久化、外部 commit store 实现和更完整 tool-call graph 仍由 adapter 或宿主接入。

## TurnLoop 输入队列

TurnLoop 必须区分三类输入：

| 输入 | 含义 |
| --- | --- |
| interrupted input | 触发当前 interrupt 的原始输入 |
| pending input | run 中断或取消期间收到、尚未处理的输入 |
| resume/new input | 恢复动作本身或恢复后的新输入 |

当前代码已完成 TurnLoop 队列第一片：`Push` 排队 user input，`Resume` 排队 `ResumeRequest`，`Pending` 返回 pending queue 副本，`Interrupted` 返回最近一次 interrupt 的原始输入和可选 `InterruptRecord`。下一次 `Run` 会把 current input、pending input 和 interrupted input 打成 `TurnInputBatch` 交给 runner/template，并产生 `TurnInputReceived`、`TurnInputMerged`、`TurnResumed` 或 `TurnInterrupted` 事件。若 interrupted state 保存了 `InterruptRecord.ResumeSchema`，TurnLoop 会在 `Resume` 和 `Run(WithResume)` 入口用 root `ValidateResumePayload` 拒绝 schema 不匹配的 payload，避免无效恢复请求进入 runner。宿主还可以通过 `WithTurnPolicy` / `WithTurnPolicyMetadata` 在 TurnLoop resume 边界注入 policy：即时 `Run(WithResume)` 和排队 `Resume` 都会产生 `PolicyRequested` / `PolicyDecided`，请求使用 `PolicyBoundaryTurn` 与 `PolicyActionResume`，`PolicyRequest.Input` 为 `TurnLoopPolicyInput`；deny 会返回结构化 `PolicyDeniedError` 并阻止 runner，review 会转成 approval interrupt。

持久化第一片已经落地：

- `TurnLoopStore` 保存 `TurnLoopState`，包含 pending inputs、pending turn events、interrupted input 和 input sequence；
- `WithTurnLoopStore(ctx, store)` 在 `NewTurnLoop` 时 hydrate 旧状态；
- `NewMemoryTurnLoopStore` 提供进程内实现，便于测试、示例和 adapter 对齐；
- `NewFileTurnLoopStore` 提供本地 JSON 文件持久化第一片，支持跨实例恢复、schema version、schema mismatch 拒绝和原子 rename 写入；
- `NewRowTurnLoopStore` 提供数据库型 row 端口第一片，`TurnLoopRowBackend` 只要求 `UpsertTurnLoopState` / `GetTurnLoopState`，可由用户适配到 SQL、KV、Redis hash 或内部控制面；`NewMemoryTurnLoopRowBackend` 用于测试和本地开发；
- `adapters/turnloop/sqlstore` 已提供 `database/sql` row/versioned CAS backend 第一片：接收 `*sql.DB`、`*sql.Tx` 或兼容 `DBTX`，提供 SQLite/Postgres/MySQL 默认 upsert/get 与 insert/update/get CAS 查询生成，也允许宿主通过 `WithQueries` / `WithVersionedQueries` 注入自己的 SQL。它不读取 SDK 配置文件、不创建表、不执行迁移，连接池、事务边界和 migration 仍由宿主项目负责；
- `adapters/turnloop/httpstore` 已提供 HTTP/JSON control-plane row/versioned CAS backend 第一片：同一个 `Backend` 同时实现 `TurnLoopRowBackend` 与 `TurnLoopVersionedBackend`，协议固定为 `GET/PUT {endpoint}/turnloop/state?key=...` 和 `GET/PUT {endpoint}/turnloop/versioned?key=...&expected_version=...`，404 映射为 not found，409 映射为 `ErrTurnLoopStoreConflict`。它只接受外部传入 endpoint、`http.Client`、headers 和响应大小上限，不读取 SDK 配置文件，也不定义业务级重试/调度策略；
- `adapters/turnloop/redisstore` 已提供 Redis-native row/versioned CAS backend 第一片：同一个 `Backend` 同时实现 `TurnLoopRowBackend` 与 `TurnLoopVersionedBackend`，只依赖宿主注入的 `GET` / `SET` / `EVAL` 窄接口；row store 使用 Redis string value 保存 `TurnLoopRowRecord` JSON，versioned store 使用内置 Lua CAS 脚本比较 `TurnLoopVersionedRecord.Version` 后原子写入，冲突映射为 `ErrTurnLoopStoreConflict`。它不创建 Redis client、不读取 SDK 配置文件，Redis nil 语义通过 `ErrNil` 或 `WithNotFound` 显式适配；
- `adapters/turnloop/objectstore` 已提供 conditional object versioned CAS backend 第一片：宿主注入暴露 `IfAbsent` / `IfVersion` 的对象 client，SDK 把对象原生 version 映射为 `TurnLoopVersionedRecord.Version`，条件写冲突映射为 `ErrTurnLoopStoreConflict`。它不创建云 client、不读取 SDK 配置文件，也不在普通 put/get/list object backend 上伪造 CAS；
- `NewBlobTurnLoopStore` 提供 remote/blob 端口第一片，`TurnLoopBlobBackend` 只要求 `GetBlob` / `PutBlob`，可由用户适配到对象存储、Redis、SQL 或内部控制面；`NewMemoryTurnLoopBlobBackend` 用于测试和本地开发；`adapters/storage/fileblob` 已提供 filesystem blob backend 第一片，`adapters/storage/objectblob` 已提供通用对象 client blob backend 第一片，`adapters/storage/s3blob` 已提供 AWS SDK v2 S3 object/blob adapter 第一片，`adapters/storage/gcsblob` 已提供 Google Cloud Storage object/blob adapter 第一片，`adapters/storage/r2blob` 已提供 Cloudflare R2 S3-compatible object/blob adapter 第一片，`adapters/storage/ossblob` 已提供 Alibaba Cloud OSS SDK v2 object/blob adapter 第一片，可复用同一对象 client 承载 checkpoint object 与 TurnLoop blob；
- `NewVersionedTurnLoopStore` 提供 optimistic CAS 端口第一片，`TurnLoopVersionedBackend` 只要求 `GetTurnLoopVersionedState` / `CompareAndSwapTurnLoopState`，可由用户适配到 SQL version、Redis WATCH、对象存储 etag 或内部控制面 revision；`NewMemoryTurnLoopVersionedBackend` 用于测试和本地开发；stale save 会返回 `ErrTurnLoopStoreConflict`，上层可以选择 reload/merge/retry；
- `NewLeasedTurnLoopStore` 提供 worker ownership wrapper 第一片：它包住任意 `TurnLoopStore`，在 `Load` / `Save` 前通过 `LeaseBackend` acquire/renew，可通过 `WithLeasedTurnLoopRenewalInterval` 为长 turn 启用后台续约，通过 `LeaseObserver` 暴露 acquire/renew/release/conflict/renewal lifecycle 事件，并通过 `Release(ctx)` / `Close(ctx)` 显式释放当前 lease；`TurnLoop.Close` 会关闭支持 `Close(ctx)` 的 store，失去 lease 后不会继续覆盖底层 store；
- `adapters/lease/redisstore` 已提供 Redis-native lease backend 第一片：只依赖宿主注入的 `GET` / `EVAL` 窄接口，用 Redis string value 保存 lease document，并通过 Lua 脚本原子完成 acquire、renew token 轮换和 release；它不创建 Redis client、不读取 SDK 配置文件，Redis nil 语义通过 `ErrNil` 或 `WithNotFound` 显式适配；
- `adapters/lease/httpstore` 已提供 HTTP/JSON control-plane lease backend 第一片：协议固定为 `POST {endpoint}/leases/acquire|renew|release?key=...` 和 `GET {endpoint}/leases?key=...`，409 acquire 映射为 `ErrLeaseConflict`，404/409 renew/release 映射为 `ErrLeaseNotHeld`。它只接受外部传入 endpoint、`http.Client`、headers 和响应大小上限，不生成 token、不维护服务器时间、不读取 SDK 配置文件；
- `adapters/lease/objectstore` 已提供 conditional object lease backend 第一片：宿主注入对象存储 client，并显式暴露 `IfAbsent` / `IfVersion` precondition，把 S3 etag、GCS generation、R2/OSS 条件写等 provider 原语映射为 acquire/renew/release CAS；它不会在普通 put/get/list object backend 上伪造 lease 原子性；
- `Push` / `Resume`、pending drain、interrupt 记录和 successful resume completion 都会把最新队列状态写回 store；`TurnLoop.Close(ctx)` 会取消 active turn，按 runner -> store 顺序关闭资源；成功资源幂等，失败资源可重试，后续 `Run` / `Push` 返回 `ErrTurnLoopClosed`，避免关闭后的新输入继续污染持久化队列。

### TurnLoop SQL backend schema 指南

`adapters/turnloop/sqlstore` 默认查询假设一行对应一个 `TurnLoopRowRecord`，最小列集如下：

| 列 | 语义 |
| --- | --- |
| `state_key` | TurnLoop store key，和 `TurnLoopRowRecord.Key` 一致 |
| `schema_version` | TurnLoop store schema version |
| `state_json` | `TurnLoopState` JSON，包含 pending inputs、pending events、interrupted input 和 input sequence |
| `updated_at` | RFC3339Nano UTC 字符串或等价可排序时间列 |
| `metadata_json` | row metadata JSON |

生产 schema 至少应保证 `state_key` 唯一。简单 row store 仍是 last-write-wins；多实例写入如果需要强一致，应优先使用 `VersionedTurnLoopStore` 这类 CAS 端口，把 `TurnLoopVersionedRecord.Version` 映射到 SQL version、Redis revision、对象存储 etag 或内部控制面 revision。

versioned CAS backend 的 SQL schema 需要额外保存版本列，最小列集如下：

| 列 | 语义 |
| --- | --- |
| `state_key` | TurnLoop store key，必须唯一 |
| `state_version` | CAS version token，由 backend 写入并在下一次 save 时作为 expected version |
| `schema_version` | TurnLoop store schema version |
| `state_json` | `TurnLoopState` JSON，包含 pending inputs、pending events、interrupted input 和 input sequence |
| `updated_at` | RFC3339Nano UTC 字符串或等价可排序时间列 |
| `metadata_json` | row metadata JSON |

`adapters/turnloop/sqlstore` 的默认 CAS 查询遵循两个约束：首次 insert 遇到已有 `state_key` 时必须返回 0 affected rows 或等价冲突结果；更新必须带 `WHERE state_key = ? AND state_version = ?`，陈旧 expected version 同样转成 `ErrTurnLoopStoreConflict`。需要长事务/worker ownership 时，宿主可以使用事务、`SELECT ... FOR UPDATE`、`adapters/lease/sqlstore` 的 database/sql lease backend、`adapters/lease/redisstore` 的 Redis lease backend、`adapters/lease/httpstore` 的内部 control-plane lease backend，或用 root `NewLeasedTurnLoopStore` 把 `LeaseBackend` 包到具体 `TurnLoopStore` 外层；SDK adapter 只提供参数化读写、CAS 边界和原子 ownership wrapper，不把 schema lifecycle 或长期 worker 调度沉入 gopact。

preempt 语义：

- 高优先级输入可以 cancel 当前 run；
- cancel 到 safe point 后，TurnLoop 合并 pending input；
- 合并规则必须产生事件；
- Runner 不维护长期输入队列。

尚未完成：

- provider-specific object blob/checkpoint wrapper，以及更完整的事务/锁策略文档；
- 业务级 input merge strategy 深化；
- 完整 JSON Schema engine adapter/plugin 与 conformance 深化。

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

## Import / Export contract

```go
type Exporter interface {
	ExportStep(ctx context.Context, threadID string, step int) (StepExport, error)
}

type Importer interface {
	ImportStep(ctx context.Context, export StepExport) (CheckpointRecord, error)
}
```

规则：

- import/export 是过程迁移能力，不是业务 harness；
- export 不应该要求调用方知道具体 checkpoint store；
- import 可以生成新的 `RunID`，但必须保留原始 `ThreadID`、step、node、event ids 和 source run metadata；
- import 后继续执行时，新的事件要能关联 source snapshot；
- import/export 失败必须是结构化错误，不能静默降级成从头执行。

## 测试要求

- node 成功后写 checkpoint；
- node 成功后可导出 step snapshot；
- interrupt 前写 checkpoint；
- interrupt 前可导出带 pending record 的 step snapshot；
- checkpoint 失败导致 run error；
- step import schema/config/artifact 不匹配时拒绝恢复；
- resume payload schema 不匹配时拒绝恢复；
- cancel safe point 写 checkpoint；
- cancel safe point 可导出 step snapshot；
- 非幂等 tool resume 不重复执行；
- config drift 产生事件。
