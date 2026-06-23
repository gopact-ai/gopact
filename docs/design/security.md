# gopact 安全设计

日期：2026-06-23

设计入口：[index.md](index.md)

Agent runtime 的安全边界必须默认存在。`gopact` 不能等生产 adapter 接入后再补 policy、redaction、sandbox 和 audit。

## 信任边界

| 边界 | 默认信任级别 | 风险 |
| --- | --- | --- |
| model provider | 外部主体 | 数据外发、prompt 注入、工具误用 |
| tool result | 不可信输入 | tool injection、伪造指令 |
| sandbox process | 受限主体 | 文件破坏、网络访问、secret 泄漏 |
| skill package | 第三方代码/资料 | supply chain、隐藏脚本 |
| MCP server | 外部工具服务 | schema 欺骗、资源泄漏、权限扩大 |
| A2A agent | 外部 agent | 任务越权、artifact 污染 |
| memory store | 敏感状态 | 跨用户泄漏、过期事实污染 |
| artifact store | 文件输出 | 敏感文件导出、media type 欺骗 |
| plugin/exporter | 外部 sink | trace 泄漏、网络失败影响主流程 |
| transfer | 格式转换层 | 脱敏遗漏、格式注入、平台限制丢失 |
| channel adapter | 投递和交互层 | action spoofing、身份映射错误、数据误展示 |

## 基本原则

- 默认拒绝，显式授权；
- secret 不进入模型上下文；
- 外部返回内容默认不可信；
- 高风险工具默认 deferred；
- 大 payload 走 artifact ref；
- 所有外部动作走 policy；
- 所有敏感输出先 redaction 再 export；
- human approval 走 interrupt/resume；
- 事件流必须能审计每次授权和副作用。

## Policy 覆盖面

Policy 至少覆盖：

- provider/model allow/deny；
- provider fallback/degradation；
- model budget、region、data residency；
- sandbox command/filesystem/network/secret；
- tool visibility/promotion/call；
- memory read/write/search/delete；
- skill activation/resource/script；
- MCP connect/list/call/resource/prompt；
- A2A agent card/send/stream/cancel；
- artifact put/read/export；
- plugin exporter strict mode；
- channel send/action dispatch。

Policy deny 必须返回结构化错误，并产生 `PolicyDecided` 事件。

## Redaction

默认敏感字段：

- prompt 原文；
- tool args；
- tool result；
- memory content；
- sandbox stdout/stderr；
- secret 引用解析结果；
- user profile；
- provider raw request/response；
- artifact bytes。

规则：

- redaction 在外部 sink 之前执行；
- redaction 状态写入事件；
- 大 payload 记录 hash、size、schema、artifact ref；
- debug 模式可以保留更多内容，但必须显式配置；
- redaction 失败默认阻止外部 export。

## Sandbox 安全

默认策略：

- no network；
- no workspace escape；
- no shell unless explicitly allowed；
- no host env inheritance；
- command argv only；
- timeout and output limit；
- allowlist commands；
- denylist 只能作为补充，不能替代 allowlist。

本地 sandbox 仍然是安全边界，不是普通 shell helper。

## Tool 和 prompt injection

规则：

- tool result 不得自动变成 system/developer instruction；
- remote tool suggestion 不自动执行；
- MCP/A2A 返回的 tool schema 必须 namespaced；
- tool promotion 需要 policy decision；
- model 生成的文件路径、命令、URL 都要经过 sandbox/policy 校验；
- human approval message 必须展示被批准的实际动作，而不是模型摘要。

## Skill supply chain

规则：

- 第三方 skill 默认只读；
- skill script 默认禁用；
- skill 请求的 MCP server 单独授权；
- skill 资源读取产生事件；
- marketplace、签名、远程安装属于 adapter/plugin，不进入 core；
- skill metadata 可以用于选择，完整 instructions 只在激活时加载。

## MCP / A2A 安全

MCP：

- 每个 server 是独立 trust boundary；
- list_changed 只能刷新 registry，不能扩大权限；
- sampling/elicitation 需要明确 policy；
- remote resource 默认不进长期 memory。

A2A：

- remote agent 不继承本地权限；
- 本地 memory 默认不发送；
- artifact 必须校验 media type、size、hash；
- multi-hop delegation 需要独立 policy；
- A2A action 和本地 tool call 必须重新授权。

## Channel / transfer 安全

规则：

- A2UI/AG-UI/TUI/Lark/Web 只能消费事件、`SurfaceMessage` 和 artifact refs；
- transfer/channel adapter 不读取 Runner 内部状态；
- agent 只输出声明式 UI，不输出可执行前端代码；
- renderer 或平台使用 client 侧预批准 catalog；
- user action 通过 TurnLoop input/resume/action 边界回到 runtime；
- action payload 必须携带 channel/session/run/call/nonce 关联信息；
- Lark、Slack、Web 等外部 channel 的 action 必须校验签名或 nonce；
- artifact export 到 channel 前必须经过 policy；
- redaction 必须早于 transfer。

## 测试要求

- sandbox path escape 被拒绝；
- secret 不进入事件和 checkpoint；
- policy deny 有事件；
- tool result 中的指令不被当成 system instruction；
- remote MCP/A2A suggestion 不自动执行；
- memory scope 隔离；
- artifact export 需要 policy；
- redaction 早于 OTel/LangSmith exporter。
- redaction 早于 channel transfer；
- channel action spoofing 被拒绝。
