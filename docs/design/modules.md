# gopact 运行时模块设计

日期：2026-06-23

设计入口：[index.md](index.md)

本文定义 `gopact` 运行时必须具备的业务运行时模块：

- model provider routing 模块；
- tool registry 模块；
- sandbox 模块；
- memory 模块；
- skill 模块；
- MCP 模块；
- A2A 模块。

这些不是后续“锦上添花”的 plugin。它们是 agent runtime 的业务运行时模块，必须在第一版可用运行时里就有稳定契约、默认内存实现、事件观测和测试方式。

`artifact`、`policy`、`config`、`event`、`checkpoint` 是基础契约或支撑能力。它们支撑所有模块，但不归入上面的业务运行时模块清单。

## 设计立场

### 业务运行时模块的含义

业务运行时模块不等于第一版就内置所有生产后端。它必须满足：

1. core contract 稳定；
2. 有最小可运行实现；
3. 可通过事件流观察；
4. 可被 middleware/plugin 改写、审计或替换；
5. 可在不接真实云服务的情况下测试。

生产级后端应该通过 adapter 或 plugin 接入。核心不能直接绑定 OpenRouter、CC Switch、Docker、Kubernetes、LangSmith、mem0、某个 vector database、某个 MCP server registry 或某个 A2A gateway。

### 模块边界

| 模块 | 回答的问题 | 运行时角色 |
| --- | --- | --- |
| model provider routing | agent 如何在多 provider、多模型、多 fallback 策略间选择 | 模型调用边界 |
| tool registry | agent 如何管理当前可见工具、延迟发现工具和工具搜索 | 工具可见性边界 |
| sandbox | agent 如何安全执行代码、命令、文件操作 | 执行动作边界 |
| memory | agent 如何跨步骤、跨 thread、跨 session 记忆和召回 | 状态生命周期边界 |
| skill | agent 如何加载可复用的过程知识、脚本和资源 | 能力封装边界 |
| MCP | agent 如何连接工具、资源和 prompt server | agent-to-tool/data 边界 |
| A2A | agent 如何发现、调用、协同远程 agent | agent-to-agent 边界 |

这些模块必须共享同一组运行身份：`UserID`、`SessionID`、`ThreadID`、`RunID`、`AgentID`、`AppID`、`CallID`、`TraceID`。任何跨模块调用都要能追溯到这些 id。

## 总体架构

目标运行时由 Runner 编排，但模块实现不属于 Runner 内部。Runner 持有的是模块接口引用，具体实现由应用配置、默认实现或 adapter 注入。

```text
Runner
  -> Graph Runtime
  -> RunContext / RuntimeIDs
  -> Event Stream
  -> Checkpoint Store
  -> Plugin Lifecycle
  -> Runtime Modules through interfaces

Runtime Modules
  provider.Router
  tools.Registry
  sandbox.Manager
  memory.Store
  skill.Registry
  mcp.Manager
  a2a.Client / a2a.Server

Foundational Contracts
  event.Stream
  step.Export / step.Import
  checkpoint.Store
  artifact.Store
  policy.Policy
  gopact.ConfigSnapshot
```

关键原则：

- provider routing、tool registry、sandbox、memory、skill、MCP、A2A 都通过 runner 组合；
- artifact、policy、event、checkpoint、config snapshot 是基础支撑能力，通过 runner 注入并进入事件流；
- node/model/tool 不直接持有这些模块的具体实现；
- 所有模块动作都发事件；
- 所有外部边界都经过 policy 和 redaction；
- 默认实现用于本地开发和测试，生产实现通过 adapter 替换。

模块依赖应该保持单向：

- provider routing 只依赖模型 adapter、能力目录、健康状态、policy 和事件；
- tool registry 负责 visible/deferred tools、promotion、direct invocation 和 model-visible invocation guard，不负责执行本地命令；本地命令仍通过 sandbox；
- local tool 执行可以依赖 sandbox，sandbox 产物可以写 artifact；
- MCP/A2A 通过 bridge 把远程能力注册成 tool 或 node adapter，不直接污染 graph；
- skill 可以使用 sandbox、memory、artifact，但不直接管理 MCP/A2A 生命周期；
- policy 和 event stream 是横切能力，不能反向依赖具体模块。

## Model Provider Routing 模块

### 目标

Provider routing 负责模型调用边界。它不是简单的“把多个 API key 放进配置”，而是把一次 `ModelRequest` 映射成可审计的执行计划：

- 选择 provider；
- 选择 model；
- 选择 provider endpoint 或 gateway；
- 选择 fallback 链；
- 记录为什么这样选；
- 在错误、限流、超时、预算、能力不匹配或健康状态变化时自动切换。

这个模块吸收 OpenRouter 的两层思路：model routing 和 provider routing 应该分开。`gopact` 不能假设“换 provider”一定等价于“换 model”，也不能假设“换 model”一定仍满足原请求的工具调用、结构化输出、上下文长度、视觉或区域合规要求。

### 核心契约

```go
package provider

type Registry interface {
	Register(ctx context.Context, provider Provider) error
	Resolve(ctx context.Context, name string) (Provider, bool)
	List(ctx context.Context) ([]Info, error)
}

type Provider interface {
	Name() string
	Models(ctx context.Context) ([]ModelInfo, error)
	Generate(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error)
	Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error]
}

type Router interface {
	Plan(ctx context.Context, req RouteRequest) (RoutePlan, error)
	Generate(ctx context.Context, req gopact.ModelRequest) (gopact.ModelResponse, error)
	Stream(ctx context.Context, req gopact.ModelRequest) iter.Seq2[gopact.Event, error]
}

type RouteRequest struct {
	IDs       gopact.RuntimeIDs
	Request   gopact.ModelRequest
	Hints     Hints
	Attempt   int
	LastError error
	Metadata  map[string]any
}

type RoutePlan struct {
	Primary       Candidate
	Fallbacks     []Candidate
	Reason        string
	ConfigVersion string
}

type RetryDecision struct {
	Retry        bool
	Backoff      time.Duration
	NextRequest  *gopact.ModelRequest
	Reason       string
}

type FailoverDecision struct {
	UseFallback  bool
	Candidate    Candidate
	NextRequest  *gopact.ModelRequest
	Reason       string
}

type Candidate struct {
	Provider string
	Model    string
	Endpoint string
	Weight   float64
	Metadata map[string]any
}
```

Template 层可以继续依赖 root `ChatModel` 的最小 `Generate -> Message` 契约。需要接入 provider router 时，应用层使用 `gopact.AdaptStreamingModel(router)` 生成 `ChatModel`；支持 streaming 的 template 应检测 `gopact.StreamingModel` 并把 route planned、provider attempt、fallback started 和 model message events 原样纳入自身 event stream。

`provider.Provider` 是 core contract；具体商业模型服务都应该是 adapter。第一优先级是 OpenAI native adapter 和 Anthropic native adapter；第二优先级是 OpenAI-compatible generic adapter + provider profile，覆盖 GLM/BigModel、Z.AI、Volcengine Ark、Alibaba DashScope/Model Studio、OpenRouter、企业网关和本地模型服务。Gemini、Ollama、Bedrock、Vertex、DeepSeek、Moonshot、xAI、Mistral 等继续作为同一 adapter 仓的扩展目标。OpenAI-compatible gateway 的 gateway routing 参数必须留在 adapter metadata 或 typed route policy 中，不能污染 `ModelRequest` 的核心字段。

### Provider 支持目标

模型层是 LLM Agent SDK 的核心能力，但实现仍然分层：

- core：保留 provider-neutral `ModelRequest` / `ModelResponse`、stream event、tool call、structured output、usage/cost、capability、router、fallback 和 error taxonomy；
- native adapter：OpenAI 优先覆盖 Responses API / Chat Completions，Anthropic 优先覆盖 Messages API / tool use / streaming；
- OpenAI-compatible adapter：用 provider profile 处理 base URL、auth header、endpoint 形态、role/tool/structured output/streaming/reasoning/usage/error 差异，优先覆盖 GLM/BigModel、Z.AI、Volcengine Ark、Alibaba DashScope/Model Studio、OpenRouter 和企业网关；
- catalog adapter：可选接入 models.dev 这类模型目录作为 capability / limit / price hint，但不能替代真实 provider conformance，也不能让 core 主动联网或读取配置文件；
- conformance：每个真实 provider adapter 至少覆盖文本、streaming、tool call、tool result round trip、structured output、image/file 输入、context length error、rate limit/auth error classification、usage parsing 和 cancel/timeout。

### Provider 注入模型

SDK 不读取 provider 配置文件。应用层可以从任何配置系统读取 provider 信息，再转换成 typed registry 和 typed route set 注入。示例：

```go
registry := provider.NewRegistry(
	provider.Register("anthropic", anthropic.New(appSecrets.Anthropic)),
	provider.Register("openrouter", openaiCompatible.New(openaiCompatible.Options{
		BaseURL: "https://openrouter.ai/api/v1",
		APIKey:  appSecrets.OpenRouter,
	})),
	provider.Register("openai", openai.New(appSecrets.OpenAI)),
)

routes := provider.RouteSet{
	Routes: []provider.Route{
		{
			Name:     "coding-fast",
			Require: []provider.Capability{provider.ToolCalling, provider.Streaming},
			Candidates: []provider.Candidate{
				{Provider: "anthropic", Model: "claude-sonnet-4.5"},
				{Provider: "openrouter", Model: "anthropic/claude-sonnet-4.5"},
				{Provider: "openai", Model: "gpt-5-mini"},
			},
			Fallback: provider.FallbackPolicy{
				OnErrors: []provider.ErrorClass{
					provider.RateLimited,
					provider.Timeout,
					provider.Unavailable,
					provider.QuotaExceeded,
				},
				MaxAttempts: 3,
				Backoff:     500 * time.Millisecond,
			},
		},
	},
	Selectors: []provider.Selector{
		{When: provider.Match{Task: "coding", Tier: "premium"}, Use: "coding-fast"},
		{When: provider.Match{Needs: []provider.Capability{provider.JSONSchema}}, Use: "structured-output"},
	},
}
```

Route set 必须支持应用层热替换，但替换要原子化。每次 route plan 记录 `ConfigVersion`，方便 replay 和线上排障。

### 自动切换条件

Router 至少支持这些条件：

- request capability：tool calling、forced tool choice、JSON schema、vision、audio、streaming、long context、reasoning effort；
- runtime scope：`UserID`、`AppID`、`AgentID`、tenant、环境、用户 tier；
- task hint：coding、search、summarization、structured extraction、cheap background job；
- policy：provider allow/deny、region、data residency、BYOK、PII policy；
- budget：单次请求预算、用户预算、run 预算、token 上限；
- health：provider down、endpoint down、p95/p99 延迟、吞吐、错误率、circuit breaker；
- provider error：rate limit、timeout、quota、context length、moderation refusal、unavailable；
- request shape：上下文长度、是否有文件/图片、是否需要稳定 session stickiness。

自动切换不是无条件“失败就换”。必须先判断 fallback candidate 是否仍满足原请求的硬能力和 policy。比如一个请求要求 tool calling + structured output，fallback 到不支持 tool calling 的模型只能在配置明确允许 degradation 时发生。

### Retry 和 Failover 决策

Retry/failover 不能只是固定次数重试。Router 应该暴露决策点，让策略读取：

- 当前 `ModelRequest`；
- 失败 attempt 的 partial output；
- provider error；
- attempt 序号；
- 已用 token、latency、estimated cost；
- 当前 route plan 和 config snapshot version。

决策可以：

- 不重试，直接返回错误；
- 原样重试；
- 改写下一次输入后重试；
- 切换到 fallback candidate；
- 复用最近一次在同一 route 上成功的 model/provider，避免每次都从固定主模型开始试错。

任何改写输入、切换模型或复用上次成功模型的行为都必须进入事件流，便于 replay、审计和 trajectory test。

### 错误分类

Adapter 必须把 provider-specific error 映射成稳定错误类型：

- `ErrRateLimited`
- `ErrTimeout`
- `ErrUnavailable`
- `ErrQuotaExceeded`
- `ErrContextTooLong`
- `ErrContentFiltered`
- `ErrInvalidRequest`
- `ErrAuthFailed`

Router 不能只靠字符串匹配决定 fallback。错误类型要支持 `errors.Is` / `errors.As`，同时保留 provider 原始错误用于诊断事件。

### Session stickiness

长对话、prompt cache、provider session、工具调用历史和 reasoning cache 都会受模型切换影响。默认策略：

- 同一个 `ThreadID` 内优先保持 model/provider sticky；
- 如果因为错误触发 fallback，事件必须记录 sticky 被打破的原因；
- 流式输出开始后，默认不允许自动切换；除非 provider 没有产生任何 token，或 adapter 支持明确的 resumable generation；
- 切换 provider 后，adapter 必须清理 provider session cache，避免把旧 provider 的 state 泄露到新 provider。

### 事件

- `ModelRoutePlanned`
- `ModelRouteSelected`
- `ModelRouteSkipped`
- `ModelProviderAttemptStarted`
- `ModelProviderAttemptCompleted`
- `ModelProviderAttemptFailed`
- `ModelProviderRetryDecided`
- `ModelProviderFallbackStarted`
- `ModelProviderCircuitOpened`
- `ModelProviderHealthChanged`
- `ModelRouteSnapshotReloaded`

这些事件要包含 `RuntimeIDs`、route name、provider、model、attempt、error type、latency、token usage、estimated cost、config snapshot version 和 redaction 状态。

### 默认实现

- `provider.StaticRegistry`：内存 registry，用于注册 fake provider 和 adapter。
- `provider.StaticRouter`：接收 typed `RouteSet`，按候选顺序、能力和策略决策。
- `provider.OrderedFallback`：按候选顺序选择，并按错误分类 fallback。
- `provider.CapabilityFilter`：按 model capability 过滤候选。
- `provider.SimpleHealth`：内存健康状态和基础 circuit breaker。
- `provider.FakeProvider`：无外部依赖测试用 provider。
- `github.com/gopact-ai/gopact-adapters-model/openaicompatible`：已作为第一批外部 model adapter 从 core 迁出，因为 GLM/BigModel、Z.AI、Volcengine Ark、Alibaba DashScope/Model Studio、OpenRouter、很多企业网关和本地模型服务都能走这个协议形态。

OpenAI 和 Anthropic native adapter 是 M6 model adapter 的优先目标；Gemini、Bedrock、Vertex、DeepSeek、Moonshot、xAI、Mistral 等 native adapter 可以在独立 adapter module 里继续扩展。Core 只保留 contract、router 和 fake provider。

### 安全规则

- 不跨 region/data residency 自动 fallback，除非配置显式允许；
- 不把 BYOK 请求 fallback 到非 BYOK provider，除非配置显式允许；
- 不降低硬能力要求，除非 typed route policy 声明 degradation；
- 不重试非幂等 tool call。模型请求可以重试，工具副作用不能被 provider retry 连带重复执行；tool retry 必须通过 root `ToolRetryPolicy` / `ToolRetryDecision` / `ToolRetryMiddleware` 显式决策并只在 tool middleware 边界重跑下游 handler，默认要求 idempotency key；工具可通过 `ToolResult.Commit` 为 registry 默认 `tool_call` effect 声明 idempotency key；恢复 replay 可通过 `tools.CommitStore` / `WithReplayCommitStore` 接入宿主 commit ledger，生产级 exactly-once 持久化仍由 adapter/host 实现；
- usage、cost、prompt、tool args 默认经过 redaction 后再发到外部观测系统；
- router 失败必须产生事件，不能静默使用默认 provider。

## Tool Registry 模块

### 目标

Tool Registry 负责工具可见性和工具命名边界。它不是工具执行器，也不是 MCP/A2A 的替代品。

它必须解决：

- 哪些工具直接暴露给当前模型调用；
- 哪些工具只是候选工具，需要搜索或激活后才暴露；
- 工具名如何 namespace 化，避免 MCP server、skill、本地工具和 A2A agent 冲突；
- middleware 如何动态调整模型可见工具集合。

### 核心契约

```go
package tools

type Registry interface {
	Register(ctx context.Context, tool Tool, opts RegisterOptions) error
	Visible(ctx context.Context, scope Scope) ([]ToolInfo, error)
	Deferred(ctx context.Context, scope Scope) ([]ToolInfo, error)
	Search(ctx context.Context, query SearchQuery) ([]ToolInfo, error)
	Promote(ctx context.Context, names []string, scope Scope) error
}

type ToolInfo struct {
	Name        string
	Namespace   string
	Description string
	Schema      JSONSchema
	Source      Source
	Visibility  Visibility
	Metadata    map[string]any
}

type Visibility string

const (
	VisibleTool  Visibility = "visible"
	DeferredTool Visibility = "deferred"
)
```

桥接规则：

- local tool -> `tools.Registry`
- MCP tool -> namespaced deferred 或 visible tool；
- A2A remote agent -> tool adapter 或 node adapter；
- skill 提供的工具默认 deferred，激活后才进入 visible；
- tool search 本身可以作为一个受控 visible tool，但返回结果必须经过 policy。

### 可见性规则

- 默认暴露最小工具集合。
- 高风险工具默认 deferred，不能直接进入模型上下文。
- deferred tool 只能通过 tool search、skill activation 或 middleware promotion 进入 visible set；model-driven invocation 必须拒绝未 promote 的 deferred tool，宿主直接调用和 effect replay 才能使用 direct invocation。
- promotion 必须产生事件，并记录 `CallID`、来源、原因和 policy decision。
- 模型看到的 tool schema 必须是当前 visible set 的快照，不能在一次模型调用中静默变化。

### 事件

- `ToolRegistered`
- `ToolVisibleListed`
- `ToolDeferredListed`
- `ToolSearched`
- `ToolPromoted`
- `ToolVisibilityChanged`

## Sandbox 模块

### 目标

Sandbox 负责执行非模型动作，包括：

- 运行命令；
- 执行脚本；
- 读写工作目录内文件；
- 生成 artifact；
- 为 skill script、tool、MCP local server、A2A adapter 提供受控执行环境。

Sandbox 不是一个“shell helper”。它是安全边界。所有会触碰本地进程、文件系统、网络或 secret 的行为，都必须经过 sandbox policy。

### 核心契约

```go
package sandbox

type Manager interface {
	Create(ctx context.Context, spec Spec) (Session, error)
}

type Session interface {
	ID() string
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
	ReadFile(ctx context.Context, path string) (File, error)
	WriteFile(ctx context.Context, file File) error
	Close(ctx context.Context) error
}

type Spec struct {
	WorkingDir string
	Mounts     []Mount
	Env        map[string]string
	Network    NetworkPolicy
	Files      FilePolicy
	Limits     ResourceLimits
	Metadata   map[string]any
}

type Profile struct {
	Name              string
	AllowedCommands   []string
	AllowedReadPaths  []string
	AllowedWritePaths []string
	AllowedEnvKeys    []string
	Limits            ResourceLimits
	Metadata          map[string]any
}

type ExecRequest struct {
	Command []string
	Stdin   []byte
	Timeout time.Duration
	Metadata map[string]any
}

type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	Files    []FileRef
	Usage    ResourceUsage
}
```

默认实现：

- `sandbox.Local`：本地进程执行，但必须有工作目录 root、路径校验、命令 allowlist、env allowlist、timeout、输出大小限制。
- `sandbox.MemoryFS`：测试用文件系统，不执行真实命令。
- `sandbox.ProfileManager`：fail-closed profile wrapper，先按宿主传入的 `Profile` 检查 env key、allowed command、read/write path 和 resource limit，再调用底层 `Manager` / `Session`；它只做 SDK 原子边界，不替代生产级容器隔离。

生产 adapter：

- Docker；
- Firecracker；
- Kubernetes Job；
- remote sandbox service；
- 企业内部代码执行平台。

### 安全规则

- 默认拒绝网络；
- 默认拒绝读取工作目录外文件；
- 默认不传递宿主环境变量；
- 命令必须以 argv 形式传递，不能默认走 shell；
- 输出必须有 byte limit；
- 运行必须有 timeout；
- secret 只能通过受控 secret provider 注入；
- sandbox 事件必须记录命令、hash、exit code、resource usage，但默认不记录敏感 stdout。

Secret 不属于 sandbox session 的隐式环境。当前 root `SecretRef` / `SecretProvider` / `SecretValue` 提供宿主注入的 secret 原子契约，`NewPolicySecretProvider` 可在 `ResolveSecret` 前走 `PolicyBoundarySecret` / `PolicyActionResolve`，deny 不调用底层 provider，review 返回 approval interrupt；policy input 和 event 只携带 `SecretRef`、runtime ids 和 metadata，不携带 raw secret。更复杂的跨 agent/skill/MCP secret 继承模型仍由 adapter、plugin 或宿主 policy 定义。

当前 `sandbox.PolicyManager` 会把 session create、exec、read file 和 write file 包成 `PolicyBoundarySandbox` 请求。policy input 不包含 stdin/file payload，只包含命令、路径、大小和 metadata。需要更强隔离时，应用应把 policy wrapper 和生产 sandbox backend 一起注入，而不是直接把底层 backend 暴露给 agent template。

当前 `sandbox.Profile` / `sandbox.ProfileManager` 已提供第一片 sandbox profile contract。profile wrapper 默认 fail-closed：未在 allowlist 中的 command、read path、write path 和 env key 都会在调用底层 session 前被拒绝；`ResourceLimits` 会在 create 时写入 `Spec.Limits`，显式超过 profile limit 的请求会被拒绝。`gopacttest/promptinjectionconformance` 已为外部 prompt-injection detector/classifier adapter 提供最小合规测试；生产级网络隔离、seccomp、容器/rootfs、复杂 secret 继承授权模型、prompt-injection classifier 具体实现和策略调参仍属于外部 adapter、plugin 或宿主 policy 的责任。

### 事件

- `SandboxCreated`
- `SandboxExecStarted`
- `SandboxExecCompleted`
- `SandboxExecFailed`
- `SandboxFileRead`
- `SandboxFileWritten`
- `SandboxClosed`

## Memory 模块

### 目标

Memory 负责长期记忆，不替代 checkpoint。

必须拆分：

- checkpoint：thread-scoped graph snapshot，用于 resume/time travel/HITL；
- memory：cross-thread 或 cross-session 的可召回知识；
- runtime state：一次 run 中正在流动的业务 state；
- artifact：文件、图片、报告等版本化输出。

### 记忆类型

基础 memory 模块支持四类 memory：

| 类型 | 内容 | 示例 |
| --- | --- | --- |
| semantic | 用户或世界事实 | 用户偏好、组织信息 |
| episodic | 历史经历和执行轨迹 | 上次任务如何完成 |
| procedural | 可复用做法 | 特定团队的审批流程 |
| profile | 稳定画像 | 用户角色、语言、默认约束 |

### 核心契约

```go
package memory

type Store interface {
	Put(ctx context.Context, memory Memory) (MemoryID, error)
	Get(ctx context.Context, id MemoryID) (Memory, error)
	Search(ctx context.Context, query Query) (SearchResult, error)
	Delete(ctx context.Context, id MemoryID) error
}

type Memory struct {
	ID        MemoryID
	Scope     Scope
	Type      Type
	Content   Content
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Scope struct {
	UserID    string
	SessionID string
	ThreadID  string
	RunID     string
	AgentID   string
	AppID     string
}

type Query struct {
	Scope     Scope
	Text      string
	Types     []Type
	Limit     int
	MinScore  float64
	Metadata  map[string]any
}
```

默认实现：

- `memory.InMemoryStore`：内存 store，支持 scope filter、type filter、metadata filter、简单文本匹配。
- `memory.NoopStore`：禁用 memory 时使用。
- `memory.PolicyStore`：显式 wrapper，在 `Put` / `Get` / `Search` / `Delete` 前走 `PolicyBoundaryMemory`，deny 阻止底层 store，review 返回 approval interrupt，可选 event sink 接回 `PolicyRequested` / `PolicyDecided`。

生产 adapter：

- SQL；
- KV；
- vector database；
- mem0；
- LangGraph-style store；
- 企业画像系统。

### 写入路径

Memory 写入分两种：

- hot path：node/tool 运行中显式写入，影响当前 run；
- background path：run 完成后异步抽取，默认不影响当前 run。

基础 memory 模块只要求支持显式写入和搜索。自动抽取可以作为后续 middleware，但 memory contract 必须从第一版运行时存在。

当前 ReAct template 第一片提供显式热路径写入：用户通过 `WithMemory(store, WithMemoryExtractor(fn))` 启用，template 会在模型消息进入 state 后调用 extractor，补齐缺失的 runtime scope，写入 store，发出 `MemoryPut` 事件，并在当前 `call_model` step snapshot 中记录已 applied 的 `memory_put` effect。用户也可以配置 `WithMemoryWriteMode(MemoryWriteDeferred)`，让 template 只记录 pending `MemoryPut` event 和可重放 idempotent `memory_put` effect，不提前写入 store；宿主可以把该 effect 交给后台 executor、队列或 `memory.NewReplayHandler` 提交。若用户配置 `WithMemoryExtractMode(MemoryExtractDeferred)`，template 会连 extractor 都不在当前 run 调用，只在 `call_model` step snapshot 中记录带 final state 和 runtime ids 的 pending `memory_extract` effect；宿主可以在 worker、队列或 resume 流程中把该 effect 交给 `memory.NewExtractionReplayHandler`，由 SDK 原子执行器调用 extractor、补齐 runtime scope 并写入 store。默认不做自动抽取。

当前基础 memory policy wrapper 已覆盖直接 store 调用；template 或应用层如果绕过 wrapper 直接持有底层 store，则需要自行承担授权边界。

### 事件

- `MemoryPut`
- `MemorySearched`
- `MemoryDeleted`

后续如果宿主实现后台 worker，可以在 adapter/plugin 层补更细的 extraction lifecycle 事件，例如 `MemoryExtractionStarted`、`MemoryExtractionCompleted`、`MemoryExtractionFailed`。当前 SDK 第一片把 extraction request 记录为 step effect，并提供 `memory.NewExtractionReplayHandler` 作为可注册到 `EffectReplayRegistry` 的原子执行器。

## Skill 模块

### 目标

Skill 是可复用能力包，用来封装：

- 指令；
- 流程；
- 参考资料；
- 脚本；
- 模板；
- 测试样例；
- 需要的工具或 MCP server。

Skill 的价值是 progressive disclosure：启动时只加载轻量 metadata，任务匹配时再加载 `SKILL.md`，需要时再读取 references/scripts/assets。

### 格式

基础 skill 模块兼容 Agent Skills 的目录格式：

```text
my-skill/
  SKILL.md
  scripts/
  references/
  assets/
  tests/
```

`SKILL.md` 必须有 frontmatter。这里的 frontmatter 是 Agent Skills 文件格式的一部分，不是 `gopact` 的 SDK 配置文件：

```yaml
---
name: data-report
description: Create a data-backed executive report.
version: 0.1.0
---
```

`name` 和 `description` 是必需字段。其他字段由 `gopact` 扩展：

- `version`
- `requires`
- `permissions`
- `mcp_servers`
- `sandbox`
- `memory`
- `tags`

### 核心契约

```go
package skill

type Registry interface {
	List(ctx context.Context) ([]Descriptor, error)
	Load(ctx context.Context, name string) (Skill, error)
}

type Skill interface {
	Descriptor() Descriptor
	Read(ctx context.Context, path string) (Resource, error)
	Instructions(ctx context.Context) (string, error)
}

type Descriptor struct {
	Name        string
	Description string
	Version     string
	Tags        []string
	Permissions Permissions
}

type Selector interface {
	Select(ctx context.Context, input SelectionInput) ([]Descriptor, error)
}
```

默认实现：

- filesystem registry；
- static selector；
- metadata validation；
- script execution through sandbox；
- skill activation events。

当前第一片：

- `skill.Registry` 提供 register/get/search/activate 的进程内 registry；
- `skill.FilesystemLoader` 从显式传入的 skills root 读取 Agent Skills-style 目录，解析 `SKILL.md` frontmatter，生成轻量 `Skill` descriptor，并可注册到现有 `Registry`；
- `skill.PolicyRegistry` 覆盖 register/get/search/activate，使用 `PolicyBoundarySkill` 和 `PolicyActionCreate` / `PolicyActionGet` / `PolicyActionSearch` / `PolicyActionActivate`；
- `skill.ResourceReader` / `skill.ScriptRunner` 是 resource read 和 script exec 的原子接口，`skill.FakeResourceReader` / `skill.FakeScriptRunner` 用于测试和本地示例；
- `skill.FileResourceReader` 从本地 root 读取 skill resource，拒绝路径逃逸，并为 text/json/xml 类资源填充 `Text`；
- `skill.SandboxScriptRunner` 通过注入的 `sandbox.Manager` 创建 session、执行 `Script.Command + Args`、关闭 session，并返回标准 `ScriptResult`；
- `skill.PolicyResourceReader` 覆盖 resource read，使用 `PolicyBoundarySkill` / `PolicyActionRead`，policy input 携带 skill name、resource descriptor 和 URI；
- `skill.PolicyScriptRunner` 覆盖 script exec，使用 `PolicyBoundarySkill` / `PolicyActionExec`，policy input 携带 skill name、script descriptor、args、env key 列表和 stdin 字节数，不把 stdin 原文放入 policy input；
- policy deny 会阻止底层 registry/read/exec 动作，policy review 会返回 approval interrupt，policy requested/decided events 可通过可选 event sink 接回统一 event stream；
- remote install、signing registry、script artifact capture 和更完整的 limits/workdir 策略仍属于后续。

不在业务运行时模块中强制实现：

- marketplace；
- remote install；
- signing registry；
- automatic skill authoring。

### 安全规则

- 第三方 skill 默认不可信；
- skill 脚本必须通过 sandbox 执行；
- skill 不能直接拿 secret；
- skill 请求的 MCP server 必须单独授权；
- skill activation 必须进入事件流；
- 加载 references/assets 必须可审计。

### 事件

- `SkillDiscovered`
- `SkillActivated`
- `SkillLoaded`
- `SkillResourceRead`
- `SkillScriptStarted`
- `SkillScriptCompleted`
- `SkillScriptFailed`

## MCP 模块

### 目标

MCP 负责 agent-to-tool/data/prompt 的标准协议边界。

`gopact` 的基础 MCP 模块应该同时支持：

- MCP client：连接外部 MCP servers，把 tools/resources/prompts 接入运行时；
- MCP server：把 `gopact` 的 tools/resources/prompts 暴露给其他 MCP hosts。

### 官方边界

MCP 是 client-server 架构。Host 管理多个 MCP client，每个 MCP client 连接一个 MCP server。协议基于 JSON-RPC，传输层包括 stdio 和 Streamable HTTP。Server 暴露 tools/resources/prompts，Client 也可以暴露 sampling、elicitation、logging 等能力。

### 核心契约

```go
package mcp

type Manager interface {
	Connect(ctx context.Context, spec ServerSpec) (Session, error)
}

type Session interface {
	ID() string
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
	ListResources(ctx context.Context) ([]Resource, error)
	ReadResource(ctx context.Context, uri string) (ResourceContent, error)
	ListPrompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, name string, args map[string]any) (PromptContent, error)
	Close(ctx context.Context) error
}
```

桥接规则：

- MCP tool -> `gopact.Tool`
- MCP resource -> context provider 或 memory source
- MCP prompt -> skill/prompt material
- MCP notifications -> event stream + registry refresh
- MCP sampling/elicitation -> runtime interrupt 或 model callback

默认实现：

- stdio MCP client；
- Streamable HTTP MCP client，其中 POST + JSON/SSE response/notification、GET listen stream、continuous listen reconnect/retry、session/protocol header、DELETE session termination 和 404 session-expired 处理先落地；
- tool bridge；
- resource read；
- prompt get；
- sampling/elicitation handler contract + policy wrapper + JSON-RPC dispatch + HTTP/SSE dispatch；
- URL-mode elicitation completion notification handler 第一片；
- minimal MCP server exposing `gopact.Tool`。

后续增强：

- OAuth；
- MCP registry integration；
- tasks experimental primitive；
- 生产 adapter，以及后续协议版本中 URL-mode elicitation completion 被移除后的兼容策略。

当前第一片：

- `mcp.Manager` 可连接多个 MCP-like server，并聚合 namespaced tools/resources/prompts；
- `mcp.PolicyManager` 已覆盖 server connect 和 tools/resources/prompts list 的 `PolicyBoundaryMCP` 检查，支持 deny/review、policy requested/decided events 和 approval interrupt；
- `mcp.Client` / `mcp.FakeClient` / `mcp.PolicyClient` 已覆盖 client list tools/resources/prompts、tool call、resource read、prompt get 的 `PolicyBoundaryMCP` 检查，支持 deny/review、policy requested/decided events 和 approval interrupt；
- `mcp.SamplingHandler` / `mcp.ElicitationHandler` 已定义 server-initiated client capability 原子契约；`mcp.PolicySamplingHandler` / `mcp.PolicyElicitationHandler` 已把 sampling/createMessage 和 elicitation/create 纳入 `PolicyBoundaryMCP`，支持 allow/deny/review、policy requested/decided events 和 approval interrupt；`mcp.CapabilityServer` 已提供 sampling/createMessage 与 elicitation/create 的 JSON-RPC dispatch / newline serve loop 第一片；
- `mcp.LineTransport` 已提供 newline-delimited JSON-RPC 2.0 transport 第一片，支持 request/response、notification，以及在等待 response 时通过 `WithLineTransportRequestHandler` 处理带 id 的 interleaved server-to-client JSON-RPC requests，通过 `WithLineTransportNotificationHandler` 处理无 id 的 server-to-client JSON-RPC notifications；`mcp.HTTPTransport` 已提供 Streamable HTTP POST + JSON/SSE response 和 GET listen stream 第一片，支持 request JSON response、request-scoped SSE response、POST request-scoped SSE resume、通过 `WithHTTPTransportRequestHandler` 在 request-scoped / GET SSE 中处理带 id 的 interleaved server-to-client capability requests 并用 POST 写回 JSON-RPC response、通过 `WithHTTPTransportNotificationHandler` 在 request-scoped / GET SSE 中处理无 id 的 server-to-client notifications、保留未消费 notification 透传、notification `202 Accepted`、HTTP status error、bounded response body、`StreamEvent` 迭代、`ListenContinuously` GET SSE 重连、`retry` 字段等待、`Last-Event-ID` resume header、`MCP-Session-Id` capture/propagate、`MCP-Protocol-Version` header、`TerminateSession` DELETE、405 termination unsupported 映射和 404 session-expired 清理；`mcp.JSONRPCClient` 已支持 legacy initialize/initialized 兼容握手，并把 `tools/list`、`tools/call`、`resources/list`、`resources/read`、`prompts/list`、`prompts/get` 映射到 SDK `Client` 契约；
- `mcp.ElicitationCompleteHandler` / `mcp.ElicitationCompleteNotification` 已提供 URL-mode elicitation completion notification 的 typed 原子契约；`mcp.CapabilityServer` 已能 dispatch `notifications/elicitation/complete`，并通过 Line/HTTP transport notification handler 接入 server-to-client notification stream；
- `mcp.ToolServer` 已提供 minimal MCP server adapter 第一片，把 `tools.Registry` 中的 model-visible tools 暴露为 `tools/list` / `tools/call`，并提供 initialize、空 resources/prompts list 和 newline serve loop；
- 更完整的 MCP server capabilities、OAuth 和 registry integration 仍是后续项。

### 安全规则

- 每个 MCP server 是单独 trust boundary；
- tool name 必须 namespace 化，避免跨 server 冲突；
- remote MCP server 默认不允许访问 local secrets；
- MCP tool call 必须走 tool middleware；
- list_changed 通知只能刷新 registry，不能自动扩大权限；
- MCP result 必须经过 redaction 和 content size limit。

### 事件

- `MCPServerConnected`
- `MCPServerDisconnected`
- `MCPToolsListed`
- `MCPToolCalled`
- `MCPResourceRead`
- `MCPPromptLoaded`
- `MCPNotificationReceived`

## A2A 模块

### 目标

A2A 负责 agent-to-agent 协作边界。

它和 MCP 的边界必须清楚：

- MCP：让 agent 使用工具、资源、prompt；
- A2A：让 agent 委派任务给另一个 agent，并接收状态、消息和 artifact。

### 核心模型

基础 A2A 模块必须建模：

- AgentCard：远程 agent 的发现与能力声明；
- Task：跨 agent 的工作单元；
- Message：协作和澄清；
- Part：多模态内容片段；
- Artifact：任务产出；
- TaskStatusUpdateEvent；
- TaskArtifactUpdateEvent。

### 核心契约

```go
package a2a

type Client interface {
	GetAgentCard(ctx context.Context, endpoint string) (AgentCard, error)
	Send(ctx context.Context, req SendRequest) (TaskOrMessage, error)
	Stream(ctx context.Context, req SendRequest) iter.Seq2[StreamEvent, error]
	GetTask(ctx context.Context, id string) (Task, error)
	CancelTask(ctx context.Context, id string) error
}

type Server interface {
	AgentCard(ctx context.Context) (AgentCard, error)
	HandleSend(ctx context.Context, req SendRequest) (TaskOrMessage, error)
	HandleStream(ctx context.Context, req SendRequest) iter.Seq2[StreamEvent, error]
}
```

桥接规则：

- remote A2A agent 可以作为 `Tool`：输入是 task request，输出是 artifact refs；
- remote A2A agent 可以作为 graph `Node`：用于 multi-agent workflow；
- local `gopact` runner 可以暴露为 A2A server；
- A2A task id 映射到 `RunID` 或 `CallID`，context 映射到 `ThreadID`/`SessionID`；
- A2A artifacts 进入 artifact store，并通过事件流暴露。

当前第一片：

- `a2a.AgentCard`、`Task`、`Result`、`Agent`、`Registry` 和 `FakeAgent` 最小契约；
- `a2a.NewRunnableAgent` 已提供 local `gopact.Runnable` -> A2A `Agent` / `StreamingAgent` 的直接适配第一片；`Send` 会把 task input 转成 user message，把 final assistant message、artifact refs 和 child event count 聚合成 `a2a.Result`，`Stream` 会把本地 runtime message/artifact/completed/failed 投影成 `TaskEvent`；
- `a2a.NewHTTPAgent` 和 `a2a.NewHTTPHandler` 已提供 HTTP JSON/JSONL client/server wrapper 第一片；card/send/cancel 使用 JSON，task stream 使用 JSONL，`Auth` 只作为 sanitized task/context 透传，不读取配置文件、不持有 secret；
- `a2a.NewJSONRPCAgent` 和 `a2a.NewJSONRPCHandler` 已提供 JSON-RPC 2.0 + SSE client/server wrapper 第一片；method 覆盖 `SendMessage`、`SendStreamingMessage`、`CancelTask`，agent card 使用 well-known endpoint，text task 可映射到 A2A `message.parts[].text`，stream 使用 SSE `data:` frame，HTTP client、headers、response limit 和 card metadata 都由宿主 typed option 注入；
- `templates/agenttool.NewA2A` 可以把 direct `a2a.Agent` 包装成 `gopact.Tool`；
- A2A task id 映射到 child `CallID`，`RunID`、`ThreadID`、`UserID` 和 `ParentCallID` 会透传；
- `a2a.Result.Output`、artifact refs 和 metadata 会映射回 `ToolResult`；
- remote task send 会产生 `a2a_task_sent`、`a2a_task_completed` 或 `a2a_task_failed` 事件；
- direct A2A tool adapter 已支持 remote send 使用 `PolicyBoundaryA2A` / `PolicyActionSend`，会产生 policy requested/decided 事件，并在 deny/review 时阻止 remote send；
- direct A2A tool adapter 已支持 send timeout，timeout 会通过 context 传入 remote `Send`，并返回 failed task event；
- `a2a.Registry.Cancel` 和 `agenttool.A2ATool.Cancel` 已提供显式 task cancel 第一片；cancel 会走 `PolicyBoundaryA2A` / `PolicyActionCancel`，成功时产生 `a2a_task_canceled` 事件。
- `a2a.StreamingAgent`、`TaskEvent` 和 `TaskStatus` 已提供 remote task stream 第一片；`a2a.Registry.Stream` 可转发 streaming agent 的状态、消息和 artifact 更新，`agenttool.A2ATool.Stream` 会先使用 `PolicyBoundaryA2A` / `PolicyActionStream` 授权，再把 message、artifact update、running/completed/failed/canceled 状态映射成 SDK event stream，并保留 parent/child call chain、status message、metadata 和 artifact refs。
- `a2a.Discoverer`、`DiscoveryQuery` 和 `DiscoveryResult` 已提供 agent card discovery 第一片；`a2a.Registry.Discover` 会缓存 discovered card，返回 `a2a_agent_card_fetched` event evidence，`agenttool.WithCard` 可把 discovered card 用于 tool spec；agent card discovery 已有单 event golden trajectory fixture 固定。
- `a2a.Authenticator`、`AuthRequest` 和 `Auth` 已提供 auth context 第一片；`agenttool.WithAuth` 会在 policy/send/stream/cancel 前注入 sanitized auth 到 task/context，并把 scheme、principal、credential ref 写入审计 metadata。SDK 不读取配置文件、不持有 secret 原文。

后续默认 adapter：

- production agent discovery registry；
- official A2A proto/schema 完整 Task/Message/Artifact 数据模型；
- production discovery registry；
- resumable / production SSE streaming adapter；
- artifact store 深度转换。

后续增强：

- push notification；
- agent discovery registry；
- OAuth / advanced auth negotiation；
- resumable streaming；
- multi-hop delegation policy。

### 安全规则

- A2A remote agent 是外部主体，不继承本地权限；
- 委派必须经过 policy；当前 direct tool adapter 可通过 `WithPolicy` 显式接入，生产 adapter 必须默认接入宿主 policy；
- artifact 必须有 media type、size、hash；
- 不能把本地 memory 默认发送给远程 agent；
- 每次 A2A 调用必须带 `UserID`、`RunID`、`CallID` 和 audit metadata；
- 远程 agent 返回的 tool suggestion 不能自动执行，必须重新进入本地 policy。

### 事件

- `[done: first slice]` `a2a_agent_registered`
- `[done: first slice]` `a2a_task_sent`
- `[done: first slice]` `a2a_task_completed`
- `[done: first slice]` `a2a_task_failed`
- `[done: first slice]` `a2a_task_canceled`
- `[done: first slice]` `a2a_task_status_updated`
- `[done: first slice]` `a2a_agent_card_fetched`
- `[done: first slice]` `a2a_artifact_updated`
- `[done: first slice]` `a2a_message_received`

## Artifact 基础契约

Artifact 不是业务运行时模块，但 sandbox、A2A、channel/transfer、event、checkpoint 都依赖它。它是基础契约，必须有最小 contract 和默认实现。

```go
package artifact

type Store interface {
	Put(ctx context.Context, artifact Artifact) (Ref, error)
	Get(ctx context.Context, ref Ref) (Artifact, error)
	List(ctx context.Context, scope Scope) ([]Ref, error)
}

type Artifact struct {
	Name      string
	MediaType string
	Bytes     []byte
	URI       string
	Metadata  map[string]any
}
```

默认实现：

- in-memory artifact store；
- policy store wrapper：在 artifact `Put` / `Get` / `List` 前走 `PolicyBoundaryArtifact`，policy input 只包含 payload-free metadata、size 和 ref，避免把 artifact bytes 直接送入 policy/event；
- filesystem artifact store for local development；
- hash、size、media type metadata；
- payload integrity verifier。当前 `artifact.VerifyRef` / `artifact.VerifyRefs` 已提供第一片，`graph.WithArtifactVerifier` 已接入 step import/checkpoint load 边界，`artifact.RecordVerifyRefs` 已提供 verification evidence 桥接第一片，`artifact.NewReplayHandler` 已接入 effect replay verify；root `RecordModelCallCheck` 已提供已观察 model call evidence 桥接第一片，root `RecordToolCallCheck` 已提供已观察 tool call evidence 桥接第一片，root `RecordChannelEventCheck` 已提供已观察 channel event evidence 桥接第一片，root `RecordFailureAttributionCheck` 已提供已观察 failure attribution failed evidence 桥接第一片，root `RecordEffectReplayCheck` 已提供已观察 step-level effect replay evidence 桥接第一片，root `RecordRunEffectReplayCheck` 已提供已观察 run-level effect replay evidence 桥接第一片，`memory.RecordReplayCheck` 已提供 memory replay work evidence 桥接第一片，`templates/react.NewMemoryDeferredMemoryWorkQueue` 已提供本地 deferred memory queue 第一片，`templates/react.DeferredMemoryWorkWorker` 已提供 queue worker executor、统一 recorder 记录 worker pass `memory_replay` 与 retry/stop/dead-letter `memory_work_schedule` evidence、细粒度 pass/schedule recorder 第一片，`gopacttest/reactconformance` 已提供 deferred memory work queue conformance helper 第一片，包含 concurrent dequeue 不重复分发同一 job 的基础 contract 和 visibility timeout contract，`templates/react.RecordDeferredMemoryWorkScheduleCheck` 已提供 memory worker retry/stop/dead-letter schedule evidence 桥接第一片；ReAct template verification node、Dev Agent channel reviewer prompt/bridge adapter、CI reviewer adapter、远端 CI run -> `ci_gate` evidence 桥接、model reviewer adapter prompt/eval metadata governance 和 Lark callback source 已有第一片，更多 evidence 来源、model review 真实评测治理深化、CI provider 拉取/重跑/secret 治理、Lark 真实 client/plugin 和生产级 gate 策略后续补齐。

## Policy 基础契约

这些模块共享一套 policy 入口。Policy 不是业务运行时模块；它是所有外部动作的统一授权基础契约。

```go
type Policy interface {
	Authorize(ctx context.Context, req DecisionRequest) (Decision, error)
}
```

policy 要覆盖：

- provider/model allow/deny；
- provider fallback/degradation；
- model budget and data residency；
- sandbox command；
- sandbox filesystem；
- sandbox network；
- memory read/write/search；
- skill activation；
- skill script execution；
- MCP server connection；
- MCP tool call；
- A2A delegation；
- artifact export。

默认策略：

- 本地测试允许内存实现；
- 模型路由默认不允许跨安全域降级；
- 文件/命令/网络默认最小权限；
- 外部 MCP/A2A 默认需要显式授权；
- third-party skill 默认只读，脚本禁用，除非授权。

## 与 plugin/middleware 的关系

业务运行时模块不是 plugin，但可以被 plugin 增强。基础契约也可以有 adapter 或 plugin 增强，但语义不能被绕过。

| 模块/契约 | core contract | middleware/plugin 可做的事 |
| --- | --- | --- |
| provider routing | registry/router/model capability/health contract | external gateway adapter、route analytics、cost optimizer |
| tool registry | visible/deferred/search/promotion contract | namespace policy、tool analytics、dynamic filtering |
| sandbox | execution/session/file contract | command audit、resource limit、remote backend |
| memory | store/search/extraction contract | memory extraction、redaction、mem0 adapter |
| skill | registry/loader/selector contract | remote install、signature verification、marketplace |
| MCP | client/server/bridge contract | registry sync、OAuth、server health monitor |
| A2A | client/server/task contract | remote discovery、delegation policy、gateway |
| artifact | ref/store/hash/media type contract | filesystem/S3/GCS/R2/OSS backend、export policy |
| policy | decision request/decision contract | tenant policy、approval policy、external policy engine |

Middleware 仍然作用于 node/model/tool/event 边界。模块 adapter 不应该绕过 middleware。

A2UI、AG-UI、SSE、TUI、Lark bot、飞书卡片等不属于这里的运行时模块。它们应该作为 transfer、channel adapter 或 plugin 消费 event stream、`SurfaceMessage`、artifact refs 和 interrupt/resume 事件，把运行时轨迹翻译成目标平台 payload。Root package 已提供 `SurfaceMessage`、`Transfer`、`Channel`、`ChannelEvent` 和 `ChannelEvent.ResumeRequest()` 的第一片边界契约；`adapters/channel/tui` 已提供 writer-based TUI adapter 第一片，`adapters/channel/sse` 已提供 HTTP SSE adapter 第一片，`adapters/channel/lark` 已提供 host-injected Lark text/interactive payload、sender、callback source 和 action value decode 第一片，`adapters/channel/a2ui` 已提供 A2UI v0.9 JSON message transfer、JSONL channel、history replay、local catalog registry、structural validation、component JSON Schema validator 注入、client-supported catalog negotiation、in-memory reference renderer 和 action decode 第一片，`adapters/channel/agui` 已提供 AG-UI event transfer、HTTP SSE event stream channel 和 action POST 回流第一片，其他具体平台 adapter 仍放在 adapter/plugin 层。用户交互再以 input、resume payload 或受控 action 进入 TurnLoop，而不是让 channel adapter 直接调用 graph/node。

## 包布局建议

模块包布局：

```text
gopact
  event.go
  ids.go
  step.go
  export.go
  options.go
  model.go
  tool.go
  runner.go

graph
checkpoint
artifact
policy

provider
tools
sandbox
memory
skill
mcp
a2a
```

Adapter 包建议后置或独立 module：

```text
adapters/memory/mem0
adapters/memory/sqlite
adapters/model/openai
adapters/model/anthropic
adapters/model/openai-compatible
adapters/sandbox/docker
adapters/mcp/registry
adapters/a2a/http
adapters/channel/a2ui
adapters/channel/agui
adapters/channel/sse
adapters/channel/websocket
adapters/channel/lark
adapters/channel/tui
plugins/otel
plugins/langsmith
plugins/channel/larkbot
plugins/channel/tui
```

## 模块实现顺序

这些模块都属于第一版运行时范围，但实现仍要按依赖顺序推进：

1. API ergonomics examples、SDK `Setup`/defaults、默认 logger、`RuntimeIDs`、core contracts、event stream、runner root facade。
2. `gopact.ConfigSnapshot`、`artifact.Store`、`policy.Policy`，因为后续模块都依赖它们。
3. step export/import 与 checkpoint/resume contract，包含 step snapshot、checkpoint store、interrupt/resume record 和 cancel-safe point。
4. `provider.Registry` + `provider.Router` + fake/openai-compatible provider。
5. `tools.Registry` + visible/deferred tools + tool search。
6. `sandbox.Manager` + local/memory implementation。
7. `memory.Store` + in-memory implementation。
8. `skill.Registry` + filesystem loader + resource/script policy wrapper + local resource reader + sandbox script runner。
9. `mcp.Manager` + stdio/HTTP client + tool bridge。
10. `a2a.Client`/`Server` + task/message/artifact model。
11. 模块级 event assertion test helpers。

不要等 ReAct template 完成后再补这些能力。ReAct、planner、supervisor、多 agent graph 都应该建在这些模块之上。

## 测试策略

每个模块都必须有无外部依赖的测试：

- provider routing：capability filter、ordered fallback、错误分类、circuit breaker、config snapshot version、policy deny；
- tool registry：visible/deferred isolation、tool search、promotion policy、namespace collision；
- sandbox：路径逃逸、命令 allowlist、timeout、output limit；
- memory：scope isolation、type filter、metadata filter、delete；
- skill：frontmatter validation、progressive loading、script sandbox policy；
- MCP：fake server，tool list/call/resource/prompt/notification；
- A2A：fake agent card、send、stream status、artifact update、cancel；
- cross-system：skill script 通过 sandbox 生成 artifact，artifact 被 A2A task 返回，事件流可断言。

## 资料来源

- OpenRouter model fallbacks：https://openrouter.ai/docs/guides/routing/model-fallbacks
- OpenRouter provider routing：https://openrouter.ai/docs/guides/routing/provider-selection
- OpenRouter auto router：https://openrouter.ai/docs/guides/routing/routers/auto-router
- CC Switch：https://github.com/farion1231/cc-switch
- oh-my-pi model configuration：https://github.com/can1357/oh-my-pi/blob/main/docs/models.md
- Eino v0.9 agentic-runtime：https://www.cloudwego.io/zh/docs/eino/release_notes_and_migration/eino_v0.9._agentic-runtime/
- A2UI official site：https://a2ui.org/
- A2UI concepts：https://a2ui.org/concepts/overview/
- A2UI transports：https://a2ui.org/concepts/transports/
- MCP overview：https://modelcontextprotocol.io/docs/getting-started/intro
- MCP architecture：https://modelcontextprotocol.io/docs/learn/architecture
- A2A specification：https://github.com/a2aproject/A2A/blob/main/docs/specification.md
- Google A2A announcement：https://developers.googleblog.com/en/a2a-a-new-era-of-agent-interoperability/
- Agent Skills overview：https://agentskills.io/home
- Anthropic Agent Skills article：https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills
- LangGraph memory overview：https://docs.langchain.com/oss/python/concepts/memory
- LangGraph persistence：https://docs.langchain.com/oss/python/langgraph/persistence
