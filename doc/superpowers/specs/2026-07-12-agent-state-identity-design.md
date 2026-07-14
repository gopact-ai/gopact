# Agent State and Identity Design

Status: `superseded`

Superseded on 2026-07-14 by [ADR-0001](../../decisions/0001-run-closure.md), [ADR-0002](../../decisions/0002-durable-store-authority.md), and the [Run、Store 与历史语义对齐 RFC](../../rfcs/run-store-history-alignment.md). 本文正文完整保留 2026-07-12 已批准并实现的历史结论，不再是现行实现依据；Run 终态、source-lineage 新 Run、单一 Store 与恢复规则以新决策为准。

## Purpose

Remove the orphaned Session, ContextManager, and Memory abstractions produced by the old Agent Runtime design. Preserve one Workflow runtime, keep default execution stateless outside configured Workflow stores, and add only the identity needed to correlate multiple Runs as one business or Agent conversation.

## Research basis

The design was checked against the public implementations and documentation of Codex, OpenCode, Oh My Pi, Claude Code, OpenTelemetry, and Mem0.

- Coding agents commonly expose two or three identity layers such as thread/session, turn/message, and item/part. They do not require a universal in-process Session object in every SDK integration.
- OpenTelemetry owns TraceID/SpanID propagation. Its GenAI conventions separately define `gen_ai.conversation.id` and Agent attributes.
- Mem0 scopes operations with combinations of `user_id`, `agent_id`, and `run_id`; it does not require the host framework to own a Session container.
- Context construction differs materially between Agent implementations and is part of the Agent/business algorithm, not a provider-neutral storage contract.

Primary references:

- <https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md>
- <https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/session/prompt.ts>
- <https://github.com/can1357/oh-my-pi/blob/main/packages/coding-agent/src/session/session-context.ts>
- <https://code.claude.com/docs/en/agent-sdk/agent-loop>
- <https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/>
- <https://github.com/mem0ai/mem0/blob/main/mem0/memory/main.py>

## Identity model

The durable domain identity is:

```text
SessionID
└── RunID
    └── ParentRunID (child Runs only)
```

- `SessionID` is a correlation value. It can group multiple independent root Runs across Agent invocations, Workflow definitions, tasks, and time.
- `RunID` identifies exactly one Workflow execution and remains the identity used by checkpoint resume, retry, external jump-to, terminate, Snapshot, and per-Run history.
- `ParentRunID` expresses direct child lineage. `Depth` remains runtime safety metadata, not a new identity namespace.
- A fresh root invocation generates SessionID and RunID when the caller does not provide them.
- `WithSessionID` lets the caller reuse a SessionID. Child Runs inherit and constrain the parent SessionID; a conflicting child value is invalid. An intentionally unrelated Session must be started as a root invocation.
- Resume loads the stored SessionID for the selected RunID. If the caller also supplies a different SessionID, resume fails rather than silently moving the Run.
- Retry, resume, external jump-to, and same-Run control epochs retain SessionID and RunID.

SessionID does not identify a unique resumable execution. A Session can contain multiple Runs, so resume always selects RunID.

## Removed identities

Domain `TraceID` is removed. In the current implementation it is the root RunID copied through child Runs, while ParentRunID already records the execution tree. It is not a conforming OpenTelemetry TraceID and should not pretend to be one.

`ExecutionID` is also removed. It is currently generated as `"execution:" + RunID` and has no independent lifecycle: retry and external jump-to retain both values. Keeping a second spelling creates metadata without additional information.

This removal does not affect `ExecutionEpoch`, NodeExecutionVersion, DefinitionID/Version, NodeID, ActivationID, AttemptID, RevisionID, SourceRunID, or per-Run Sequence. Those values retain their existing distinct semantics.

## Metadata propagation

SessionID is added to all execution read and persistence models that need cross-Run correlation:

- `RunConfig` and `workflow.RunInfo`;
- runtime `Event`;
- Workflow `CheckpointRecord` and checkpoint history;
- `runlog.Record` and query filters;
- Workflow Snapshot RunMeta;
- Agent code that reads Workflow RunInfo.

Internal scheduler data does not acquire Session-specific state. SessionID is copied and validated like RunID metadata; it does not own lifecycle transitions.

## Query model

Session is not a container and has no SessionStore or SessionSnapshot.

- A Session query projects the Runs carrying one SessionID.
- Each result contains at least RunID, DefinitionID/Version, ParentRunID, status, and start/end timestamps when known.
- Detailed timeline, checkpoints, missing refs, and control history continue to use Snapshot by RunID.
- Per-Run Sequence remains the only authoritative event order. Concurrent Runs in one Session are not flattened into a fabricated global event sequence.
- A UI may sort Run summaries by timestamps for presentation, but that ordering is a view rather than scheduler truth.

The first implementation should extend the existing RunLog query/projection path. It must not add a second Session persistence subsystem.

## Default state behavior

Reusing SessionID does not load messages, values, or prior Agent Context. The framework remains stateless across independent Invokes except for facts saved by configured Workflow Checkpointer/RunLog stores.

- Applications pass any desired prior messages in `agent.Request`.
- Business code can load history from its own database, RunLog projections, or another service before building the next Agent Context.
- Core does not create `Session`, `SessionEntry`, `SessionStore`, or a general-purpose per-session key/value map.
- Existing default in-memory Workflow stores still provide process-lifetime retry, interrupt/resume, control, and inspection. They are execution stores, not semantic Memory.

## The three Context meanings

The documentation and APIs must qualify the word Context:

1. Go `context.Context` carries cancellation, deadlines, request-scoped values, and external telemetry context.
2. Workflow Context is typed business execution state owned by one Workflow Run and saved by Workflow checkpoints when configured.
3. Agent Context is the exact content visible to one LLM call. Its code representation is the final provider-neutral `gopact.ModelRequest`.

Workflow Context is never automatically exposed to the model. Only business code can decide which data becomes part of Agent Context.

## Context construction

Core does not define a universal ContextManager, ContextRequest, ContextResult, ContextAudit, trimming policy, summary policy, or ModelContext wrapper.

- Business code constructs `gopact.ModelRequest` in an explicit Workflow node.
- A concrete official Agent may implement the minimal context preparation required by its own algorithm, using its own typed input. It does not expose or depend on a core generic ContextManager.
- A user-authored Workflow can construct `gopact.ModelRequest` directly with an ordinary typed node.
- The final `gopact.ModelRequest` becomes the Model node input, so the effective Agent Context follows normal Workflow input/output, checkpoint, history, retry, and jump semantics.
- Context construction must not hide external I/O. Database, Memory, retrieval, or remote calls occur in explicit preceding nodes if their result must be observed or replayed.

## Interrupt and resume

User-owned Agent Context construction does not weaken Workflow recovery:

- Before the Context node starts, resume executes it normally.
- After the Context node output is covered by a successful checkpoint, resume reuses the saved `gopact.ModelRequest` rather than rebuilding it.
- If the latest checkpoint did not save that output, recovery starts from the last successful boundary and may execute the Context node again.
- An interrupted in-flight Context node has no completed output and may be retried. It should therefore be pure; external effects retain the normal business idempotency responsibility.
- Values placed in Workflow Context or node output must be serializable. Clients, functions, channels, streams, and other process objects remain injected dependencies rather than checkpoint data.
- Definition upgrades continue to follow the approved topology mismatch and explicit force-jump rules. Custom Context construction adds no compatibility promise.

## Memory boundary

Core does not define MemoryReader, MemoryRequest, MemoryResult, MemoryHit, Memory Provider, Memory Store, default Memory, or automatic Memory injection.

Memory is an external data source that business code may use while constructing Agent Context:

- call Mem0, a database, a vector store, or an internal service in an explicit Workflow node;
- transform selected results into `gopact.ModelRequest` in business code;
- expose `remember` or `recall` as an ordinary Tool or existing `gopact.Invokable` when model-driven access is desired;
- define a consumer-owned interface inside a concrete Agent package only when that package has a real replacement need.

UserID or TenantID belongs to the application's Memory scope, not every Workflow Event. Agent Identity and SessionID are available for adapters when useful. A Mem0 integration can map SessionID to Mem0 `run_id`, Agent Identity to `agent_id`, application identity to `user_id`, and Workflow RunID to provenance metadata.

## OpenTelemetry boundary

Core takes no OpenTelemetry dependency and does not persist a fake domain TraceID.

- An observability adapter reads the real SpanContext from Go `context.Context` at Workflow, Model, Tool, retrieval, and child-Agent boundaries.
- SessionID maps to `gen_ai.conversation.id`.
- Agent Identity maps to `gen_ai.agent.id`, name, and version as supported by the selected semantic-convention version.
- RunID and Workflow identities use gopact-specific span attributes.
- A resumed execution may create a new trace and link the previous trace. SessionID and RunID provide durable correlation across telemetry retention and trace boundaries.

## Public API direction

Conceptually, the cross-runtime invocation surface becomes:

```go
type RunConfig struct {
    SessionID string
    RunID     string
    Lineage   RunLineage
    // existing sinks and extensions
}

type RunLineage struct {
    ParentRunID string
    Depth       int
}

func WithSessionID(string) RunOption
func WithRunID(string) RunOption
```

The exact implementation may keep a runtime-owned lineage constraint function, but it must not reintroduce domain TraceID or expose child-lineage configuration on the normal business path.

## Deletions

Core:

- remove `agent.Session`, SessionKey, SessionStore, MemorySessionStore, related errors, bounds, and tests;
- remove ContextManager, ContextManagerFunc, ContextRequest, ContextResult, ContextAudit, RunMeta, AgentMeta, TokenBudget, and all Memory types that only support ContextManager;
- remove TraceID and ExecutionID from domain runtime, Event, RunLog, Snapshot, and Agent metadata;
- revise constitution wording that treats Session or semantic Memory as framework-owned state.

Official extensions:

- remove ReAct and Deep `WithContextManager`, configuration fields, default ContextManager implementations, and tests that only exercise the deleted abstraction;
- keep their required algorithmic message preparation as ordinary typed Workflow node logic;
- do not add a replacement generic Context framework.

No compatibility aliases are retained before v1.

## Examples

The examples repository will add focused, compile-tested scenarios:

1. reuse one SessionID across multiple Agent and Workflow Runs, list related Runs, then inspect/resume a selected RunID;
2. instrument Agent/Workflow/Model/Tool boundaries with real OpenTelemetry context and show SessionID/RunID attribute mapping;
3. query Mem0 or a Memory-like external system in a business Workflow node and explicitly construct `gopact.ModelRequest` from selected results;
4. build business-specific Agent Context from typed Workflow Context without any ContextManager.

Real external examples use local `.env` credentials and isolated smoke tests. Deterministic core identity and recovery tests do not require a model or external service.

## Error rules

- Empty explicit SessionID is invalid.
- Conflicting SessionID options are invalid.
- Child SessionID conflicts are invalid.
- Resume with a SessionID different from the stored checkpoint is an identity mismatch.
- Session query with an empty SessionID is invalid.
- SessionID never selects one Run implicitly when several Runs match.
- Context construction and external Memory failures are ordinary Workflow node failures with their original error chain.

## Verification

Core tests must prove:

- default SessionID generation and explicit override;
- concurrent invocations do not reuse generated IDs;
- child inheritance and ParentRunID lineage;
- checkpoint, resume, retry, jump-to, terminate, Event, RunLog, and Snapshot retain SessionID;
- mismatch and conflict errors support `errors.Is` where public sentinels exist;
- Session queries find multiple Definitions/Runs without inventing cross-Run sequence;
- no TraceID or ExecutionID remains in the public domain records;
- custom Context node output survives checkpoint/resume and is rerun only from a boundary that did not save it.

Cross-repository tests must prove the public Agent-first path with both in-memory and SQLite execution stores. The existing rule remains: do not run full `golangci-lint` while its bundled Staticcheck is incompatible with Go 1.27 generic methods; use gofmt, tests, race, vet, govulncheck, focused static checks, and real-provider smoke tests as appropriate.

## Constitutional corrections

The implementation must amend at least:

- `CON-002`: replace ExecutionID/trace-correlation identity with SessionID, RunID, ParentRunID, and existing fine-grained Workflow identities;
- `CON-003`: define the three Context meanings and remove Session/Memory as framework-owned state;
- `AGENT-003`: make Agent Context business-owned `gopact.ModelRequest`, Session an ID, and Memory external;
- `AGENT-004`: replace inherited domain trace with SessionID, parent lineage, cancellation, budget, and external TraceContext;
- `OBS-002`: remove ExecutionID and add SessionID to applicable facts;
- `EXT-002` and `API-001`: say in-memory execution backend rather than ambiguous semantic memory;
- `DRIFT-020` through `DRIFT-022`: replace the previous implementation direction with deletion and SessionID propagation.

## Non-goals

- Conversation transcript storage or automatic history loading.
- Session lifecycle, timeout, mutable metadata, or Session Store.
- A flattened cross-Run Session event order.
- A universal Context policy or automatic token compaction.
- A universal Memory API, vector-store abstraction, or implicit recall/write pipeline.
- Replacing application UserID/TenantID or OpenTelemetry TraceContext.
- Changing the approved Workflow node idempotency responsibility or definition-upgrade rules.
