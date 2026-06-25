package gopacttest

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestEventFramesExtractsStableTrajectoryFields(t *testing.T) {
	events := []gopact.Event{
		{Type: gopact.EventRunStarted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
		{Type: gopact.EventNodeStarted, Node: "plan", Step: 1},
		{Type: gopact.EventNodeCompleted, Node: "plan", Step: 1},
		{Type: gopact.EventRunCompleted, IDs: gopact.RuntimeIDs{RunID: "run-1"}},
	}

	got := EventFrames(events)
	want := []TrajectoryFrame{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeStarted, Node: "plan", Step: 1},
		{Type: gopact.EventNodeCompleted, Node: "plan", Step: 1},
		{Type: gopact.EventRunCompleted},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EventFrames() = %+v, want %+v", got, want)
	}
}

func TestRunExportFramesValidatesExport(t *testing.T) {
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted},
			{Type: gopact.EventNodeCompleted, Node: "answer", Step: 2},
			{Type: gopact.EventRunCompleted},
		},
	}

	got, err := RunExportFrames(export)
	if err != nil {
		t.Fatalf("RunExportFrames() error = %v", err)
	}
	want := []TrajectoryFrame{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeCompleted, Node: "answer", Step: 2},
		{Type: gopact.EventRunCompleted},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RunExportFrames() = %+v, want %+v", got, want)
	}

	if _, err := RunExportFrames(gopact.RunExport{}); err == nil {
		t.Fatal("RunExportFrames(invalid) error = nil, want validation error")
	}
}

func TestRequireTrajectoryFramesPassesForMatchingFrames(t *testing.T) {
	RequireTrajectoryFrames(t,
		[]gopact.Event{
			{Type: gopact.EventNodeStarted, Node: "plan", Step: 1},
			{Type: gopact.EventNodeCompleted, Node: "plan", Step: 1},
		},
		TrajectoryFrame{Type: gopact.EventNodeStarted, Node: "plan", Step: 1},
		TrajectoryFrame{Type: gopact.EventNodeCompleted, Node: "plan", Step: 1},
	)
}

func TestWriteAndLoadTrajectoryFramesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.golden.json")
	want := []TrajectoryFrame{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeCompleted, Node: "answer", Step: 2},
		{Type: gopact.EventRunCompleted},
	}

	if err := WriteTrajectoryFrames(path, want); err != nil {
		t.Fatalf("WriteTrajectoryFrames() error = %v", err)
	}
	got, err := LoadTrajectoryFrames(path)
	if err != nil {
		t.Fatalf("LoadTrajectoryFrames() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadTrajectoryFrames() = %+v, want %+v", got, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("golden file should be newline terminated, got %q", raw)
	}
}

func TestRequireGoldenTrajectoryFramesUpdatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.golden.json")
	events := []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeCompleted, Node: "answer", Step: 1},
		{Type: gopact.EventRunCompleted},
	}

	RequireGoldenTrajectoryFrames(t, path, events, WithGoldenUpdate(true))

	got, err := LoadTrajectoryFrames(path)
	if err != nil {
		t.Fatalf("LoadTrajectoryFrames() error = %v", err)
	}
	want := EventFrames(events)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updated golden = %+v, want %+v", got, want)
	}
}

func TestRequireGoldenTrajectoryFramesMatchesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.golden.json")
	events := []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeCompleted, Node: "answer", Step: 1},
		{Type: gopact.EventRunCompleted},
	}
	if err := WriteTrajectoryFrames(path, EventFrames(events)); err != nil {
		t.Fatalf("WriteTrajectoryFrames() error = %v", err)
	}

	RequireGoldenTrajectoryFrames(t, path, events)
}

func TestRequireRunExportGoldenTrajectoryFramesUpdatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-export.golden.json")
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted},
			{Type: gopact.EventNodeCompleted, Node: "answer", Step: 1},
			{Type: gopact.EventRunCompleted},
		},
	}

	RequireRunExportGoldenTrajectoryFrames(t, path, export, WithGoldenUpdate(true))

	got, err := LoadTrajectoryFrames(path)
	if err != nil {
		t.Fatalf("LoadTrajectoryFrames() error = %v", err)
	}
	want, err := RunExportFrames(export)
	if err != nil {
		t.Fatalf("RunExportFrames() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updated golden = %+v, want %+v", got, want)
	}
}

func TestRecordGoldenTrajectoryCheckRecordsPassedCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.golden.json")
	events := []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventNodeCompleted, Node: "answer", Step: 1},
		{Type: gopact.EventRunCompleted},
	}
	if err := WriteTrajectoryFrames(path, EventFrames(events)); err != nil {
		t.Fatalf("WriteTrajectoryFrames() error = %v", err)
	}
	recorder := gopact.NewVerificationRecorder()

	if err := RecordGoldenTrajectoryCheck(recorder, path, events); err != nil {
		t.Fatalf("RecordGoldenTrajectoryCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != VerificationCheckTrajectoryGolden || check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check identity/status = %q/%q, want trajectory golden passed", check.ID, check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Type != VerificationEvidenceTypeTrajectoryGolden || check.Evidence[0].Ref != path {
		t.Fatalf("check evidence = %+v, want trajectory golden evidence for %q", check.Evidence, path)
	}
	if check.Metadata["actual_frame_count"] != 3 || check.Metadata["expected_frame_count"] != 3 {
		t.Fatalf("check metadata = %+v, want frame counts", check.Metadata)
	}
}

func TestRecordGoldenTrajectoryCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trajectory.golden.json")
	if err := WriteTrajectoryFrames(path, []TrajectoryFrame{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventRunFailed},
	}); err != nil {
		t.Fatalf("WriteTrajectoryFrames() error = %v", err)
	}
	recorder := gopact.NewVerificationRecorder()
	events := []gopact.Event{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventRunCompleted},
	}

	err := RecordGoldenTrajectoryCheck(recorder, path, events)
	if !errors.Is(err, ErrTrajectoryMismatch) {
		t.Fatalf("RecordGoldenTrajectoryCheck() error = %v, want ErrTrajectoryMismatch", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if len(check.Evidence) != 1 || check.Evidence[0].Ref != path {
		t.Fatalf("check evidence = %+v, want failed trajectory evidence", check.Evidence)
	}
	if check.Metadata["error"] == "" || check.Metadata["actual_frame_count"] != 2 || check.Metadata["expected_frame_count"] != 2 {
		t.Fatalf("check metadata = %+v, want error and frame counts", check.Metadata)
	}
}

func TestRecordRunExportGoldenTrajectoryCheckRecordsPassedCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run-export.golden.json")
	export := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-1"},
		Outcome: gopact.RunCompleted,
		Events: []gopact.Event{
			{Type: gopact.EventRunStarted},
			{Type: gopact.EventRunCompleted},
		},
	}
	if err := WriteTrajectoryFrames(path, []TrajectoryFrame{
		{Type: gopact.EventRunStarted},
		{Type: gopact.EventRunCompleted},
	}); err != nil {
		t.Fatalf("WriteTrajectoryFrames() error = %v", err)
	}
	recorder := gopact.NewVerificationRecorder()

	if err := RecordRunExportGoldenTrajectoryCheck(recorder, path, export); err != nil {
		t.Fatalf("RecordRunExportGoldenTrajectoryCheck() error = %v", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 || checks[0].Status != gopact.VerificationStatusPassed {
		t.Fatalf("checks = %+v, want passed trajectory golden check", checks)
	}
	if checks[0].Metadata["source"] != "run_export" {
		t.Fatalf("check metadata = %+v, want run_export source", checks[0].Metadata)
	}
}
