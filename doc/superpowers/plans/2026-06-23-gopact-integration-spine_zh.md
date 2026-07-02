# Gopact Integration Spine Implementation Plan

<!-- gopact:doc-language: zh -->

[英文文档](./2026-06-23-gopact-integration-spine.md)

## 中文

> **For agentic workers:** Continue from M3 Tool and Sandbox Spine. Keep external protocols as contracts and fakes until concrete adapters need to bind real network formats.

**Goal:** Build the first M4 Integration Spine slice so `gopact` has testable memory, skill, MCP, and A2A module boundaries before TurnLoop orchestration is implemented.

## Implemented In This Pass

- `memory`
  - `Store` contract;
  - in-memory implementation;
  - memory types: semantic, episodic, procedural, profile;
  - scope filtering by user/session/thread/agent/app;
  - text search and type filtering;
  - delete and not-found behavior;
  - metadata copy-on-write.
- `skill`
  - skill registry;
  - resource/script declarations;
  - search;
  - activation record;
  - duplicate and missing skill errors.
- `mcp`
  - server contract;
  - manager for connected fake/in-memory servers;
  - namespaced tools and prompts;
  - resources and prompt listing;
  - duplicate and invalid server checks.
- `a2a`
  - agent card contract;
  - task/result contract;
  - registry and fake agent;
  - send task behavior;
  - duplicate/missing/invalid agent checks.
- root event type constants for memory, skill, MCP, and A2A lifecycle events.

## Not Completed Yet

- `TurnLoop`;
- cancel/preempt/resume queue;
- runner-level module injection;
- policy middleware around memory/skill/MCP/A2A;
- MCP client/server protocol transport;
- A2A HTTP/SSE transport;
- skill filesystem loader and sandbox script bridge;
- cross-module event sink integration.

## Verification

Commands used:

```bash
go test ./... -count=1
go vet ./...
git diff --check
```
