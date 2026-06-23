package graph

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// Start 是 graph 的虚拟入口节点。
	Start = "__start__"
	// End 是 graph 的虚拟终止节点。
	End = "__end__"
)

// NodeFunc 表示 graph 中一步状态转换。
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)

// Checkpoint 是节点完成后的可持久化状态快照。
type Checkpoint[S any] struct {
	ThreadID  string
	Step      int
	Node      string
	State     S
	CreatedAt time.Time
}

// Checkpointer 持久化 graph 状态快照。
type Checkpointer[S any] interface {
	Put(ctx context.Context, checkpoint Checkpoint[S]) error
}

// Graph 是一个小型的类型化执行 graph。
type Graph[S any] struct {
	nodes map[string]NodeFunc[S]
	edges map[string][]string
}

// New 创建一个空 graph。
func New[S any]() *Graph[S] {
	return &Graph[S]{
		nodes: make(map[string]NodeFunc[S]),
		edges: make(map[string][]string),
	}
}

// AddNode 注册一个状态转换节点。
func (g *Graph[S]) AddNode(name string, node NodeFunc[S]) {
	g.nodes[name] = node
}

// AddEdge 连接两个节点。Graph 边界使用 Start 和 End。
func (g *Graph[S]) AddEdge(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

// Compile 校验 graph 结构，并返回不可变 runnable。
func (g *Graph[S]) Compile() (*Runnable[S], error) {
	if g == nil {
		return nil, errors.New("graph: nil graph")
	}
	for from, tos := range g.edges {
		if from != Start {
			if _, ok := g.nodes[from]; !ok {
				return nil, fmt.Errorf("graph: missing source node %q", from)
			}
		}
		for _, to := range tos {
			if to == End {
				continue
			}
			if _, ok := g.nodes[to]; !ok {
				return nil, fmt.Errorf("graph: missing target node %q", to)
			}
		}
	}

	nodes := make(map[string]NodeFunc[S], len(g.nodes))
	for name, node := range g.nodes {
		if node == nil {
			return nil, fmt.Errorf("graph: node %q is nil", name)
		}
		nodes[name] = node
	}
	edges := make(map[string][]string, len(g.edges))
	for from, tos := range g.edges {
		edges[from] = append([]string(nil), tos...)
	}

	return &Runnable[S]{nodes: nodes, edges: edges, maxSteps: 1024}, nil
}

// Runnable 是编译后的 graph。
type Runnable[S any] struct {
	nodes    map[string]NodeFunc[S]
	edges    map[string][]string
	maxSteps int
}

type invokeConfig struct {
	threadID     string
	checkpointer any
}

// InvokeOption 配置一次 graph 调用。
type InvokeOption func(*invokeConfig)

// WithThreadID 设置 checkpointer 使用的对话或 workflow thread 标识。
func WithThreadID(threadID string) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.threadID = threadID
	}
}

// WithCheckpointer 在每个节点完成后持久化 checkpoint。
func WithCheckpointer[S any](checkpointer Checkpointer[S]) InvokeOption {
	return func(cfg *invokeConfig) {
		cfg.checkpointer = checkpointer
	}
}

// Invoke 从 Start 运行 graph，直到到达 End 或没有后续节点。
func (r *Runnable[S]) Invoke(ctx context.Context, initial S, opts ...InvokeOption) (S, error) {
	if r == nil {
		return initial, errors.New("graph: nil runnable")
	}

	cfg := invokeConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	var checkpointer Checkpointer[S]
	if cfg.checkpointer != nil {
		cp, ok := cfg.checkpointer.(Checkpointer[S])
		if !ok {
			return initial, errors.New("graph: checkpointer state type mismatch")
		}
		checkpointer = cp
	}

	state := initial
	queue := append([]string(nil), r.edges[Start]...)
	step := 0

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		if step >= r.maxSteps {
			return state, fmt.Errorf("graph: exceeded max steps %d", r.maxSteps)
		}

		name := queue[0]
		queue = queue[1:]
		if name == End {
			continue
		}

		node, ok := r.nodes[name]
		if !ok {
			return state, fmt.Errorf("graph: missing node %q", name)
		}

		next, err := node(ctx, state)
		if err != nil {
			return state, fmt.Errorf("graph: node %q: %w", name, err)
		}
		state = next
		step++

		if checkpointer != nil {
			err := checkpointer.Put(ctx, Checkpoint[S]{
				ThreadID:  cfg.threadID,
				Step:      step,
				Node:      name,
				State:     state,
				CreatedAt: time.Now(),
			})
			if err != nil {
				return state, fmt.Errorf("graph: checkpoint node %q: %w", name, err)
			}
		}

		queue = append(queue, r.edges[name]...)
	}

	return state, nil
}
