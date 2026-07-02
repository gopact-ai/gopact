# Gopact TurnLoop and Extensibility Implementation Plan

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


> **For agentic workers:** Continue from Integration Spine. Keep TurnLoop as the control plane and avoid moving business harness concepts into core.

**Goal:** Build the first M4 control-plane and extensibility slice so `gopact` can wrap runs as turns, cancel/preempt active work, mark resume input, and let plugins install node middleware and event subscribers.

## Implemented In This Pass

- `TurnLoop`
  - wraps a root `Runner`;
  - emits `turn_started`, `turn_completed`, `turn_failed`, `turn_canceled`;
  - supports `Cancel(reason)`;
  - supports `WithPreempt()` to cancel an active turn before starting a new one;
  - supports `WithResume(ResumeRequest)` and emits `turn_resumed`;
  - preserves runner events while translating terminal runner errors into turn terminal events.
- `NodeContext`
  - carries `RuntimeIDs`, `Node`, `Step`, `Input`, `Output`, `Err`, metadata, and `context.Context`;
  - supports Gin-style `Next()` control;
  - middleware can run before/after final handler or short-circuit by not calling `Next()`.
- Graph node middleware integration
  - `graph.WithNodeMiddleware(...)` wraps every graph node execution;
  - middleware can mutate `NodeContext.Input` before `Next()`;
  - middleware can mutate `NodeContext.Output` after `Next()`;
  - middleware errors produce `node_failed` and `run_failed` events;
  - short-circuit output is accepted when it matches graph state type.
- `PluginHost`
  - installs named plugins once;
  - lets plugins register node middleware;
  - lets plugins register event middleware;
  - lets plugins subscribe to events;
  - publishes events to subscribers in registration order;
  - closes installed plugins in reverse install order and returns joined close errors.
- Runner event middleware integration
  - `WithRunnerEventMiddleware(...)` wraps events before they are yielded;
  - `WithPluginHost(...)` attaches plugin event middleware and subscribers to a Runner;
  - middleware can enrich, redact, drop, or fail event emission;
  - middleware failures produce `run_failed` and preserve `errors.Is`.
- provider/tools middleware integration
  - provider `WithModelMiddleware(...)` wraps routed provider calls;
  - tools `WithToolMiddleware(...)` wraps registry tool invocations;
  - `PluginHost` can register model/tool middleware and provider/tools can attach them via `WithPluginHost(...)`;
  - both support request/args rewrite, response/result rewrite, short-circuit, and error propagation.
- Runner/TurnLoop close integration
  - `Runner.Close(ctx)` closes attached plugin host resources;
  - `TurnLoop.Close(ctx)` cancels the active turn and closes its runner.
- root event type constants for turn lifecycle.

## Not Completed Yet

- persistent resume queues;
- input merging strategy;
- interrupt record integration;
- runner-level module injection;
- full plugin startup/shutdown policy;
- policy event/approval interrupt, redaction state, and sink strict/fallback policy. The first policy/redaction middleware slice now exists in root `Policy`, `ModelPolicyMiddleware`, `ToolPolicyMiddleware`, `TextRedactor`, and `EventRedactionMiddleware`.

## Verification

Commands used:

```bash
go test ./... -count=1
go vet ./...
git diff --check
```
