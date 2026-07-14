package workflow

import (
	"context"
	"errors"
	"fmt"
)

// ActivationPhase describes one node activation execution state.
type ActivationPhase string

// Activation phases.
const (
	ActivationReady       ActivationPhase = "ready"
	ActivationRunning     ActivationPhase = "running"
	ActivationCompleted   ActivationPhase = "completed"
	ActivationFailed      ActivationPhase = "failed"
	ActivationCanceled    ActivationPhase = "canceled"
	ActivationInterrupted ActivationPhase = "interrupted"
	ActivationSkipped     ActivationPhase = "skipped"
)

type activationPhase = ActivationPhase

const (
	activationReady       = ActivationReady
	activationRunning     = ActivationRunning
	activationCompleted   = ActivationCompleted
	activationFailed      = ActivationFailed
	activationCanceled    = ActivationCanceled
	activationInterrupted = ActivationInterrupted
	activationSkipped     = ActivationSkipped
)

type activationRecord struct {
	activation           activation
	phase                activationPhase
	attempt              int
	nodeExecutionVersion int64
	origin               string
	contextRevision      int64
	result               nodeRunResult
	hasResult            bool
	cause                error
}

func (state *runState) trackActivation(item activation) {
	if state.activations == nil {
		state.activations = map[string]*activationRecord{}
	}
	if state.activations[item.id] != nil {
		return
	}
	state.activations[item.id] = &activationRecord{activation: item, phase: activationReady, origin: "natural"}
}

func (state *runState) retryActivation(id string, input any) error {
	record, err := state.activationRecord(id)
	if err != nil {
		return err
	}
	if record.phase != activationRunning {
		return fmt.Errorf("workflow: activation %q phase %q cannot retry", id, record.phase)
	}
	if err := state.removeReady(id); err != nil {
		return err
	}
	record.phase = activationReady
	record.activation.input = input
	record.origin = "guard_retry"
	record.result = nodeRunResult{}
	record.hasResult = false
	record.cause = nil
	state.queue = append(state.queue, record.activation)
	return nil
}

func (state *runState) startActivation(id string) error {
	record, err := state.activationRecord(id)
	if err != nil {
		return err
	}
	if record.phase != activationReady {
		return fmt.Errorf("workflow: activation %q phase %q cannot start", id, record.phase)
	}
	record.phase = activationRunning
	record.attempt++
	if state.nodeVersions == nil {
		state.nodeVersions = map[string]int64{}
	}
	state.nodeVersions[record.activation.node]++
	record.nodeExecutionVersion = state.nodeVersions[record.activation.node]
	record.contextRevision = state.contextRevision
	return nil
}

func (state *runState) finishActivation(id string, result nodeRunResult, cause error) error {
	record, err := state.activationRecord(id)
	if err != nil {
		return err
	}
	if record.phase != activationRunning {
		return fmt.Errorf("workflow: activation %q phase %q cannot finish", id, record.phase)
	}
	record.phase = completedActivationPhase(result, cause)
	record.result = result
	record.hasResult = cause == nil
	record.cause = cause
	return nil
}

func completedActivationPhase(result nodeRunResult, cause error) activationPhase {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return activationCanceled
	}
	if cause != nil {
		return activationFailed
	}
	if result.skipped {
		return activationSkipped
	}
	return activationCompleted
}

func (state *runState) cancelActivation(id string, cause error) error {
	record, err := state.activationRecord(id)
	if err != nil {
		return err
	}
	if record.phase != activationReady {
		return fmt.Errorf("workflow: activation %q phase %q cannot cancel", id, record.phase)
	}
	record.phase = activationCanceled
	record.cause = cause
	return nil
}

func (state *runState) interruptActivation(id string) error {
	record, err := state.activationRecord(id)
	if err != nil {
		return err
	}
	if record.phase != activationRunning {
		return fmt.Errorf("workflow: activation %q phase %q cannot interrupt", id, record.phase)
	}
	record.phase = activationInterrupted
	return nil
}

func (state *runState) activationRecord(id string) (*activationRecord, error) {
	record := state.activations[id]
	if record == nil {
		return nil, fmt.Errorf("workflow: activation %q is not tracked", id)
	}
	return record, nil
}

func (state *runState) prepareResume(resolved bool) {
	for _, record := range state.activations {
		record.prepareResume(resolved)
	}
}

func (record *activationRecord) prepareResume(resolved bool) {
	if record.origin == "" {
		record.origin = "natural"
	}
	if record.phase == activationRunning {
		record.phase = activationReady
		return
	}
	if resolved && record.phase == activationInterrupted {
		record.phase = activationReady
	}
}
