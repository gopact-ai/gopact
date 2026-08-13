package gopacttest

import (
	"testing"

	"github.com/gopact-ai/gopact/workflow"
)

func TestMemoryStoreConformance(t *testing.T) {
	created := 0
	RequireStoreConformance(t, func(*testing.T) workflow.Store {
		created++
		return workflow.NewMemoryStore()
	})
	if created != 5 {
		t.Fatalf("store factory calls = %d, want one per conformance subtest", created)
	}
}
