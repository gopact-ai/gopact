# gopact Development Plan

<!-- gopact:doc-language: en -->

Chinese documentation: [development-plan_zh.md](development-plan_zh.md)

Engineering plan and release-readiness record. It tracks staged SDK work, self-bootstrap goals, repository governance, and public-release criteria.

## Release Readiness Index

- [migration-guide.md](migration-guide.md)
- [template-guide.md](template-guide.md)
- [deprecation-policy.md](deprecation-policy.md)
- [versioning-policy.md](versioning-policy.md)
- [v1-migration-plan.json](v1-migration-plan.json)
- [milestone-readiness.json](milestone-readiness.json)
- [extension-scaffold-spec.json](extension-scaffold-spec.json)
- [../../CONTRIBUTING.md](../CONTRIBUTING.md)
- [../../SECURITY.md](../SECURITY.md)
- [../../CHANGELOG.md](../CHANGELOG.md)
- [../maintainers/repository-governance.md](../maintainers/repository-governance.md)
- LICENSE

Core CI evidence is recorded through `gopacttest.RecordCIGateSuiteCheck` as `ci_gate` evidence. Extension scaffold materialization is owned by `internal/extensionscaffold`, including `LoadRepositoriesFromDesign`, `WriteRepositoriesFromDesign`, and `RenderSyncPlanFromDesign`. The command entry point is `cmd/gopact-extscaffold`, and generated material must preserve `V1 Migration Ownership`.

Extension scaffold sync uses `go.work`, `sync-plan.json`, `-verify`, `-plan-json`, and `-remote-status-json`.

The v1 migration release gate documents `release_gate_checks` and `required_check_ids`.

Dev Agent process evidence is recorded through `RecordProcessRecords`, `BuildWorkflowProcessRecords`, `RecordWorkflowProcessRecords`, `BuildReleaseBundle`, `ReleaseBundle`, `RecordReleaseBundleCheck`, and `release_bundle` evidence.
