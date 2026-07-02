# gopact Migration Guide

<!-- gopact:doc-language: en -->

Chinese documentation: [migration-guide_zh.md](migration-guide_zh.md)

Migration guide for pre-v1 and v1 changes. It explains compatibility promises, API changes, adapter split, checkpoint/resume migration, and verification requirements.

## Compatibility Promise

Pre-v1 APIs may change, but public changes must be represented in [v1-migration-plan.json](v1-migration-plan.json). The v1 release gate uses `release_gate_checks` and `required_check_ids`.

Compatibility follows [versioning-policy.md](versioning-policy.md).

## API Changes

Public API changes must preserve examples in `public-api-examples.json` or document a replacement.

## Adapter Split

Provider, backend, channel, and template integrations should move to `gopact-ext` or host applications.

## Checkpoint and Resume

Checkpoint and resume changes must preserve migration notes for stored state and pending interrupts.

## Verification

Migration work must pass local tests, conformance suites, and release-gate checks.
