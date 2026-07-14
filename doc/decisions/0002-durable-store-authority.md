# ADR-0002：Durable Store 是恢复权威

<!-- gopact:doc-language: zh -->

状态：`accepted`

日期：2026-07-14

关联：[对齐 RFC](../rfcs/run-store-history-alignment.md) · [系统宪法](../design/system-constitution_zh.md)

## 背景

旧设计把 checkpoint、history 和 run log 分成多个配置面，并允许外部持久化按 best-effort 失败。执行可能已确认 Interrupt 或继续推进，却没有足以恢复和 fencing 的 durable 事实。

## 决策

- Workflow 公开一个 `Store` interface，组合 `Checkpointer`、`CheckpointHistory` 与 `runlog.FencedLog`；统一通过 `WithStore` 配置。
- Store 是已配置 Run 的 durable 恢复与历史权威，不决定业务调度。
- durable 写入、Interrupt acknowledgement 与 fencing fail closed；失败时停止推进并返回错误。best-effort 只适用于不参与恢复或控制正确性的 observer。
- crash 后仅允许 ADR-0001 定义的同 Run Resume；其他继续执行创建带 source lineage 的新 Run。
- 不建设 distributed scheduler、worker discovery、queue 或 control plane。

## 后果

- 用户只配置一个一致的持久化边界，不能组合出 checkpoint 与 run log 相互矛盾的状态。
- durable backend 故障会直接影响 Run 可用性，这是不伪造恢复保证的必要代价。
- memstore 可实现同一接口并提供进程期能力，但不能宣称跨进程 durability。

## 备选方案

- **保留多个 Store option**：拒绝；允许权威事实分叉。
- **durable 写入 best-effort**：拒绝；执行确认与可恢复状态可能不一致。
- **引入分布式控制面**：暂不采用；当前没有真实消费者，Store fencing 已覆盖所需边界。
