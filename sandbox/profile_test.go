package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProfileManagerAppliesLimitsAndRejectsDisallowedEnv(t *testing.T) {
	ctx := context.Background()
	base := &profileRecordingManager{session: &profileRecordingSession{id: "session-1"}}
	manager, err := NewProfileManager(base, Profile{
		AllowedEnvKeys: []string{"GOCACHE"},
		Limits: ResourceLimits{
			OutputBytes: 1024,
			Timeout:     time.Second,
		},
	})
	if err != nil {
		t.Fatalf("NewProfileManager() error = %v", err)
	}

	_, err = manager.Create(ctx, Spec{Env: map[string]string{"SECRET": "raw"}})
	if !errors.Is(err, ErrProfileViolation) {
		t.Fatalf("Create() error = %v, want ErrProfileViolation", err)
	}
	if base.creates != 0 {
		t.Fatalf("underlying creates = %d, want 0", base.creates)
	}

	_, err = manager.Create(ctx, Spec{Env: map[string]string{"GOCACHE": "/tmp/gocache"}})
	if err != nil {
		t.Fatalf("Create() allowed env error = %v", err)
	}
	if base.lastSpec.Limits.OutputBytes != 1024 {
		t.Fatalf("output limit = %d, want 1024", base.lastSpec.Limits.OutputBytes)
	}
	if base.lastSpec.Limits.Timeout != time.Second {
		t.Fatalf("timeout = %s, want 1s", base.lastSpec.Limits.Timeout)
	}
}

func TestProfileSessionRejectsDisallowedCommandBeforeExec(t *testing.T) {
	ctx := context.Background()
	session := &profileRecordingSession{id: "session-1"}
	manager, err := NewProfileManager(&profileRecordingManager{session: session}, Profile{
		AllowedCommands: []string{"go"},
	})
	if err != nil {
		t.Fatalf("NewProfileManager() error = %v", err)
	}
	wrapped, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = wrapped.Exec(ctx, ExecRequest{Command: []string{"bash", "-lc", "echo unsafe"}})
	if !errors.Is(err, ErrProfileViolation) {
		t.Fatalf("Exec() error = %v, want ErrProfileViolation", err)
	}
	if session.execs != 0 {
		t.Fatalf("underlying execs = %d, want 0", session.execs)
	}

	_, err = wrapped.Exec(ctx, ExecRequest{Command: []string{"go", "version"}})
	if err != nil {
		t.Fatalf("Exec() allowed command error = %v", err)
	}
	if session.execs != 1 {
		t.Fatalf("underlying execs = %d, want 1", session.execs)
	}
}

func TestProfileSessionRespectsCanceledContextBeforeProfileChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session := &profileRecordingSession{id: "session-1"}
	manager, err := NewProfileManager(&profileRecordingManager{session: session}, Profile{
		AllowedCommands: []string{"go"},
	})
	if err != nil {
		t.Fatalf("NewProfileManager() error = %v", err)
	}
	wrapped, err := manager.Create(context.Background(), Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = wrapped.Exec(ctx, ExecRequest{Command: []string{"bash"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Exec() error = %v, want context.Canceled", err)
	}
	if session.execs != 0 {
		t.Fatalf("underlying execs = %d, want 0", session.execs)
	}
}

func TestProfileSessionRestrictsReadAndWritePaths(t *testing.T) {
	ctx := context.Background()
	session := &profileRecordingSession{id: "session-1"}
	manager, err := NewProfileManager(&profileRecordingManager{session: session}, Profile{
		AllowedReadPaths:  []string{"docs"},
		AllowedWritePaths: []string{"tmp"},
	})
	if err != nil {
		t.Fatalf("NewProfileManager() error = %v", err)
	}
	wrapped, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := wrapped.ReadFile(ctx, "../secret.txt"); !errors.Is(err, ErrProfileViolation) {
		t.Fatalf("ReadFile(escape) error = %v, want ErrProfileViolation", err)
	}
	if _, err := wrapped.ReadFile(ctx, "src/main.go"); !errors.Is(err, ErrProfileViolation) {
		t.Fatalf("ReadFile(outside allowlist) error = %v, want ErrProfileViolation", err)
	}
	if _, err := wrapped.ReadFile(ctx, "docs/index.md"); err != nil {
		t.Fatalf("ReadFile(allowed) error = %v", err)
	}
	if session.reads != 1 {
		t.Fatalf("underlying reads = %d, want 1", session.reads)
	}

	err = wrapped.WriteFile(ctx, File{Path: "docs/generated.md", Content: []byte("blocked")})
	if !errors.Is(err, ErrProfileViolation) {
		t.Fatalf("WriteFile(outside allowlist) error = %v, want ErrProfileViolation", err)
	}
	err = wrapped.WriteFile(ctx, File{Path: "tmp/generated.md", Content: []byte("allowed")})
	if err != nil {
		t.Fatalf("WriteFile(allowed) error = %v", err)
	}
	if session.writes != 1 {
		t.Fatalf("underlying writes = %d, want 1", session.writes)
	}
}

type profileRecordingManager struct {
	session  *profileRecordingSession
	creates  int
	lastSpec Spec
}

func (m *profileRecordingManager) Create(_ context.Context, spec Spec) (Session, error) {
	m.creates++
	m.lastSpec = spec
	return m.session, nil
}

type profileRecordingSession struct {
	id     string
	execs  int
	reads  int
	writes int
}

func (s *profileRecordingSession) ID() string { return s.id }

func (s *profileRecordingSession) Exec(_ context.Context, _ ExecRequest) (ExecResult, error) {
	s.execs++
	return ExecResult{ExitCode: 0}, nil
}

func (s *profileRecordingSession) ReadFile(_ context.Context, path string) (File, error) {
	s.reads++
	return File{Path: path, Content: []byte("payload")}, nil
}

func (s *profileRecordingSession) WriteFile(_ context.Context, _ File) error {
	s.writes++
	return nil
}

func (s *profileRecordingSession) Close(context.Context) error { return nil }
