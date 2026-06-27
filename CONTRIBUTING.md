# Contributing to gopact

`gopact` is a Go-first agent SDK. Contributions should preserve the SDK boundary:
core contracts, light default implementations, reference adapters, conformance
helpers, and external adapter/plugin/template ownership.

## Development Setup

Prerequisites:

- Go 1.25.11
- Git
- `golangci-lint` v2.8.0
- `govulncheck` v1.1.4
- GitHub CLI for maintainers running remote readiness checks

Clone and verify the repository:

```bash
git clone git@github.com:gopact-ai/gopact.git
cd gopact
go test -count=1 ./...
```

## Change Guidelines

- Keep SDK core provider-neutral and host-configured.
- Do not add production provider, backend, or channel integrations to the core
  module unless design manifests are updated and the package is marked
  reference-only or transitional.
- Add or update tests before changing behavior.
- Update design manifests and public examples for public API changes.
- Do not commit secrets, raw prompts, raw model responses, raw tool payloads, or
  private customer data.

## Verification

Before opening a pull request, run:

```bash
git diff --check
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
golangci-lint run ./...
go test -coverprofile=coverage.out ./...
go test -run '^Example' ./...
govulncheck ./...
```

External adapter/template readiness may also require:

```bash
go run ./cmd/gopact-extscaffold -root . -remote-status-json
```

That command is read-only. Secret configuration must be performed explicitly by
maintainers.

## Pull Request Checklist

- Tests cover the changed behavior or documentation guarantee.
- Public API changes update `docs/design/public-api-boundary.json` and examples
  when applicable.
- Repository-boundary changes update `docs/design/repository-boundary.json`,
  `docs/design/v1-migration-plan.json`, and extension manifests when applicable.
- Documentation avoids unsupported maturity claims such as production ready
  unless the release gate proves them.
- The worktree contains no secrets or generated noise.
