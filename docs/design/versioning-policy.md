# gopact Versioning Policy

本文定义 `gopact` core SDK、可序列化 schema 和外部 extension 的版本策略。它和 [deprecation-policy.md](deprecation-policy.md) 配套使用：deprecation policy 管理“如何废弃”，versioning policy 管理“什么时候能发布”。

## Module Version

`gopact` 遵守 semver：

- `v0.x`：允许快速收敛，但所有 public API 变化仍必须更新 `public-api-boundary.json`、示例和迁移说明。
- `v1`：承诺稳定 root public API、核心可序列化 schema 和 conformance contract。
- `major`：允许破坏性 public API、schema 或 conformance 变更，但必须提供 migration guide。
- `minor`：允许向后兼容的新 API、新 conformance case、新 reference implementation 和新 extension target。
- `patch`：只允许 bug fix、文档修正、非破坏性测试补强和安全修复。

SDK 自身不读取配置文件，版本升级不能通过隐藏配置兼容旧行为。需要兼容时，应通过 typed options、adapter、plugin 或显式 migration hook 暴露。

## Stability States

root public API 的稳定性以 `public-api-boundary.json` 为准：

- `stable`：`v1` 后只能在 `major` 中删除或破坏性修改。
- `experimental`：`v1` 前可以调整；`v1` 后如仍保留，必须写明兼容窗口。
- `transitional`：必须在 `v1` 前稳定、移出 core、降级为 reference-only，或删除。

`v1-migration-plan.json` 是 transitional public API 和主仓 adapter/template 外迁的逐项执行清单。它必须覆盖所有 transitional root API group，以及所有 `repository-boundary.json` 中的 `move-to-adapter-repo`、`move-to-template-repo` 和 `remove-before-v1` 条目。

稳定性状态变化本身就是 public API 变更，必须更新 deprecation policy、migration guide 和 examples。

## Release Gates

每次 core SDK release 至少需要满足：

- `core-ci-gates.json` 中的 whitespace、unit、race、vet、lint、coverage、examples 和 security gate 在 CI 通过；
- `.golangci.yml` 中 required linters/formatters 与 manifest 一致；
- public examples 可运行；
- root public API 已被 `public-api-boundary.json` 覆盖；
- transitional public API 和需要外迁/删除的主仓 adapter/template 已被 `v1-migration-plan.json` 覆盖，且该计划的 `release_gate_checks` 已把每个 v1 gate 绑定到 evidence type、来源 manifest、`required_check_ids`、required status 和 blocker summary；CI 相关 gate 还必须用 `required_ci_gates` 对齐核心 CI 或外部仓跨仓 CI gate；
- 关键 root facade 已被 `public-api-examples.json` 覆盖；
- 若发生废弃、移动或删除，必须更新 `deprecation-policy.md` 和 `migration-guide.md`；
- 若发生可序列化 schema 变化，必须说明 `RunExport`、`StepExport`、`CheckpointRecord`、resume payload、verification report 的兼容性。

测试通过只是 release gate 的一部分。release 还需要确认没有把新的生产 provider/backend/channel/observability/template 沉进 core。

## External Extensions

外部 adapter、plugin 和 template 使用独立 module version，但必须声明兼容的 core SDK 范围：

- extension target 和 required suites 来自 `extension-conformance.json`；
- ready/pending 路线来自 `external-integration-roadmap.json`；
- 外部仓库粒度、module path、私有初始化状态、必备 scaffold 文件和 CI 命令来自 `external-repositories.json`；
- 外部仓库初始文件规则和 target package layout 来自 `extension-scaffold-spec.json`；
- extension release 必须运行自己的 offline conformance suite；
- 生产 provider/backend/channel/plugin 的破坏性变更不应强迫 core SDK 升 major，除非 core contract 本身变化；
- core `minor` 可以新增 conformance case，extension 可以在自己的 `minor` 或 `patch` 中补齐兼容；
- core `major` 可以重新定义 extension contract，但必须给出迁移说明和外部仓库影响列表。

外部 extension 不应读取 SDK-owned 配置文件。宿主应用仍通过 typed constructors、options、clients、providers、adapters 或 plugins 注入配置。

## Schema Versions

以下结构必须显式维护 schema/version 字段或等价兼容策略：

- `RunExport`
- `StepExport`
- `CheckpointRecord`
- verification report/evidence
- surface/channel action payload
- extension conformance manifest

schema 变化分三类：

- backward compatible：新增 optional 字段、增加 metadata，不改变旧字段语义；
- migration required：字段重命名、结构拆分、`ConfigVersion` 变化，需要 migration hook 或文档流程；
- breaking：旧 checkpoint/run export/step export 无法恢复或校验，只能进入 `major`，或在 `v0` 明确标为 transitional 收敛。

涉及 checkpoint/resume 的变化必须优先保证失败可解释。不能悄悄忽略旧状态，也不能自动重复执行已经完成的 model/tool/sandbox/effect。
