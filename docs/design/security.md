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
- secret ref resolve；
- plugin exporter strict mode；
- channel send/action dispatch。
- TurnLoop resume action。

当前代码已完成第一批 policy 覆盖：model/tool middleware、A2A send adapter、memory store wrapper、sandbox manager/session wrapper、artifact store wrapper、skill registry/resource/script wrapper、trace exporter wrapper、root `PolicyChannel`、root `PolicySecretProvider`、MCP manager policy wrapper、MCP client policy wrapper 和 TurnLoop resume policy gate 第一片。sandbox 已有 `Profile` / `ProfileManager` fail-closed profile wrapper 第一片，可在底层 sandbox manager/session 前限制 allowed commands、read/write path、env key 和 resource limit，并在 create 时把 profile limit 写入 `Spec.Limits`；生产级容器隔离、网络隔离、seccomp、复杂 secret 继承授权模型，以及生产级 prompt-injection classifier、策略调参和业务展示仍由后续 adapter/plugin/host policy 深化。root `SecretRef` / `SecretProvider` / `SecretProviderFunc` / `SecretValue` 已提供 secret provider 原子契约第一片：SDK 只接收宿主传入的 reference 和 provider，不读取 env/file/config，`SecretValue.String`、`fmt` 和 JSON 输出都固定 redacted，只有显式 `Bytes()` 会返回原始 secret 拷贝；`NewPolicySecretProvider` 可在 `ResolveSecret` 前发起 `PolicyBoundarySecret` / `PolicyActionResolve` 检查，deny 不调用底层 provider，review 返回 approval interrupt，policy input/event 只携带 `SecretRef`、runtime ids 和 metadata，不携带 raw secret；`gopacttest/secretconformance` 已提供宿主或外部 secret provider adapter 可复用的 presence、canceled context、invalid ref、fixture resolve、redaction 和 byte-copy 最小合规测试；`gopacttest/promptinjectionconformance` 已提供外部 prompt-injection detector adapter 可复用的 presence、canceled context、clean/risky fixture、finding 完整性、request immutability 和 raw payload leak 最小合规测试。model I/O redaction 已有 root `RedactModelRequest`、`RedactModelResponse` 和 `ModelIORedactionMiddleware`，可在 model middleware chain 中于 provider call 之前脱敏 request messages/tool schema/metadata，并在 provider call 之后脱敏 response message/route metadata/metadata；model rate limit 已有 root `ModelRateLimiter`、`ModelRateLimiterFunc` 和 `ModelRateLimitMiddleware`，可在 provider call 前等待宿主注入 limiter，SDK 不内置限流算法、不读取配置文件；tool result redaction 已有 root `RedactToolResult` 原子函数和 `ToolResultRedactionMiddleware`，可在 tools registry middleware chain 中于 tool handler 之后脱敏 `ToolResult.Content` 与 string-like metadata；event redaction 仍通过 `EventRedactionMiddleware` 写入 `Event.Redaction` 状态。memory/sandbox/artifact/skill/channel/MCP/exporter wrapper 都只消费外部注入的 store/manager/registry/reader/runner/channel/exporter/client、policy 和可选 event sink，不读取配置文件；TurnLoop resume gate 也只消费宿主通过 `WithTurnPolicy` 注入的 `Policy`、通过 `WithTurnJSONSchemaValidator` 注入的可选 `JSONSchemaValidator` 和可选 metadata，不读取配置文件；policy deny 会阻止底层动作，policy review 会返回 approval interrupt。A2A 当前已有 `Discoverer` / `DiscoveryQuery` / `DiscoveryResult` agent card discovery 第一片、`Authenticator` / `AuthRequest` / `Auth` auth context 第一片、`NewHTTPAgent` / `NewHTTPHandler` HTTP JSON/JSONL client/server wrapper 第一片，以及 `NewJSONRPCAgent` / `NewJSONRPCHandler` JSON-RPC 2.0 + SSE client/server wrapper 第一片；HTTP/JSON-RPC wrapper 只通过宿主 typed option 注入 endpoint、client、headers、response limit 和 card metadata，不读取配置文件，不持有 secret 原文；auth 只携带 scheme、principal、credential ref 与宿主提供的 metadata，secret 原文必须留在宿主 secret provider 或 transport adapter 内，不能进入事件、checkpoint、模型上下文、discovered card metadata 或 HTTP/JSON-RPC task payload 之外的持久化记录。Channel 当前已有 root `SurfaceMessage` / `Transfer` / `Channel` / `ChannelEvent` 契约、send/receive policy wrapper、writer-based TUI adapter、HTTP SSE adapter、host-injected Lark adapter/callback source 和 A2UI v0.9 transfer/JSONL channel/history replay/schema catalog validation/component JSON Schema validator 注入/client-supported catalog negotiation/in-memory reference renderer/action decode 第一片，以及 AG-UI event transfer/HTTP SSE channel 第一片；Lark 第一片支持 outbound action value 携带签名/nonce，callback source 通过宿主注入的 `CallbackVerifier` / `ActionVerifier` 接入 raw callback 签名校验、nonce 防重放和 action 防伪，不持有密钥或配置；真实 Lark client/OAuth/plugin、artifact export policy、用户身份映射，以及完整前端 A2UI renderer、完整 JSON Schema engine adapter/plugin 与 conformance 深化、更完整 catalog negotiation 深化、AG-UI WebSocket/plugin 等生产级 adapter 安全边界仍是后续项。Skill 当前覆盖 filesystem registry loader、registry register/get/search/activate、resource read 和 script exec 的 policy 第一片，并已有 local `FileResourceReader` 和 sandbox-backed `SandboxScriptRunner`；remote install、signing registry 和生产级脚本 artifact/limits 策略仍是后续项。Exporter 当前已覆盖 trace `SpanRecord` export、HTTP/JSON 外发 adapter、OTLP/HTTP JSON 外发 adapter、LangSmith-compatible HTTP run exporter、LangGraph-style HTTP event exporter 和 export policy wrapper 第一片；真实 LangSmith Go SDK、dataset/evaluation/feedback/run query、LangGraph 专项 exporter 的 redaction/policy 深化仍是后续项。MCP 当前覆盖 manager connect/list tools/resources/prompts、newline JSON-RPC transport、newline interleaved server-to-client request handler、newline notification handler、Streamable HTTP POST + JSON/SSE response transport 第一片、POST request-scoped SSE resume、Streamable HTTP GET listen stream、HTTP/SSE interleaved server-to-client capability request dispatch、HTTP/SSE notification handler、URL-mode elicitation completion notification handler 第一片、continuous listen reconnect/retry、session/protocol header、DELETE session termination 与 404 session-expired 处理第一片、legacy initialize/initialized 兼容握手、JSON-RPC client、`ToolServer` minimal server adapter、sampling/elicitation handler contract、policy wrapper 与 `CapabilityServer` JSON-RPC dispatch 第一片，以及 client list tools/resources/prompts、tool call、resource read、prompt get 的 policy 第一片；MCP registry/OAuth 集成仍是后续项。

Policy deny 必须返回结构化错误，并产生 `PolicyDecided` 事件。Policy review 必须转成 `InterruptApproval`，通过 interrupt/resume 回到运行时，不能在 middleware 内阻塞等待人工审批。

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
- redaction 状态写入事件，trace span 只保留 `redaction.applied` / `redaction.field_count` 等低基数字段用于证明边界已执行；
- 大 payload 记录 hash、size、schema、artifact ref；
- debug 模式可以保留更多内容，但必须显式配置；
- redaction 失败默认阻止外部 export；
- 非关键 exporter 可通过 PluginHost fallback 把失败写入 `plugin_subscriber_errors` metadata 并继续业务 run。

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

当前 root `PromptInjectionDetector` / `PromptInjectionGuardMiddleware` 已提供第一片模型输入检查边界。detector 在宿主信任边界内检查原始 `ModelRequest`；检测到 finding 后，middleware 用 `PolicyBoundaryModel` / `PolicyActionInspect` 发起 policy gate，deny 不调用底层 model，review 返回 approval interrupt。进入 policy/event 的 `PromptInjectionPolicyInput` 只携带 `PromptInjectionReport` finding 摘要，不携带 raw prompt。`gopacttest/promptinjectionconformance` 已提供外部 detector/classifier adapter 可复用的 presence、canceled context、clean/risky fixture、finding 完整性、request immutability 和 raw payload leak 最小合规测试；生产级 classifier、跨来源风险归因、策略调参和面向用户的解释展示仍由 adapter、plugin 或宿主实现。

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
- sampling/elicitation 必须先过 MCP policy handler，再进入 model callback、runtime interrupt 或 UI/channel adapter；
- remote resource 默认不进长期 memory。

A2A：

- remote agent 不继承本地权限；
- 本地 memory 默认不发送；
- artifact 必须校验 media type、size、hash；
- remote send/stream/cancel 必须能走 `PolicyBoundaryA2A`，并分别使用 `PolicyActionSend` / `PolicyActionStream` / `PolicyActionCancel`；当前 direct tool adapter 的 `WithPolicy` 已覆盖 deny/review，生产 adapter 不能绕过该边界；
- remote send 必须有 timeout；当前 direct tool adapter 的 `WithTimeout` 已覆盖调用级 deadline；
- remote HTTP/JSON-RPC adapter 只能发送 sanitized `Auth` 和 task payload/metadata extension；transport credential 必须由宿主注入的 client/header/signer 持有，不能落入 SDK 配置、事件或 checkpoint；
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
