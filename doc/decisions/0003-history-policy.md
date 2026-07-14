# ADR-0003：执行历史默认只记录 Metadata

<!-- gopact:doc-language: zh -->

状态：`accepted`

日期：2026-07-14

关联：[对齐 RFC](../rfcs/run-store-history-alignment.md) · [系统宪法](../design/system-constitution_zh.md)

## 背景

旧条款要求每个 Attempt 自动保存完整 input、Workflow Context 和 output。这既放大存储与序列化成本，也会在应用未显式授权时复制秘密、个人数据或大对象。Checkpoint 又是恢复内部格式，不适合作为查询数据库。

## 决策

- Runtime Node 历史默认只记录 metadata，必填维度是 identity、sequence、causality/source、timestamp、type、phase、status 与 error。
- Runtime 不自动复制业务 input、Workflow Context 或 output 到 Event/RunLog/Snapshot。
- 业务可显式写入有界、安全的 `Event.Payload`，或保存 `PayloadRef`/`ArtifactRef`；应用负责授权、脱敏、完整性、访问控制和保留期。
- Checkpoint 是 opaque、可能敏感的恢复数据；Query/View 不解析其 payload。
- 框架拥有的 buffer、serialization 与 query 有默认上限，并明确报告 overflow、截断或缺口。

## 后果

- 默认历史可用于状态、拓扑与故障诊断，不等同于业务数据回放。
- 需要业务内容的产品必须显式选择安全 payload 或 artifact 存储方案。
- Snapshot/View 可以稳定演进，不依赖私有 checkpoint schema。

## 备选方案

- **自动保存完整业务数据**：拒绝；默认泄露与无界增长风险不可接受。
- **查询时解析 checkpoint**：拒绝；耦合恢复格式并绕过权限边界。
- **完全禁止 payload**：拒绝；显式、有界、安全的数据对审计和产品观测仍有价值。
