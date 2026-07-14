# Run、Store 与历史语义对齐

<!-- gopact:doc-language: zh -->

状态：`accepted`

批准日期：2026-07-14

本 RFC 统一 Run 终态、恢复、控制、持久化和历史边界。最终决策分别记录于 [ADR-0001](../decisions/0001-run-closure.md)、[ADR-0002](../decisions/0002-durable-store-authority.md) 与 [ADR-0003](../decisions/0003-history-policy.md)，并已写入[系统宪法](../design/system-constitution_zh.md)。三份 ADR 与本 RFC 冲突时，以 ADR 为准。

## 决策

### Run 与控制

- `completed`、`failed`、`terminated` 是不可变终态，不得在同一 Run 上 `Reopen` 或追加 execution/control epoch。
- 失败后的用户 Retry 创建新 Run，并以 `SourceRunID` 加 `SourceRevisionID` 或 `SourceEventSeq` 记录来源。Node 自动重试仍在同一非终态 Run 内创建新 Attempt。
- 只有 `interrupted` 或因 lease 过期而失去执行者的 `running` Run 可以 Resume 同一 Run。跨 `DefinitionVersion` 必须创建新 Run，并由业务显式迁移输入、Context 与目标。
- 保留 `Snapshot.Fork`。失败 Retry 与 typed `Workflow.JumpTo` 都复用同一 source-lineage 新 Run 模型；不引入 `Restart`。
- 删除同一 Run 终态 `Reopen`、`CheckpointController.Reopen`，以及 Agent 按 private `NodeID` 接收任意值的 `ForceJumpTo`。
- `Terminate` 只作用于本进程正在执行的 Run；原 Run 一旦 `terminated` 永远不变。之后只能显式 Fork 新 Run。

### Agent 边界

- 官方 Agent 必须由 Workflow 支撑。用户实现最小 `Agent` interface 仍合法，但不自动获得恢复、控制和历史保证。
- Agent control 可以窄化暴露 `Snapshot`、`Retry`、`Terminate`；不得暴露 raw Workflow、private graph 或任意节点跳转。

### Store 与故障语义

- Workflow 只公开一个 `Store` interface，组合 `Checkpointer`、`CheckpointHistory` 与 `runlog.FencedLog`，并统一通过 `WithStore` 配置。
- durable 状态写入、Interrupt acknowledgement 和 fencing 全部 fail closed；失败时不得继续推进并假装可恢复。只有 observer 可以 best-effort。
- crash 后只按上述 Resume 规则继续；不建设 distributed scheduler、worker discovery、queue 或 control plane。

### 历史与数据边界

- Runtime Node 历史默认只记录 metadata，必填维度是 identity、sequence、causality/source、timestamp、type、phase、status 与 error；不自动复制业务 input、Workflow Context 或 output。
- 业务需要时可显式写入有界且安全的 `Event.Payload`，或保存 `PayloadRef`/`ArtifactRef`。Checkpoint 是 opaque、可能敏感的恢复数据，不供 Query/View 解析。
- 框架拥有的 buffer、serialization 和 query 必须有默认上限及可诊断的超限行为；业务 payload 的授权、脱敏与保留期仍由应用负责。

### 身份、安全与扩展

- `SessionID` 只做关联，不是认证或授权凭据。隔离与授权由应用在 core 外实施；core 不引入 tenant/scope 模型。
- child Run 继承 `SessionID`、记录 `ParentRunID`，并继承或收窄上游取消、budget 与 policy 约束。
- Plugin 只做编译期注册，不拥有资源 lifecycle。领域 telemetry 从 `Event`/`View` 投影；基础设施 telemetry 可以包装 runtime 或 adapter。
- Provider adapter 可以拥有协议适配、transport retry/backoff 等传输韧性，不得拥有业务编排。

### 模块与发布

- ext 的目标公开模块是 `github.com/gopact-ai/gopact-ext` 与 `github.com/gopact-ai/gopact-ext/stores`；保留现有 package path。测试 module 不发布。
- 发 tag 前以协调源码做跨仓 E2E；发 tag 后用 `GOWORK=off` 和精确 tag 再验证。
- RC 是 `production evaluation candidate`（生产评估候选），不是 production-ready 声明；stable 经 burn-in 后才可以称为 production-ready。

## 非目标

- 同一 Run 终态重开、`Restart` 或跨版本隐式继续。
- Agent raw Workflow/private graph escape hatch。
- best-effort durable state、从 observer 反推权威状态，或解析 checkpoint 做查询。
- core tenant/auth 模型、distributed scheduler、托管 control plane。
- Plugin 资源容器、Provider 业务流程引擎、自动保存任意业务 payload。

## 迁移影响

旧的同 Run Retry/Jump/Reopen 调用应迁移到 source-lineage 新 Run；多个 Store 配置点应收敛到 `WithStore`；依赖完整 node payload 的查询应改为显式安全 Event 或引用。具体公共 API 删除和迁移由后续实现计划负责，本 RFC 不保留 pre-v1 双轨兼容层。
