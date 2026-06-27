package toolconformance

import (
	"context"
	"errors"
	"testing"

	"github.com/gopact-ai/gopact/tools"
)

func TestCheckCommitStoreConformancePassesMemoryStore(t *testing.T) {
	results := CheckCommitStoreConformance(context.Background(), CommitStoreConformanceHarness{
		NewStore: func() tools.CommitStore {
			return tools.NewMemoryCommitStore()
		},
	})
	if failed := failedCommitStoreConformanceCases(results); len(failed) > 0 {
		t.Fatalf("CheckCommitStoreConformance() failed cases: %v", failed)
	}
	RequireCommitStoreConformance(t, CommitStoreConformanceHarness{
		NewStore: func() tools.CommitStore {
			return tools.NewMemoryCommitStore()
		},
	})
}

func TestCheckCommitStoreConformanceReportsBrokenStore(t *testing.T) {
	tests := []struct {
		name  string
		fault string
		want  string
	}{
		{name: "missing load returns record", fault: "load_missing_returns_record", want: "load-missing"},
		{name: "store mutates input", fault: "store_mutates_input", want: "store-does-not-mutate-input"},
		{name: "duplicate overwrites first", fault: "duplicate_overwrites", want: "store-preserves-first-record"},
		{name: "load returns shared record", fault: "load_returns_shared_record", want: "load-returns-copy"},
		{name: "load drops mutable metadata", fault: "load_drops_mutable_state", want: "load-returns-copy"},
		{name: "store ignores canceled context", fault: "store_ignores_context", want: "context"},
		{name: "load ignores canceled context", fault: "load_ignores_context", want: "context"},
		{name: "store accepts empty key", fault: "store_accepts_empty_key", want: "invalid-record"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := CheckCommitStoreConformance(context.Background(), CommitStoreConformanceHarness{
				NewStore: func() tools.CommitStore {
					return newFaultyCommitStore(tt.fault)
				},
			})
			if !hasFailedCommitStoreConformanceCase(results, tt.want) {
				t.Fatalf("CheckCommitStoreConformance() did not report %s: %+v", tt.want, results)
			}
		})
	}
}

type faultyCommitStore struct {
	fault   string
	records map[string]tools.CommitRecord
}

func newFaultyCommitStore(fault string) *faultyCommitStore {
	return &faultyCommitStore{
		fault:   fault,
		records: map[string]tools.CommitRecord{},
	}
}

func (s *faultyCommitStore) Load(ctx context.Context, key string) (tools.CommitRecord, bool, error) {
	if s.fault != "load_ignores_context" {
		if err := ctx.Err(); err != nil {
			return tools.CommitRecord{}, false, err
		}
	}
	if key == "missing-key" && s.fault == "load_missing_returns_record" {
		return tools.CommitRecord{IdempotencyKey: key}, true, nil
	}
	record, ok := s.records[key]
	if !ok {
		return tools.CommitRecord{}, false, nil
	}
	if s.fault == "load_returns_shared_record" {
		return record, true, nil
	}
	if s.fault == "load_drops_mutable_state" {
		record.Metadata = nil
		record.Result.Metadata = nil
		record.Result.Commit = nil
		return record, true, nil
	}
	return cloneCommitRecordForFaultyStore(record), true, nil
}

func (s *faultyCommitStore) Store(ctx context.Context, record tools.CommitRecord) error {
	if s.fault != "store_ignores_context" {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if record.IdempotencyKey == "" && s.fault != "store_accepts_empty_key" {
		return tools.ErrCommitRecordRequired
	}
	if s.fault == "store_mutates_input" {
		if record.Metadata != nil {
			record.Metadata["mutated"] = true
		}
		if record.Result.Metadata != nil {
			record.Result.Metadata["mutated"] = true
		}
	}
	if record.IdempotencyKey == "" && s.fault == "store_accepts_empty_key" {
		return nil
	}
	if _, exists := s.records[record.IdempotencyKey]; exists && s.fault != "duplicate_overwrites" {
		return nil
	}
	if record.IdempotencyKey == "error-key" {
		return errors.New("faulty commit store")
	}
	s.records[record.IdempotencyKey] = cloneCommitRecordForFaultyStore(record)
	return nil
}

func cloneCommitRecordForFaultyStore(record tools.CommitRecord) tools.CommitRecord {
	out := record
	out.Metadata = cloneMapForFaultyStore(record.Metadata)
	out.Result.Metadata = cloneMapForFaultyStore(record.Result.Metadata)
	if record.Result.Commit != nil {
		commit := *record.Result.Commit
		commit.Metadata = cloneMapForFaultyStore(record.Result.Commit.Metadata)
		out.Result.Commit = &commit
	}
	return out
}

func cloneMapForFaultyStore(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func failedCommitStoreConformanceCases(results []CommitStoreConformanceResult) []string {
	failed := []string{}
	for _, result := range results {
		if !result.Passed {
			failed = append(failed, result.Case)
		}
	}
	return failed
}

func hasFailedCommitStoreConformanceCase(results []CommitStoreConformanceResult, name string) bool {
	for _, result := range results {
		if result.Case == name && !result.Passed {
			return true
		}
	}
	return false
}
