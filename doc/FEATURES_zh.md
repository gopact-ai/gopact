# Feature Coverage

<!-- gopact:doc-language: zh -->

[英文文档](./FEATURES.md)

## 中文

本表只记录 core 仓库已经具备离线测试或 conformance 证明的能力。未出现在这里的能力可以在设计文档中规划，但不能在 README 或 release note 中描述为已完成。

| Capability | Package or contract | Offline proof | Boundary |
| --- | --- | --- | --- |
| workflow graph execution | `graph` | `go test -count=1 ./graph ./gopacttest/graphconformance` | Typed graph runtime, node/edge execution, branch routing, DAG fan-in, dynamic fan-out, loop step limits, runnable subgraphs, node-emitted nested events, middleware, event stream, and reusable graph conformance |
| checkpoint and resume | `checkpoint` | `go test -count=1 ./checkpoint ./gopacttest/checkpointconformance` | Checkpoint records, codecs, store interfaces, import/export, and resume validation |
| provider-neutral model contract | `model.go` | `go test -count=1 . ./provider ./gopacttest/providerconformance` | Model request/response, routing metadata, streaming events, tool choice, fake providers, and provider conformance |
| tool registry and replay | `tools` | `go test -count=1 ./tools ./gopacttest/toolconformance` | Visible/deferred tools, tool result effects, replay commit records, and registry conformance |
| MCP client/server contracts | `mcp` | `go test -count=1 ./mcp` | JSON-RPC, streamable HTTP/SSE, capability server, resource/tool/prompt wire contracts, and policy hooks |
| A2A agent mesh | `a2a` | `go test -count=1 ./a2a ./gopacttest/a2aconformance` | Agent card discovery, readiness-gated HTTP discovery, lease registration, heartbeat renewal, health-driven eviction, capability/tag routing, auth context, stable retry IDs, HTTP/JSON-RPC/SSE task transport |
| A2A env mesh sync | `a2a/env.go` | `go test -count=1 ./a2a -run 'TestMesh(BootstrapEnv|SyncEnv)'` | One-shot environment discovery sync, HTTP option propagation, callable HTTP/JSON-RPC registration, readiness eviction, final card snapshot, and aggregated sync evidence |
| A2A HTTP registry discovery | `a2a/http_example_test.go` | `go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a` | Publish and consume HTTP agent-card registries for local or service-discovery-backed mesh bootstrap |
| A2A continuous mesh sync | `a2a/mesh.go` | `go test -count=1 ./a2a -run TestMeshSyncEvery` | immediate sync, interval resync, context cancellation, and positive interval validation |
| A2A continuous env mesh sync | `a2a/env.go` | `go test -count=1 ./a2a -run TestMeshSyncEnvEvery` | standard A2A environment variables, immediate sync, interval resync, context cancellation, and positive interval validation |
| agent scaffold generator | `cmd/gopact` | `go test -count=1 ./cmd/gopact` | `gopact agent init` emits a standalone A2A HTTP agent module; `gopact agent run` executes a generated agent and loads local `.env` without overriding existing environment variables |
| channel and surface transfer | `channel_policy.go` | `go test -count=1 -run Channel . ./gopacttest` | Surface messages, transfer policy, channel events, and verification evidence |
| policy, redaction, and safety contracts | `policy.go` | `go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance` | Policy gates, redaction, sandbox profiles, secret handling, and prompt-injection conformance |
| verification evidence and release gate | `gopacttest` | `go test -count=1 ./gopacttest` | Verification reports, evidence bridges, CI gate evidence, file snapshots, review evidence, and command evidence |
| self-bootstrap release bundle | `gopacttest` | `go test -count=1 -run SelfBootstrap ./gopacttest` | Run export plus embedded report, replay plan evidence, release bundle checks, and self-bootstrap gate coverage |

Downstream matrices:

- `gopact-ext/doc/FEATURES.md` tracks official extension modules, provider mocks, and optional local integration commands.
- `gopact-examples/doc/FEATURES.md` tracks runnable quickstarts and scaffold examples.
