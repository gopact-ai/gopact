# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化工作流执行，以及可恢复的运行时状态。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加模型适配器或完整的 ReAct 执行能力。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件和检查点都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。



`gopact` 从第一版运行时开始就要具备 model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的 core contract 和默认实现。`artifact`、`policy`、typed options/config snapshot 是基础契约和支撑能力，不归入业务运行时模块；生产后端通过 adapter 或 plugin 接入。

## 当前形态

- `gopact`：provider-neutral 的消息、模型请求、模型响应、streaming model adapter、工具规格、工具调用、tool retry decision contract / middleware、执行事件、surface message / transfer / channel root 契约、run option、step/resume import、root `JSONSchemaValidator` / `JSONSchemaValidatorFunc` 可插拔 schema validator 契约、root `ValidateJSONSchemaValue` portable schema subset 校验、root `ValidateResumePayload` resume payload schema gate（含 pattern、exclusive bounds、multipleOf 第二片）、run export、run export JSON schema、run export -> verification evidence 桥接、已观察 model call / tool call / channel event -> verification evidence 桥接、policy decision -> verification evidence 桥接、task/input/intervention/failure process record、细粒度 failure attribution taxonomy、failure attribution -> verification evidence 桥接、entropy audit、entropy audit -> verification evidence 桥接、run-level verification report 和 verification recorder 契约。
- `provider`：模型 provider registry、route set、router、model middleware、model rate limit middleware、fake provider、fallback/error classification。
- `adapters/model/openaicompatible`：OpenAI-compatible chat completion adapter，适合 OpenRouter、企业网关和兼容接口。
- `a2a`：A2A agent registry、local runnable agent adapter、HTTP JSON/JSONL client/server wrapper、JSON-RPC 2.0 + SSE client/server wrapper、agent card discovery、auth context、task send/cancel，以及 streaming task message、artifact update 和 status 的最小契约。
- `Policy` / `TextRedactor`：root policy contract、model/tool/A2A send/channel/exporter 以及 memory/sandbox/artifact/skill/MCP wrapper policy boundary、policy requested/decided events、review-to-approval interrupt、`RedactModelRequest` / `RedactModelResponse` / `ModelIORedactionMiddleware`、`RedactToolResult` / `ToolResultRedactionMiddleware`、event redaction middleware、`Event.Redaction` 状态和结构化 policy denial error。
- `gopacttest`：event stream 收集、event type 断言、compact trajectory frame helper、golden trajectory fixture helper、template trajectory conformance helper、trajectory golden -> `VerificationRecorder` evidence 桥接、portable JSON Schema validator conformance helper、extension scaffold conformance helper、turnloop store conformance helper、channel/transfer conformance helper、verification evidence conformance helper（可要求 report valid、指定 check id、指定 evidence type 和具体 CI gate 已 passed）、已观察命令结果 -> command evidence 桥接、`RecordCIGateSuiteCheck` 已观察 CI gate suite -> `ci_gate` evidence 聚合桥接、已观察文件快照 -> file snapshot evidence 桥接、已观察 diff -> diff evidence 桥接，以及已观察 reviewer decision -> review evidence 桥接；review evidence metadata 会保留 reviewer source/status 和宿主传入的 prompt/eval/policy governance metadata；`gopacttest/providerconformance` 提供 provider conformance helper，`gopacttest/checkpointconformance` 提供 checkpoint store conformance helper，`gopacttest/reactconformance` 提供 ReAct deferred memory work queue conformance helper。
- Dev Agent action metadata 会从 `EvaluateAction` 防御性复制到 `ActionResult`，并贯穿 process/workflow child task、input、intervention metadata，保留 prompt/eval/policy governance ref；SDK canonical 字段仍覆盖冲突键。
- `cmd/gopact-extscaffold`：本仓维护工具入口，可运行 `go run ./cmd/gopact-extscaffold -root . -out /tmp/gopact-ext -verify` 批量生成外部仓库 scaffold workspace、本地 `go.work`、`sync-plan.json`、`sync-repos.sh`、`sync-secrets.sh` 并逐仓库运行 manifest required CI commands（默认 `git diff --check`、`go test -count=1 ./...`、`go vet ./...`），用 `-dry-run` 查看将生成的仓库和文件数，用 `-plan-json` 输出远端私有仓库 bootstrap/sync 的 JSON 计划，用 `-plan-sh` 输出 shell 同步脚本，用 `-plan-secrets-sh` 输出显式配置 `GOPACT_GITHUB_TOKEN` repo secret 的脚本，或用 `-remote-status-json` 只读审计 gopact-ai 远端仓库是否存在、是否私有、CI workflow 是否存在、`GOPACT_GITHUB_TOKEN` secret 是否存在以及最新 Actions run 是否通过；`go.work` 只用于把生成的外部模块绑定到当前 SDK root 进行本地 conformance，`sync-plan.json` / `sync-repos.sh` / `sync-secrets.sh` 是给后续 GitHub 初始化/同步流程消费的计划文件，三者都不是外部仓库发布文件；`sync-repos.sh` 会在推送前用 `GOWORK=off go mod tidy` 生成远端可用的 `go.sum`，外部仓库 CI 在主仓仍私有时需要 `GOPACT_GITHUB_TOKEN` 或具备跨仓读权限的 `github.token`。

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
