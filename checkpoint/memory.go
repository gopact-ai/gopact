package checkpoint

import (
	"context"
	"sync"

	"github.com/gopact-ai/gopact/graph"
)

// Memory 是用于测试、示例和原型的进程内 checkpointer。
type Memory[S any] struct {
	mu       sync.RWMutex
	byThread map[string][]graph.Checkpoint[S]
}

// NewMemory 创建一个空的内存 checkpoint store。
func NewMemory[S any]() *Memory[S] {
	return &Memory[S]{
		byThread: make(map[string][]graph.Checkpoint[S]),
	}
}

// Put 按 thread id 存储 checkpoint。
func (m *Memory[S]) Put(ctx context.Context, checkpoint graph.Checkpoint[S]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.byThread[checkpoint.ThreadID] = append(m.byThread[checkpoint.ThreadID], checkpoint)
	return nil
}

// List 按插入顺序返回某个 thread 的 checkpoint 副本。
func (m *Memory[S]) List(ctx context.Context, threadID string) []graph.Checkpoint[S] {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]graph.Checkpoint[S](nil), m.byThread[threadID]...)
}

// Latest 返回某个 thread 最近的一条 checkpoint。
func (m *Memory[S]) Latest(ctx context.Context, threadID string) (graph.Checkpoint[S], bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	checkpoints := m.byThread[threadID]
	if len(checkpoints) == 0 {
		var zero graph.Checkpoint[S]
		return zero, false
	}
	return checkpoints[len(checkpoints)-1], true
}
