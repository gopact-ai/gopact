# Repository Governance

<!-- gopact:doc-language: en -->

Chinese documentation: [repository-governance_zh.md](repository-governance_zh.md)

This document defines maintenance rules for the public repository. Even when there is only one active maintainer, `main` must be updated through pull requests and CI gates so every change has audit history, test evidence, and auto-merge state.

## Pull Request Flow

The `main` branch requires:

- Require status checks to pass before merge;
- updates through pull requests only;
- all required status checks passing;
- the branch to be up to date with `main`;
- no force pushes or deletion;
- linear history;
- squash merge only;
- all conversations resolved.

Required checks:

- `hygiene`
- `unit`
- `race`
- `static`
- `coverage`
- `conformance`, core repository only
- `security`
- `test`
- `author-policy`

`author-policy` rules:

- Admin-authored PRs can merge after CI passes;
- Non-admin-authored PRs require at least one admin approval on the latest commit;
- approval on an older commit does not satisfy the gate after another push.
- Do not configure a global required review count; `author-policy` implements
  the conditional review rule without blocking a single admin maintainer.

## Admin Auto-Merge

The `admin-automerge` workflow applies only to admin-authored PRs. It enables squash auto-merge, and GitHub merges the PR after required checks pass.

The workflow does not check out or execute PR code. Non-admin PRs do not get auto-merge enabled and must be approved by an admin before merge.

Repository settings should remain:

- `allow_auto_merge: true`
- `allow_squash_merge: true`
- `allow_merge_commit: false`
- `allow_rebase_merge: false`
- `delete_branch_on_merge: true`
- secret scanning and push protection enabled
- Dependabot security updates enabled

## Public Release Checks

Before making a public release, changing branch rules, or publishing a tag, maintainers should verify:

```bash
./scripts/public-readiness-check.sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
golangci-lint run ./...
go test -coverprofile=coverage.out ./...
govulncheck ./...
```

`public-readiness-check.sh` scans tracked files and commit messages for high-confidence secret patterns. It reports file names or commit hashes only; it does not print matched secret content.
