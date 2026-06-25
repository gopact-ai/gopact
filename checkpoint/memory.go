package checkpoint

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gopact-ai/gopact/graph"
)

// Memory 是用于测试、示例和原型的进程内 checkpointer。
type Memory[S any] struct {
	mu            sync.RWMutex
	codec         StateCodec[S]
	configVersion string
	driftPolicy   ConfigDriftPolicy
	migrations    map[string]RecordMigrator
	byThread      map[string][]Record
	byID          map[string]Record
}

// MemoryOption configures a Memory checkpoint store.
type MemoryOption[S any] interface {
	applyMemory(*Memory[S])
}

// NewMemory 创建一个空的内存 checkpoint store。
func NewMemory[S any](opts ...MemoryOption[S]) *Memory[S] {
	m := &Memory[S]{
		codec:    JSONCodec[S]{},
		byThread: make(map[string][]Record),
		byID:     make(map[string]Record),
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyMemory(m)
		}
	}
	return m
}

// Put 按 thread id 存储 checkpoint。
func (m *Memory[S]) Put(_ context.Context, checkpoint graph.Checkpoint[S]) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	checkpoint = m.prepareCheckpoint(checkpoint)
	record, err := EncodeCheckpoint(checkpoint, m.codec)
	if err != nil {
		return err
	}
	m.byThread[record.ThreadID] = append(m.byThread[record.ThreadID], record)
	m.byID[record.ID] = record
	return nil
}

// Get returns one checkpoint by id.
func (m *Memory[S]) Get(_ context.Context, id string) (graph.Checkpoint[S], bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, ok := m.byID[id]
	if !ok {
		var zero graph.Checkpoint[S]
		return zero, false, nil
	}
	checkpoint, err := decodeCheckpointWithConfig[S](record, m.codec, m.decodeConfig())
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	return checkpoint, true, nil
}

// List 按插入顺序返回某个 thread 的 checkpoint 副本。
func (m *Memory[S]) List(_ context.Context, threadID string) []graph.Checkpoint[S] {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records := m.byThread[threadID]
	out := make([]graph.Checkpoint[S], 0, len(records))
	for _, record := range records {
		checkpoint, err := decodeCheckpointWithConfig[S](record, m.codec, m.decodeConfig())
		if err != nil {
			return nil
		}
		out = append(out, checkpoint)
	}
	return out
}

// Latest 返回某个 thread 最近的一条 checkpoint。
func (m *Memory[S]) Latest(_ context.Context, threadID string) (graph.Checkpoint[S], bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records := m.byThread[threadID]
	if len(records) == 0 {
		var zero graph.Checkpoint[S]
		return zero, false, nil
	}
	checkpoint, err := decodeCheckpointWithConfig[S](records[len(records)-1], m.codec, m.decodeConfig())
	if err != nil {
		var zero graph.Checkpoint[S]
		return zero, false, err
	}
	return checkpoint, true, nil
}

func (m *Memory[S]) prepareCheckpoint(checkpoint graph.Checkpoint[S]) graph.Checkpoint[S] {
	if checkpoint.ThreadID == "" {
		checkpoint.ThreadID = checkpoint.IDs.ThreadID
	}
	if checkpoint.ID == "" {
		checkpoint.ID = fmt.Sprintf("%s:%d:%d", checkpoint.ThreadID, checkpoint.Step, len(m.byThread[checkpoint.ThreadID])+1)
	}
	if checkpoint.ConfigVersion == "" {
		checkpoint.ConfigVersion = m.configVersion
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now()
	}
	return checkpoint
}

func (m *Memory[S]) decodeConfig() decodeConfig {
	return decodeConfig{
		currentConfigVersion: m.configVersion,
		driftPolicy:          m.driftPolicy,
		migrations:           copyMigrations(m.migrations),
	}
}
