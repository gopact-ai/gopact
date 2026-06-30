# Changelog

All notable changes to `gopact` will be documented here.

This project is pre-v1. Entries describe user-visible SDK, schema, conformance,
template, adapter-boundary, and release-process changes.

## Unreleased

### Added

- MIT license in `LICENSE`.
- Model reviewer governance field requirements through
  `adapters/devagent/modelreview.WithRequiredGovernanceFields`.
- CI reviewer required `ci_gate` checks through
  `github.com/gopact-ai/gopact-templates-devagent/cireview.WithRequiredCIGates`.
- External repository readiness evidence export and remote CI gate evidence for
  `gopact-ai` extension repositories.

### Changed

- v1 migration release gate now consumes explicit core and external CI gate
  requirements.
- Development docs now distinguish core SDK readiness from external
  adapter/plugin/template readiness.

### Known Blockers

- External private scaffold repositories still require `GOPACT_GITHUB_TOKEN` and
  passing latest CI before M6 can be complete.
