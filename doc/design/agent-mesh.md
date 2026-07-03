# gopact Agent Mesh

<!-- gopact:doc-language: en -->

Chinese documentation: [agent-mesh_zh.md](agent-mesh_zh.md)

Design notes for treating domain agents as discoverable services. It covers A2A discovery, routing, registry expectations, and cluster-level governance.

## Goals

Agent Mesh is the distributed runtime shape for gopact domain agents. It should let users start vertical domain agents, expose a standard agent card, discover peers automatically, and collaborate through A2A without adopting a hosted platform or a private protocol.

Users should be able to:

- create a domain agent with a small scaffold;
- expose a stable agent card by default;
- accept tasks, return results, and stream progress over A2A;
- register with a local or remote registry;
- route by name, capability, tag, or metadata;
- preserve runtime identity, policy decisions, artifact references, task evidence, and parent-child call chains across agent calls;
- start a local development cluster with minimal configuration and replace discovery or transport with production adapters later.

## Positioning

Agent Mesh is not a new private agent protocol. It is gopact's Go-native A2A runtime, scaffold, discovery, policy, and evidence layer.

Core owns stable contracts and lightweight defaults:

- agent cards;
- task requests, results, and task events;
- agent registry and discovery contracts;
- local runnable agent adapters;
- transport-neutral client and server contracts;
- policy, event, runtime identity, and RunExport mapping.

Production service discovery, gateways, service mesh, identity systems, and external registries belong in extension adapters.

## Target Local Experience

Local development should be short and credential-free:

```bash
gopact agent init support-agent
gopact agent run ./support-agent
```

Application code should stay direct:

```go
agent, err := a2a.NewRunnableAgent(card, runner)
result, err := mesh.Call(ctx, "research-agent", a2a.Task{
	Input: "Find the latest incident summary.",
})
```

Capability routing should work like an RPC client with discovery:

```go
result, err := mesh.Route(ctx, a2a.RouteQuery{
	Require: []string{"code.review", "git.diff"},
	Task:    a2a.Task{Input: "Review this patch."},
})
```

## Agent Card

Every agent must expose a stable agent card. A standard agent card describes at least:

- name and description;
- endpoint URL;
- protocol bindings;
- capabilities and tags;
- input and output schema hints;
- streaming and artifact support;
- auth requirements;
- owner, version, and metadata;
- health and readiness hints.

HTTP agents should support well-known discovery at:

```text
/.well-known/agent-card.json
```

The agent card is the shared entry point for discovery, routing, tooling, governance, and evidence.

## Discovery

Agent Mesh should support layered discovery:

| Mode | Purpose |
| --- | --- |
| in-memory registry | single-process tests and local demos |
| file registry | local development clusters |
| HTTP well-known discovery | direct remote agent discovery |
| HTTP card registry | lightweight registration, lease renewal, and heartbeat |
| static registry | simple production deployments |
| external registry adapter | Consul, etcd, Kubernetes, Nacos, or enterprise service discovery |

Core defines the discovery contract and lightweight implementations. The current HTTP card registry supports TTL registration, heartbeat renewal, and readiness-aware import or eviction. It is a local and adapter-conformance primitive, not a replacement for a production registry's consistency, leader election, permissions, or fleet eviction model.

## Server Scaffold

The server scaffold should expose a local Runner, template, or handler as an A2A-compatible agent.

The default server path should provide:

- agent card endpoint;
- task send endpoint;
- task stream endpoint;
- task cancel endpoint;
- health endpoint;
- readiness endpoint;
- event projection;
- artifact reference projection;
- policy boundary;
- runtime ID propagation.

Users should not need to understand transport details before they can start a domain agent.

## Client And Router

The Agent Mesh client should provide RPC-like operations:

- call by agent name;
- route by capability;
- route by tag or metadata;
- stream task events;
- apply timeout, retry, fallback, and cancellation;
- inject auth;
- pass policy gates;
- propagate runtime identity;
- capture event and verification evidence.

Cross-agent calls must enter the parent run's event stream and preserve parent and child call IDs.

## Trust Boundary

The default security model must fail closed.

Cross-agent calls require explicit governance:

- caller identity;
- target identity;
- allowed capabilities;
- auth scheme;
- credential scope;
- task input redaction;
- artifact access policy;
- policy requested and decided events;
- denial and approval interrupt;
- audit metadata.

Text, tool suggestions, artifacts, or task metadata returned by a remote agent must not become privileged instructions automatically. The parent agent accepts them only through template decisions, policy, and schema gates.

## Evidence

Agent Mesh inherits gopact's evidence-first runtime.

Cross-agent calls should record:

- discovery event;
- selected agent card;
- task sent event;
- task status events;
- message events;
- artifact events;
- completion, failure, or cancellation;
- policy decisions;
- runtime IDs;
- parent call ID;
- child call ID;
- route and fallback metadata.

This evidence must be exportable through RunExport for audit, replay, failure attribution, and release gates.

## Example Cluster

The examples repository should provide a minimal local agent cluster:

- gateway-agent;
- planner-agent;
- research-agent;
- code-agent;
- review-agent.

The cluster should cover:

- local agent card discovery;
- agent-to-agent task calls;
- streaming progress;
- artifact handoff;
- policy-gated remote calls;
- checkpoint and resume;
- run export;
- failure attribution.

## Layer Boundaries

| Layer | Responsibility |
| --- | --- |
| core | A2A contract, registry, discovery contract, events, policy, runtime identity, local adapters |
| ext / adapter | production transport, service discovery, auth provider, gateway, registry backend |
| examples | local cluster, domain agents, deployment references |
| application | business prompts, tools, approval policy, domain routing strategy |

Core must not bind to a concrete registry, gateway, cloud platform, identity provider, or deployment platform.

## Success Criteria

The first Agent Mesh phase is complete when users can:

- scaffold a domain agent;
- start multiple local agents;
- discover agent cards automatically;
- register and renew cards through the lightweight HTTP card registry;
- call agents by name;
- route agents by capability;
- inspect cross-agent events;
- export RunExport evidence that includes cross-agent task evidence;
- replace discovery and transport with production adapters.
