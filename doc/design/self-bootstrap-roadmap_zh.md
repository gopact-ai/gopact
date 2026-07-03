# gopact Self-Bootstrap Roadmap

<!-- gopact:doc-language: zh -->

[英文文档](./self-bootstrap-roadmap.md)

## 中文

日期：2026-06-30

本文定义 `gopact` 从当前 SDK 形态推进到可自举 production-grade Agent SDK 的阶段性目标、验收门槛和测试标准。

## 目标

`gopact` 应达到可自举状态：可以用 `gopact` agent 维护 `gopact` 自身，完成分析、计划、受控修改、测试、review、release gate 和证据封存。

可自举不是单次模型生成代码，而是一条可观察、可恢复、可审计、可验证的工程流程。

## 当前长程阶段目标

下一阶段目标是把 `gopact` 推进为 production-grade、可自举的 Agent SDK。SDK 必须能够运行垂域 agent，将它们组合成 A2A agent mesh，通过 mock 与真实 provider 双轨测试固化行为，并用同一套 runtime evidence 维护 `gopact` 自身仓库。

目标状态不是一个 demo，而是一条可重复的工程闭环：

- 分析仓库或 agent cluster；
- 生成结构化计划；
- 执行受控代码或配置修改；
- 运行测试并采集 command evidence；
- 封存 diff、policy、checkpoint、artifact、review、A2A task 和 release-gate evidence；
- 在 approval、cancel 或 failure 后从稳定 checkpoint 恢复；
- 只有 mock CI gate 与本地 integration gate 都满足时才允许产出 release bundle。

## 差异化约束

`gopact` 的核心优势必须落在低门槛、evidence-first 的 agent 工程组合能力上：用户应能快速创建垂域 agent，暴露 agent card，加入 A2A mesh，并验证行为；不要求采用 hosted platform，也不要求重写 provider 或 template 代码。

这带来三个产品边界：

- core 负责稳定契约、本地默认实现、conformance kit、graph/workflow runtime、evidence、checkpoint/resume、policy 和 A2A 生命周期语义；
- `gopact-ext` 负责生产 provider、存储、channel、registry 和 template 实现；
- `gopact-examples` 负责可运行 workflow 和 agent cluster，同时作为 smoke test 与用户用法参考。

## 核心原则

- Evidence-first：每次 run 必须能留下结构化过程证据。
- Test-first：测试是行为和标准的唯一真理。
- Mock-stable CI：CI 不依赖真实 provider、外部密钥或不稳定网络。
- Real-provider local validation：本地开发必须能用 `.env` 中的 Agnes provider 跑真实链路。
- Scaffold-first：常见 agent 和 agent cluster 应能低门槛拉起。
- Core stays small：core 保留稳定契约和轻量默认实现，生产 provider、registry、gateway 和平台 adapter 放在 ext 或 adapter 层。

## 阶段路线

| 阶段 | 目标 | 完成标准 |
| --- | --- | --- |
| S0 | 定位与标准冻结 | `why-gopact`、`agent-mesh`、self-bootstrap roadmap 和测试策略进入设计入口；所有目标有验收标准 |
| S1 | 编排地基 | graph 支持 branch、DAG fan-in、dynamic fan-out、loop/step limit、subgraph / runnable node，并由 [workflow-orchestration-matrix.json](workflow-orchestration-matrix.json) 绑定 conformance tests |
| S2 | Scaffold 地基 | 提供低门槛 agent scaffold，覆盖 chat、ReAct、Plan-Execute、checkpoint/resume、human approval |
| S3 | Provider 双轨 | ext 中 OpenAI-compatible、Agnes、Ark provider 示例可本地真实跑通；CI 使用 mock provider 固化行为 |
| S4 | Agent Mesh | 支持 agent card、readiness-aware discovery、lease registration、heartbeat renewal、A2A call/stream/cancel、RPC-like router、cross-agent evidence；CI 使用 mock HTTP registry 固化注册、续约、bootstrap 和 route 行为 |
| S5 | Example Cluster | example 仓库提供 gateway、planner、research、code、review agent 本地集群 |
| S6 | Dev Agent 自举 | Dev Agent 能执行 analyze、plan、write、test、review、release gate，并导出完整 `RunExport` |
| S7 | 发布门禁 | core、ext、examples 的 mock CI、coverage、conformance、golden trajectory 和本地 Agnes integration 均有明确通过标准 |

## 当前已交付切片

`gopact-ext/devagent/selfbootstrap` 提供第一段 provider-neutral 的 Dev Agent 自举 workflow。它编排由宿主注入的 analyze、plan、write、test、review 阶段，封存已观察到的 diff、file snapshot、command、CI gate、review、run export、failure attribution 和 verification report 证据，并登记在 [workflow-orchestration-matrix.json](workflow-orchestration-matrix.json)。

该切片本身不调用模型、不执行命令、不应用 patch、不读取工作区。命令执行、受控修改、sandbox、policy、checkpoint 和 release gate 自动化必须在 runtime contract 明确后再升级为内置能力。

## 可自举定义

可自举状态必须同时满足：

- 能读取并分析 `gopact` 仓库；
- 能生成结构化计划；
- 能执行受控文件修改；
- 能运行测试并采集结果；
- 能把测试、diff、policy、checkpoint、artifact 和 review 证据写入 run export；
- 能处理 human approval interrupt 和 resume；
- 能通过 release gate；
- 能在失败时给出 failure attribution；
- 能从稳定 step 或 checkpoint 恢复；
- 能用 mock CI 固化行为，用本地 Agnes integration 验证真实 provider 链路。

## 测试标准

测试覆盖率是底线，不是完成标准。每个预期功能点都必须有能够固化行为的测试。

| 测试层 | 覆盖内容 | 运行位置 |
| --- | --- | --- |
| Unit | 小函数、状态转换、错误分类、option 解析、输入校验 | CI + local |
| Contract | provider、checkpoint、tool、memory、A2A、channel、template 的最小公共语义 | CI + local |
| Conformance | ext / adapter / template 是否满足 core 契约 | CI + local |
| Golden trajectory | agent/template 的事件序列、step snapshot、run export、failure path | CI + local |
| Mock integration | 编排、provider、tool、A2A、Agent Mesh、Dev Agent 的稳定端到端行为 | CI + local |
| Real integration | Agnes provider、真实 streaming、tool call、template、Agent Mesh | local only |

新增功能必须声明测试归属。没有测试归属的功能不算完成。

## 本地 Agnes 测试

本地开发使用 `.env` 中配置的 Agnes provider 跑真实集成测试。

要求：

- `.env` 不进入 git；
- 测试通过 `-tags=integration` 显式开启；
- provider 配置从环境变量读取；
- 测试不得在输出、golden、日志或文档中泄露真实 token；
- 真实 provider 测试必须覆盖 streaming、tool call、structured output、reasoning / thinking 开关、错误分类和 cancel/timeout；
- 真实 provider 测试失败不能用 mock 通过替代。

推荐命令：

```bash
GOPRIVATE=github.com/gopact-ai/* go test -tags=integration -count=1 ./...
```

## CI Mock 测试

CI 只使用 mock、fake 或本地内存实现。

要求：

- 不依赖真实 provider；
- 不读取 `.env`；
- 不需要外部密钥；
- 不依赖公网服务可用性；
- 不把 integration tag 纳入默认 CI；
- mock 行为必须覆盖真实 provider 预期契约；
- 每个 mock fixture 必须表达明确能力点，而不是只返回成功。

推荐默认 CI gate：

```bash
git diff --check
go test -count=1 ./...
go test -race ./...
go vet ./...
```

## 功能覆盖矩阵

| 能力 | 必测行为 |
| --- | --- |
| Graph branch | 单分支、多分支、无目标、错误、checkpoint 后恢复 |
| DAG fan-in | 多前驱等待、部分失败、merge 顺序、deterministic reducer |
| Dynamic fan-out | 空 fan-out、N 个任务、部分失败、恢复只重跑未完成任务 |
| Graph loop | 条件退出、无限循环 step limit |
| Subgraph | nested run events、runtime ids、checkpoint 继承和隔离 |
| ReAct | direct final、tool call、multi-tool、tool error、approval interrupt/resume、max iterations |
| Plan-Execute | plan、execute、replan、cancel、approval、summary |
| Agent-as-Tool | child events、artifact refs、failure propagation、action scoping |
| Agent Mesh | discovery、call、stream、cancel、fallback、policy deny、policy review |
| Provider | generate、stream、tool call、structured output、usage、rate limit、auth error、timeout |
| Checkpoint/resume | completed step、interrupted step、canceled step、artifact verification、config drift |
| Evidence | run export validation、effect replay plan、verification report、failure attribution |
| Scaffold | init、run、example smoke test、missing config error、mock provider path |

## Release Gate

自举阶段的 release gate 至少要求：

- core mock CI 全绿；
- ext mock CI 全绿；
- examples mock CI 全绿；
- 本地 Agnes integration 有最近一次通过证据；
- graph conformance command 通过；
- golden trajectory 未漂移；
- run export schema validation 通过；
- verification report status 必须为 passed；
- release bundle 包含 run export、run effect replay、verification report、diff、test、policy、A2A task、checkpoint、artifact 和 review evidence；
- secret scan 通过，不存在敏感信息泄漏；
- public API boundary、public API examples、repository boundary、v1 migration 和 extension ecosystem readiness 检查通过。

`gopacttest.SelfBootstrapReleaseGateRequirements` / `CheckSelfBootstrapReleaseGate` /
`RequireSelfBootstrapReleaseGateForExport` 提供最小可自举 release gate 的可复用校验入口，覆盖
core mock CI、ext mock CI、examples mock CI、Agnes integration、run export、run effect replay、
graph conformance、release bundle、public API boundary、public API examples、repository boundary、v1 migration、
extension ecosystem readiness、extension ecosystem CI、secret scan、diff、file snapshot、command、A2A task、checkpoint、
artifact、golden trajectory、policy 和 review evidence，并要求 release export 已 completed、无
failure attribution、已封存同一份 verification report，verification report 已 passed 且与 run export
对齐。

## 文档与示例

每个阶段必须同步维护：

- design 文档；
- public API examples；
- examples 仓库 smoke test；
- conformance checklist；
- README 快速路径；
- failure path 说明。

## 非目标

自举路线不要求 core 内置：

- 真实生产注册中心；
- 云网关；
- 企业认证系统；
- hosted observability；
- 生产级 provider SDK；
- 业务 prompt 策略；
- 私有部署平台。

这些能力通过 ext、adapter、plugin、example 或宿主应用接入。
