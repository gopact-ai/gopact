package gopacttest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

var (
	ErrCommandFailed   = errors.New("gopacttest: command failed")
	ErrCommandRequired = errors.New("gopacttest: command is required")
)

const (
	// VerificationCheckCommand is the standard check ID prefix for command results.
	VerificationCheckCommand = "command"

	// VerificationEvidenceTypeCommand is the evidence type for command execution results.
	VerificationEvidenceTypeCommand = "command"
)

// CommandResult is an already-observed command execution result.
type CommandResult struct {
	ID       string
	Name     string
	Command  []string
	Dir      string
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	Duration time.Duration
	Skipped  bool
	Summary  string
	Metadata map[string]any
}

// RecordCommandCheck records a command result as verification evidence without executing it.
func RecordCommandCheck(recorder *gopact.VerificationRecorder, result CommandResult) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if len(result.Command) == 0 {
		return ErrCommandRequired
	}

	check := commandCheck(result)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if result.Err != nil {
			return errors.Join(ErrCommandFailed, result.Err)
		}
		return ErrCommandFailed
	}
	return nil
}

func commandCheck(result CommandResult) gopact.VerificationCheck {
	ref := commandRef(result.Command)
	id := result.ID
	if id == "" {
		id = VerificationCheckCommand + ":" + ref
	}
	name := result.Name
	if name == "" {
		name = "command"
	}

	status := gopact.VerificationStatusPassed
	if result.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if result.Err != nil || result.ExitCode != 0 {
		status = gopact.VerificationStatusFailed
	}

	summary := result.Summary
	if summary == "" {
		summary = commandSummary(status, result)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeCommand,
				Ref:      ref,
				Summary:  commandEvidenceSummary(status, result),
				Metadata: commandEvidenceMetadata(result),
			},
		},
		Metadata: commandCheckMetadata(result),
	}
}

func commandSummary(status gopact.VerificationStatus, result CommandResult) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "command check skipped"
	case gopact.VerificationStatusFailed:
		if result.Err != nil {
			return "command failed: " + result.Err.Error()
		}
		return fmt.Sprintf("command exited with %d", result.ExitCode)
	default:
		return fmt.Sprintf("command exited with %d", result.ExitCode)
	}
}

func commandEvidenceSummary(status gopact.VerificationStatus, result CommandResult) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	return fmt.Sprintf("exit code %d", result.ExitCode)
}

func commandCheckMetadata(result CommandResult) map[string]any {
	metadata := commandBaseMetadata(result)
	if keys := sortedSupplementalMetadataKeys(result.Metadata, commandReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalMetadata(metadata, result.Metadata, commandReservedMetadataKey)
	return metadata
}

func commandEvidenceMetadata(result CommandResult) map[string]any {
	return commandCheckMetadata(result)
}

func commandBaseMetadata(result CommandResult) map[string]any {
	metadata := map[string]any{
		"command":   append([]string(nil), result.Command...),
		"exit_code": result.ExitCode,
	}
	if result.Dir != "" {
		metadata["dir"] = result.Dir
	}
	if result.Stdout != "" {
		metadata["stdout"] = result.Stdout
	}
	if result.Stderr != "" {
		metadata["stderr"] = result.Stderr
	}
	if result.Err != nil {
		metadata["error"] = result.Err.Error()
	}
	if result.Duration > 0 {
		metadata["duration_ms"] = result.Duration.Milliseconds()
	}
	if result.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func commandReservedMetadataKey(key string) bool {
	switch key {
	case "command",
		"exit_code",
		"dir",
		"stdout",
		"stderr",
		"error",
		"duration_ms",
		"metadata_keys",
		"skipped":
		return true
	default:
		return false
	}
}

func commandRef(command []string) string {
	parts := make([]string, 0, len(command))
	for _, arg := range command {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\") {
			parts = append(parts, strconv.Quote(arg))
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}
