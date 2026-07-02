# Gopact Tool and Sandbox Spine Implementation Plan

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


> **For agentic workers:** Continue from M2 Model Spine. Keep local execution conservative: no shell by default, no path escape, no host env inheritance, explicit command allowlist.

**Goal:** Build the first M3 Tool and Sandbox Spine slice so `gopact` can safely manage tool visibility, store artifacts, and run local or in-memory sandbox operations under explicit boundaries.

## Implemented In This Pass

- `tools.Registry`
  - visible/deferred visibility;
  - namespace-qualified tool names;
  - search over name/description;
  - promotion from deferred to visible;
  - duplicate/invalid registration errors.
- `artifact.Memory`
  - process-local artifact store;
  - stable `ArtifactRef` id generation from SHA-256 when missing;
  - `Size` and `SHA256` metadata;
  - content copy-on-write behavior;
  - missing artifact error.
- `sandbox.Local`
  - required filesystem root;
  - path escape rejection;
  - argv-style command execution;
  - command allowlist;
  - no host env inheritance by default;
  - default timeout;
  - stdout/stderr output limit;
  - controlled read/write within root.
- `sandbox.Memory`
  - in-memory file read/write;
  - no real command execution.
- root event type constants for tool registry and sandbox lifecycle events.

## Not Completed Yet

- unified plugin registration hooks for tool middleware and policy hooks;
- event sink integration inside `tools.Registry` and `sandbox` sessions;
- tool-call graph recording;
- file/shell tools exposed through `tools.Registry`;
- sandbox artifact emission;
- memory, skill, MCP, and A2A modules from later M3/M4 scope;
- remote sandbox adapters.

## Verification

Commands used:

```bash
go test ./... -count=1
go vet ./...
git diff --check
```
