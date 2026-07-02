# gopact Public API 废弃策略

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


日期：2026-06-25

设计入口：[index.md](index.md)

`gopact` 是 SDK。用户会把 root package 的 facade、contract 和 option 写进业务代码里，所以 public API 的废弃不能靠口头约定。任何会影响调用处、序列化格式、恢复边界或 conformance 的变更，都必须先进入边界清单、示例契约和 release note，再考虑删除。

版本策略见 [versioning-policy.md](versioning-policy.md)。本文只定义废弃和移除流程；release 类型、schema version 和外部 extension 兼容性由 versioning policy 约束。

## 目标

1. 用户能从 godoc 和设计文档判断一个 API 是否稳定。
2. 新增、废弃、移动和删除 root API 都有同一套 review 入口。
3. v1 前允许快速收敛，但不能静默打破已经被示例和 conformance 固定的 SDK 调用形态。
4. v1 后遵守 semver，破坏性变更只能进入 major release。
5. 废弃流程不引入配置文件；迁移建议仍通过 typed options、facade、adapter 或 plugin 表达。

## 稳定性等级

`public-api-boundary.json` 中的 `stability` 只能使用以下状态：

- `stable`：计划长期保留。v1 后不能在 minor/patch release 中删除或改变行为语义。v1 前如确需调整，也必须先给出迁移示例和废弃窗口。
- `experimental`：可在 v1 前调整，但仍属于公开 SDK 面。新增或调整时必须更新 public API 边界、测试和必要 Example；不能绕过 review 直接暴露。
- `transitional`：过渡 API 或 reference implementation。常见于迁移到外部 adapter/template repo 前的临时入口。它必须有明确归属、迁移路径或 v1 前处理计划。

稳定性属于 API contract，不等同于实现成熟度。一个实现可以很轻量，但只要调用形态被 root facade 暴露，就必须按上述状态管理。

## 废弃标记

废弃 exported symbol 时必须同时完成：

1. 在 godoc 注释中加入 Go 标准格式：

```go
// Deprecated: use NewThing with WithThing instead.
```

2. 更新 `doc/design/public-api-boundary.json`，说明该符号的当前稳定性、来源文件和废弃原因。
3. 如果影响关键调用形态，更新 `doc/design/public-api-examples.json`，新增迁移后的可执行 Example，或把旧 Example 改成新入口。
4. 在 `doc/design/development-plan.md` 或 release note 中记录迁移窗口。
5. 保留测试，证明旧入口在废弃窗口内仍按文档行为工作，或明确返回迁移错误。

禁止只在 README 写“不要用了”却不更新 godoc 和边界清单。

## 移除窗口

v1 前：

- `experimental` API 可以在一个 milestone 内调整或移除，但必须同步更新 boundary、Example 和迁移说明。
- `transitional` API 必须在 v1 前归入 `stable` / `experimental`、迁移到外部仓库，或明确删除。
- 已经被 conformance helper 或 `public-api-examples.json` 覆盖的调用形态，删除前至少要有一个替代 Example。

v1 后：

- `stable` API 删除或破坏性语义修改只能进入下一个 major release。
- `stable` API 废弃后至少保留两个 minor release。
- `experimental` API 废弃后至少保留一个 minor release，除非存在安全、数据损坏或无法兼容的恢复边界问题。
- `transitional` API 不应该出现在 v1 stable surface 中；如果必须保留，应明确标成实验性或外部 adapter 入口。

安全漏洞、数据损坏、隐私泄漏和恢复边界不一致可以缩短窗口，但必须在 release note 中解释原因，并提供最小迁移路径。

## 兼容性审查

任何 public API 变更合入前检查：

- 是否更新 `public-api-boundary.json`？
- 如果调用处改变，是否更新 `public-api-examples.json` 并补齐可运行 Example？
- 是否需要 `Deprecated:` godoc 标记？
- 是否改变 JSON、checkpoint、run export、step export、resume payload 或 verification report 的可序列化 contract？
- 是否改变 middleware/plugin 的执行顺序、错误语义或 `Next()` 语义？
- 是否改变 conformance helper 的最低语义？
- 是否需要外部 adapter/template/plugin 仓库同步迁移？
- 是否仍遵守 SDK 不读取配置文件、依赖由宿主注入的原则？

如果答案不清楚，先把 API 标成 `experimental`，不要过早承诺稳定。
