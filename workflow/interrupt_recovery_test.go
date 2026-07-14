package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/runlog"
)

type interruptFailpointStore struct {
	*MemoryStore
	mu     sync.Mutex
	target string
	fired  bool
}

func (store *interruptFailpointStore) Save(ctx context.Context, record CheckpointRecord, version int64) error {
	if store.shouldFailSave(record) {
		return errors.New("interrupt failpoint: " + store.target)
	}
	return store.MemoryStore.Save(ctx, record, version)
}

func (store *interruptFailpointStore) AppendFenced(ctx context.Context, record runlog.Record, fence runlog.Fence) error {
	store.mu.Lock()
	fail := !store.fired && store.target == "append:"+record.EventType
	if fail {
		store.fired = true
	}
	store.mu.Unlock()
	if fail {
		return errors.New("interrupt failpoint: " + store.target)
	}
	return store.MemoryStore.AppendFenced(ctx, record, fence)
}

func (store *interruptFailpointStore) shouldFailSave(record CheckpointRecord) bool {
	payload, err := decodeCheckpointPayload[string](record.Payload)
	if err != nil {
		return false
	}
	phase := ""
	if payload.InterruptProgress != nil {
		if payload.PendingEvent != nil {
			phase = fmt.Sprintf("pending:%d", payload.InterruptProgress.Next)
		} else {
			phase = fmt.Sprintf("confirm:%d", payload.InterruptProgress.Next)
		}
	} else if record.Status == CheckpointInterrupted && len(payload.PendingInterrupts) > 0 {
		phase = "final"
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.fired || store.target != phase {
		return false
	}
	store.fired = true
	return true
}

func (store *interruptFailpointStore) expire(runID string) {
	store.MemoryCheckpointer.mu.Lock()
	defer store.MemoryCheckpointer.mu.Unlock()
	record := store.MemoryCheckpointer.records[runID]
	record.LeaseExpiresAt = time.Now().Add(-time.Second)
	store.MemoryCheckpointer.records[runID] = record
}

func TestInterruptBatchRecoversAtDurabilityBoundaries(t *testing.T) {
	targets := []string{"pending:1", "append:" + EventGuardInterrupted, "confirm:1", "append:" + EventWorkflowInterrupted, "confirm:2", "final"}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			store := &interruptFailpointStore{MemoryStore: NewMemoryStore(), target: target}
			guardRuns := 0
			build := func() *Workflow[string, string] {
				wf := New[string, string]("interrupt-boundaries", WithStore(store))
				node := wf.Node("node", func(_ context.Context, input string) (string, error) { return input, nil })
				node.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
					guardRuns++
					return GuardInterrupt[string, string]{Request: InterruptRequest{ID: "approval-1"}}, nil
				})))
				wf.Entry(node)
				wf.Exit(node)
				return wf
			}
			_, err := build().Invoke(context.Background(), "input", gopact.WithRunID("interrupt-boundary-run"))
			if err == nil || !store.fired {
				t.Fatalf("first Invoke() error = %v, failpoint fired = %v", err, store.fired)
			}
			store.expire("interrupt-boundary-run")
			_, err = build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "interrupt-boundary-run"}))
			var interrupted InterruptError
			if !errors.As(err, &interrupted) {
				t.Fatalf("resume Invoke() error = %v, want InterruptError", err)
			}
			if guardRuns != 1 {
				t.Fatalf("guard runs = %d, want 1", guardRuns)
			}
			records, err := store.List(context.Background(), runlog.Query{RunID: "interrupt-boundary-run"})
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]int{}
			for _, record := range records {
				counts[record.EventType]++
			}
			if counts[EventGuardInterrupted] != 1 || counts[EventWorkflowInterrupted] != 1 {
				t.Fatalf("event counts = %v", counts)
			}
		})
	}
}

func TestMultiInterruptBatchRecoversAfterMiddleObserverFailure(t *testing.T) {
	store := NewMemoryStore()
	guardRuns := map[string]int{}
	var guardMu sync.Mutex
	build := func() *Workflow[string, string] {
		wf := New[string, string]("multi-interrupt-recovery", WithStore(store), WithMaxParallelism(2))
		plan := wf.Node("plan", func(_ context.Context, input string) (string, error) { return input, nil })
		first := wf.Node("first", func(_ context.Context, input string) (string, error) { return input, nil })
		second := wf.Node("second", func(_ context.Context, input string) (string, error) { return input, nil })
		for name, node := range map[string]*Node[string, string]{"approval-first": first, "approval-second": second} {
			name := name
			node.Guard(BeforeRun("approval", GuardFunc[string, string](func(context.Context, GuardContext[string, string]) (GuardDecision[string, string], error) {
				guardMu.Lock()
				guardRuns[name]++
				guardMu.Unlock()
				return GuardInterrupt[string, string]{Request: InterruptRequest{ID: name}}, nil
			})))
		}
		merge := wf.Merge("merge", func(_ context.Context, inputs Inputs) (string, error) { return "done", nil })
		plan.Route(func(_ context.Context, input string) (Dispatch, error) {
			return plan.Once(first, input).And(plan.Once(second, input)).WithSettle(SettleAll()), nil
		})
		wf.Entry(plan)
		wf.Edge(plan, first)
		wf.Edge(plan, second)
		wf.Edge(first, merge)
		wf.Edge(second, merge)
		wf.Exit(merge)
		return wf
	}

	guardEvents := 0
	_, err := build().Invoke(context.Background(), "input", gopact.WithRunID("multi-interrupt-run"),
		gopact.WithStrictEventHandler(func(_ context.Context, event gopact.Event) error {
			if event.Type == EventGuardInterrupted {
				guardEvents++
				if guardEvents == 2 {
					return errors.New("middle observer failure")
				}
			}
			return nil
		}))
	if err == nil {
		t.Fatal("first Invoke() error = nil")
	}
	_, err = build().Invoke(context.Background(), "ignored", WithResume(ResumeRequest{RunID: "multi-interrupt-run"}))
	var interrupted InterruptError
	if !errors.As(err, &interrupted) || len(interrupted.Requests) != 2 {
		t.Fatalf("resume Invoke() error = %v, requests = %v", err, interrupted.Requests)
	}
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardRuns["approval-first"] != 1 || guardRuns["approval-second"] != 1 {
		t.Fatalf("guard runs = %v", guardRuns)
	}
	records, err := store.List(context.Background(), runlog.Query{RunID: "multi-interrupt-run"})
	if err != nil {
		t.Fatal(err)
	}
	ids, workflowInterrupted := interruptFactCounts(t, records)
	if ids["approval-first"] != 1 || ids["approval-second"] != 1 || workflowInterrupted != 1 {
		t.Fatalf("interrupt facts = %v workflow.interrupted=%d", ids, workflowInterrupted)
	}
}

func interruptFactCounts(t *testing.T, records []runlog.Record) (map[string]int, int) {
	t.Helper()
	ids := map[string]int{}
	workflowInterrupted := 0
	for _, record := range records {
		if record.EventType == EventWorkflowInterrupted {
			workflowInterrupted++
			continue
		}
		if record.EventType == EventGuardInterrupted {
			recordInterruptFact(t, ids, record.Payload)
		}
	}
	return ids, workflowInterrupted
}

func recordInterruptFact(t *testing.T, ids map[string]int, payload []byte) {
	t.Helper()
	var request InterruptRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	ids[request.ID]++
}
