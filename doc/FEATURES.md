# Feature Coverage

<!-- gopact:doc-language: en -->

Chinese documentation: [FEATURES_zh.md](FEATURES_zh.md)

This matrix lists only behavior that has offline tests or reusable conformance coverage in the core repository. Planned features may appear in design documents, but they must not be presented as complete in README files or release notes until they have proof here.

Workflow orchestration details, including completed graph capabilities and planned gaps, are tracked in [doc/design/workflow-orchestration-matrix.json](design/workflow-orchestration-matrix.json).

| Capability | Package or contract | Offline proof | Boundary |
| --- | --- | --- | --- |
| workflow graph execution | `graph` | `go test -count=1 ./graph ./gopacttest/graphconformance` | Typed graph runtime, node/edge execution, step export/import, interrupted step resume, branch routing, DAG fan-in, dynamic fan-out, loop step limits, runnable subgraphs, node-emitted nested events, topology export, middleware, event stream, and reusable graph conformance |
| checkpoint and resume | `checkpoint` | `go test -count=1 ./checkpoint ./gopacttest/checkpointconformance` | Checkpoint records, codecs, store interfaces, import/export, and resume validation |
| provider-neutral model contract | `model.go` | `go test -count=1 . ./provider ./gopacttest/providerconformance` | Model request/response, routing metadata, streaming events, tool choice, fake providers, and provider conformance |
| tool registry and replay | `tools` | `go test -count=1 ./tools ./gopacttest/toolconformance` | Visible/deferred tools, tool result effects, replay commit records, and registry conformance |
| MCP client/server contracts | `mcp` | `go test -count=1 ./mcp` | JSON-RPC, streamable HTTP/SSE, capability server, resource/tool/prompt wire contracts, and policy hooks |
| A2A agent mesh | `a2a` | `go test -count=1 ./a2a ./gopacttest/a2aconformance` | Agent card discovery, readiness-gated HTTP discovery, lease registration, heartbeat renewal, health-driven eviction, capability/tag routing, auth context, stable retry IDs, HTTP/JSON-RPC/SSE task transport |
| A2A env mesh sync | `a2a/env.go` | `go test -count=1 ./a2a -run 'TestMesh(BootstrapEnv|SyncEnv)'` | One-shot environment discovery sync, mesh-level and per-call HTTP option propagation, callable HTTP/JSON-RPC registration, readiness eviction, final card snapshot, and aggregated sync evidence |
| A2A HTTP registry discovery | `a2a/http_example_test.go` | `go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a` | Publish and consume HTTP agent-card registries for local or service-discovery-backed mesh bootstrap |
| A2A continuous mesh sync | `a2a/mesh.go` | `go test -count=1 ./a2a -run TestMeshSyncEvery` | immediate sync, interval resync, context cancellation, and positive interval validation |
| A2A continuous env mesh sync | `a2a/env.go` | `go test -count=1 ./a2a -run TestMeshSyncEnvEvery` | standard A2A environment variables, mesh-level and per-call HTTP option propagation, immediate sync, interval resync, context cancellation, and positive interval validation |
| agent scaffold generator | `cmd/gopact` | `go test -count=1 ./cmd/gopact` | `gopact agent init` emits a standalone A2A HTTP agent module; `gopact agent run` executes a generated agent and loads local `.env` without overriding existing environment variables |
| channel and surface transfer | `channel_policy.go` | `go test -count=1 -run Channel . ./gopacttest` | Surface messages, transfer policy, channel events, and verification evidence |
| policy, redaction, and safety contracts | `policy.go` | `go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance` | Policy gates, redaction, sandbox profiles, secret handling, and prompt-injection conformance |
| verification evidence and release gate | `gopacttest` | `go test -count=1 ./gopacttest` | Verification reports, evidence bridges, CI gate evidence, file snapshots, review evidence, and command evidence |
| self-bootstrap release bundle | `gopacttest` | `go test -count=1 -run SelfBootstrap ./gopacttest` | Run export plus embedded report, replay plan evidence, release bundle checks, and self-bootstrap gate coverage |

Downstream matrices:

- `gopact-ext/doc/FEATURES.md` tracks official extension modules, provider mocks, and optional local integration commands.
- `gopact-examples/doc/FEATURES.md` tracks runnable quickstarts and scaffold examples.
