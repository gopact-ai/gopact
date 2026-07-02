# gopact Versioning Policy

<!-- gopact:doc-language: en -->

Chinese documentation: [versioning-policy_zh.md](versioning-policy_zh.md)

Versioning policy for the core module, serialized schemas, and official extensions. It explains semver, stability states, release gates, and compatibility rules.

## Module Version

`gopact` follows semver. `v1` marks the first stable public API line. After v1, `major`, `minor`, and `patch` changes follow Go module compatibility rules.

## Stability States

Public APIs are tracked in `public-api-boundary.json` and may be `experimental`, `stable`, or `deprecated`.

## Release Gates

Release gates use `core-ci-gates.json`, `extension-conformance.json`, and `external-repositories.json`. Serialized evidence types such as `RunExport`, `StepExport`, and `CheckpointRecord` must stay compatible with their schema versions.

## External Extensions

Official extensions live in `gopact-ext`. Legacy scaffold material is described in `extension-scaffold-spec.json` and the v1 plan.

The v1 plan is tracked in [v1-migration-plan.json](v1-migration-plan.json).
It documents `release_gate_checks` and `required_check_ids` for the v1 release gate.

## Schema Versions

Schema changes must declare compatibility for run exports, step exports, checkpoints, conformance manifests, and release evidence.
