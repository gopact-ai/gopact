# Agent Control Surface Design

Status: `superseded`

Superseded on 2026-07-14 by [ADR-0001: Run 终态闭合](../../decisions/0001-run-closure.md). 本文保留当时的审议记录，不再是实现依据；尤其是同 Run Retry、同 Run jump、`Reopen`、`ForceJumpTo(private NodeID, any)` 及相关验收要求均已失效。现行总览见 [Run、Store 与历史语义对齐 RFC](../../rfcs/run-store-history-alignment.md)。

## Constitutional basis

- `SYS-001`: Agent-first product, Workflow-native architecture.
- `AGENT-001`: an Agent may hide graph construction but must not hide Workflow facts or bypass Workflow control and observability.
- `API-002`: scheduler metadata stays off the default business path and appears only in advanced control/debug APIs.

## Problem

Official Agents keep a private `*agent.WorkflowAgent` and expose only `Identity` and `Invoke`. Resume remains possible through `Invoke(..., workflow.WithResume(...))`, but Snapshot, Retry, Terminate and external jump-to are unreachable from the Agent surface. Applications must retain a separate Workflow pointer or write a wrapper, breaking the Agent-first path.

## Decision

Keep each concrete Agent's WorkflowAgent field private. Do not anonymously embed an exported `*agent.WorkflowAgent`: callers could replace or nil the embedded field, and future WorkflowAgent methods would silently expand every concrete Agent's top-level API.

`WorkflowAgent` will delegate the advanced operations to its same private Workflow:

```go
func (a *WorkflowAgent) Snapshot(context.Context, workflow.SnapshotRequest) (workflow.Snapshot, error)
func (a *WorkflowAgent) Retry(context.Context, workflow.RetryRequest, ...gopact.RunOption) (Response, error)
func (a *WorkflowAgent) Terminate(string) error
func (a *WorkflowAgent) ForceJumpTo(context.Context, string, workflow.JumpRequest, any, ...gopact.RunOption) (Response, error)
```

Each official concrete Agent adds exactly one accessor:

```go
func (a *Agent) Control() *agent.WorkflowAgent
```

`WorkflowAgent.Control` returns itself. The optional capability interface is:

```go
type ControllableAgent interface {
	Agent
	Control() *WorkflowAgent
}
```

The minimal `Agent` interface remains unchanged.

## Force jump

Typed `Workflow.JumpTo` remains the normal API for Workflow authors. Official Agent nodes often use private input types, so the advanced Agent escape hatch selects a node by stable NodeID and accepts dynamic input. `Workflow.ForceJumpTo` must:

- reject an empty or unknown NodeID;
- validate the dynamic input against the target node input type before reopening the Run;
- reuse the existing same-Run jump implementation, context patch validation and lineage facts;
- create no Agent-owned state, scheduler or event stream;
- retain the explicit `Force` name because this operation crosses the Agent's encapsulation boundary and may fail after a definition upgrade.

## Error and nil behavior

- A nil Agent or nil WorkflowAgent returns an error/zero result; it must not panic.
- Control errors remain Workflow errors and support the existing `errors.Is/As` contracts.
- `Control` returns the immutable facade pointer. It exposes no setter and no raw Workflow builder.

## Verification

No coverage-only tests are added. The cross-repository reference scenario will use a real Agnes and GLM model/tool Agent and verify through the public Agent surface:

- `Control().Snapshot` returns natural-flow facts;
- `Control().Retry` appends a same-Run Attempt;
- `Control().ForceJumpTo` appends a same-Run activation from a selected Revision and context patch;
- `Control().Terminate` creates a distinct terminated terminal fact;
- hooks and middleware expose the effective node input, output and Workflow context.

Existing core contract tests continue to validate the scheduler invariants underneath these forwarding methods.

## Non-goals

- No raw Workflow getter.
- No second Agent controller or storage model.
- No expansion of the minimal `Agent` interface.
- No compatibility alias for the deleted Agent Runtime or `Cancel` API.
