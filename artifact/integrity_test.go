package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestVerifyRefReturnsArtifactWhenIntegrityMatches(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{
		Ref:     gopact.ArtifactRef{Name: "trace.json"},
		Content: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := VerifyRef(ctx, store, ref)
	if err != nil {
		t.Fatalf("VerifyRef() error = %v", err)
	}
	if got.Ref.ID != ref.ID || string(got.Content) != `{"ok":true}` {
		t.Fatalf("VerifyRef() = %+v, want stored artifact", got)
	}
}

func TestVerifyRefRejectsTamperedPayload(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{Content: []byte("original")})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	tampered := store.items[ref.ID]
	tampered.Content = []byte("tampered")
	store.items[ref.ID] = tampered

	_, err = VerifyRef(ctx, store, ref)
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("VerifyRef() error = %v, want ErrIntegrityMismatch", err)
	}
}

func TestVerifyRefsPreservesOrder(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	first, err := store.Put(ctx, gopact.Artifact{Content: []byte("first")})
	if err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	second, err := store.Put(ctx, gopact.Artifact{Content: []byte("second")})
	if err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	got, err := VerifyRefs(ctx, store, []gopact.ArtifactRef{second, first})
	if err != nil {
		t.Fatalf("VerifyRefs() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("VerifyRefs() count = %d, want 2", len(got))
	}
	if string(got[0].Content) != "second" || string(got[1].Content) != "first" {
		t.Fatalf("VerifyRefs() order = %q/%q, want second/first", got[0].Content, got[1].Content)
	}
}

func TestRecordVerifyRefsRecordsPassedCheck(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{
		Ref:     gopact.ArtifactRef{Name: "trace.json", MIMEType: "application/json"},
		Content: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	recorder := gopact.NewVerificationRecorder()

	artifacts, err := RecordVerifyRefs(ctx, recorder, store, []gopact.ArtifactRef{ref})
	if err != nil {
		t.Fatalf("RecordVerifyRefs() error = %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].Ref.ID != ref.ID {
		t.Fatalf("artifacts = %+v, want verified artifact %q", artifacts, ref.ID)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != VerificationCheckArtifactIntegrity || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check identity/status = %q/%q, want artifact integrity passed", check.ID, check.Status)
	}
	if len(check.Evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(check.Evidence))
	}
	evidence := check.Evidence[0]
	if evidence.Type != VerificationEvidenceTypeArtifact || evidence.Ref != ref.ID {
		t.Fatalf("evidence = %+v, want artifact evidence for %q", evidence, ref.ID)
	}
	if evidence.Metadata["sha256"] != ref.SHA256 || evidence.Metadata["size"] != ref.Size {
		t.Fatalf("evidence metadata = %+v, want ref integrity metadata", evidence.Metadata)
	}
}

func TestRecordVerifyRefsRecordsFailedCheckBeforeReturningError(t *testing.T) {
	ctx := context.Background()
	store := NewMemory()
	ref, err := store.Put(ctx, gopact.Artifact{Content: []byte("original")})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	tampered := store.items[ref.ID]
	tampered.Content = []byte("tampered")
	store.items[ref.ID] = tampered
	recorder := gopact.NewVerificationRecorder()

	_, err = RecordVerifyRefs(ctx, recorder, store, []gopact.ArtifactRef{ref})
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("RecordVerifyRefs() error = %v, want ErrIntegrityMismatch", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Ref != ref.ID {
		t.Fatalf("check evidence = %+v, want failed artifact evidence", check.Evidence)
	}
	if check.Metadata["error"] == "" {
		t.Fatalf("check metadata = %+v, want error detail", check.Metadata)
	}
}

func TestRecordVerifyRefsRecordsSkippedCheckForNoRefs(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	artifacts, err := RecordVerifyRefs(context.Background(), recorder, NewMemory(), nil)
	if err != nil {
		t.Fatalf("RecordVerifyRefs() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts count = %d, want 0", len(artifacts))
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	if checks[0].Status != gopact.VerificationStatusSkipped || len(checks[0].Evidence) != 0 {
		t.Fatalf("check = %+v, want skipped check without evidence", checks[0])
	}
}
