# gopact Architecture Overview

<!-- gopact:doc-language: en -->

Chinese documentation: [index_zh.md](index_zh.md)

Main architecture map for gopact. It links design philosophy, runtime modules, template boundaries, release gates, migration documents, and research background.

## Index

- [../FEATURES.md](../FEATURES.md): executable feature coverage matrix. Canonical path: `doc/FEATURES.md`.
- [modules.md](modules.md): runtime module boundaries.
- [workflow-orchestration-matrix.json](workflow-orchestration-matrix.json): completed graph orchestration capabilities, proof commands, conformance cases, and planned orchestration gaps.
- [templates.md](templates.md): ReAct, Agent-as-Tool, Dev Agent, and template ownership.
- [migration-guide.md](migration-guide.md): compatibility and migration guide.
- [template-guide.md](template-guide.md): external graph template authoring rules.
- [deprecation-policy.md](deprecation-policy.md): public API deprecation rules.
- [versioning-policy.md](versioning-policy.md): module, schema, and extension versioning rules.
- [ecosystem-topology.json](ecosystem-topology.json): official core/ext/examples repository topology.
- [v1-migration-plan.json](v1-migration-plan.json): v1 migration and release gate manifest.
- [milestone-readiness.json](milestone-readiness.json): milestone readiness evidence.
- [extension-scaffold-spec.json](extension-scaffold-spec.json): legacy external scaffold record superseded by `ecosystem-topology.json`.
- [self-bootstrap-roadmap.md](self-bootstrap-roadmap.md): long-running self-bootstrap phase goal, test standards, and release gates.

## Release Evidence

Core CI evidence is recorded through `gopacttest.RecordCIGateSuiteCheck` as `ci_gate` evidence. Legacy extension scaffold materialization is retained in `internal/extensionscaffold`, including `LoadRepositoriesFromDesign`, `WriteRepositoriesFromDesign`, and `RenderSyncPlanFromDesign`; generated material must preserve `V1 Migration Ownership`.

Legacy extension scaffold sync records include `go.work` and `sync-plan.json`. New official extensions live in `gopact-ext`.

The v1 migration release gate documents `release_gate_checks` and `required_check_ids`.

Dev Agent process evidence is recorded through `RecordProcessRecords`, `BuildWorkflowProcessRecords`, `RecordWorkflowProcessRecords`, `BuildReleaseBundle`, `ReleaseBundle`, `RecordReleaseBundleCheck`, and `release_bundle` evidence.
