# Contributing to gopact

<!-- gopact:doc-language: en -->

Chinese documentation: [CONTRIBUTING_zh.md](CONTRIBUTING_zh.md)

Thank you for contributing to `gopact`. This repository is the core SDK. It should keep provider, UI, storage, and business-agent integrations out of core unless they are explicit reference implementations or public contracts. Production integrations belong in `gopact-ext`.

## Development Setup

Prerequisites:

- Go 1.25 or newer;
- Git;
- `golangci-lint` v2.8.x;
- `govulncheck` v1.1.x;
- GitHub CLI for maintainers who operate PRs, CI, or releases.

Clone and run the minimal verification:

```bash
git clone git@github.com:gopact-ai/gopact.git
cd gopact
go test -count=1 ./...
```

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
./scripts/public-readiness-check.sh
```

If you change graph, checkpoint, provider, tool, MCP, A2A, sandbox, verification, or release-gate behavior, also run the relevant conformance command from [FEATURES.md](FEATURES.md).

## Pull Request Checklist

A pull request should:

- include tests or conformance coverage for behavior changes;
- update `doc/design/public-api-examples.json` and executable examples for public API changes;
- update `doc/design/ecosystem-topology.json`, `doc/design/v1-migration-plan.json`, or related design docs when core/ext/examples ownership changes;
- keep the root README focused on first-time readers and move detailed design notes to `doc/design/`;
- avoid committing secrets, tokens, raw prompts, raw model responses, raw tool payloads, customer data, or generated noise;
- list verification commands and results in the PR body.

Repository rules:

- `main` is updated only through pull requests;
- admin-authored PRs can squash auto-merge after required checks pass;
- non-admin-authored PRs require at least one admin approval on the latest commit;
- CI is the merge standard and is not replaced by local verbal confirmation.
