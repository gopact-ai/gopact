package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestPolicyStoreDenySkipsArtifactPut(t *testing.T) {
	ctx := context.Background()
	base := NewMemory()
	store, err := NewPolicyStore(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		if req.Boundary != gopact.PolicyBoundaryArtifact {
			t.Fatalf("boundary = %q, want %q", req.Boundary, gopact.PolicyBoundaryArtifact)
		}
		if req.Action != gopact.PolicyActionPut {
			t.Fatalf("action = %q, want %q", req.Action, gopact.PolicyActionPut)
		}
		input, ok := req.Input.(PolicyInput)
		if !ok {
			t.Fatalf("policy input type = %T, want PolicyInput", req.Input)
		}
		if input.Artifact.Size != 7 || input.Artifact.Ref.Name != "secret.txt" {
			t.Fatalf("policy input = %+v, want artifact metadata without payload", input)
		}
		return gopact.PolicyDecision{Action: gopact.PolicyDeny, Reason: "artifact blocked"}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyStore() error = %v", err)
	}

	_, err = store.Put(ctx, gopact.Artifact{
		Ref:     gopact.ArtifactRef{Name: "secret.txt"},
		Content: []byte("payload"),
	})
	if !errors.Is(err, gopact.ErrPolicyDenied) {
		t.Fatalf("Put() error = %v, want ErrPolicyDenied", err)
	}
	list, err := base.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("base artifacts = %+v, want none", list)
	}
}

func TestPolicyStoreAllowsArtifactGetAndList(t *testing.T) {
	ctx := context.Background()
	base := NewMemory()
	ref, err := base.Put(ctx, gopact.Artifact{Content: []byte("payload")})
	if err != nil {
		t.Fatalf("seed Put() error = %v", err)
	}
	var actions []gopact.PolicyRequestAction
	store, err := NewPolicyStore(base, gopact.PolicyFunc(func(_ context.Context, req gopact.PolicyRequest) (gopact.PolicyDecision, error) {
		actions = append(actions, req.Action)
		return gopact.PolicyDecision{Action: gopact.PolicyAllow}, nil
	}))
	if err != nil {
		t.Fatalf("NewPolicyStore() error = %v", err)
	}

	if _, err := store.Get(ctx, ref.ID); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := store.List(ctx); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(actions) != 2 || actions[0] != gopact.PolicyActionGet || actions[1] != gopact.PolicyActionList {
		t.Fatalf("actions = %+v, want get then list", actions)
	}
}
