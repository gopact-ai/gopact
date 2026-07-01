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
- adapter / plugin / template：生产 provider、backend、channel、observability 和业务 agent 组装应通过外部 adapter/plugin/template 表达。

## 当前稳定性

`gopact` 仍是 pre-v1 SDK。当前适合内部实验、SDK API 评审、template/conformance 开发和外部 adapter scaffold，不应被包装成成熟的完整 agent 平台。


## 文档地图

- [FEATURES.md](FEATURES.md)：core SDK 可执行能力覆盖矩阵；ext/examples 仓库各自维护对应矩阵。


## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件、检查点、artifact、policy 和 verification evidence 都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。

[FEATURES.md](FEATURES.md) 提供 core SDK 的可执行能力矩阵；包状态、template 过程记录、Dev Agent release evidence 和外部仓库 scaffold 细节都放在 design docs 中维护。README 只保留 SDK 用户入口，避免把研发流水账当成公开文档。

## 贡献与安全

贡献入口见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全策略见 [SECURITY.md](SECURITY.md)，变更记录见 [CHANGELOG.md](CHANGELOG.md)。本仓库采用 [MIT 协议](LICENSE)。

## 开发

```bash
make fmt
make test
make vet
```

当前模块路径是 `github.com/gopact-ai/gopact`。如果最终 GitHub owner 不同，请在第一次公开发布前替换。
