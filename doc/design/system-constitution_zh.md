# gopact 系统宪法

<!-- gopact:doc-language: zh -->

状态：十章条款已完成并通过组合一致性检查；2026-07-14 Run、Store 与历史语义修正已批准

本文是 `gopact`、`gopact-ext`、`gopact-examples` 三仓的最高层设计契约。重建期间，只有经过共同审议并写入本文的条款具有宪法效力。现有设计文档、代码、测试和示例都是证据，不因已经存在而自动成为正确设计。

2026-07-12，用户批准 [`Agent State and Identity Design`](../superpowers/specs/2026-07-12-agent-state-identity-design.md)，正式订正身份、Context、Memory 和执行存储术语。2026-07-14，用户又批准 [Run、Store 与历史语义对齐 RFC](../rfcs/run-store-history-alignment.md) 及 [ADR-0001](../decisions/0001-run-closure.md)、[ADR-0002](../decisions/0002-durable-store-authority.md)、[ADR-0003](../decisions/0003-history-policy.md)；本文已是两次修正后的现行条款，旧规格的状态以其页首为准。

## 权威顺序

系统宪法完成后，设计权威顺序固定为：

1. 本系统宪法。
2. 已接受的 ADR。
3. 已接受的 RFC。
4. 未被替代的已批准专题设计规格。
5. 从上述文档派生的 conformance / contract tests。
6. 实现。
7. 示例和说明文档。

下层与上层冲突时，必须修正下层或正式修宪，不能用当前实现反向覆盖已经批准的设计。

## 角色

- 用户是系统意图和宪法条款的最终裁决者。
- Codex 负责调查三仓证据、识别矛盾、提出备选方案和明确推荐、说明代价、记录裁决、传播影响并检查全局一致性。
- 文档、代码、测试、示例和历史提交只提供证据，不参与多数表决。

## 审议方法

采用从系统不变量向下推导的宪法会议，不逐行批准旧文档，也不从当前实现反推目标架构。

每个候选条款只处理一个可以独立裁决的问题。Codex 可以把多个候选条款一次性预写到批量审阅文档，用户可以按 ID 在一次反馈中批量裁决；每个候选条款的决策包仍必须包含：

1. 一句可验证的候选条款。
2. 条款保护的系统性质。
3. 历史意图和外部约束。
4. 当前文档、代码、测试和示例的证据。
5. 已发现的矛盾。
6. 两到三个可行方案。
7. Codex 的推荐及理由。
8. 代价、失败模式和受影响的后续条款。

候选条款只有在用户明确表示批准该含义后才能标记为 `ratified`。沉默、切换话题、要求继续调查或批准另一条款都不构成批准。

裁决状态只有以下四种：

- `ratified`：批准并写入本文。
- `rejected`：否决，理由保留在会议进程草稿中。
- `deferred`：证据或上游条款不足，暂缓审议。
- `superseded`：经正式修宪被新条款替代。

每章结束后必须执行一次组合一致性检查，确认逐条成立的规则放在一起仍然成立。

## 审议顺序

1. 系统使命、用户价值和明确非目标。
2. 核心概念及唯一术语。
3. 执行模型、状态迁移和状态所有权。
4. 三仓职责、包边界和依赖方向。
5. Workflow 的 compile、node、activation、route、join、循环、并行和嵌套语义。
6. Agent、Model、Tool、Context 和子 Agent 模型。
7. Session、Checkpoint、Resume、Interrupt、Effect、Fork 和 RunLog。
8. Event、Streaming、Tracing、Snapshot 和可观测性。
9. Provider、Plugin、Middleware、Store Adapter 和其他扩展机制。
10. Go 公共 API、错误模型、并发、安全、测试和发布门槛。

后续章节可以发现前置条款不完整，但不能静默改写前置条款；必须回到对应章节正式修订。

## 审议状态文档

实时进度保存在 [`system-constitution-session_zh.md`](./system-constitution-session_zh.md)，剩余候选项保存在 [`system-constitution-batch-review_zh.md`](./system-constitution-batch-review_zh.md)。前者是上下文恢复与裁决记录，后者是唯一活动批审表面；两者都不是第二份宪法。

每次审议批次结束前必须更新草稿中的：

- 当前批次和未决 ID。
- 本批次最后裁决。
- 已批准、否决和暂缓的条款。
- 新发现的文档、代码、测试和示例漂移。
- 受本批次裁决影响但尚未处理的事项。
- 下一批次的审阅入口。

发生上下文切换、对话压缩或新会话接续时，Codex 必须先完整读取本文件、会议进程草稿和仍在活动的批审文档，再开展调查或处理反馈。聊天记录不能作为唯一恢复来源。

## 纠偏门禁

系统使命、核心概念、状态所有权和依赖拓扑四个基础章节批准前，不修改架构实现。必要的独立缺陷修复必须与宪法纠偏分开，且不能预设未批准的架构结论。

基础章节批准后，Codex 根据已批准条款生成三仓漂移映射：

- `P0`：实现或文档违反架构不变量。
- `P1`：缺失必需语义、失败路径或契约。
- `P2`：公共 API、命名、工程体验或文档一致性问题。

每个纠偏切片必须引用对应宪法条款，同时修改必要的实现、测试和文档。执行中发现宪法冲突时停止该切片并回到审议流程，不能在代码中临时发明规则。

## 防漂移机制

- 每个长期有效的关键条款最终都应有最小可执行检查。
- 依赖方向、禁止包和公共 API 形状优先使用 import / AST contract test。
- 状态迁移、恢复、失败和并发语义使用行为测试或故障注入测试。
- 新设计和实现计划必须列出所依据的宪法条款。
- 修宪必须记录替代条款、原因、影响范围和迁移要求。
- 在 Go 1.27 与相关工具链正式稳定前，不使用已知会触发 Staticcheck 泛型方法缺陷的全量 `golangci-lint run ./...` 作为验证手段。

## 完成标准

系统宪法只有在以下条件同时满足时才算完成：

- 不存在未决的基础架构问题。
- 每个仓库和公开包都有唯一存在理由和合法依赖方向。
- 每类长期状态和每次状态迁移都有唯一所有者。
- Agent、Workflow 和嵌套运行的执行、身份、恢复与事件语义无矛盾。
- 每个公开能力都能追溯到已批准条款。
- 所有已发现漂移都已进入纠偏计划，或被明确接受、暂缓或删除。

## 元条款

| ID | 条款 | 状态 |
|---|---|---|
| `META-001` | 采用从系统不变量向下推导、证据驱动、逐条裁决的宪法会议流程。 | `ratified` |
| `META-002` | 现有文档、代码、测试和示例都是证据，不是自动权威。 | `ratified` |
| `META-003` | 每轮只裁决一个独立问题，并在章节结束时检查组合一致性。 | `superseded` |
| `META-004` | 每轮结束前必须更新会议进程草稿；上下文恢复必须先读取宪法和草稿。 | `ratified` |
| `META-005` | 四个基础章节批准前，不修改架构实现。 | `ratified` |
| `META-006` | 宪法与下层材料冲突时，只能修正下层或正式修宪。 | `ratified` |
| `META-007` | 候选条款只有经用户明确批准才能成为宪法；不得从沉默或上下文切换推定批准。 | `ratified` |
| `META-008` | 多个独立候选条款可以预写到同一批量审阅文档并由用户按 ID 批量批准、否决、修改或暂缓；每个 ID 仍独立裁决，依赖未决的下游条款保持候选状态，章节结束后仍执行组合一致性检查。 | `ratified` |

### 元条款修订记录

2026-07-11，用户明确要求停止逐轮聊天审议，改为把所有剩余候选项写入文档后批量反馈。`META-008` 因此取代 `META-003` 的“一次只处理一个候选项”限制，但保留原子 ID、明确批准、依赖顺序和章节组合一致性检查；批量反馈不得被解释为对未点名候选项的默认批准。

2026-07-14，用户接受 Run、Store 与历史语义对齐 RFC 和 ADR-0001 至 ADR-0003。`SYS-004`、`CON-001`、`CON-002`、`CON-004`、`EXEC-002` 至 `EXEC-004`、`REPO-003`、`AGENT-001`、`AGENT-002`、`AGENT-004`、`DUR-001` 至 `DUR-004`、`OBS-001` 至 `OBS-004`、`EXT-001`、`EXT-003`、`API-002` 与 `API-004` 升级为下文现行版本；各条 2026-07-11/12 的原批准事实仍保留，但冲突语义由本次修正替代。

## 第一章：系统使命、用户价值和明确非目标

状态：已完成（组合一致性检查通过）

### `SYS-001` Agent-first 产品，Workflow-native 架构

状态：`ratified`

批准日期：2026-07-10

> `gopact` 是一套面向 Go 开发者的 Agent-first、Workflow-native ADK：它以优雅、可靠、白盒的 Agent 开发与运行体验作为首要产品价值，以领域无关且可独立使用的 Workflow 作为唯一官方执行内核。官方 Agent 必须由 Workflow 实现；用户的最小 Agent 实现合法，但不自动获得恢复、控制与历史保证。Agent 领域能力留在 Agent 层，不进入 Workflow runtime，也不形成第二套官方执行引擎。

### `SYS-002` 首要 API 用户与采用边界

状态：`ratified`

批准日期：2026-07-11

> `gopact` 首先服务于把 Agent 作为可长期维护的软件能力嵌入真实 Go 服务或产品的应用开发者和 Agent 作者；默认开发路径优先保证业务流程可读、类型安全和可测试，并沿同一心智模型渐进接入可观测、持久化与恢复能力。平台工程师、快速原型用户和独立 Workflow 用户都应被明确支持，但不主导 Agent 产品叙事、默认抽象或路线图优先级。

### `SYS-003` 三仓产品形态与平台边界

状态：`ratified`

批准日期：2026-07-11

> `gopact`、`gopact-ext` 和 `gopact-examples` 共同交付可嵌入、可由用户在自有进程与基础设施中独立运行的 Go SDK/ADK、官方扩展和可执行示例，而不是一个运行 Agent 所必需的托管平台；Agent 与 Workflow 的构建、执行、持久化、恢复和观测不得依赖 gopact 托管服务，未来的可视化观测与控制、分布式执行、团队治理或托管能力必须通过公开契约作为可选产品或适配层接入，不得把平台专有依赖、状态模型或网络可用性前提反向写入 core。

### `SYS-004` 首个成熟版本的核心产品闭环

状态：`ratified`

批准日期：2026-07-11

> `gopact` 的首个成熟版本必须至少通过一个包含 Model 决策与 Tool 执行的非玩具官方 Agent 场景及其跨三仓端到端验收，证明业务流程可由可读、类型安全的 Go 代码表达，同一业务流程代码无需改写即可使用默认 memstore 或显式 SQLite 后端运行；自然流转、失败、terminate、业务 Retry、interrupt-resume、typed jump-to 与 Fork 必须形成统一可查询、不可变且带 source lineage 的 metadata 历史，业务数据只经显式安全 payload 或引用进入历史。该验收不表示持久化是所有 Workflow 或用户最小 Agent 的执行前提，Agent 或 Provider 数量、UI、分布式执行和托管能力均不能替代这一核心产品闭环。

### 第一章组合一致性检查

状态：通过

`SYS-001` 定义 Agent-first、Workflow-native 的产品与架构方向，`SYS-002` 固定首要用户和渐进采用路径，`SYS-003` 固定独立 SDK/ADK 与可选平台的单向边界，`SYS-004` 把前三条转成 Agent-first 端到端成熟验收。四条的产品主语、目标用户、交付形态和成功标准一致，不存在互相覆盖或双重执行内核。

## 第二章：核心概念及唯一术语

状态：已完成（组合一致性检查通过）

### `CON-001` 核心执行概念

状态：`ratified`

批准日期：2026-07-11

> 系统的一等执行概念固定为：Workflow Definition 是构建完成后保持不可变、可并发复用且不持有跨 Run 状态的流程定义；Workflow Execution 聚合一个 root Run 及其 child/source lineage Runs；Run 是一次从 Start 到 `completed`、`failed` 或 `terminated` 的不可变闭合 timeline，只有 `interrupted` 或 lease-expired `running` 可以 Resume 同一 Run。失败后的业务 Retry、typed jump-to、Fork、跨 DefinitionVersion 迁移以及显式新 Start 都创建新 Run；Node 自动 retry 只在同一非终态 Run 内创建新 Attempt。Node、Activation、Attempt 与 Execution Revision 各自保持独立语义。

### `CON-002` 身份与谱系

状态：`ratified`

批准日期：2026-07-11

> Runtime 产生的每个执行事实必须携带其语义层级所需的 SessionID 与 RunID。
> SessionID 可由调用方显式复用，以关联跨任务、跨 Definition 的多个 Run，不是唯一执行身份，也不是认证、授权或租户隔离凭据；应用必须在 core 外完成访问控制，core 不定义 tenant/scope 模型。
> root Run 不携带 ParentRunID，child Run 必须以 ParentRunID 指向直接父 Run；RunID 与 ActivationID、AttemptID、RevisionID 等细粒度执行 ID 在各自作用域及所属 Store 生命周期内不得重新分配，写入 durable Store 后必须跨进程稳定，且任何 ID 不得兼任另一层身份。
> 外部 telemetry TraceContext 由 Go `context.Context` 传播，不作为 Workflow 领域身份持久化。

### `CON-003` Context 与状态分类

状态：`ratified`

批准日期：2026-07-11

> Go `context.Context` 只传播取消、deadline、调用域值和外部 telemetry context；Workflow Context/state 是一次 Run 内节点可读写、typed、可由已配置 Store 保存的业务执行状态；Agent Context 是一次 LLM 调用最终可见的 provider-neutral `gopact.ModelRequest`。runtime scheduler state 由 Workflow runtime 所有，Session 只是关联多个 Run 的 ID，semantic Memory 是业务显式调用的外部数据源，Event 与 Snapshot 是观察或读取模型。Workflow Definition 不持有任何跨 Run 状态，各类语义不得相互替代，持久化数据不得包含客户端、函数、channel 等进程对象。

### `CON-004` 控制操作、NodeExecutionVersion 与查询术语

状态：`ratified`

批准版本：v2

批准日期：2026-07-11

> 每个 Run 从 Start 开始，由 natural flow、同 Run Node 自动 retry 和合法 Resume 推进，最终只形成一个不可变终态。业务 Retry、typed external jump-to 与 Fork 以新 Run 执行，并用 `SourceRunID` 加 `SourceRevisionID` 或 `SourceEventSeq` 记录因果来源；不定义 Restart 或终态 Reopen。`Terminate` 只作用于本进程活动执行，不能改变已终止 Run。Run 内 Workflow route、branch 或 loop 仍是 natural flow；每次 Node Attempt 实际开始前为 `(RunID, NodeID)` 分配不复用的递增 NodeExecutionVersion。纯查询不得创建 Attempt、Revision 或状态迁移。

### 第二章组合一致性检查

状态：通过

`CON-001` 定义闭合 Run 与跨 Run source lineage，`CON-002` 用 SessionID、RunID 和 ParentRunID 定义关联与直接父子谱系，并把授权留给应用，`CON-003` 分离三种 Context、scheduler state 与外部 Memory，`CON-004` 固定 natural flow、控制与纯查询。NodeExecutionVersion 只作为 `(RunID, NodeID)` 范围内的执行序号，不能替代 ActivationID、AttemptID、RevisionID、Run sequence 或 source lineage；外部 TraceContext 也不取代领域身份。

## 第三章：执行模型、状态迁移和状态所有权

状态：已完成（组合一致性检查通过）

### `EXEC-001` 唯一执行引擎

状态：`ratified`

批准日期：2026-07-11

> Workflow runtime 是官方能力中唯一有权创建 Activation、调度 Attempt、形成 Execution Revision、推进 Run head 和执行控制命令的引擎；Workflow-backed Agent 只能通过构建、包装和调用 Workflow 提供领域能力，不得拥有第二套 scheduler、checkpoint/resume 状态机或 event identity 分配器。用户最小 Agent 若不接入 Workflow，不得宣称具备这些框架保证。事实由默认 memstore 还是用户注入的 backend 保存，不得改变 Workflow 调度语义。

### `EXEC-002` 状态推进与 Store 边界

状态：`ratified`

批准版本：v4

批准日期：2026-07-11

> Workflow runtime 独自决定状态迁移，Store 不得反向决定调度。Workflow 通过 `WithStore` 接收唯一公开 Store interface，该接口组合 `Checkpointer`、`CheckpointHistory` 与 `runlog.FencedLog`；配置后，它是 durable 恢复与历史的唯一权威。durable 写入、Interrupt acknowledgement 与 fencing 必须 fail closed，失败时不得继续推进；只有不参与执行正确性的 observer 可以 best-effort。未配置 Store 时 core memstore 只提供进程生命周期内的能力。

### `EXEC-003` Attempt 提交与 exactly-once 边界

状态：`ratified`

批准版本：v3

批准日期：2026-07-11

> 每次 Node body 执行前，runtime 必须形成含 ActivationID、AttemptID、NodeExecutionVersion、Run sequence 与触发来源的 AttemptStarted metadata；每个 Attempt 最多形成一个 terminal outcome，并记录 phase、status 与 error。Runtime 不自动复制业务 input、Workflow Context 或 output 到执行历史；业务只能显式写入有界安全的 `Event.Payload` 或 `PayloadRef`/`ArtifactRef`。natural flow 再次选择同一 Node 时创建新 Activation，Node 自动 retry 在同一非终态 Run 创建新 Attempt，两者都分配新 NodeExecutionVersion。框架不承诺业务副作用 exactly-once。

### `EXEC-004` 可转移执行所有权

状态：`ratified`

批准版本：v4

批准日期：2026-07-11

> 单进程默认模式不需要 lease。共享 durable backend 的每次状态写入必须携带 Store 授予的 fencing token/generation，mismatch 时 fail closed；lease 过期的 `running` Run 可由新执行者在同一 Run 上恢复，其余 Retry、Jump 或跨版本迁移都创建带 source lineage 的新 Run。任何操作不得覆盖旧历史、复用 identity 或伪装成自然流转。Day 0 不实现 distributed scheduler、worker discovery、queue 或 control plane。

### 第三章组合一致性检查

状态：通过

`EXEC-001` 固定 Workflow runtime 为唯一执行引擎；`EXEC-002` 固定一个 fail-closed durable Store 权威；`EXEC-003` 让每次 Node 执行形成 metadata 事实并把业务 payload 与副作用责任留给业务；`EXEC-004` 只在共享执行所有权时启用 fencing，不引入分布式控制面。默认 memstore 与显式 backend 不改变调度语义，也不形成第二权威状态源。

## 第四章：三仓职责、包边界和依赖方向

状态：已完成（组合一致性检查通过）

### `REPO-001` 三仓职责

状态：`ratified`

批准日期：2026-07-11

> `gopact` 只承载稳定公共事实、唯一 Workflow runtime、Agent 领域 facade/builders、Store/observability contracts、测试契约和无需外部依赖的 in-memory execution backend；`gopact-ext` 承载 concrete Agent、provider、SQLite 及其他可选 backend/plugin adapter；`gopact-examples` 只作为真实外部消费者提供可运行 reference scenario，不拥有框架语义。

### `REPO-002` 包依赖方向

状态：`ratified`

批准日期：2026-07-11

> core 内共享 facts 包不得依赖 runtime 子包，`workflow` 可以依赖共享 facts 与领域无关的 Store/history contracts，`agent` 可以依赖 `workflow` 和共享 facts，但 `workflow` 不得依赖 `agent`、provider 或 concrete Agent；ext 只依赖 core，examples 可以依赖 core/ext，任何反向 import 或通过 callback 隐藏的反向状态所有权都非法。

### `REPO-003` 跨仓公共边界与模块隔离

状态：`ratified`

批准日期：2026-07-11

> 三仓之间只能通过公开、可独立发布的 Go module API 协作，`internal` import、`go.work`、本地 `replace`、测试专用 hook 和未发布源码路径都不得成为产品契约。ext 的目标公开模块固定为 `github.com/gopact-ai/gopact-ext` 与 `github.com/gopact-ai/gopact-ext/stores`，现有 package path 保持不变；测试 module 不发布。

### 第四章组合一致性检查

状态：通过

`REPO-001` 固定 core、official extensions 与 examples 的唯一职责，`REPO-002` 把 Agent-first、Workflow-native 的所有权关系落实为单向 package DAG，`REPO-003` 固定 ext 两个公开 module 与不发布的测试 module。core 内置 in-memory execution backend 不要求用户部署外部基础设施，SQLite 只是 ext 中首批官方持久化 adapter；该 backend 不是 semantic Memory。ext 和 examples 只能消费 core 公开契约，不能反向定义执行语义。

## 第五章：Workflow 执行模型

状态：已完成（组合一致性检查通过）

### `WF-001` Definition 与不可变执行计划

状态：`ratified`

批准日期：2026-07-11

> Workflow Definition 只在构建期可变，进入可执行状态前必须一次性验证 identity/version、typed node/edge、entry/exit、route/join、可达性和静态限制，并固定 immutable、可并发复用且不持有 Run 状态的执行计划；公共 API 可以由同一个 Workflow 类型直接 Invoke，不强制暴露独立 Compiled 类型，但首次 Invoke 不得再隐式修改拓扑。

### `WF-002` Node、Activation 与 Attempt

状态：`ratified`

批准日期：2026-07-11

> Node 是带稳定 NodeID、typed input/output 和执行函数的不可变定义；每次满足调度条件都创建新的 Activation，loop、fan-out 和重复路由不得复用 ActivationID；每个 Attempt 在执行前固定读取的 input 与 Workflow Context revision，Node 是否幂等及其外部副作用由业务负责。

### `WF-003` Route 与 Dispatch

状态：`ratified`

批准日期：2026-07-11

> Node 完成后只能通过 typed Route 结果产生零个、一个或多个 Dispatch，runtime 必须先在当前 Run 中固定 route decision 与 source revision 这一 natural-flow fact，再 materialize downstream Activation；Node body、middleware 和 plugin 都不得直接调用 scheduler、修改 ready queue 或伪造 Dispatch。

### `WF-004` Join 与动态源集合

状态：`ratified`

批准日期：2026-07-11

> Join 必须按显式 correlation scope 聚合输入，并由声明的 source expectation、source-set close fact 和 settle policy 决定何时触发；每个 join bucket 最多 materialize 一次 downstream Activation，缺失、失败、取消和迟到输入的处理必须形成明确 Run 事实，并在配置的 Store 生命周期内可查询，不能用“等待所有静态前驱”猜测动态 fan-out 已结束。

### `WF-005` Loop、并行、fan-out 与 settle

状态：`ratified`

批准日期：2026-07-11

> Loop 的每次迭代和 fan-out 的每个分支都创建独立 Activation 并受显式 step/parallelism/budget 上限约束；node body 可以并发或未来跨机器执行，但同一 Run 的状态迁移必须形成按 version 排序的确定序列，completion order、settle winner、loser cancel/supersede 和释放进度一经固定即不得因 resume 重新选择；是否持久化该序列由 Store 配置决定。

### `WF-006` Workflow 组合、错误与取消

状态：`ratified`

批准日期：2026-07-11

> 可复用 graph fragment 只在执行计划固定前以 namespaced NodeID 内联到同一 Workflow，运行期调用另一个 Workflow 必须创建显式 child Run 与 parent lineage；未配置策略的 node/child error 默认失败当前 Run，retry 必须显式，取消向所有 running child/branch 传播，所有受管 goroutine 退出后才能形成 terminal revision。

### 第五章组合一致性检查

状态：通过

`WF-001` 固定可变构建期与不可变、无状态执行计划的边界，但不强制公开 Compiled 类型；`WF-002` 分离 Node、Activation 与 Attempt；`WF-003` 先固定 route fact 再 materialize downstream；`WF-004` 用 correlation、expectation 与 close fact 收敛动态 Join；`WF-005` 允许并发执行但要求同一 Run 形成 version 全序并固定 settle 决策；`WF-006` 区分构建期 fragment 与运行期 child Run，并统一错误和结构化取消。事实由默认 memstore 或显式 backend 保存，不引入运行期可变拓扑、重复 identity、恢复后重新决策或第二 scheduler。

## 第六章：Agent、Model、Tool、Context 和子 Agent

状态：已完成（由既有条款推导并经用户授权确认）

### `AGENT-001` Agent 是 Workflow 特化

状态：`ratified`

> 官方 Agent 是由 Agent Identity、领域配置和一个 Workflow 组成的 facade，其 Invoke/streaming 必须委托该 Workflow 执行。用户实现最小 `Agent` interface 仍合法，但除非同样接入 Workflow，否则不获得框架的恢复、控制与历史保证。Workflow-backed Agent 可以隐藏 graph；其窄化 control surface 可暴露 Snapshot、业务 Retry 与进程内 Terminate，但不得暴露 raw Workflow、private graph 或任意 `ForceJumpTo(private NodeID, any)`。

### `AGENT-002` Model 与 Tool 边界

状态：`ratified`

> Model、Tool、ToolCall、ToolOutcome 和结构化输出属于 Agent/domain 层，通过 typed Workflow node 或 node 内受观测边界执行；Workflow runtime 只理解通用 node metadata/error/control facts。Provider adapter 可以拥有协议归一化、transport retry/backoff 与连接韧性，但不得拥有业务编排；Tool 的业务拒绝、可回灌错误、interrupt 与基础设施失败必须用 typed outcome 区分。

### `AGENT-003` Agent Context、Session 与 Memory

状态：`ratified`

> Agent Context 的权威表示是最终 `gopact.ModelRequest`，由业务或具体 Agent 算法在显式 typed Workflow node 中构造；只有被显式写入该请求的 Workflow state、Observation、调用输入、Tool 结果或外部检索结果才对 LLM 可见。core 已删除且不再提供通用 ContextManager，Session 只是关联 ID，semantic Memory 是显式 Workflow node 调用的外部数据源；三者均不形成隐式 Agent state 或第二 scheduler/checkpoint 语义。

### `AGENT-004` 子 Agent、Human Input 与 Harness

状态：`ratified`

> 子 Agent 是以 child Run 调用的另一个 Workflow-backed Agent，必须继承 SessionID、以 ParentRunID 表达直接 lineage，并继承或收窄 caller 的 cancellation、budget/policy constraints 与 Go `context.Context` 中的外部 telemetry context；不传播或持久化伪领域 trace identity。Human Input 通过 Workflow interrupt/resume 表达；Agent harness 只能组合 Workflow node middleware 与 Agent-domain model/tool/subagent middleware、guard 和 policy，任何扩展都不得调度 Activation、形成 Revision、分配 identity 或扩大上游限制。

### 第六章组合一致性检查

状态：通过

四条共同保证官方与 Workflow-backed Agent 拥有完整领域体验但不形成第二 runtime；用户最小 Agent 保持合法而不继承未接入的保证。Model、Tool、Agent Context、Human Input 与子 Agent 映射到 Workflow 执行事实，SessionID 只做关联，semantic Memory 由业务显式接入。

## 第七章：持久化、恢复与执行控制

状态：已完成（由既有条款推导并经用户授权确认）

### `DUR-001` Journal、Checkpoint 与 Snapshot

状态：`ratified`

> Runtime 始终按 Run version 形成不可变 Revision 与控制 metadata；默认 memstore 在进程生命周期内保存这些事实，配置的 Store 将它们作为 durable history。Checkpoint 是绑定某个 Revision、runtime-owned、versioned、opaque 且可能敏感的恢复物化；Snapshot/View 只从 Store 事实、公开 checkpoint metadata 与受控 payload/artifact refs 生成，不得解析 checkpoint payload 或成为写入状态源。

### `DUR-002` Store interface 与共享 backend

状态：`ratified`

> Workflow 公开一个完成恢复语义所需的 Store interface，组合 `Checkpointer`、`CheckpointHistory` 与 `runlog.FencedLog`，并只通过 `WithStore` 配置；未提供时 Runtime 使用 core memstore。注入 backend 不改变调度语义，但其 durable 写入、Interrupt acknowledgement 与 fencing 都是 fail-closed 权威边界。Core 不定义 service locator、注册表或第二个状态配置入口。

### `DUR-003` Interrupt 与 Resume

状态：`ratified`

> Interrupt 是 Workflow 在 stable boundary 主动进入的可恢复暂停态；只有 Store 已 durable 记录等待 Activation、InterruptID、resolution schema/ref 与 context revision 后才能 acknowledgement。Resume 仅在定义兼容时继续 `interrupted` Run，或在 fencing 接管后继续 lease-expired `running` Run，并通过条件更新至多一次消费 resolution；其他 crash 后继续方式创建新 Run。Terminate 是不可 Resume 的进程内终止命令，取消信号不能冒充 Interrupt。

### `DUR-004` Retry 与 external jump-to

状态：`ratified`

批准版本：v2

批准日期：2026-07-11

> Node 自动 Retry 在同一非终态 Run、同一 Activation 下创建新 Attempt。失败后的业务 Retry、typed `Workflow.JumpTo` 与 `Snapshot.Fork` 都创建新 Run，并以 `SourceRunID` 加 `SourceRevisionID` 或 `SourceEventSeq` 记录来源；代码内 route 永远不是 jump-to。跨 DefinitionVersion 必须创建新 Run，由业务显式迁移输入、Context 与目标。不存在 Restart、同 Run terminal Reopen、`CheckpointController.Reopen` 或 Agent 任意 private Node jump。

### `DUR-005` Effect 与业务幂等边界

状态：`ratified`

> Node、Model、Tool 或外部系统操作是否幂等、使用何种幂等键或补偿策略由业务/domain adapter 负责；框架保证普通 Resume 不重做当前 Store 已确认完成的 Attempt，并记录 AttemptID、EffectID 与结果确定性 metadata。业务 input/output 仅在显式安全 payload 或引用中保存。任何重做都创建新 Attempt；业务 Retry/jump-to 同时创建 source-lineage 新 Run。框架不宣称 exactly-once side effect。

### 第七章组合一致性检查

状态：通过

默认 memstore 与 durable backend 共享调度语义但承诺不同生命周期；普通 Resume 只继续真实暂停或 lease-expired running，业务 Retry、typed jump-to、Fork 与跨版本迁移通过 source-lineage 新 Run 保留历史，业务副作用责任不转移给框架。

## 第八章：Event、Streaming、Tracing、Snapshot 和可观测性

状态：已完成（由既有条款推导并经用户授权确认）

### `OBS-001` 最终输出、过程事件与历史事实

状态：`ratified`

> Invoke 只返回 typed final output/error，typed output streaming 只承载业务输出增量，live Event 用于过程订阅，配置的 Store 用于事后查询；这些通道不得互相冒充。Runtime 始终产生 identity/type/phase/status/error 等过程 metadata，但不自动把业务 input、Context 或 output 复制到历史。未监听且未保存的 event 不承诺补回，没有 durable Store 时不承诺跨重启历史。

### `OBS-002` Event identity、顺序与投递

状态：`ratified`

批准日期：2026-07-11

> 每个 runtime Event 必须携带适用的 SessionID、RunID，并在适用时携带 ParentRunID、NodeID、ActivationID、AttemptID 与 RevisionID，同时包含 event type、origin、UTC timestamp 和 Run 内单调 sequence；冗余 ExecutionID 已删除。Store 中参与恢复、Interrupt 或 fencing 的事实必须 fail closed；只有纯 observer 可以声明 best-effort 或 at-least-once，且不得改变 Runtime 顺序或 scheduler state。业务内容仅能显式放入有界安全的 `Event.Payload` 或引用。

### `OBS-003` Query、View 与 Snapshot

状态：`ratified`

> Query/View 只从当前 Store 已保存的 metadata facts、公开 checkpoint metadata 与具备完整性/权限信息的 `PayloadRef`/`ArtifactRef` 构建；View 不解析 opaque checkpoint payload、不运行 Workflow、不补猜未保存或已丢失的数据，缺口必须明确报告。

### `OBS-004` Streaming、Tracing 与前端

状态：`ratified`

> Output stream、live Event subscription、serialization 与 Query/View 必须支持 context cancellation、默认有限的资源上限和明确 backpressure/overflow 策略；领域 trace、metrics、日志与未来前端只能从标准 Event/View 投影，基础设施 telemetry 可以包装 runtime 或 adapter。前端不得解析 checkpoint 或实现调度逻辑；只有 Store 保留对应事实时才允许按 sequence cursor 补齐，否则明确报告缺口。

### 第八章组合一致性检查

状态：通过

最终输出、业务流、live event 与可查询历史各自保持单一语义；白盒能力来自统一事实，而历史可用期由 Store 决定，观测层不成为第二状态机。

## 第九章：Provider、Plugin、Middleware、Store Adapter 和扩展机制

状态：已完成（由既有条款推导并经用户授权确认）

### `EXT-001` 扩展机制分类

状态：`ratified`

> Adapter 只把外部技术实现翻译成 consumer-owned contract，Provider 是 Model adapter 特化，Middleware 只包装明确执行边界，Plugin 只做编译期注册；Plugin 不拥有资源 lifecycle。扩展不得成为 service locator、拥有 scheduler state、绕过 Runtime/control invariants，或把 Agent 领域概念放进 Workflow core。

### `EXT-002` Interface 所有权与 conformance

状态：`ratified`

> Public interface 由消费行为的 core/domain package 定义并保持最小；Workflow Store 只组合恢复必需的 `Checkpointer`、`CheckpointHistory` 与 `runlog.FencedLog`。adapter 返回具体类型并通过官方 conformance suite 证明其声明语义；更强 durability、CAS、共享或投递能力通过接口和文档显式表达，不建立反射式 capability registry。此处 in-memory 表示执行状态保存介质，不是 Agent semantic Memory。

### `EXT-003` 扩展生命周期与故障隔离

状态：`ratified`

> Middleware/Adapter 按实例显式构造并由应用 owner 管理启动、关闭与 goroutine；Plugin 仅在编译期注册这些扩展，不接管资源。领域 telemetry 从 Event/View 投影，基础设施 telemetry 可包装 runtime/adapter；非关键 observer 可 best-effort，durable Store 与安全边界不得静默降级。Provider adapter 可管理协议和 transport resilience，但不得编排业务。

### 第九章组合一致性检查

状态：通过

扩展只实现或包装公开边界，不获得 Runtime 状态所有权；small interface 与具体实现保持 Go 原生，Plugin 只做编译期注册，资源 lifecycle 由应用 owner 管理。

## 第十章：Go 公共 API、错误、并发、安全、测试和发布

状态：已完成（由既有条款推导并经用户授权确认）

### `API-001` Go API 人体工程学

状态：`ratified`

> 公共 API 保持 Go 原生：显式 constructor、具体返回类型、consumer-owned interface、必要时 functional option，普通函数自然成为 Node/Tool，只在真实减少类型擦除时使用 Go 1.27 generics；同一能力只保留一个主入口，Workflow 可以直接 Invoke，并只通过 `WithStore` 切换默认 in-memory execution backend 与 SQLite 等 persistent backend；不为内部不可变计划强制增加公开 Compiled 类型，禁止反射式 DI、隐藏全局 registry 和仅为改名存在的 wrapper。

### `API-002` 错误、取消、并发与资源安全

状态：`ratified`

批准日期：2026-07-11

> 阻塞操作必须接收并传播 context.Context，取消/deadline 递归作用于 child/branch；预期失败以支持 errors.Is/As 的 error 返回，panic 在 runtime boundary 转为 failed fact；固定执行计划可安全并发复用且不持有 Run 状态。框架拥有的 step、并行度、嵌套深度、buffer、serialization 与 query 必须提供默认生效且可覆盖的安全上限；业务 payload 只在显式安全边界进入历史。SessionID 不是 auth，应用负责授权和隔离。内部 identity、revision、lineage 与 scheduler metadata 自动生成并默认隐藏。

### `API-003` 验证与安全门禁

状态：`ratified`

> 正式发布前必须在三仓以真实 module 依赖通过 gofmt、离线 deterministic tests、go test、race、go vet、govulncheck、公共 API compile examples、跨三仓 Agent-first 端到端验收以及按风险选择的 fuzz/failure injection；凭证不得入库，真实 provider 只做隔离 smoke test；已知会触发 Go 1.27 泛型缺陷的全量 golangci-lint 在上游修复和实测稳定前不得成为门禁。

### `API-004` 版本、兼容与三仓发布

状态：`ratified`

> 首个对外成熟版本以 Go 1.27 正式稳定工具链为基线；v1 前允许通过下一个 minor 做有迁移说明的 breaking 收敛而不保留双轨 compatibility layer，v1 后遵守语义化版本。发 tag 前必须以协调源码完成跨仓 E2E，发 tag 后必须用 `GOWORK=off` 和精确 tag 复验公开模块；RC 只能称为生产评估候选，stable 经 burn-in 后才可称为 production-ready。发布兼容矩阵与迁移说明。

### 第十章组合一致性检查

状态：通过

API 默认路径只暴露业务所需概念，元数据与安全默认值不要求普通用户配置；测试与发布门槛保护并发、模块和安全边界，同时避开已确认失控的 lint 工具链。
