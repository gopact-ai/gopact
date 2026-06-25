package sandbox

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryReadWriteFile(t *testing.T) {
	ctx := context.Background()
	manager := NewMemory()
	session, err := manager.Create(ctx, Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := session.WriteFile(ctx, File{Path: "out.txt", Content: []byte("hello")}); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := session.ReadFile(ctx, "out.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got.Content) != "hello" {
		t.Fatalf("ReadFile() content = %q, want hello", got.Content)
	}
}

func TestMemoryExecIsUnsupported(t *testing.T) {
	session, err := NewMemory().Create(context.Background(), Spec{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = session.Exec(context.Background(), ExecRequest{Command: []string{"echo", "hello"}})
	if !errors.Is(err, ErrExecUnsupported) {
		t.Fatalf("Exec() error = %v, want %v", err, ErrExecUnsupported)
	}
}
