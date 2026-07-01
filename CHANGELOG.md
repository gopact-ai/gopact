# Changelog

All notable changes to `gopact` will be documented here.

This project is pre-v1. Entries describe user-visible SDK, schema, conformance,
template, adapter-boundary, and release-process changes.

## Unreleased

### Added

- `a2a.NewHTTPCardListers` for bootstrapping a mesh from multiple HTTP agent
  card endpoints.
- MIT license in `LICENSE`.
- Open-source release checklist in `docs/design/development-plan.md`.
- Model reviewer governance field requirements through
  `adapters/devagent/modelreview.WithRequiredGovernanceFields`.
- CI reviewer required `ci_gate` checks through
  `github.com/gopact-ai/gopact-templates-devagent/cireview.WithRequiredCIGates`.
- External repository readiness evidence export and remote CI gate evidence for
  `gopact-ai` extension repositories.
- Core feature coverage matrix in `FEATURES.md`.

### Changed

- v1 migration release gate now consumes explicit core and external CI gate
  requirements.
- Self-bootstrap release gate now requires the core feature coverage snapshot.
- Self-bootstrap release gate now requires A2A mesh conformance command
  evidence.
- Self-bootstrap release gate now requires explicit local Agnes integration
  command evidence for provider, agent template, and example coverage.
- Development docs now distinguish core SDK readiness from external
  adapter/plugin/template readiness.

### Known Blockers

- External private scaffold repositories still require `GOPACT_GITHUB_TOKEN` and
  passing latest CI before M6 can be complete.
