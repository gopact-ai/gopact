# gopact Migration Guide

本文定义 `gopact` 在 v1 前后的迁移文档要求。它不是某个版本的 changelog，而是给使用方和外部 adapter/template 仓库看的兼容性边界。

版本策略见 [versioning-policy.md](versioning-policy.md)。迁移说明必须服从 semver、release gate、schema version 和 extension compatibility 规则。

v1 前的具体收敛清单见 [v1-migration-plan.json](v1-migration-plan.json)。该清单把需要外迁或删除的主仓路径、外部仓库目标、transitional root API 的目标状态和 `release_gate_checks` 绑定到机器可测条目；每个 gate 都声明 evidence type、来源 manifest、required status 和 blocker summary。

## Compatibility Promise

- root public API 的稳定性以 `docs/design/public-api-boundary.json` 为准。
- v1 前 root public API 和主仓 adapter/template 收敛动作以 `docs/design/v1-migration-plan.json` 为准。
- `stable` API 只能按 deprecation policy 迁移，不能静默删除或改变语义。
- `experimental` API 可以调整，但必须在 release note、migration note 和示例中说明。
- `transitional` API 默认要求在 v1 前移动、删除或降级为 reference-only。
- SDK 自身不读取配置文件；迁移文档只能描述 typed options、constructor 参数、provider、adapter 或 plugin 注入方式。

## API Changes

每个 public API 变化都必须记录：

- 旧符号、旧行为和所属 category；
- 新符号或新调用方式；
- 最小替换示例；
- 是否需要 `Deprecated:` godoc 标记；
- 兼容窗口和移除版本；
- 对 `RunExport`、`StepExport`、checkpoint、event stream 或 verification evidence 的影响。

## Adapter Split

生产级 provider、backend、channel、observability、transport 和 template 不作为 core 完成标准。迁移文档需要说明：

- 原主仓路径；
- 外部仓库目标；
- 模块路径；
- required conformance suites；
- scaffold status；
- host-owned config、secret、client、endpoint 和 policy 如何注入。

`reference-only` 包只能保留轻量、离线、无第三方服务 SDK 依赖的实现。

## Checkpoint and Resume

涉及恢复语义的迁移必须说明：

- `ConfigVersion` 是否变化；
- `StepSnapshot` / `StepExport` schema 是否变化；
- `CheckpointRecord` 是否需要 migration；
- artifact/effect/memory replay 是否需要重新规划；
- interrupted run 是否能从旧 checkpoint 恢复；
- 无法自动迁移时的显式失败和人工处理路径。

## Verification

每个迁移条目必须给出可复现验证方式：

- public example 是否仍可运行；
- conformance suite 是否覆盖；
- golden trajectory 或 run export 是否更新；
- `go test -count=1 ./...`、`go vet ./...` 和 `git diff --check` 是否通过；
- M6 release 前，`core-ci-gates.json` 定义的 race/lint/coverage/examples/security gate 是否仍在 CI 通过。
