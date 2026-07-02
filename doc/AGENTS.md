# AGENTS.md

<!-- gopact:doc-language: zh,en -->

## 中文

## Scope

These instructions apply to the entire repository.

## Project Overview

`gopact` is a Go-first, provider-neutral agent SDK. The core repository should
contain SDK contracts, runtime facades, lightweight reference implementations,
conformance helpers, and release-gate evidence. Production provider, storage,
channel, observability, and template integrations generally belong in external
adapter or template repositories unless the design manifests explicitly say
otherwise.

Use `doc/design/index.md` as the design map before making architectural or
public API changes.

## Repository Map

- Root package files define the public SDK surface: setup/defaults, runner,
  events, policies, messages, tools, checkpoints, artifacts, verification, and
  export/resume contracts.
- `artifact`, `checkpoint`, `memory`, and `sandbox` contain core contracts and
  local or in-memory reference implementations.
- `provider`, `mcp`, `a2a`, `tools`, `skill`, `graph`, and `templates/react`
  contain runtime modules, protocol contracts, or graph/template support.
- `adapters` contains reference-only or transitional adapters. Check
  `doc/design/repository-boundary.json` before adding, moving, or expanding an
  adapter.
- `gopacttest` contains conformance helpers and verification utilities used by
  this repo and external extension repos.
- `internal/repositorychecks` enforces design-manifest and release-readiness
  guarantees. `internal/extensionscaffold` and `cmd/gopact-extscaffold` generate
  and audit external repository scaffolds.
- `doc/design` is the source of truth for module boundaries, public API policy,
  migration status, extension readiness, and CI/release gates.

## Development Rules

- Keep the core SDK provider-neutral and host-configured. Do not add production
  model providers, cloud backends, platform channels, or vendor observability
  clients to core without updating the relevant design manifests.
- Prefer standard library and this module's own packages for reference-only
  implementations. Add dependencies only when the design and tests justify the
  cost.
- Preserve public API compatibility. Public root-package additions, removals, or
  semantic changes must update `doc/design/public-api-boundary.json` and, when
  applicable, `doc/design/public-api-examples.json`, examples, migration docs,
  and deprecation policy.
- Repository-boundary changes must update `doc/design/repository-boundary.json`,
  `doc/design/v1-migration-plan.json`, and extension manifests when applicable.
- Add or update tests before changing behavior. Keep tests focused on the
  contract being changed, and extend conformance helpers when the behavior is a
  reusable SDK guarantee.
- Keep generated or local verification artifacts such as `coverage.out` out of
  commits unless a task explicitly requires them.

## Go Conventions

- Use Go 1.25.11.
- Run `gofmt` on edited Go files. Keep code idiomatic, small, and explicit.
- Prefer table-driven tests for contract matrices and edge cases.
- Use `context.Context` at API boundaries where cancellation or deadlines can
  matter.
- Return errors with useful context and preserve wrapping with `%w` when callers
  may need `errors.Is` or `errors.As`.
- Avoid panics in SDK paths except for programmer-error cases that are already
  established by the surrounding code.

## Verification

Use the narrowest useful command while iterating, then broaden before finishing
larger changes.

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `golangci-lint run ./...`
- `go test -coverprofile=coverage.out ./...`
- `go test -run '^Example' ./...`
- `govulncheck ./...`

`make check` runs the full local verification set, including whitespace,
tests, race, vet, lint, coverage, examples, and vulnerability checks.

## Security And Data Handling

- Do not commit secrets, tokens, raw prompts, raw model responses, raw tool
  payloads, private customer data, or unredacted external transcripts.
- Prefer redacted fixtures and shape metadata in tests and documentation.
- Security-sensitive changes must preserve policy, redaction, sandbox,
  verification, and release-gate evidence boundaries.

## Documentation

- Keep documentation concise and factual. Avoid maturity claims such as
  production ready unless release-gate evidence proves them.
- Update `CONTRIBUTING.md`, `CHANGELOG.md`, and relevant design docs when a
  change affects contributor workflow, user-visible behavior, public API, or
  release readiness.
- Prefer executable examples for public API usage so `go test -run '^Example'`
  verifies them.

## Agent Workflow

- Check `git status --short` before editing. Do not overwrite unrelated user
  changes.
- Read the relevant design docs and manifests before changing package
  boundaries, public API, adapters, templates, or release gates.
- Keep edits scoped to the requested change and the directly affected tests or
  docs.
- Report verification commands that were run, and state clearly when a command
  was skipped.

## English

Repository instructions for coding agents. It summarizes the core/ext/examples boundary, required verification, security constraints, and documentation rules for future automated changes.
