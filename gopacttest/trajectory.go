package gopacttest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

var ErrTrajectoryMismatch = errors.New("gopacttest: trajectory mismatch")

const (
	// VerificationCheckTrajectoryGolden is the standard check ID for trajectory golden comparison.
	VerificationCheckTrajectoryGolden = "trajectory-golden"

	// VerificationEvidenceTypeTrajectoryGolden is the evidence type for a trajectory golden fixture.
	VerificationEvidenceTypeTrajectoryGolden = "trajectory_golden"
)

// TrajectoryFrame is a stable, compact frame for golden trajectory tests.
type TrajectoryFrame struct {
	Type gopact.EventType `json:"type"`
	Node string           `json:"node,omitempty"`
	Step int              `json:"step,omitempty"`
}

// GoldenTrajectoryOption configures golden trajectory assertions.
type GoldenTrajectoryOption func(*goldenTrajectoryOptions)

type goldenTrajectoryOptions struct {
	update bool
}

// WithGoldenUpdate writes the received trajectory frames to the golden file.
func WithGoldenUpdate(update bool) GoldenTrajectoryOption {
	return func(options *goldenTrajectoryOptions) {
		options.update = update
	}
}

// EventFrames extracts stable trajectory fields from events in order.
func EventFrames(events []gopact.Event) []TrajectoryFrame {
	frames := make([]TrajectoryFrame, 0, len(events))
	for _, event := range events {
		frames = append(frames, TrajectoryFrame{
			Type: event.Type,
			Node: event.Node,
			Step: event.Step,
		})
	}
	return frames
}

// RunExportFrames validates a run export and extracts its event trajectory frames.
func RunExportFrames(export gopact.RunExport) ([]TrajectoryFrame, error) {
	if err := export.Validate(); err != nil {
		return nil, err
	}
	return EventFrames(export.Events), nil
}

// LoadTrajectoryFrames loads compact trajectory frames from a golden JSON file.
func LoadTrajectoryFrames(path string) ([]TrajectoryFrame, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gopacttest: load trajectory frames: %w", err)
	}

	var frames []TrajectoryFrame
	if err := json.Unmarshal(raw, &frames); err != nil {
		return nil, fmt.Errorf("gopacttest: decode trajectory frames: %w", err)
	}
	return frames, nil
}

// WriteTrajectoryFrames writes compact trajectory frames to a golden JSON file.
func WriteTrajectoryFrames(path string, frames []TrajectoryFrame) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("gopacttest: create trajectory golden directory: %w", err)
	}
	raw, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		return fmt.Errorf("gopacttest: encode trajectory frames: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("gopacttest: write trajectory frames: %w", err)
	}
	return nil
}

// RequireTrajectoryFrames fails the test unless compact trajectory frames match exactly.
func RequireTrajectoryFrames(t testing.TB, events []gopact.Event, want ...TrajectoryFrame) {
	t.Helper()

	got := EventFrames(events)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trajectory frames = %+v, want %+v", got, want)
	}
}

// RequireGoldenTrajectoryFrames fails the test unless events match a golden trajectory file.
func RequireGoldenTrajectoryFrames(t testing.TB, path string, events []gopact.Event, opts ...GoldenTrajectoryOption) {
	t.Helper()

	options := goldenTrajectoryOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	got := EventFrames(events)
	requireGoldenTrajectoryFrames(t, path, got, options)
}

// RequireRunExportGoldenTrajectoryFrames fails the test unless a run export matches a golden trajectory file.
func RequireRunExportGoldenTrajectoryFrames(t testing.TB, path string, export gopact.RunExport, opts ...GoldenTrajectoryOption) {
	t.Helper()

	options := goldenTrajectoryOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	got, err := RunExportFrames(export)
	if err != nil {
		t.Fatalf("run export trajectory frames: %v", err)
	}
	requireGoldenTrajectoryFrames(t, path, got, options)
}

// RecordGoldenTrajectoryCheck compares events against a golden fixture and records verification evidence.
func RecordGoldenTrajectoryCheck(recorder *gopact.VerificationRecorder, path string, events []gopact.Event) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	got := EventFrames(events)
	want, verifyErr := loadAndCompareTrajectory(path, got)
	check := trajectoryGoldenCheck(path, got, want, verifyErr, "events")
	if err := recorder.Record(check); err != nil {
		if verifyErr != nil {
			return errors.Join(verifyErr, err)
		}
		return err
	}
	return verifyErr
}

// RecordRunExportGoldenTrajectoryCheck compares a run export against a golden fixture and records verification evidence.
func RecordRunExportGoldenTrajectoryCheck(recorder *gopact.VerificationRecorder, path string, export gopact.RunExport) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	got, err := RunExportFrames(export)
	var want []TrajectoryFrame
	verifyErr := err
	if verifyErr == nil {
		want, verifyErr = loadAndCompareTrajectory(path, got)
	}
	check := trajectoryGoldenCheck(path, got, want, verifyErr, "run_export")
	if err := recorder.Record(check); err != nil {
		if verifyErr != nil {
			return errors.Join(verifyErr, err)
		}
		return err
	}
	return verifyErr
}

func requireGoldenTrajectoryFrames(t testing.TB, path string, got []TrajectoryFrame, options goldenTrajectoryOptions) {
	t.Helper()

	if options.update {
		if err := WriteTrajectoryFrames(path, got); err != nil {
			t.Fatalf("update trajectory golden %q: %v", path, err)
		}
		return
	}

	want, err := LoadTrajectoryFrames(path)
	if err != nil {
		t.Fatalf("load trajectory golden %q: %v", path, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trajectory frames for %q = %+v, want %+v", path, got, want)
	}
}

func loadAndCompareTrajectory(path string, got []TrajectoryFrame) ([]TrajectoryFrame, error) {
	want, err := LoadTrajectoryFrames(path)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(got, want) {
		return want, fmt.Errorf("%w: %q", ErrTrajectoryMismatch, path)
	}
	return want, nil
}

func trajectoryGoldenCheck(path string, got []TrajectoryFrame, want []TrajectoryFrame, verifyErr error, source string) gopact.VerificationCheck {
	status := gopact.VerificationStatusPassed
	summary := fmt.Sprintf("trajectory matches golden fixture %q", path)
	metadata := map[string]any{
		"actual_frame_count":   len(got),
		"expected_frame_count": len(want),
		"source":               source,
	}
	if verifyErr != nil {
		status = gopact.VerificationStatusFailed
		summary = "trajectory golden verification failed"
		metadata["error"] = verifyErr.Error()
		metadata["actual_frames"] = append([]TrajectoryFrame(nil), got...)
		metadata["expected_frames"] = append([]TrajectoryFrame(nil), want...)
	}
	return gopact.VerificationCheck{
		ID:      VerificationCheckTrajectoryGolden,
		Name:    "trajectory golden",
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:    VerificationEvidenceTypeTrajectoryGolden,
				Ref:     trajectoryEvidenceRef(path),
				Summary: fmt.Sprintf("%d trajectory frames", len(got)),
				Metadata: map[string]any{
					"path": path,
				},
			},
		},
		Metadata: metadata,
	}
}

func trajectoryEvidenceRef(path string) string {
	if path == "" {
		return "trajectory_golden"
	}
	return path
}
