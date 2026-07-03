# Changelog

<!-- gopact:doc-language: en -->

Chinese documentation: [CHANGELOG_zh.md](CHANGELOG_zh.md)

This file records user-visible changes to `gopact` core. Internal reshuffling, pure test movement, and maintenance changes without behavior impact usually belong in Git history only.

## Unreleased

### Added

- A2A registry and mesh lease registration plus heartbeat renewal for long-running agent discovery.
- A2A mesh health-driven eviction with `a2a_agent_evicted` evidence for unready HTTP-backed agents.
- `a2a.Mesh.Sync` and `a2a.Mesh.SyncEnv` for one-shot discovery sync, readiness eviction, final card snapshots, and aggregated mesh evidence.
- `a2a.Mesh.SyncEvery` for continuous A2A mesh sync until context cancellation.
- `a2a.Mesh.SyncEnvEvery` for continuous environment-driven A2A mesh sync until context cancellation.
- A2A HTTP agent-card discovery with readiness checks for mesh bootstrap.
- `gopact agent init` and `gopact agent run` for generating and running a standalone A2A HTTP agent scaffold.
- `gopact agent verify` for mock-only scaffold validation of required files, A2A registry shape, and `go test ./...`.
- Core feature coverage matrix in [FEATURES.md](FEATURES.md).
- Public repository governance: PR-only `main`, required CI gates, admin auto-merge, non-admin admin-approval gate, and public readiness checks.
- MIT license.
- Provider-neutral tool choice contract for automatic, required, named, and disabled tool selection.
- `graph.EmitNodeEvent` and `graph.ErrNodeEventYieldStopped` for adapter nodes that need to publish child runtime events into the parent graph stream.
- Self-bootstrap release gates now require run replay plan evidence derived from the run export.

### Changed

- `a2a.Mesh.BootstrapEnv` and `a2a.Mesh.SyncEnv` now apply mesh-level HTTP agent options while discovering environment-configured agents.
- Feature coverage now names the tested graph orchestration surface: step export/import, interrupted step resume, branch routing, DAG fan-in, dynamic fan-out, loop step limits, runnable subgraphs, and node-emitted nested events.
- Workflow orchestration matrix now records the `gopact-ext` human review template, durable scheduler, Dev Agent self-bootstrap workflow, and Dev Agent workspace adapter including policy-approved plan patch apply as completed with offline proof.
- README and `doc/` structure now separate first-reader documentation, design records, maintainer process, and historical execution plans.
- CI runs hygiene, unit, race, static analysis, coverage, conformance, and security gates in parallel while preserving a required aggregate `test` job.
- A2A file discovery accepts both `{"agents":[...]}` documents and bare agent-card arrays.
- Generated A2A agent scaffolds include health/readiness tests and graceful HTTP shutdown.
- Local `gopact agent init` fallback SDK version is pinned to the current released core tag.

### Known Limitations

- The project is pre-v1; public API may still change under the documented migration and deprecation policies.
- Production provider, storage, channel, observability, and agent-template integrations live in `gopact-ext`, not this core repository.
