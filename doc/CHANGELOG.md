# Changelog

<!-- gopact:doc-language: en -->

Chinese documentation: [CHANGELOG_zh.md](CHANGELOG_zh.md)

This file records user-visible changes to `gopact` core. Internal reshuffling, pure test movement, and maintenance changes without behavior impact usually belong in Git history only.

## Unreleased

### Added

- A2A registry and mesh lease registration plus heartbeat renewal for long-running agent discovery.
- A2A mesh health-driven eviction with `a2a_agent_evicted` evidence for unready HTTP-backed agents.
- A2A HTTP agent-card discovery with readiness checks for mesh bootstrap.
- `gopact agent init` and `gopact agent run` for generating and running a standalone A2A HTTP agent scaffold.
- Core feature coverage matrix in [FEATURES.md](FEATURES.md).
- Public repository governance: PR-only `main`, required CI gates, admin auto-merge, non-admin admin-approval gate, and public readiness checks.
- MIT license.
- Provider-neutral tool choice contract for automatic, required, named, and disabled tool selection.

### Changed

- Feature coverage now names the tested graph orchestration surface: branch routing, DAG fan-in, dynamic fan-out, loop step limits, and runnable subgraphs.
- README and `doc/` structure now separate first-reader documentation, design records, maintainer process, and historical execution plans.
- CI runs hygiene, unit, race, static analysis, coverage, conformance, and security gates in parallel while preserving a required aggregate `test` job.
- A2A file discovery accepts both `{"agents":[...]}` documents and bare agent-card arrays.
- Generated A2A agent scaffolds include health/readiness tests and graceful HTTP shutdown.

### Known Limitations

- The project is pre-v1; public API may still change under the documented migration and deprecation policies.
- Production provider, storage, channel, observability, and agent-template integrations live in `gopact-ext`, not this core repository.
