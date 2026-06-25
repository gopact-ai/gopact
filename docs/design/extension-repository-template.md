# <extension-name>

This repository implements a gopact extension outside the core SDK.

## Compatibility

- SDK module: `github.com/gopact-ai/gopact`
- Supported Go versions: `<fill from extension-conformance.json>`
- Extension kind: `adapter | plugin | template`
- Core source paths replaced or extended: `<fill source_paths>`

## Installation

```bash
go get <module-path>
```

## Usage

Show the smallest idiomatic constructor-based setup. The example must inject all credentials, endpoints, clients, stores, loggers, or policies from the host application. The extension must not read SDK-owned configuration files.

```go
// Replace this block with a minimal compileable setup example.
// Keep credentials, endpoints, clients, stores, loggers, and policies owned by
// the host application and passed through typed constructors or options.
```

## Conformance

Run the required conformance suites from `docs/design/extension-conformance.json`.

```bash
git diff --check
go test -count=1 ./...
go vet ./...
```

The offline test suite should include `gopacttest.RequireExtensionScaffoldConformance` so the repository layout, module path, host-owned configuration notes, CONFORMANCE commands, known helper references, and minimal example stay aligned with the scaffold contract.

While `github.com/gopact-ai/gopact` remains private, GitHub Actions should use a repository secret named `GOPACT_GITHUB_TOKEN` with read access to the core SDK repository; the generated workflow falls back to `github.token` when organization settings allow cross-repository private reads. The local `sync-repos.sh` prepares `go.sum` with `GOWORK=off go mod tidy` before pushing scaffold updates.

If the extension needs live services, keep those tests behind an explicit integration build tag and keep the default conformance suite offline.

Each scaffolded extension repository should start with:

- `go.mod`
- `README.md`
- `CONFORMANCE.md`
- `examples/minimal_test.go`
- `.github/workflows/ci.yml` copied from `docs/design/extension-ci-workflow.yml`

Only scaffold roadmap entries whose `scaffold_status` is `ready` in `docs/design/external-integration-roadmap.json`. Entries marked `pending` must first resolve their `scaffold_pending_reason`, usually by splitting protocol-specific conformance targets from core contracts.

`CONFORMANCE.md` should start from `docs/design/extension-conformance-template.md` and list the extension target name, the required suites, known gopacttest helper references, the exact CI commands, and any integration build tags that are intentionally excluded from the offline default suite.

## Examples

Each extension repository must include:

- a minimal constructor example;
- an extension scaffold conformance test;
- a helper-reference check that keeps `CONFORMANCE.md` aligned with available gopacttest conformance helpers;
- at least one error-path example or test;
- a policy/redaction/resume note when the extension crosses a trust boundary;
- a migration note when it replaces a package that currently exists in the core repository.

## Security

Document the trust boundary, secret ownership, outbound network behavior, persistence behavior, and redaction policy. Secrets must stay in host-owned clients, secret providers, or transport adapters, and must not be copied into events, checkpoints, run exports, or model-visible context.
