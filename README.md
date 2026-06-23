# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化工作流执行，以及可恢复的运行时状态。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加模型适配器或完整的 ReAct 执行能力。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件和检查点都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。

总体设计入口见 [docs/design/index.md](docs/design/index.md)。项目级原则见 [docs/design/philosophy.md](docs/design/philosophy.md)，核心契约设计见 [docs/design/contracts.md](docs/design/contracts.md)，事件流设计见 [docs/design/events.md](docs/design/events.md)，checkpoint/resume 设计见 [docs/design/checkpoint-resume.md](docs/design/checkpoint-resume.md)，配置设计见 [docs/design/config.md](docs/design/config.md)，安全设计见 [docs/design/security.md](docs/design/security.md)，channel/transfer 设计见 [docs/design/channels.md](docs/design/channels.md)，扩展性设计见 [docs/design/extensibility.md](docs/design/extensibility.md)，运行时模块设计见 [docs/design/modules.md](docs/design/modules.md)。

`gopact` 从第一版运行时开始就要具备 model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的 core contract 和默认实现。`artifact`、`policy`、typed options/config snapshot 是基础契约和支撑能力，不归入业务运行时模块；生产后端通过 adapter 或 plugin 接入。

## 当前形态

- `gopact`：provider-neutral 的消息、模型请求、工具规格、工具调用和执行事件。
- `graph`：类型化 graph 执行，包含 `Start`、`End`、节点函数、编译期校验和 checkpoint hook。
- `checkpoint`：用于测试、示例和本地原型的内存 checkpoint store。
- `docs/design/index.md`：总体设计入口、架构图、组件交互和 milestone。
- `docs/design/contracts.md`：`Message`、`ContentPart`、`RuntimeIDs`、`Event`、`SurfaceMessage`、`CheckpointRecord`、`ArtifactRef`、`PolicyDecision` 等基础契约。
- `docs/design/events.md`：事件顺序、event stream API、redaction、sink 失败策略和 channel/OTel 映射。
- `docs/design/checkpoint-resume.md`：checkpoint、interrupt、resume、cancel-safe point 和副作用幂等设计。
- `docs/design/config.md`：runner、模块、adapter、plugin、transfer、channel 的 typed option 注入、热替换和 secret provider 设计。
- `docs/design/security.md`：信任边界、policy、redaction、sandbox、MCP/A2A、skill 和 channel 安全模型。
- `docs/design/channels.md`：`SurfaceMessage`、transfer、channel adapter 和 Lark/TUI/A2UI 等展示接入设计。
- `docs/design/templates.md`：ReAct、Agent-as-Tool、Dev Agent 等 graph template 的边界和测试要求。
- `docs/design/extensibility.md`：hook、middleware、plugin 的扩展性设计。
- `docs/design/modules.md`：model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的运行时模块设计。
- `docs/research/agent-sdk-landscape.md`：对 LangGraph、LangChain、Eino、Google ADK、OpenRouter、CC Switch、oh-my-pi 的调研记录。

## 示例

```go
package main

import (
	"context"
	"fmt"

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
	result, err := run.Invoke(ctx, State{}, graph.WithThreadID("demo"), graph.WithCheckpointer(store))
	if err != nil {
		panic(err)
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
