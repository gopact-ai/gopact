# gopact Extension Conformance

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


This file documents the compatibility contract for one external gopact extension repository.

## Extension Target

- Target name: `<fill from doc/design/extension-conformance.json>`
- Kind: `adapter | plugin | template`
- Module path: `github.com/gopact-ai/<target-name>`
- Core paths replaced or extended: `<fill source_paths>`
- SDK module: `github.com/gopact-ai/gopact`
- Supported Go versions: `<fill sdk_compatibility.go_versions>`

## Required Suites

List every required conformance suite from the target entry in `doc/design/extension-conformance.json`.

- `<suite-name>`

Default conformance suites must run offline. Live provider, platform, cloud, or network checks belong behind explicit integration build tags.

## CI Commands

The default external repository CI must run:

```bash
git diff --check
go mod tidy && git diff --exit-code
go test -count=1 ./...
go vet ./...
```

The default offline suite should also call `gopacttest.RequireExtensionScaffoldConformance` with the repository module path, required scaffold files, and already-observed file contents. When a target suite has a known gopacttest helper, `CONFORMANCE.md` should record that helper reference and the offline suite should machine-check that the reference stays present.

While the core SDK repository is private, CI should set `GOPACT_GITHUB_TOKEN` to a token with read access to `github.com/gopact-ai/gopact`; the generated workflow falls back to `github.token` when organization settings allow cross-repository private reads. Local scaffold sync must materialize `go.sum` with `GOWORK=off go mod tidy` before pushing.

## Integration Tests

Document any optional live-service tests here.

- Build tag: `<integration tag, if any>`
- Required host-owned credentials or clients: `<none by default>`
- Services touched: `<none by default>`
- How to run manually: `go test -tags=<tag> ./...`

Integration tests must not run in the offline default CI path unless the repository explicitly owns the required service fixtures.

## Security Boundary

Describe the extension trust boundary:

- Who owns credentials, clients, endpoints, stores, and policies.
- What outbound network calls can happen.
- What persistence is used.
- What data is redacted before events, checkpoints, run exports, logs, traces, or model-visible context.

The SDK core must remain configuration-file free. Host applications inject configuration through typed constructors, options, clients, providers, adapters, or plugins.
