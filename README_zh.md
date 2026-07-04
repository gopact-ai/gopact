# gopact

<!-- gopact:doc-language: zh -->

[英文文档](./README.md)

## 中文

`gopact` 是一个面向 Go 应用的 Agent SDK core。它不绑定具体模型厂商、存储后端或 UI 渠道，而是提供可测试的运行时契约：typed graph、event stream、checkpoint/resume、tool/MCP/A2A 边界、policy/redaction，以及 release evidence。

官方仓库分工：

| 仓库 | 职责 |
| --- | --- |
| [`gopact`](https://github.com/gopact-ai/gopact) | core SDK、公共契约、参考实现、conformance helper |
| [`gopact-ext`](https://github.com/gopact-ai/gopact-ext) | 官方 provider、agent template、dev-agent helper 等扩展模块 |
| [`gopact-examples`](https://github.com/gopact-ai/gopact-examples) | 可运行 quickstart、provider 示例、workflow 示例 |

## 安装

需要 Go 1.25 或更新版本：

```bash
go get github.com/gopact-ai/gopact
```

SDK 不读取 `.env`、配置文件或本地 secret。模型、工具、存储、channel 和安全策略都由宿主应用通过 Go options 或接口显式注入。

## 快速开始

运行 core 中最小 graph 示例：

```bash
go test -run Example_graphRun .
```

生成一个可测试的 A2A HTTP agent scaffold：

```bash
go run ./cmd/gopact agent init support-agent -module example.com/support-agent -out /tmp/support-agent
(cd /tmp/support-agent && go test ./...)
go run ./cmd/gopact agent verify /tmp/support-agent
go run ./cmd/gopact agent run /tmp/support-agent
```

需要让生成的 agent 自动注册到可写 A2A registry 时，设置 `GOPACT_A2A_REGISTRAR_URL`；运行时会注册 agent card 并维持可续租 lease。

生成一个本地 A2A agent cluster scaffold：

```bash
go run ./cmd/gopact agent init-cluster support-cluster -module example.com/support-cluster -out /tmp/support-cluster \
  -agent triage:support.triage:"Classify support requests." \
  -agent docs:knowledge.search:"Search product documentation." \
  -agent billing:billing:"Handle billing questions."
(cd /tmp/support-cluster && go test ./...)
go run ./cmd/gopact agent verify /tmp/support-cluster
```

不传 `-agent` 时会生成默认 planner/worker/reviewer cluster。生成的 cluster 使用 `GOPACT_A2A_REGISTRY_URL` 做 mesh bootstrap，使用 `GOPACT_A2A_REGISTRAR_URL` 做可选外部注册。

从已记录的 run export 和已观察 verification report 构建 self-bootstrap release evidence bundle：

```bash
go run ./cmd/gopact release-bundle -run-export /path/to/run-export.json -report /path/to/verification-report.json > release-bundle.json
```

需要模型 provider 或完整 agent template 时，从 [`gopact-examples`](https://github.com/gopact-ai/gopact-examples) 开始；core 仓库只保留 provider-neutral 契约和离线可测实现。

## 核心概念

| 概念 | 说明 |
| --- | --- |
| `graph` | 类型化 workflow runtime，负责 node、edge、middleware、event 和 step 边界 |
| `checkpoint` | checkpoint store、codec、resume payload 校验和稳定恢复点 |
| `ModelRequest` / provider | provider-neutral 模型请求、响应、tool call、streaming 和 conformance 契约 |
| `tools` / `mcp` / `a2a` | 本地工具、MCP server、远程 agent 的统一能力边界 |
| `Policy` / redaction | 外部动作、secret、prompt/tool payload 和 artifact 的安全边界 |
| `VerificationRecorder` | 记录已经观察到的测试、CI、文件快照、review 和 release evidence |

## 当前稳定性

`gopact` 仍处于 pre-v1。当前适合：

- 评审和收敛 Agent SDK public API；
- 编写可恢复 workflow、A2A mesh、MCP/tool 边界和 conformance 测试；
- 为 `gopact-ext` 和业务应用开发 provider、backend、channel、agent template。

当前不承诺：

- v1 前 public API 完全稳定；
- core 直接内置生产模型厂商、云存储、向量库或外部 UI 渠道；
- 不经过宿主应用配置就自动读取环境变量、secret 或远端配置。

## 文档地图

| 文档 | 用途 |
| --- | --- |
| [doc/README.md](doc/README.md) | 完整文档索引 |
| [doc/FEATURES.md](doc/FEATURES.md) | core 能力矩阵和离线验收命令 |
| [doc/design/index.md](doc/design/index.md) | 架构入口、模块边界、路线图索引 |
| [doc/design/modules.md](doc/design/modules.md) | provider、tool、sandbox、memory、skill、MCP、A2A 运行时模块设计 |
| [doc/design/templates.md](doc/design/templates.md) | ReAct、Agent-as-Tool、Dev Agent 等 template 边界 |
| [doc/design/public-api-examples.json](doc/design/public-api-examples.json) | public API 示例覆盖契约 |
| [doc/design/migration-guide.md](doc/design/migration-guide.md) | v1 迁移说明 |
| [doc/design/template-guide.md](doc/design/template-guide.md) | 外部 graph template 编写要求 |
| [doc/design/deprecation-policy.md](doc/design/deprecation-policy.md) | public API 废弃和移除规则 |
| [doc/design/versioning-policy.md](doc/design/versioning-policy.md) | core、schema、extension 版本策略 |
| [doc/design/ecosystem-topology.json](doc/design/ecosystem-topology.json) | `core + ext + examples` 官方仓库拓扑 |
| [doc/design/v1-migration-plan.json](doc/design/v1-migration-plan.json) | v1 前边界收敛和 release gate manifest |
| [doc/design/milestone-readiness.json](doc/design/milestone-readiness.json) | 阶段 readiness evidence |
| [doc/design/external-integration-roadmap.json](doc/design/external-integration-roadmap.json) | provider/backend/channel/template 扩展路线 |
| [doc/design/extension-scaffold-spec.json](doc/design/extension-scaffold-spec.json) | legacy 外部仓库 scaffold 记录 |
| [doc/maintainers/repository-governance.md](doc/maintainers/repository-governance.md) | PR、CI、自动合并和公开仓库治理 |

## 贡献与安全

- 贡献流程：[doc/CONTRIBUTING.md](doc/CONTRIBUTING.md)
- 安全策略：[doc/SECURITY.md](doc/SECURITY.md)
- 变更记录：[doc/CHANGELOG.md](doc/CHANGELOG.md)
- 许可证：[MIT](LICENSE)
