package sandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyManagerDenySkipsCreate(t *testing.T) {
	ctx := context.Background()
	base := &recordingManager{session: &recordingSession{id: "session-1"}}
	manager, err := NewPolicyManager(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundarySandbox {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundarySandbox)
		}
		if req.Action != gopact.PolicyActionCreate {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionCreate)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.Spec.WorkingDir != "work" {
			t.Fatalf("working dir = %q, want work", input.Spec.WorkingDir)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "sandbox blocked"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}

	_, err = manager.Create(ctx, Spec{WorkingDir: "work"})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Create() error = %v, want ErrPolicyDenied", err)
	}
	if base.creates != 0 {
		t.Fatalf("underlying creates = %d, want 0", base.creates)
	}
}

func TestPolicySessionDenySkipsExec(t *testing.T) {
	ctx := context.Background()
	session := &recordingSession{id: "session-1"}
	manager, err := NewPolicyManager(&recordingManager{session: session}, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action == gopact.PolicyActionCreate {
			return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
		}
		if req.Action != gopact.PolicyActionExec {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionExec)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if len(input.Command) != 2 || input.Command[0] != "echo" || input.Command[1] != "hello" {
			t.Fatalf("command = %+v, want echo hello", input.Command)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "exec blocked"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}
	wrapped, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = wrapped.Exec(ctx, ExecRequest{Command: []string{"echo", "hello"}})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Exec() error = %v, want ErrPolicyDenied", err)
	}
	if session.execs != 0 {
		t.Fatalf("underlying execs = %d, want 0", session.execs)
	}
}

func TestPolicySessionReviewWriteFileReturnsInterrupt(t *testing.T) {
	ctx := context.Background()
	manager, err := NewPolicyManager(&recordingManager{session: &recordingSession{id: "session-1"}}, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Action == gopact.PolicyActionCreate {
			return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
		}
		if req.Action != gopact.PolicyActionWrite {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionWrite)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyReview, Reason: "review write"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyManager() error = %v", err)
	}
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err = session.WriteFile(ctx, File{Path: "out.txt", Content: []byte("payload")})
	if !errors.Is(err, gopact.ErrInterrupted) {
		t.Fatalf("WriteFile() error = %v, want ErrInterrupted", err)
	}
	var interruptErr *gopact.InterruptError
	if !errors.As(err, &interruptErr) {
		t.Fatalf("WriteFile() error type = %T, want *InterruptError", err)
	}
	if interruptErr.Record.RequiredBy != string(gopact.PolicyBoundarySandbox) {
		t.Fatalf("RequiredBy = %q, want sandbox", interruptErr.Record.RequiredBy)
	}
}

type recordingManager struct {
	session *recordingSession
	creates int
}

func (m *recordingManager) Create(_ context.Context, _ Spec) (Session, error) {
	m.creates++
	return m.session, nil
}

type recordingSession struct {
	id     string
	execs  int
	reads  int
	writes int
	closed bool
}

func (s *recordingSession) ID() string { return s.id }

func (s *recordingSession) Exec(_ context.Context, _ ExecRequest) (ExecResult, error) {
	s.execs++
	return ExecResult{ExitCode: 0}, nil
}

func (s *recordingSession) ReadFile(_ context.Context, path string) (File, error) {
	s.reads++
	return File{Path: path, Content: []byte("payload")}, nil
}

func (s *recordingSession) WriteFile(_ context.Context, _ File) error {
	s.writes++
	return nil
}

func (s *recordingSession) Close(_ context.Context) error {
	s.closed = true
	return nil
}
