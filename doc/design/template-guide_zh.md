# gopact Template Guide

<!-- gopact:doc-language: zh -->

[英文文档](./template-guide.md)

## 中文

本文定义外部 `gopact-templates-*` 仓库应如何组织 graph template。Template 是业务 harness 的可复用组合，不是 SDK core 的原子能力。

## Template Boundary

Template 可以组合：

- graph、Runner、TurnLoop；
- model provider router、tool registry、memory、sandbox、MCP、A2A；
- middleware、policy、redaction、plugin；
- channel/transfer、reviewer、verification recorder。

Template 不应该把业务策略伪装成 core 默认值。Prompt、loop、context compression、planner、review gate、release gate、worker queue、retry/DLQ 都属于 template 或宿主应用层。

## Step Export and Resume

Template 必须把关键过程落到稳定 step 边界：

- 每个外部动作前后有明确 node/step 身份；
- completed step 可以导出 `StepExport`；
- interrupted step 必须带 `InterruptRecord` 和可校验 `ResumeRequest`；
- 恢复时不能重复已完成的 model/tool/sandbox/effect；
- checkpoint load、step import、resume payload 和 artifact verification 必须产生事件。

Code agent、问答机器人、ReAct、supervisor 和 plan-execute 可以有不同 harness 逻辑，但必须共享同一套 export/import/resume 语义。

## Events and Verification

Template 必须让过程可观察：

- run、node、model、tool、memory、policy、channel、checkpoint、replay、verification 事件要能解释关键决策；
- `RunRecorder` 应能从事件流导出 `RunExport`；
- verification evidence 只记录已观察事实，不执行命令、不读取环境、不替代业务 gate；
- golden trajectory 可以是最小语义轨迹，不要求绑定完整内部实现顺序，但 required frame 应能按 metadata 子集固定 action/mode 等关键治理语义。

## Memory and Side Effects

Template 可以提供 memory 抽取、压缩、合并或后台提交，但必须保持宿主可控：

- memory extractor、merge、store、worker executor 由宿主注入；
- deferred memory/effect 必须可规划、可重放、可记录 verification evidence；
- 生产队列、重试、并发和 DLQ 属于 adapter 或宿主 worker；
- side effect 必须有 idempotency key、artifact refs、policy boundary 和 replay plan。

## Conformance

外部 template 仓库至少需要：

- `gopacttest` template trajectory conformance；
- public examples；
- error-path tests；
- interrupted/resume tests；
- policy deny/review tests；
- run export 或 verification evidence 测试；
- `CONFORMANCE.md` 记录 required suites、CI commands、integration tags 和安全边界。

Template 可以更聪明，但不能更隐形。过程边界比最终答案更重要。
