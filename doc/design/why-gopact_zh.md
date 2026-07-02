# Why gopact

<!-- gopact:doc-language: zh -->

[英文文档](./why-gopact.md)

## 中文

日期：2026-06-30

`gopact` 是面向生产交付的 Go Agent SDK。它的核心目标不是复刻某一种 agent template，也不是绑定某一家模型服务，而是提供一套可观察、可恢复、可审计、可验证的 agent 运行时契约。

## 核心定位

`gopact` 的核心优势是 evidence-first runtime：每次 agent run 都应该能留下可携带、可检查、可恢复的过程证据。

这意味着 SDK 必须把 agent 执行中的关键事实结构化表达出来：

- 输入、输出、运行身份和 step 边界；
- 模型、工具、memory、sandbox、artifact、channel 等动作事件；
- checkpoint、interrupt、resume、cancel 和 failure 语义；
- 外部副作用、artifact 引用和 replay policy；
- verification、policy decision、release gate 和 run export 证据。

编排能力是入口，可信过程是核心。

## 目标用户

`gopact` 面向需要把 agent 放进真实后端系统、开发平台、自动化平台或企业应用的 Go 开发者。

典型场景包括：

- 代码开发 agent、测试修复 agent、运维 agent；
- 企业知识问答、审批流、数据分析助手；
- 多模型、多工具、多 channel 的 agent 应用；
- 需要 checkpoint、HITL、审计、回放和 release gate 的生产 agent。

## 设计目标

`gopact` 应该做到：

- Go-native：以 Go 的类型系统、接口、context 和单 binary 部署体验为基础。
- Production-ready：默认支持事件、checkpoint、resume、policy、redaction、verification 和 export。
- Evidence-first：每次 run 不只返回答案，还能交付可验证的过程包。
- Extension-friendly：provider、tool、memory、sandbox、channel、MCP、A2A、observability 和 template 都可替换。
- Scaffold-friendly：常见 agent template 和 example 必须能快速启动，复杂能力通过显式 option 打开。
- Provider-neutral：core 不绑定具体模型厂商，模型与服务接入放在 ext 或 adapter 层。

## 非目标

`gopact` 不应成为：

- 某个模型服务的 SDK 包装器；
- 只面向 demo 的 agent toy framework；
- 隐藏执行顺序、无法恢复和审计的不透明 agent class；
- 把业务 harness、prompt 策略、平台配置和生产 adapter 全部塞进 core 的大框架。

Core 只沉淀稳定契约和轻量默认实现。生产级 provider、外部存储、企业 channel、observability exporter 和业务 template 应通过 ext、adapter、plugin 或 example 接入。

## 差异化能力

`gopact` 的差异化不应只建立在“也有 graph”上，而应建立在完整运行证据上。

Agent 执行完成后，宿主系统应能回答：

- 本次 run 执行了哪些 step；
- 每个 step 的输入、输出、事件和副作用是什么；
- 哪些工具或外部系统被调用；
- 哪些 artifact 被写入或引用；
- 哪些 policy 被请求和决策；
- run 是否可恢复、可重放或可封存；
- 失败归因是否明确；
- release 或上线所需证据是否齐全。

这些问题应由 SDK 的结构化契约回答，而不是依赖日志文本或业务系统临时拼接。

## 编排方向

`gopact` 的编排层应逐步具备：

- 顺序 graph；
- 条件路由；
- DAG fan-in；
- 动态 fan-out；
- subgraph / runnable node；
- checkpoint-aware replay；
- typed stream projection；
- template conformance。

编排 API 必须服务生产运行时，而不是为了追求 DSL 表达力牺牲可观测、可恢复和可审计。

## 脚手架目标

`gopact` 应提供开箱即用的 agent scaffold：

- chat；
- ReAct with tools；
- Plan-Execute；
- human approval；
- checkpoint and resume；
- artifact output；
- OpenAI-compatible provider；
- MCP tools；
- Agent-as-Tool。

常见路径必须简洁，生产能力必须显式。用户应能先用少量代码跑通，再按需要逐步接入 policy、checkpoint、memory、artifact、verification 和 external adapters。
