# Changelog

All notable changes to `gopact` will be documented here.

This project is pre-v1. Entries describe user-visible SDK, schema, conformance,
template, adapter-boundary, and release-process changes.

## Unreleased

### Added

- `a2a.NewHTTPCardListers` for bootstrapping a mesh from multiple HTTP agent
  card endpoints.
- `a2a.WithHTTPReadinessCheck` for opt-in readiness-gated HTTP agent discovery.
- `gopact agent init` for generating a standalone, testable A2A HTTP agent
  scaffold.
- Public readiness checks for tracked files and commit messages before changing
  repository visibility.
- PR governance workflows that allow admin-authored PRs to auto-merge after CI
  and require admin approval for non-admin-authored PRs.
- MIT license in `LICENSE`.
- Model reviewer governance field requirements through
  `adapters/devagent/modelreview.WithRequiredGovernanceFields`.
- CI reviewer required `ci_gate` checks through
  `github.com/gopact-ai/gopact-templates-devagent/cireview.WithRequiredCIGates`.
- External repository readiness evidence export and remote CI gate evidence for
  `gopact-ai` extension repositories.
- Core feature coverage matrix in `FEATURES.md`.
- Provider-neutral `ToolChoice` request contract with options for auto,
  required, named, and disabled tool selection.

### Changed

- `a2a.FileDiscoverer` now accepts either `{"agents":[...]}` documents or a
  bare agent-card JSON array for lower-friction local mesh registries.
- v1 migration release gate now consumes explicit core and external CI gate
  requirements.
- CI now runs independent hygiene, unit, race, static-analysis, coverage,
  conformance, and security gates in parallel while preserving `ci/test` as the
  required aggregate status check.
- Self-bootstrap release gate now requires the core feature coverage snapshot.
- Self-bootstrap release gate now requires A2A mesh conformance command
  evidence.
- Self-bootstrap release gate now requires explicit local Agnes integration
  command evidence for provider, agent template, and example coverage.
- Development docs now distinguish core SDK readiness from external
  adapter/plugin/template readiness.
- `gopact agent init` development-build fallback now targets `gopact v0.0.21`.
- Generated A2A agent scaffolds now handle interrupt/terminate signals with graceful HTTP shutdown.
- Generated A2A agent scaffold tests now verify the advertised health and readiness endpoints.

### Known Blockers

- Branch protection or repository rulesets must be applied after the repository
  is public so `main` requires the `ci` and `author-policy` checks.
