# gopact

`gopact` 是一个 Go-first 的 agent SDK 骨架，重点放在显式契约、类型化工作流执行，以及可恢复的运行时状态。

这个仓库仍处于早期阶段。当前目标是先确定 SDK 的公共形态，再增加模型适配器或完整的 ReAct 执行能力。

## 设计哲学

`gopact` 把“契约”视为产品本身。消息、工具、模型请求、事件和检查点都应该是 provider-neutral 的契约，连接应用代码和运行时代码。

运行时优先于 agent 模式：ReAct、plan-execute、supervisor、多 agent 流程都应该是建立在同一套执行、事件、检查点和中断原语之上的 graph template。


`gopact` 从第一版运行时开始就要具备 model provider routing、tool registry、sandbox、memory、skill、MCP、A2A 的 core contract 和默认实现。`artifact`、`policy`、typed options/config snapshot 是基础契约和支撑能力，不归入业务运行时模块；生产后端通过 adapter 或 plugin 接入。

## 当前形态

- `gopact`：provider-neutral 的消息、模型请求、工具规格、工具调用和执行事件。
- `graph`：类型化 graph 执行，包含 `Start`、`End`、节点函数、编译期校验和 checkpoint hook。
- `checkpoint`：用于测试、示例和本地原型的内存 checkpoint store。

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
