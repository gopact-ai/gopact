package sandbox

import (
	"context"
	"sync"
	"sync/atomic"
)

var memorySessionSeq atomic.Uint64

// Memory is an in-memory sandbox manager for tests.
type Memory struct{}

// NewMemory creates an in-memory sandbox manager.
func NewMemory() *Memory {
	return &Memory{}
}

// Create starts an in-memory sandbox session.
func (m *Memory) Create(ctx context.Context, _ Spec) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &memorySession{
		id:    "memory-" + itoa(memorySessionSeq.Add(1)),
		files: make(map[string]File),
	}, nil
}

type memorySession struct {
	id     string
	mu     sync.RWMutex
	files  map[string]File
	closed atomic.Bool
}

func (s *memorySession) ID() string {
	return s.id
}

func (s *memorySession) Exec(ctx context.Context, _ ExecRequest) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{}, err
	}
	return ExecResult{}, ErrExecUnsupported
}

func (s *memorySession) ReadFile(ctx context.Context, path string) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	file, ok := s.files[path]
	if !ok {
		return File{}, ErrFileNotFound
	}
	file.Content = append([]byte(nil), file.Content...)
	return file, nil
}

func (s *memorySession) WriteFile(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file.Content = append([]byte(nil), file.Content...)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[file.Path] = file
	return nil
}

func (s *memorySession) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.closed.Store(true)
	return nil
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	for v > 0 {
		n--
		buf[n] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[n:])
}
