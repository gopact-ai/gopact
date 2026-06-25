package sandbox

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	// ErrExecCheckFailed is returned when sandbox exec verification records a failed check.
	ErrExecCheckFailed = errors.New("sandbox: exec check failed")
	// ErrExecCheckCommandRequired is returned when sandbox exec verification has no command.
	ErrExecCheckCommandRequired = errors.New("sandbox: exec check command is required")
)

const (
	// VerificationCheckSandboxExec is the standard check ID prefix for sandbox exec results.
	VerificationCheckSandboxExec = "sandbox-exec"

	// VerificationEvidenceTypeSandboxExec is the evidence type for sandbox exec results.
	VerificationEvidenceTypeSandboxExec = "sandbox_exec"
)

// ExecCheck is an already-observed sandbox command execution result.
type ExecCheck struct {
	ID        string
	Name      string
	SessionID string
	Request   ExecRequest
	Result    ExecResult
	Err       error
	Skipped   bool
	Summary   string
	Metadata  map[string]any
}

// RecordExecCheck records an already-observed sandbox exec result as verification evidence.
func RecordExecCheck(recorder *gopact.VerificationRecorder, input ExecCheck) error {
	if recorder == nil {
		return errors.New("sandbox: verification recorder is nil")
	}
	if len(input.Request.Command) == 0 {
		return ErrExecCheckCommandRequired
	}

	check := execCheck(input)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		if input.Err != nil {
			return errors.Join(ErrExecCheckFailed, input.Err)
		}
		return ErrExecCheckFailed
	}
	return nil
}

func execCheck(input ExecCheck) gopact.VerificationCheck {
	ref := execCommandRef(input.Request.Command)
	id := input.ID
	if id == "" {
		id = VerificationCheckSandboxExec + ":" + ref
	}
	name := input.Name
	if name == "" {
		name = "sandbox exec"
	}

	status := gopact.VerificationStatusPassed
	if input.Skipped {
		status = gopact.VerificationStatusSkipped
	} else if input.Err != nil || input.Result.ExitCode != 0 {
		status = gopact.VerificationStatusFailed
	}

	summary := input.Summary
	if summary == "" {
		summary = execCheckSummary(status, input)
	}
	return gopact.VerificationCheck{
		ID:      id,
		Name:    name,
		Status:  status,
		Summary: summary,
		Evidence: []gopact.VerificationEvidence{
			{
				Type:     VerificationEvidenceTypeSandboxExec,
				Ref:      ref,
				Summary:  execEvidenceSummary(status, input),
				Metadata: execEvidenceMetadata(input),
			},
		},
		Metadata: execCheckMetadata(input),
	}
}

func execCheckSummary(status gopact.VerificationStatus, input ExecCheck) string {
	switch status {
	case gopact.VerificationStatusSkipped:
		return "sandbox exec check skipped"
	case gopact.VerificationStatusFailed:
		if input.Err != nil {
			return "sandbox exec failed: " + input.Err.Error()
		}
		return fmt.Sprintf("sandbox exec exited with %d", input.Result.ExitCode)
	default:
		return fmt.Sprintf("sandbox exec exited with %d", input.Result.ExitCode)
	}
}

func execEvidenceSummary(status gopact.VerificationStatus, input ExecCheck) string {
	if status == gopact.VerificationStatusSkipped {
		return "skipped"
	}
	return fmt.Sprintf("exit code %d", input.Result.ExitCode)
}

func execCheckMetadata(input ExecCheck) map[string]any {
	metadata := execBaseMetadata(input)
	mergeExecMetadata(metadata, input.Metadata)
	return metadata
}

func execEvidenceMetadata(input ExecCheck) map[string]any {
	return execBaseMetadata(input)
}

func execBaseMetadata(input ExecCheck) map[string]any {
	metadata := map[string]any{
		"command":   append([]string(nil), input.Request.Command...),
		"exit_code": input.Result.ExitCode,
	}
	if input.SessionID != "" {
		metadata["session_id"] = input.SessionID
	}
	if len(input.Request.Metadata) > 0 {
		metadata["request_metadata"] = copyAnyMap(input.Request.Metadata)
	}
	if len(input.Result.Stdout) > 0 {
		metadata["stdout"] = string(input.Result.Stdout)
	}
	if len(input.Result.Stderr) > 0 {
		metadata["stderr"] = string(input.Result.Stderr)
	}
	if input.Err != nil {
		metadata["error"] = input.Err.Error()
	}
	if input.Result.Usage.Duration > 0 {
		metadata["duration_ms"] = input.Result.Usage.Duration.Milliseconds()
	}
	if input.Skipped {
		metadata["skipped"] = true
	}
	return metadata
}

func mergeExecMetadata(metadata map[string]any, supplemental map[string]any) {
	for key, value := range supplemental {
		if execReservedMetadataKey(key) {
			continue
		}
		metadata[key] = value
	}
}

func execReservedMetadataKey(key string) bool {
	switch key {
	case "command",
		"exit_code",
		"session_id",
		"request_metadata",
		"stdout",
		"stderr",
		"error",
		"duration_ms",
		"skipped":
		return true
	default:
		return false
	}
}

func execCommandRef(command []string) string {
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
