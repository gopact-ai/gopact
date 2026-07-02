# Feature Coverage

This matrix is the core repository contract for expected SDK capabilities. Commands are offline and provider-neutral unless a downstream extension repository explicitly adds integration tags.

| Capability | Contract path | Offline proof | Boundary |
| --- | --- | --- | --- |
| workflow graph execution | `graph` | `go test -count=1 ./graph ./gopacttest/graphconformance` | Typed graph runtime, events, middleware, and reusable graph conformance |
| checkpoint and resume | `checkpoint` | `go test -count=1 ./checkpoint ./gopacttest/checkpointconformance` | Stable checkpoint records, import/export, and reusable store conformance |
| provider-neutral model contract | `model.go` | `go test -count=1 . ./provider ./gopacttest/providerconformance` | Model request/response, routing, middleware, tool choice, fake providers, and provider conformance |
| tool registry and replay | `tools` | `go test -count=1 ./tools ./gopacttest/toolconformance` | Visible/deferred tools, tool result effects, replay commit store conformance |
| MCP client/server contracts | `mcp` | `go test -count=1 ./mcp` | JSON-RPC, streamable HTTP/SSE, capability server, policy hooks |
| A2A agent mesh | `a2a` | `go test -count=1 ./a2a ./gopacttest/a2aconformance` | Agent card discovery, expiry-aware routing, capability/tag/metadata routing, auth context, stable task-id retry, HTTP/JSON-RPC/SSE task transport |
| A2A HTTP registry discovery | `a2a/http_example_test.go` | `go test -count=1 -run ExampleNewHTTPRegistryHandler ./a2a` | Publish and consume HTTP agent-card registries for mesh bootstrap |
| agent scaffold generator | `cmd/gopact` | `go test -count=1 ./cmd/gopact` | `gopact agent init` emits a standalone, testable A2A HTTP agent module; `gopact agent run` executes it with `go run .` |
| channel and surface transfer | `channel_policy.go` | `go test -count=1 -run Channel . ./gopacttest` | Surface messages, transfer, channel policy, event evidence |
| policy, redaction, and safety contracts | `policy.go` | `go test -count=1 . ./sandbox ./gopacttest/secretconformance ./gopacttest/promptinjectionconformance` | Policy gates, redaction, sandbox profiles, secret and prompt-injection conformance |
| verification evidence and release gate | `gopacttest` | `go test -count=1 ./gopacttest` | Verification reports, evidence bridges, CI gates, file snapshots, review evidence |
| self-bootstrap release bundle | `gopacttest` | `go test -count=1 -run SelfBootstrap ./gopacttest` | Run export plus embedded report, release bundle checks, self-bootstrap gate |

Downstream repositories keep their own matrices:

- `gopact-ext/FEATURES.md` records official extension modules, provider mocks, and local integration commands.
- `gopact-examples/FEATURES.md` records runnable quickstarts and scaffold examples.
