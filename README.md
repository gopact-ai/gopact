# gopact

#### Go SDK contracts for typed, observable, and resumable agent workflows.

[![CI](https://github.com/gopact-ai/gopact/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/gopact-ai/gopact/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gopact-ai/gopact.svg)](https://pkg.go.dev/github.com/gopact-ai/gopact)
[![License](https://img.shields.io/github/license/gopact-ai/gopact)](LICENSE)

<!-- gopact:doc-language: en -->

Chinese documentation: [README_zh.md](README_zh.md)

`gopact` is the provider-neutral core for building Go agent systems with typed workflow graphs, event streams, checkpoints, resumable runs, tool/MCP/A2A boundaries, policy hooks, and release evidence.

`gopact` stays small on purpose. Model providers, production agent templates, development-agent helpers, and runnable examples live in sibling repositories:

- [`gopact`](https://github.com/gopact-ai/gopact): core SDK, public contracts, reference implementations, and conformance helpers.
- [`gopact-ext`](https://github.com/gopact-ai/gopact-ext): official providers, agent templates, and development-agent helpers.
- [`gopact-examples`](https://github.com/gopact-ai/gopact-examples): runnable quickstarts for workflows, providers, and agent clusters.

## Installation

Go 1.25 or newer is required.

```bash
go get github.com/gopact-ai/gopact
```

The core SDK does not read `.env`, configuration files, or local secrets. Hosts inject models, tools, storage, channels, and policy through Go interfaces and options.

## Usage

Run the smallest executable graph example:

```bash
go test -run Example_graphRun .
```

Generate and test an A2A HTTP agent scaffold:

```bash
go run ./cmd/gopact agent init support-agent -module example.com/support-agent -out /tmp/support-agent
(cd /tmp/support-agent && go test ./...)
go run ./cmd/gopact agent run /tmp/support-agent
```

Use [`gopact-examples`](https://github.com/gopact-ai/gopact-examples) when you want a complete runnable provider or agent-template path.

## Features

- Typed graph runtime with named nodes, edges, middleware, events, and step snapshots.
- Checkpoint stores, codecs, resume payload validation, and stable recovery boundaries.
- Provider-neutral `ModelRequest`, model response, streaming, tool-call, and conformance contracts.
- Local tools, MCP servers, and A2A agents behind explicit capability boundaries.
- Policy, redaction, artifact verification, and secret-handling hooks for host-owned safety.
- `VerificationRecorder` support for tests, CI, file snapshots, review evidence, and release evidence.

See [doc/FEATURES.md](doc/FEATURES.md) for the executable feature matrix.

## Stability

`gopact` is pre-v1. It is ready for API review, conformance work, extension development, and early application integration. Public APIs may still change before v1.

The core repository intentionally does not ship production model providers, cloud storage adapters, vector databases, or UI channels. Those belong in `gopact-ext` or host applications.

## Documentation

- [doc/README.md](doc/README.md): documentation index.
- [doc/FEATURES.md](doc/FEATURES.md): core capability matrix and offline verification commands.
- [doc/design/index.md](doc/design/index.md): architecture and roadmap entry point.
- [doc/design/modules.md](doc/design/modules.md): runtime module boundaries.
- [doc/design/templates.md](doc/design/templates.md): ReAct, Agent-as-Tool, Dev Agent, and template boundaries.
- [doc/design/migration-guide.md](doc/design/migration-guide.md): compatibility and migration guide.
- [doc/design/template-guide.md](doc/design/template-guide.md): external graph template guide.
- [doc/design/deprecation-policy.md](doc/design/deprecation-policy.md): public API deprecation rules.
- [doc/design/versioning-policy.md](doc/design/versioning-policy.md): module, schema, and extension versioning rules.
- [doc/design/public-api-examples.json](doc/design/public-api-examples.json): public API example coverage contract.
- [doc/design/ecosystem-topology.json](doc/design/ecosystem-topology.json): official repository topology.
- [doc/design/v1-migration-plan.json](doc/design/v1-migration-plan.json): v1 release-gate and migration manifest.
- [doc/design/milestone-readiness.json](doc/design/milestone-readiness.json): milestone readiness evidence.
- [doc/design/external-integration-roadmap.json](doc/design/external-integration-roadmap.json): provider/backend/channel/template extension roadmap.
- [doc/design/extension-scaffold-spec.json](doc/design/extension-scaffold-spec.json): legacy external scaffold record.
- [doc/maintainers/repository-governance.md](doc/maintainers/repository-governance.md): PR, CI, auto-merge, and public repository governance.

## Contributing

- [doc/CONTRIBUTING.md](doc/CONTRIBUTING.md): development setup and pull request checklist.
- [doc/SECURITY.md](doc/SECURITY.md): security policy and vulnerability reporting.
- [doc/CHANGELOG.md](doc/CHANGELOG.md): user-visible changes.
- [MIT License](LICENSE).
