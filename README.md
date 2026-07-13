# 🧠 gopact

<!-- gopact:doc-language: en -->

Chinese documentation: [README_zh.md](README_zh.md)

`gopact` is an Agent-first, Workflow-native Go ADK core.

> **Go 1.27+ only.** This project is built around generic methods and celebrates what we see as one of Go's most consequential language changes of the past decade. Until Go 1.27 is officially released, it requires a development toolchain and should be treated as a preview, not a stable release.

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
- `provider` model registry/routing helpers and basic provider normalization;
- `gopacttest` reusable model and Agent conformance helpers.

Official providers, concrete Agents, and the SQLite adapter live in `gopact-ext`; runnable examples live in `gopact-examples`.

`SessionID` is runtime correlation metadata for relating multiple Runs, not a Session container. Agent Context is the final `gopact.ModelRequest` explicitly built by business or concrete-Agent Workflow logic; core does not inject implicit conversation or semantic-memory state.

## Requirements

Go 1.27 or newer is required. This repository intentionally uses Go 1.27+ generic object methods in both Agent and workflow implementer APIs.

## Quick Check

```bash
go test ./...
go test -race ./...
go vet ./...
```

Validation uses focused native gates: `gofmt`, `go mod tidy -diff`, `go test`, `go test -race`, `go vet`, and `govulncheck`. No aggregate third-party linter is required.

## Production execution

Without persistence options, a Workflow keeps checkpoints and RunLog events in memory. That default is intended for tests and short-lived local programs; a long-running service should configure durable stores with an explicit retention policy:

```go
wf := workflow.New[Input, Output](
    "agent",
    workflow.WithCheckpointer(store),
    workflow.WithJournal(store),
    workflow.WithCheckpointLease(3*time.Minute, time.Minute),
)
```

Deployment scope is deliberate: `workflow.MemoryStore` is for tests and short-lived local processes; `stores/sqlite` is for one machine or multiple processes that safely share the same local database file. Multi-host deployments need a distributed database Store that implements atomic Claim and fencing.

Configured stores are authoritative and fail closed. A Checkpointer must claim and renew leases atomically; the runtime cancels the node context with `workflow.ErrCheckpointLeaseLost` when renewal fails. Node implementations must stop promptly when their Context is canceled.

Workflow recovery is at-least-once. Heartbeats prevent a healthy long-running node from being reclaimed solely because its original lease expired, but no checkpoint protocol can make an arbitrary external API exactly-once. Calls that send messages, charge money, mutate inventory, or invoke billable models must use an idempotency key stable across resume, such as `RunInfo.RunID + "/" + RunInfo.ActivationID`.

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
