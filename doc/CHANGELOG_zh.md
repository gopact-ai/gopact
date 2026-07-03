# Changelog

<!-- gopact:doc-language: zh -->

[英文文档](./CHANGELOG.md)

## 中文

本文件记录 `gopact` core 的用户可见变化。内部重排、纯测试搬迁和没有行为影响的维护任务通常只保留在 Git 历史中。

## Unreleased

### Added

- A2A registry and mesh lease registration plus heartbeat renewal for long-running agent discovery.
- A2A mesh health-driven eviction with `a2a_agent_evicted` evidence for unready HTTP-backed agents.
- `a2a.Mesh.Sync` and `a2a.Mesh.SyncEnv` for one-shot discovery sync, readiness eviction, final card snapshots, and aggregated mesh evidence.
- `a2a.Mesh.SyncEvery` for continuous A2A mesh sync until context cancellation.
- `a2a.Mesh.SyncEnvEvery` for continuous environment-driven A2A mesh sync until context cancellation.
- A2A HTTP agent-card discovery with readiness checks for mesh bootstrap.
- `gopact agent init` and `gopact agent run` for generating and running a standalone A2A HTTP agent scaffold.
- Core feature coverage matrix in [FEATURES.md](FEATURES.md).
- Public repository governance: PR-only `main`, required CI gates, admin auto-merge, non-admin admin-approval gate, and public readiness checks.
- MIT license.
- Provider-neutral tool choice contract for automatic, required, named, and disabled tool selection.
- `graph.EmitNodeEvent` and `graph.ErrNodeEventYieldStopped` for adapter nodes that need to publish child runtime events into the parent graph stream.
- Self-bootstrap release gates now require run replay plan evidence derived from the run export.

### Changed

- Feature coverage now names the tested graph orchestration surface: step export/import, interrupted step resume, branch routing, DAG fan-in, dynamic fan-out, loop step limits, runnable subgraphs, and node-emitted nested events.
- README and `doc/` structure now separate first-reader documentation, design records, maintainer process, and historical execution plans.
- CI runs hygiene, unit, race, static analysis, coverage, conformance, and security gates in parallel while preserving a required aggregate `test` job.
- A2A file discovery accepts both `{"agents":[...]}` documents and bare agent-card arrays.
- Generated A2A agent scaffolds include health/readiness tests and graceful HTTP shutdown.

### Known Limitations

- The project is pre-v1; public API may still change under the documented migration and deprecation policies.
- Production provider, storage, channel, observability, and agent-template integrations live in `gopact-ext`, not this core repository.
