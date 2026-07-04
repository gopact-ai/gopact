# gopact Agent Mesh

<!-- gopact:doc-language: zh -->

[英文文档](./agent-mesh.md)

## 中文

日期：2026-06-30

Agent Mesh 是 `gopact` 面向垂域 agent 集群的分布式运行形态。它的目标是让每个垂域 agent 像微服务一样可启动、可发现、可调用、可治理、可观测，并通过 A2A 协议进行跨 agent 协作。

## 目标

`gopact` 应支持低门槛拉起垂域 agent，并把多个 agent 组织成可协作的 agent 集群。

用户应该能够：

- 快速创建一个垂域 agent；
- 自动暴露标准 agent card；
- 通过 A2A 接收任务、返回结果和流式进度；
- 被本地或远程 registry 自动发现；
- 通过名称、能力、标签或 metadata 被其他 agent 路由调用；
- 在跨 agent 调用中保留 runtime identity、policy decision、artifact refs 和 run evidence；
- 以最少配置启动本地开发集群，并平滑迁移到生产服务发现和网关。

## 定位

Agent Mesh 不是新的私有 agent 协议。它是 `gopact` 基于 A2A 的 Go-native runtime、scaffold、discovery、policy 和 evidence layer。

Core 保留稳定抽象：

- agent card；
- task request / result / event；
- agent registry；
- discovery contract；
- local runnable agent adapter；
- transport-neutral client/server contract；
- policy、event、runtime identity 和 run export 映射。

生产级 discovery、gateway、service mesh、认证系统和外部 registry 通过 adapter 或 plugin 接入。

## 目标体验

本地开发路径应该足够短：

```bash
gopact agent init support-agent
gopact agent run ./support-agent
gopact mesh up
```

也可以直接从本地垂域 agent cluster 开始：

```bash
gopact agent init-cluster support-cluster \
  -agent triage:support.triage:"Classify support requests." \
  -agent docs:knowledge.search:"Search product documentation." \
  -agent billing:billing:"Handle billing questions."
```

应用代码应该足够直接：

```go
agent, err := gopact.NewAgent(
	gopact.WithName("support-agent"),
	gopact.WithModel(model),
	gopact.WithTools(search, ticket),
)
```

跨 agent 调用应该像 RPC client 一样自然：

```go
result, err := mesh.Call(ctx, "research-agent", gopact.Task{
	Input: "Find the latest incident summary.",
})
```

能力路由应该支持按 skill 或 capability 选择 agent：

```go
result, err := mesh.Route(ctx, gopact.RouteTask{
	Require: []gopact.AgentCapability{"code.review", "git.diff"},
	Input:   "Review this patch.",
})
```

## Agent Card

每个 agent 必须暴露稳定 agent card。Agent card 至少描述：

- agent name；
- description；
- endpoint；
- protocol bindings；
- skills；
- capabilities；
- input/output schema；
- streaming support；
- artifact support；
- auth requirements；
- owner / version / metadata；
- health and readiness hints。

HTTP agent 默认应支持 well-known discovery path：

```text
/.well-known/agent-card.json
```

Agent card 是发现、路由、工具化和治理的共同入口。

## Discovery

Agent Mesh 应支持分层发现：

| 模式 | 用途 |
| --- | --- |
| in-memory registry | 单进程测试和本地 demo |
| file registry | 本地开发集群 |
| HTTP well-known discovery | 直接发现远程 agent |
| HTTP card registry | 本地集群和轻量部署中的 lease registration / heartbeat |
| static registry | 简单生产部署 |
| external registry adapter | Consul、etcd、Kubernetes、Nacos 或企业服务发现 |

Core 只定义 discovery contract 和轻量实现。当前 lightweight HTTP registry 支持按 TTL 注册/续约 agent card，并可在导入和发现时过滤未 ready 的 HTTP agent card，供本地集群、example 和 adapter conformance 使用；它不替代生产注册中心的一致性、选主、权限和驱逐能力。外部服务发现系统放在 adapter 层。

## Server Scaffold

`gopact` 应提供 agent server scaffold，把一个本地 `Runner`、template 或 handler 暴露为 A2A-compatible agent。

默认 server 应提供：

- agent card endpoint；
- task send endpoint；
- task stream endpoint；
- task cancel endpoint；
- health endpoint；
- readiness endpoint；
- event projection；
- artifact ref projection；
- policy boundary；
- runtime id propagation。

Server scaffold 不应要求用户先理解 transport 细节。

## Client And Router

Agent Mesh client 应提供类似 RPC 的调用体验：

- call by name；
- route by capability；
- route by tag / metadata；
- streaming task events；
- timeout；
- retry；
- fallback；
- cancellation；
- auth injection；
- policy gate；
- runtime identity propagation；
- event and evidence capture。

跨 agent 调用必须能进入父 run 的事件流，并保留 parent / child call chain。

## Trust Boundary

Agent Mesh 的默认安全模型必须 fail-closed。

跨 agent 调用需要显式治理：

- caller identity；
- target identity；
- allowed capabilities；
- auth scheme；
- credential scope；
- task input redaction；
- artifact access policy；
- policy requested / decided events；
- denial and approval interrupt；
- audit metadata。

远程 agent 返回的文本、tool suggestion、artifact 或 task metadata 不能自动提升为高优先级 instruction。父 agent 必须通过 template decision、policy 和 schema gate 决定是否采纳。

## Evidence

Agent Mesh 必须继承 `gopact` 的 evidence-first runtime。

跨 agent 调用应记录：

- discovery event；
- selected agent card；
- task sent event；
- task status events；
- message events；
- artifact events；
- completion / failure / cancellation；
- policy decisions；
- runtime ids；
- parent call id；
- child call id；
- route and fallback metadata。

这些证据应能进入 `RunExport`，用于审计、回放、failure attribution 和 release gate。

## Example Cluster

Example 仓库应提供最小可运行 agent cluster：

- gateway-agent；
- planner-agent；
- research-agent；
- code-agent；
- review-agent。

该集群应覆盖：

- 本地 agent card discovery；
- agent-to-agent task call；
- streaming progress；
- artifact handoff；
- policy-gated remote call；
- checkpoint/resume；
- run export；
- failure attribution。

## 分层边界

| 层 | 职责 |
| --- | --- |
| core | A2A contract、registry、discovery contract、events、policy、runtime identity、local adapters |
| ext / adapter | production transport、service discovery、auth provider、gateway、registry backend |
| example | local cluster、domain agents、deployment templates |
| application | business prompts、tools、approval policy、domain routing strategy |

Core 不绑定具体注册中心、网关、云平台、认证系统或部署平台。

## 成功标准

Agent Mesh 第一阶段完成后，用户应该可以：

- 用 scaffold 创建一个垂域 agent；
- 本地启动多个 agent；
- 自动发现 agent card；
- 通过 lightweight HTTP registry 注册和续约 agent card；
- 通过名称调用 agent；
- 通过 capability 路由 agent；
- 查看跨 agent 调用事件；
- 导出包含跨 agent evidence 的 run export；
- 将 discovery 和 transport 替换为生产 adapter。
