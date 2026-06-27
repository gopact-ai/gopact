# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化工作流执行，以及可恢复的运行时状态。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加模型适配器或完整的 ReAct 执行能力。

## 安装

```bash
go get github.com/gopact-ai/gopact
```

当前仓库仍为私有，安装需要具备 `gopact-ai/gopact` 的访问权限。SDK 自身不读取配置文件、环境变量或本地 secret；provider、backend、channel、plugin 的配置都应由宿主应用通过 Go options、接口或 typed snapshot 注入。

## 快速开始

最短可执行路径是 `example_test.go` 里的 `Example_graphRun`：它创建一个 typed graph，运行单个 node，把事件流收集出来，并通过内存 checkpoint store 持久化 step 边界。

```bash
go test -run Example_graphRun .
```


## 核心概念

- `Setup` / `Defaults`：SDK 级默认值入口，支持宿主注入 logger、log level 和 runtime identity defaults。
- `Runner` / `TurnLoop`：`Runner` 执行一次 run；`TurnLoop` 处理多轮输入、抢占、取消和恢复。
- `graph`：类型化 workflow 执行层，负责 node、edge、middleware、event stream 和 step 边界。
- `RunExport` / `StepExport`：过程导出和恢复契约，目标是在任意稳定 step 边界中断后可以 import/resume。
- `VerificationRecorder`：记录已观察证据，不替宿主执行隐藏命令，也不保存 raw prompt、raw response 或 secret。
- adapter / plugin / template：生产 provider、backend、channel、observability 和业务 agent 组装应通过外部 adapter/plugin/template 表达。

## 当前稳定性

`gopact` 仍是 pre-v1 SDK。当前适合内部实验、SDK API 评审、template/conformance 开发和外部 adapter scaffold，不应被包装成生产级完整 agent 平台。


## 文档地图


## 贡献与安全

贡献入口见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全策略见 [SECURITY.md](SECURITY.md)，变更记录见 [CHANGELOG.md](CHANGELOG.md)。公开发布前仍需要项目 owner 确认并添加 `LICENSE`。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件和检查点都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。



开源治理入口见 [CONTRIBUTING.md](CONTRIBUTING.md)、[SECURITY.md](SECURITY.md) 和 [CHANGELOG.md](CHANGELOG.md)；公开发布前仍需要项目 owner 确认并添加 `LICENSE`。

`gopact` 从第一版运行时开始就要具备 model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的 core contract 和默认实现。`artifact`、`policy`、typed options/config snapshot 是基础契约和支撑能力，不归入业务运行时模块；生产后端通过 adapter 或 plugin 接入。

## 当前形态

- `gopact`：provider-neutral 的消息、模型请求、模型响应、streaming model adapter、工具规格、工具调用、tool retry decision contract / middleware、执行事件、surface message / transfer / channel root 契约、run option、step/resume import、root `JSONSchemaValidator` / `JSONSchemaValidatorFunc` 可插拔 schema validator 契约、root `ValidateJSONSchemaValue` portable schema subset 校验、root `ValidateResumePayload` resume payload schema gate（含 pattern、exclusive bounds、multipleOf 第二片）、run export、run export JSON schema、run export -> verification evidence 桥接、已观察 model call / tool call / channel event -> verification evidence 桥接、policy decision -> verification evidence 桥接、task/input/intervention/failure process record、细粒度 failure attribution taxonomy、failure attribution -> verification evidence 桥接、entropy audit、entropy audit -> verification evidence 桥接、run-level verification report 和 verification recorder 契约。
- `provider`：模型 provider registry、route set、router、model middleware、model rate limit middleware、fake provider、fallback/error classification。
- `adapters/model/openaicompatible`：OpenAI-compatible chat completion adapter，适合 OpenRouter、企业网关和兼容接口。
- `a2a`：A2A agent registry、local runnable agent adapter、HTTP JSON/JSONL client/server wrapper、JSON-RPC 2.0 + SSE client/server wrapper、agent card discovery、auth context、task send/cancel，以及 streaming task message、artifact update 和 status 的最小契约。
- `Policy` / `TextRedactor` / `SecretProvider`：root policy contract、secret reference/provider/value 原子契约、model/tool/A2A send/channel/exporter 以及 memory/sandbox/artifact/skill/MCP wrapper policy boundary、policy requested/decided events、review-to-approval interrupt、`RedactModelRequest` / `RedactModelResponse` / `ModelIORedactionMiddleware`、`RedactToolResult` / `ToolResultRedactionMiddleware`、event redaction middleware、`Event.Redaction` 状态和结构化 policy denial error；`SecretValue` 的 string/fmt/JSON 输出固定 redacted，SDK 不读取 env/file/config。
- `gopacttest`：event stream 收集、event type 断言、compact trajectory frame helper、golden trajectory fixture helper、template trajectory conformance helper、trajectory golden -> `VerificationRecorder` evidence 桥接、portable JSON Schema validator conformance helper、extension scaffold conformance helper、turnloop store conformance helper、channel/transfer conformance helper、verification evidence conformance helper（可要求 report valid、指定 check id 已 passed、指定 evidence type 来自 passed check、具体 CI gate 已 passed，也可通过 `VerificationEvidenceRequirement` 批量校验多组 release/readiness gate requirement）、已观察命令结果 -> command evidence 桥接、`RecordCIGateSuiteCheck` 已观察 CI gate suite -> `ci_gate` evidence 聚合桥接、`RecordCIRunCheck` 已观察远端 CI run/job/step -> `ci_gate` evidence 桥接、`RecordCIRunSetCheck` 已观察多仓远端 CI run/job/step -> 单个跨仓 `ci_gate` check 聚合桥接、已观察文件快照 -> file snapshot evidence 桥接、已观察 diff -> diff evidence 桥接，以及已观察 reviewer decision -> review evidence 桥接；review evidence metadata 会保留 reviewer source/status 和宿主传入的 prompt/eval/policy governance metadata；`gopacttest/providerconformance` 提供 provider conformance helper，`gopacttest/checkpointconformance` 提供 checkpoint store conformance helper，`gopacttest/reactconformance` 提供 ReAct deferred memory work queue conformance helper（含 retry 保留 job metadata 并合并 decision metadata）。
- `WorkflowActionProcessRecordsByAction` / `WorkflowActionProcessRecordsFromRunExportByAction`：可按唯一 `ActionKind` 提取单个 action 的 process records，若 workflow 中存在重复 action kind 会显式报歧义，要求调用方改用 action index 或 child task id；`WorkflowActionProcessRecordsByTaskID` / `WorkflowActionProcessRecordsFromRunExportByTaskID` 可按 child task id 提取，并依赖 workflow conformance 先拒绝重复 child task id；`WorkflowActionProcessRecordsFromRunExport` 则按 1-based action index 从已落盘 `RunExport` 恢复并提取。`ImportProcessRecords` / `ImportWorkflowRecords` 可把已恢复的 process/workflow records 防御性拷贝后写回 `RunRecorder`。它们适合外部 template 做 step 级 release/apply/plan export/import，只恢复、校验、导入和提取已观察过程证据，不调度 workflow、不重新执行 action。
- Dev Agent action metadata 会从 `EvaluateAction` 防御性复制到 `ActionResult`，并贯穿 process/workflow child task、input、intervention metadata，保留 prompt/eval/policy governance ref；SDK canonical 字段仍覆盖冲突键。
- `cmd/gopact-extscaffold`：本仓维护工具入口，可运行 `go run ./cmd/gopact-extscaffold -root . -out /tmp/gopact-ext -verify` 批量生成外部仓库 scaffold workspace、本地 `go.work`、`sync-plan.json`、`sync-repos.sh`、`sync-secrets.sh`、`rerun-ci.sh` 并逐仓库运行 manifest required CI commands（默认 `git diff --check`、`go test -count=1 ./...`、`go vet ./...`），用 `-dry-run` 查看将生成的仓库和文件数，用 `-plan-json` 输出远端私有仓库 bootstrap/sync 的 JSON 计划，用 `-plan-sh` 输出 shell 同步脚本，用 `-plan-secrets-sh` 输出显式配置 `GOPACT_GITHUB_TOKEN` repo secret 的脚本，用 `-plan-rerun-sh` 输出 secret 配置后批量 rerun 外部仓库 CI 的脚本，用 `-remote-status-json` 只读审计 gopact-ai 远端仓库是否存在、是否私有、CI workflow 是否存在、`GOPACT_GITHUB_TOKEN` secret 是否存在以及最新 Actions run 是否通过，用 `-remote-status-evidence-json` 输出同一远端审计对应的标准 `external_repository_readiness` verification check，或用 `-remote-ci-evidence-json` 输出 `external-ci:gopact-ai` 跨仓 `ci_gate` verification check；未 ready 时输出会包含 `not_ready_count`、`blocking_reasons` 和 `required_actions` 供 release gate 或人工处理，`missing_count` 仅作为兼容旧消费者的 legacy alias 保留，evidence JSON 用 check status 表达 failed readiness/CI，而不是让 CLI 直接失败；`ready` 同时要求仓库存在、visibility 匹配、CI workflow 存在、`GOPACT_GITHUB_TOKEN` 存在且最新 CI 通过；`go.work` 只用于把生成的外部模块绑定到当前 SDK root 进行本地 conformance，`sync-plan.json` / `sync-repos.sh` / `sync-secrets.sh` / `rerun-ci.sh` 是给后续 GitHub 初始化/同步流程消费的计划文件，不是外部仓库发布文件；`sync-repos.sh` 对已存在仓库会先 `gh repo clone` 远端历史、覆盖生成的 scaffold 文件，再用 `GOWORK=off go mod tidy` 生成远端可用的 `go.sum` 并 push，`rerun-ci.sh` 会先检查所有外部仓库的 `GOPACT_GITHUB_TOKEN` secret，再 rerun 最新 `ci` workflow 或在无历史 run 时触发 workflow，外部仓库 CI 在主仓仍私有时需要 `GOPACT_GITHUB_TOKEN` 或具备跨仓读权限的 `github.token`。

## 示例

```go
package main

import (
	"context"
	"fmt"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/checkpoint"
	"github.com/gopact-ai/gopact/graph"
)

type State struct {
	Trace []string
}

func main() {
	ctx := context.Background()
	g := graph.New[State]()

	g.AddNode("plan", func(ctx context.Context, state State) (State, error) {
		state.Trace = append(state.Trace, "plan")
		return state, nil
	})
	g.AddNode("act", func(ctx context.Context, state State) (State, error) {
		state.Trace = append(state.Trace, "act")
		return state, nil
	})
	g.AddEdge(graph.Start, "plan")
	g.AddEdge("plan", "act")
	g.AddEdge("act", graph.End)

	run, err := g.Compile()
	if err != nil {
		panic(err)
	}

	store := checkpoint.NewMemory[State]()
	var result State
	for event, err := range run.Run(
		ctx,
		State{},
		graph.WithRuntimeIDs(gopact.RuntimeIDs{RunID: "demo-run", ThreadID: "demo-thread"}),
		graph.WithCheckpointStore(store),
	) {
		if err != nil {
			panic(err)
		}
		if event.Type == gopact.EventNodeCompleted {
			result = event.StepSnapshot.Output.(State)
		}
	}

	fmt.Println(result.Trace)
}
```

## 开发

```bash
make fmt
make test
make vet
```

当前模块路径是 `github.com/gopact-ai/gopact`。如果最终 GitHub owner 不同，请在第一次公开发布前替换。
