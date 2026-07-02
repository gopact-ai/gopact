# gopact

[![CI](https://github.com/gopact-ai/gopact/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/gopact-ai/gopact/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/gopact-ai/gopact)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/gopact-ai/gopact.svg)](https://pkg.go.dev/github.com/gopact-ai/gopact)

<!-- gopact:doc-language: zh,en -->

## 中文

`gopact` 是一个 Go-first、provider-neutral 的 agent SDK 骨架，重点放在显式契约、类型化 workflow/graph 执行、可观察事件流，以及任意稳定 step 边界的 export/import/resume。

当前仓库保存 core SDK：公共契约、运行时 facade、轻量参考实现、conformance helper 和 release gate evidence。生产 provider、存储、channel、observability 与常用 agent template 由 [gopact-ext](https://github.com/gopact-ai/gopact-ext) 提供；可运行示例由 [gopact-examples](https://github.com/gopact-ai/gopact-examples) 提供。

## 安装

```bash
go get github.com/gopact-ai/gopact
```

SDK 自身不读取配置文件、环境变量或本地 secret；provider、backend、channel、plugin 的配置都应由宿主应用通过 Go options、接口或 typed snapshot 注入。

## 快速开始

最短可执行路径是 `example_test.go` 里的 `Example_graphRun`：它创建 typed graph，运行单个 node，读取事件流，并通过内存 checkpoint store 持久化 step 边界。

```bash
go test -run Example_graphRun .
```

从零启动一个可测试的 A2A HTTP agent scaffold：

```bash
go run ./cmd/gopact agent init support-agent -module example.com/support-agent -out /tmp/support-agent
(cd /tmp/support-agent && go test ./...)
go run ./cmd/gopact agent run /tmp/support-agent
```

## 核心概念

- `Setup` / `Defaults`：SDK 级默认值入口，支持宿主注入 logger、log level 和 runtime identity defaults。
- `Runner` / `TurnLoop`：`Runner` 执行一次 run；`TurnLoop` 处理多轮输入、抢占、取消和恢复。
- `graph`：类型化 workflow 执行层，负责 node、edge、middleware、event stream 和 step 边界。
- `RunExport` / `StepExport`：过程导出和恢复契约，目标是在任意稳定 step 边界中断后可以 import/resume。
- `VerificationRecorder`：记录已观察证据，不替宿主执行隐藏命令，也不保存 raw prompt、raw response 或 secret。
- adapter / plugin / template：生产 provider、backend、channel、observability 和业务 agent 组装通过外部 extension 表达。

## 当前稳定性

`gopact` 仍是 pre-v1 SDK。当前适合 SDK API 评审、template/conformance 开发、外部 adapter scaffold 和自举实验，不应被包装成成熟的完整 agent 平台。

## 文档地图

- [doc/README.md](doc/README.md)：完整文档索引。
- [doc/FEATURES.md](doc/FEATURES.md)：core SDK 可执行能力覆盖矩阵；ext/examples 仓库各自维护对应矩阵。
- [doc/design/index.md](doc/design/index.md)：总体设计入口、模块关系和路线图。
- [doc/design/development-plan.md](doc/design/development-plan.md)：研发计划、自举门槛和开源化发布手册。
- [doc/design/public-api-examples.json](doc/design/public-api-examples.json)：root public API executable example 契约。
- [doc/design/migration-guide.md](doc/design/migration-guide.md)：v1 前后 public API、adapter split、checkpoint/resume 和 verification 迁移要求。
- [doc/design/template-guide.md](doc/design/template-guide.md)：外部 graph template 的边界、step export/resume、events/verification 和 conformance 要求。
- [doc/design/deprecation-policy.md](doc/design/deprecation-policy.md)：root public API 的废弃、迁移和移除策略。
- [doc/design/versioning-policy.md](doc/design/versioning-policy.md)：core SDK、schema 和外部 extension 的版本策略。
- [doc/design/ecosystem-topology.json](doc/design/ecosystem-topology.json)：官方三仓拓扑。
- [doc/design/v1-migration-plan.json](doc/design/v1-migration-plan.json)：v1 前 core 边界收敛和 release gate 计划；每个 gate 声明 `release_gate_checks` 和 `required_check_ids`。
- [doc/design/milestone-readiness.json](doc/design/milestone-readiness.json)：阶段性 readiness evidence。
- [doc/design/templates.md](doc/design/templates.md)：ReAct、Agent-as-Tool、Dev Agent 等 graph template 边界。
- [doc/design/modules.md](doc/design/modules.md)：provider、tool、sandbox、memory、skill、MCP、A2A 等运行时模块设计。
- [doc/design/external-integration-roadmap.json](doc/design/external-integration-roadmap.json)：生产 adapter/plugin/template 的外部仓库路线。
- [doc/design/extension-scaffold-spec.json](doc/design/extension-scaffold-spec.json)：旧外部仓库 scaffold 蓝图；实现入口在 `internal/extensionscaffold`、`LoadRepositoriesFromDesign`、`WriteRepositoriesFromDesign`、`RenderSyncPlanFromDesign` 和 `cmd/gopact-extscaffold`，并维护 `V1 Migration Ownership`、`go.work`、`sync-plan.json`、`-verify`、`-plan-json` 和 `-remote-status-json` 流程。
- [doc/design/core-ci-gates.json](doc/design/core-ci-gates.json)：core repo CI gate 清单；`gopacttest.RecordCIGateSuiteCheck` 可把已观察 gate suite 记录为 `ci_gate` evidence。
- [doc/maintainers/repository-governance.md](doc/maintainers/repository-governance.md)：公开仓库的 PR、CI、自动合并和发布前检查规则。

## 贡献与安全

贡献入口见 [doc/CONTRIBUTING.md](doc/CONTRIBUTING.md)，安全策略见 [doc/SECURITY.md](doc/SECURITY.md)，变更记录见 [doc/CHANGELOG.md](doc/CHANGELOG.md)。本仓库采用 [MIT 协议](LICENSE)。

## 开发

```bash
make fmt
make test
make vet
```

## English

`gopact` is a Go-first, provider-neutral agent SDK skeleton for explicit contracts, typed workflow/graph execution, observable event streams, and export/import/resume at stable step boundaries.

This repository contains the core SDK: public contracts, runtime facades, lightweight reference implementations, conformance helpers, and release-gate evidence. Production providers, storage backends, channels, observability adapters, and reusable agent templates live in [gopact-ext](https://github.com/gopact-ai/gopact-ext). Runnable examples live in [gopact-examples](https://github.com/gopact-ai/gopact-examples).

Install:

```bash
go get github.com/gopact-ai/gopact
```

Start with `Example_graphRun`:

```bash
go test -run Example_graphRun .
```

The main documentation index is [doc/README.md](doc/README.md). The capability matrix is [doc/FEATURES.md](doc/FEATURES.md). Contribution, security, and changelog documents are under [doc/](doc/).
