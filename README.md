# 🧠 gopact

<!-- gopact:doc-language: en -->

Chinese documentation: [README_zh.md](README_zh.md)

`gopact` is an Agent-first, Workflow-native Go ADK core.

> **Go 1.27+ only.** Until Go 1.27.0 is released, use the Go 1.27 RC 2 toolchain. gopact itself still publishes stable versions only.

## Choose your path

| You want to... | Go to |
| --- | --- |
| Build typed workflows and Agent runtimes | This repository |
| Add model adapters, Agent compositions, or stores | [gopact-ext](https://github.com/gopact-ai/gopact-ext) |
| Run complete quickstarts and integrations | [gopact-examples](https://github.com/gopact-ai/gopact-examples) |

It contains only:

- root `gopact` shared Model, Tool, Event, and Invokable protocols and facts;
- `agent` Identity/Request/Response, typed observations, tool contracts, and immutable directories;
- `workflow` as the sole execution runtime, with typed nodes/routes/joins, hooks/middleware, guards, checkpoints, history, and same-Run control;
- `runlog` append/query/sink contracts and an in-memory implementation;
- `gopacttest` reusable protocol conformance helpers for models, minimal Agents, and Workflow-backed Agents.

Official providers, concrete Agents, and the SQLite adapter live in `gopact-ext`; runnable examples live in `gopact-examples`.

The minimal `agent.Agent` interface remains directly implementable. Only Workflow-backed Agents receive the configured Workflow's checkpoint, recovery, control, and history semantics; direct implementations do not acquire those guarantees automatically.

Use `gopacttest.RequireAgentConformance` for the shared direct/Workflow-backed Agent protocol. Use `gopacttest.RequireWorkflowAgentConformance` when an implementation promises Workflow lifecycle, lineage, and run-extension semantics. Store implementations can use `gopacttest.RequireStoreConformance` for portable recovery and RunLog checks while keeping database-specific lifecycle and concurrency tests local. The response callback is a deterministic test assertion for the supplied fixture; task-quality evaluation and release acceptance remain application responsibilities, not runtime APIs.

`SessionID` is runtime correlation metadata for relating multiple Runs, not a Session container or an authentication, authorization, or tenant-isolation credential. Applications must authorize before querying and isolate data with separate Stores, database namespaces, or an outer query wrapper. Agent Context is the final `gopact.ModelRequest` explicitly built by business or concrete-Agent Workflow logic; core does not inject implicit conversation or semantic-memory state.

## Requirements

Go 1.27 or newer is required. This repository intentionally uses Go 1.27+ generic object methods in both Agent and workflow implementer APIs.

Until Go 1.27.0 is released, run local commands with the `go1.27rc2` toolchain, for example `GOTOOLCHAIN=go1.27rc2 go test ./...`. CI pins the same toolchain using the `setup-go` version `1.27.0-rc.2`.

## Optional model capabilities

`Model` and `StreamingModel` remain the minimal text-generation protocols. Providers may additionally implement the independent `Embedder` and `ModelCatalog` interfaces, so applications can discover available models and create embeddings without depending on a provider package:

```go
catalog, ok := model.(gopact.ModelCatalog)
if ok {
	models, err := catalog.ListModels(ctx)
	// Present models.Models instead of asking the user for an opaque model ID.
}

embedder, ok := model.(gopact.Embedder)
if ok {
	result, err := embedder.Embed(ctx, gopact.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"gopact"},
	})
}
```

These capabilities are deliberately optional: a provider that exposes generation but no public embedding or model-catalog API does not need to emulate one. Provider-specific account quotas, subscription limits, media generation, uploads, and other runtime operations stay in the provider adapter rather than the core protocol.

## Quick Check

```bash
go test ./...
go test -race ./...
go vet ./...
```

Validation uses focused native gates: `gofmt`, `go mod tidy -diff`, `go test`, `go test -race`, `go vet`, and `govulncheck`. No aggregate third-party linter is required.

## Release status

A stable tag requires the Go 1.27 toolchain gates, coordinated core → ext → examples source E2E, and immutable-tag clean-consumer verification. A source checkout alone does not imply that post-tag verification has passed.

## Production execution

Without persistence options, a Workflow keeps checkpoints and RunLog events in one in-memory Store. That default is intended for tests and short-lived local programs; a long-running service should configure a durable Store with an explicit retention policy:

```go
wf := workflow.New[Input, Output](
    "agent",
    workflow.WithStore(store),
    workflow.WithCheckpointLease(3*time.Minute, time.Minute),
)
```

Workflow generates Session, Run, and lease-owner IDs with the Go standard-library UUID implementation. A service may replace each identity domain independently at build time, while one invocation may override it again:

```go
wf := workflow.New[Input, Output]("agent",
    workflow.WithIDGenerator(gopact.IDKindSession, newSessionID),
)
out, err := wf.Invoke(ctx, input,
    gopact.WithIDGenerator(gopact.IDKindRun, newRunID),
)
```

The precedence is explicit ID > RunOption generator > Workflow generator > UUID default. Generated IDs are rejected unless they are non-empty valid UTF-8, at most 191 bytes, contain no NUL, and do not end in a space. Generators may be called concurrently and remain responsible for uniqueness.

Deployment scope is deliberate: `workflow.MemoryStore` is for tests and short-lived local processes; `stores/sqlite` is for one machine or multiple processes that safely share the same local database file. Multi-host deployments need a distributed database Store that implements atomic Claim and fencing.

A configured `workflow.Store` is the single authoritative persistence boundary and fails closed. One instance provides checkpoint persistence and history, RunLog append and query, plus atomic ownership fencing; the runtime does not accept separate checkpoint and journal authorities. Claim and lease renewal must be atomic, and fenced append must validate the current owner and claim under the same lock or database transaction as the journal write. The runtime passes a lease duration so distributed Stores can derive expiry from the database clock instead of trusting a host wall clock. Renewal or authoritative write failure stops the invocation, and lease loss cancels the node Context with `workflow.ErrCheckpointLeaseLost`.

Observer telemetry remains separate from durable authority. Application `EventSink` handlers receive accepted lifecycle events under their configured delivery policy, but they do not replace or participate in the Store's checkpoint, history, journal, or fencing contract. Sinks may also implement optional `ModelEventSink` or `ToolEventSink` to receive live, non-durable component observations from nodes that emit them. Domain logs, metrics, and traces are projected from Event/View data; infrastructure telemetry wraps the application-owned Store or adapter. Core adds no OpenTelemetry dependency. Plugins only register extensions during Compile and never own their resources; the creating application or adapter closes them. Node implementations must stop promptly when their Context is canceled.

Workflow recovery is at-least-once. Journal-to-consumer event delivery is also at-least-once, so event consumers should deduplicate by stable event identity such as `(RunID, Sequence)` or `RevisionID`. Heartbeats prevent a healthy long-running node from being reclaimed solely because its original lease expired, but no checkpoint protocol can make an arbitrary external API exactly-once. Calls that send messages, charge money, mutate inventory, or invoke billable models must use an idempotency key stable across resume, such as `RunInfo.RunID + "/" + RunInfo.ActivationID`.

A stable key provides a reliable guarantee only when either the external API natively deduplicates that key, or application code writes a uniquely constrained dedup/outbox record in the same database transaction as its business data. `gopact` does not provide a generic outbox and cannot atomically combine its checkpoint transaction with an arbitrary remote API. If an explicit business retry is meant to create another side effect, give that operation a new key instead of reusing the recovery key.

High-level history projections are bounded. `ListSessionRuns` and a zero-limit `Snapshot` read at most 10,000 records by default; checkpoint history and Retry/Jump scans use 256-record pages and return `workflow.ErrHistoryLimitExceeded` instead of silently returning an incomplete result. Use an explicit `SnapshotRequest.Limit` for timeline pagination and archive or purge terminal Runs before they outgrow the control-history bound.

## Minimal Workflow

```go
wf := workflow.New[string, int]("length")
count := wf.Node("count", func(_ context.Context, input string) (int, error) {
    return len(input), nil
})
wf.Entry(count)
wf.Exit(count)

out, err := wf.Invoke(ctx, "gopact")
```
