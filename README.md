# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化 workflow/graph 执行、可观察事件流，以及任意稳定 step 边界的 export/import/resume。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加生产 provider adapter 或完整业务 template。

## 安装

```bash
go get github.com/gopact-ai/gopact
```

当前仓库仍为私有，安装需要具备 `gopact-ai/gopact` 的访问权限。SDK 自身不读取配置文件、环境变量或本地 secret；provider、backend、channel、plugin 的配置都应由宿主应用通过 Go options、接口或 typed snapshot 注入。

## 快速开始

最短可执行路径是 `example_test.go` 里的 `Example_graphRun`：它创建 typed graph，运行单个 node，读取事件流，并通过内存 checkpoint store 持久化 step 边界。

```bash
go test -run Example_graphRun .
```

这个 example 覆盖 `graph.New`、`graph.WithRuntimeIDs`、`graph.WithCheckpointer`、事件流和 checkpoint 写入。更多 root facade 示例由 [docs/design/public-api-examples.json](docs/design/public-api-examples.json) 约束，并通过 `go test -run '^Example' ./...` 持续验证。

## 核心概念

- `Setup` / `Defaults`：SDK 级默认值入口，支持宿主注入 logger、log level 和 runtime identity defaults。
- `Runner` / `TurnLoop`：`Runner` 执行一次 run；`TurnLoop` 处理多轮输入、抢占、取消和恢复。
- `graph`：类型化 workflow 执行层，负责 node、edge、middleware、event stream 和 step 边界。
- `RunExport` / `StepExport`：过程导出和恢复契约，目标是在任意稳定 step 边界中断后可以 import/resume。
- `VerificationRecorder`：记录已观察证据，不替宿主执行隐藏命令，也不保存 raw prompt、raw response 或 secret。
- adapter / plugin / template：生产 provider、backend、channel、observability 和业务 agent 组装应通过外部 adapter/plugin/template 表达。

## 当前稳定性

`gopact` 仍是 pre-v1 SDK。当前适合内部实验、SDK API 评审、template/conformance 开发和外部 adapter scaffold，不应被包装成成熟的完整 agent 平台。

当前路线状态以 [docs/design/milestone-readiness.json](docs/design/milestone-readiness.json) 为准：M1 已完成，M2/M3/M4 是 first-slice complete，M5 partial，M6 in-progress。公开发布前仍需要项目 owner 选择并添加 `LICENSE`，外部私有仓库也必须完成 `GOPACT_GITHUB_TOKEN` secret 配置和 CI readiness。

## 文档地图

- [docs/design/index.md](docs/design/index.md)：总体设计入口、模块关系和路线图。
- [docs/design/development-plan.md](docs/design/development-plan.md)：研发计划、自举门槛和开源化发布手册。
- [docs/design/public-api-boundary.json](docs/design/public-api-boundary.json)：root public API 边界清单。
- [docs/design/public-api-examples.json](docs/design/public-api-examples.json)：root public API executable example 契约。
- [docs/design/deprecation-policy.md](docs/design/deprecation-policy.md)：root public API 的废弃、迁移和移除策略。
- [docs/design/versioning-policy.md](docs/design/versioning-policy.md)：core SDK、schema 和外部 extension 的版本策略。
- [docs/design/repository-boundary.json](docs/design/repository-boundary.json)：主仓、reference adapter 和外部仓库归属边界。
- [docs/design/v1-migration-plan.json](docs/design/v1-migration-plan.json)：v1 前 core 边界收敛和 `release_gate_checks` 计划；每个 gate 会声明 `required_check_ids`。
- [docs/design/modules.md](docs/design/modules.md)：provider、tool、sandbox、memory、skill、MCP、A2A 等运行时模块设计。
- [docs/design/templates.md](docs/design/templates.md)：ReAct、Agent-as-Tool、Dev Agent 等 graph template 边界。
- [docs/design/template-guide.md](docs/design/template-guide.md)：外部 graph template 的边界、step export/resume、events/verification 和 conformance 要求。
- [docs/design/migration-guide.md](docs/design/migration-guide.md)：v1 前后的 public API、adapter split、checkpoint/resume 和 verification 迁移要求。
- [docs/design/core-ci-gates.json](docs/design/core-ci-gates.json)：core repo CI gate 清单；`gopacttest.RecordCIGateSuiteCheck` 可把已观察 gate suite 记录为 `ci_gate` evidence。
- [docs/design/external-integration-roadmap.json](docs/design/external-integration-roadmap.json)：生产 adapter/plugin/template 的外部仓库路线。
- [docs/design/extension-scaffold-spec.json](docs/design/extension-scaffold-spec.json)：外部仓库 scaffold 蓝图；实现入口在 `internal/extensionscaffold`、`LoadRepositoriesFromDesign`、`WriteRepositoriesFromDesign`、`RenderSyncPlanFromDesign` 和 `cmd/gopact-extscaffold`，并在外部仓库文档中渲染 `V1 Migration Ownership`；维护命令会生成 `go.work` / `sync-plan.json`，支持 `-verify`、`-plan-json` 和 `-remote-status-json`。

调研记录见 [docs/research/agent-sdk-landscape.md](docs/research/agent-sdk-landscape.md) 和 [docs/research/harness-loop-engineering.md](docs/research/harness-loop-engineering.md)。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件、检查点、artifact、policy 和 verification evidence 都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。

完整能力清单、包状态、template 过程记录、Dev Agent release evidence 和外部仓库 scaffold 细节都放在 design docs 中维护。README 只保留 SDK 用户入口，避免把研发流水账当成公开文档。

## 贡献与安全

贡献入口见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全策略见 [SECURITY.md](SECURITY.md)，变更记录见 [CHANGELOG.md](CHANGELOG.md)。公开发布前仍需要项目 owner 确认并添加 `LICENSE`。

## 开发

```bash
make fmt
make test
make vet
```

当前模块路径是 `github.com/gopact-ai/gopact`。如果最终 GitHub owner 不同，请在第一次公开发布前替换。
