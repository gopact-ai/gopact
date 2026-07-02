# Contributing to gopact

<!-- gopact:doc-language: zh,en -->

## 中文

感谢你参与 `gopact`。这个仓库是 core SDK，不是 provider、UI、存储后端或业务 agent 的集合。贡献时请优先保持 core 小而清晰：公共契约稳定、默认实现可测试、生产集成放到 `gopact-ext`。

## Development Setup

准备环境：

- Go 1.25 或更新版本；
- Git；
- `golangci-lint` v2.8.x；
- `govulncheck` v1.1.x；
- GitHub CLI，仅维护者处理 PR/CI/发布时需要。

克隆并运行最小验证：

```bash
git clone git@github.com:gopact-ai/gopact.git
cd gopact
go test -count=1 ./...
```

## Verification

提交 PR 前至少运行：

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

如果改动了 graph、checkpoint、provider、tool、MCP、A2A、sandbox、verification 或 release gate，还要运行对应的 conformance 命令；这些命令列在 [FEATURES.md](FEATURES.md)。

## Pull Request Checklist

PR 应该满足：

- 行为变更有测试或 conformance 覆盖；
- public API 变更同步更新 `doc/design/public-api-examples.json` 和相关 Example；
- core/ext/examples 边界变化同步更新 `doc/design/ecosystem-topology.json`、`doc/design/v1-migration-plan.json` 或相关设计文档；
- README 面向首次访问者，设计细节进入 `doc/design/`；
- 不提交 secret、token、raw prompt、raw model response、raw tool payload、客户数据或生成噪声；
- PR 描述写清验证命令和结果。

维护规则：

- `main` 只能通过 PR 更新；
- admin 作者 PR 在 required checks 通过后可自动 squash merge；
- 非 admin 作者 PR 需要最新 commit 上至少一名 admin approval；
- CI 是合入标准，不使用本地口头确认替代。

## English

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
