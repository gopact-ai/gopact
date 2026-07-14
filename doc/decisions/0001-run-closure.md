# ADR-0001：Run 终态闭合

<!-- gopact:doc-language: zh -->

状态：`accepted`

日期：2026-07-14

关联：[对齐 RFC](../rfcs/run-store-history-alignment.md) · [系统宪法](../design/system-constitution_zh.md)

## 背景

旧设计允许 Retry、external jump-to 或 Reopen 在已有 terminal fact 后继续同一 Run。这使“终态”、恢复身份和历史 head 同时出现两种解释，也让 Agent 必须穿透 private graph 才能控制官方 Workflow。

## 决策

- `completed`、`failed`、`terminated` 是不可变终态。
- 失败后的 Retry、typed `Workflow.JumpTo` 和既有 `Snapshot.Fork` 创建新 Run；新 Run 用 `SourceRunID` 与 `SourceRevisionID` 或 `SourceEventSeq` 指向来源。
- Node 自动重试只在同一非终态 Run 内创建新 Attempt。
- 只有 `interrupted` 或 lease-expired `running` Run 可 Resume 同一 Run。跨 `DefinitionVersion` 一律创建新 Run，并由业务显式迁移。
- 不定义 `Restart`；删除终态 `Reopen`、`CheckpointController.Reopen` 和 Agent `ForceJumpTo(private NodeID, any)`。
- `Terminate` 只控制本进程的活动执行；终止后的原 Run 不变，后续执行必须显式 Fork。
- 官方 Agent 可以窄化暴露 `Snapshot`、`Retry`、`Terminate`，但不暴露 raw Workflow/private graph。用户最小 Agent 实现合法，不承诺 Workflow 的恢复、控制和历史能力。

## 后果

- 每个 Run 只有一个闭合结局，查询和审计不再解释“终态后的新 head”。
- 业务级 Retry/Jump 的 RunID 改变，调用方须沿 source lineage 关联执行。
- 自动 Node retry 仍保持原有 Attempt 语义；恢复仅覆盖真实暂停或执行权丢失。
- 官方 Agent 的高级控制能力变窄，但不再破坏封装或接受不安全的动态节点输入。

## 备选方案

- **同一 Run 追加 control epoch**：拒绝；终态失去闭合含义。
- **引入 Restart**：拒绝；与 Retry/Fork 重复且增加迁移语义。
- **Agent 暴露 raw Workflow 或 ForceJumpTo**：拒绝；泄露 private graph，并把动态类型检查推给运行时。
