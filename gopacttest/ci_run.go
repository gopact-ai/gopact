package gopacttest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// CIRun is an already-observed remote CI run.
type CIRun struct {
	ID            string
	Name          string
	Provider      string
	Repository    string
	Workflow      string
	RunID         string
	URL           string
	HeadSHA       string
	HeadBranch    string
	Status        string
	Conclusion    string
	RequiredGates []string
	Gates         []CIRunGate
	Metadata      map[string]any
}

// CIRunSet is a collection of already-observed CI runs that should be consumed as one readiness gate.
type CIRunSet struct {
	ID                   string
	Name                 string
	RequiredRepositories []string
	RequiredGates        []string
	Runs                 []CIRun
	Metadata             map[string]any
}

// CIRunGate is one already-observed gate inside a remote CI run.
type CIRunGate struct {
	Gate        string
	Status      gopact.VerificationStatus
	Job         string
	Step        string
	URL         string
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	Metadata    map[string]any
}

// RecordCIRunCheck records an already-observed remote CI run as one verification check.
func RecordCIRunCheck(recorder *gopact.VerificationRecorder, run CIRun) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if err := validateCIRun(run); err != nil {
		return err
	}

	check := ciRunCheck(run)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		return ErrCIGateFailed
	}
	return nil
}

// RecordCIRunSetCheck records already-observed remote CI runs as one verification check.
func RecordCIRunSetCheck(recorder *gopact.VerificationRecorder, set CIRunSet) error {
	if recorder == nil {
		return errors.New("gopacttest: verification recorder is nil")
	}
	if err := validateCIRunSet(set); err != nil {
		return err
	}

	check := ciRunSetCheck(set)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		return ErrCIGateFailed
	}
	return nil
}

func validateCIRun(run CIRun) error {
	if len(run.Gates) == 0 {
		return fmt.Errorf("%w: gates are required", ErrCIGateRequired)
	}
	gateByName := make(map[string]struct{}, len(run.Gates))
	for i, gate := range run.Gates {
		name := strings.TrimSpace(gate.Gate)
		if name == "" {
			return fmt.Errorf("%w: gate %d name is required", ErrCIGateRequired, i)
		}
		if !validCIRunGateStatus(gate.Status) {
			return fmt.Errorf("%w: gate %s status %q is invalid", ErrCIGateRequired, name, gate.Status)
		}
		gateByName[name] = struct{}{}
	}
	for _, gate := range run.RequiredGates {
		gate = strings.TrimSpace(gate)
		if gate == "" {
			return fmt.Errorf("%w: required gate is empty", ErrCIGateRequired)
		}
		if _, ok := gateByName[gate]; !ok {
			return fmt.Errorf("%w: required gate %s is missing", ErrCIGateRequired, gate)
		}
	}
	return nil
}

func validateCIRunSet(set CIRunSet) error {
	if len(set.Runs) == 0 {
		return fmt.Errorf("%w: runs are required", ErrCIGateRequired)
	}
	repositoryByName := make(map[string]struct{}, len(set.Runs))
	for i, run := range set.Runs {
		repository := strings.TrimSpace(run.Repository)
		if repository == "" {
			return fmt.Errorf("%w: run %d repository is required", ErrCIGateRequired, i)
		}
		if _, ok := repositoryByName[repository]; ok {
			return fmt.Errorf("%w: repository %s is duplicated", ErrCIGateRequired, repository)
		}
		if err := validateCIRun(run); err != nil {
			return fmt.Errorf("gopacttest: CI run set run %s: %w", repository, err)
		}
		if err := validateCIRunRequiredGates(run, set.RequiredGates); err != nil {
			return fmt.Errorf("gopacttest: CI run set run %s: %w", repository, err)
		}
		repositoryByName[repository] = struct{}{}
	}
	for _, repository := range set.RequiredRepositories {
		repository = strings.TrimSpace(repository)
		if repository == "" {
			return fmt.Errorf("%w: required repository is empty", ErrCIGateRequired)
		}
		if _, ok := repositoryByName[repository]; !ok {
			return fmt.Errorf("%w: required repository %s is missing", ErrCIGateRequired, repository)
		}
	}
	return nil
}

func validateCIRunRequiredGates(run CIRun, required []string) error {
	if len(required) == 0 {
		return nil
	}
	gateByName := make(map[string]struct{}, len(run.Gates))
	for _, gate := range run.Gates {
		gateByName[strings.TrimSpace(gate.Gate)] = struct{}{}
	}
	for _, gate := range required {
		gate = strings.TrimSpace(gate)
		if gate == "" {
			return fmt.Errorf("%w: required gate is empty", ErrCIGateRequired)
		}
		if _, ok := gateByName[gate]; !ok {
			return fmt.Errorf("%w: required gate %s is missing", ErrCIGateRequired, gate)
		}
	}
	return nil
}

func validCIRunGateStatus(status gopact.VerificationStatus) bool {
	switch status {
	case gopact.VerificationStatusPassed,
		gopact.VerificationStatusFailed,
		gopact.VerificationStatusSkipped:
		return true
	default:
		return false
	}
}

func ciRunCheck(run CIRun) gopact.VerificationCheck {
	id := run.ID
	if id == "" {
		id = ciRunCheckID(run)
	}
	name := run.Name
	if name == "" {
		name = "CI run"
	}
	status, passed, failed, skipped := ciRunStatus(run.Gates)
	return gopact.VerificationCheck{
		ID:       id,
		Name:     name,
		Status:   status,
		Summary:  ciGateSuiteSummary(status, passed, failed, skipped),
		Evidence: ciRunEvidence(run),
		Metadata: ciRunMetadata(run, passed, failed, skipped),
	}
}

func ciRunSetCheck(set CIRunSet) gopact.VerificationCheck {
	id := set.ID
	if id == "" {
		id = "ci-runs"
	}
	name := set.Name
	if name == "" {
		name = "CI runs"
	}
	status, passedGates, failedGates, skippedGates := ciRunSetGateStatus(set.Runs)
	passedRuns, failedRuns, skippedRuns := ciRunSetRunStatusCounts(set.Runs)
	return gopact.VerificationCheck{
		ID:       id,
		Name:     name,
		Status:   status,
		Summary:  ciGateSuiteSummary(status, passedGates, failedGates, skippedGates),
		Evidence: ciRunSetEvidence(set.Runs),
		Metadata: ciRunSetMetadata(set, passedRuns, failedRuns, skippedRuns, passedGates, failedGates, skippedGates),
	}
}

func ciRunCheckID(run CIRun) string {
	parts := make([]string, 0, 4)
	for _, part := range []string{run.Provider, run.Repository, run.Workflow, run.RunID} {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return VerificationCheckCIGates
	}
	return "ci-run:" + strings.Join(parts, ":")
}

func ciRunSetGateStatus(runs []CIRun) (gopact.VerificationStatus, int, int, int) {
	var gates []CIRunGate
	for _, run := range runs {
		gates = append(gates, run.Gates...)
	}
	return ciRunStatus(gates)
}

func ciRunSetRunStatusCounts(runs []CIRun) (passed, failed, skipped int) {
	for _, run := range runs {
		status, _, _, _ := ciRunStatus(run.Gates)
		switch status {
		case gopact.VerificationStatusFailed:
			failed++
		case gopact.VerificationStatusSkipped:
			skipped++
		default:
			passed++
		}
	}
	return passed, failed, skipped
}

func ciRunStatus(gates []CIRunGate) (gopact.VerificationStatus, int, int, int) {
	passed := 0
	failed := 0
	skipped := 0
	for _, gate := range gates {
		switch gate.Status {
		case gopact.VerificationStatusFailed:
			failed++
		case gopact.VerificationStatusSkipped:
			skipped++
		default:
			passed++
		}
	}
	if failed > 0 {
		return gopact.VerificationStatusFailed, passed, failed, skipped
	}
	if passed > 0 {
		return gopact.VerificationStatusPassed, passed, failed, skipped
	}
	return gopact.VerificationStatusSkipped, passed, failed, skipped
}

func ciRunEvidence(run CIRun) []gopact.VerificationEvidence {
	evidence := make([]gopact.VerificationEvidence, 0, len(run.Gates))
	for _, gate := range run.Gates {
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:     VerificationEvidenceTypeCIGate,
			Ref:      ciGateRef(gate.Gate),
			Summary:  ciGateEvidenceSummary(gate.Gate, gate.Status),
			Metadata: ciRunGateMetadata(run, gate),
		})
	}
	return evidence
}

func ciRunSetEvidence(runs []CIRun) []gopact.VerificationEvidence {
	var evidence []gopact.VerificationEvidence
	for _, run := range runs {
		for _, gate := range run.Gates {
			evidence = append(evidence, gopact.VerificationEvidence{
				Type:     VerificationEvidenceTypeCIGate,
				Ref:      ciRunSetGateRef(run.Repository, gate.Gate),
				Summary:  ciGateEvidenceSummary(gate.Gate, gate.Status),
				Metadata: ciRunGateMetadata(run, gate),
			})
		}
	}
	return evidence
}

func ciRunMetadata(run CIRun, passed, failed, skipped int) map[string]any {
	metadata := map[string]any{
		"gate_count":         len(run.Gates),
		"passed_gate_count":  passed,
		"failed_gate_count":  failed,
		"skipped_gate_count": skipped,
	}
	if len(run.RequiredGates) > 0 {
		metadata["required_gates"] = append([]string(nil), run.RequiredGates...)
	}
	addCIRunIdentityMetadata(metadata, run)
	if run.Status != "" {
		metadata["status"] = run.Status
	}
	if run.Conclusion != "" {
		metadata["conclusion"] = run.Conclusion
	}
	if keys := sortedSupplementalMetadataKeys(run.Metadata, ciRunReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalMetadata(metadata, run.Metadata, ciRunReservedMetadataKey)
	return metadata
}

func ciRunSetMetadata(set CIRunSet, passedRuns, failedRuns, skippedRuns, passedGates, failedGates, skippedGates int) map[string]any {
	metadata := map[string]any{
		"run_count":          len(set.Runs),
		"repository_count":   ciRunSetRepositoryCount(set.Runs),
		"gate_count":         passedGates + failedGates + skippedGates,
		"passed_run_count":   passedRuns,
		"failed_run_count":   failedRuns,
		"skipped_run_count":  skippedRuns,
		"passed_gate_count":  passedGates,
		"failed_gate_count":  failedGates,
		"skipped_gate_count": skippedGates,
	}
	if len(set.RequiredRepositories) > 0 {
		metadata["required_repositories"] = append([]string(nil), set.RequiredRepositories...)
	}
	if len(set.RequiredGates) > 0 {
		metadata["required_gates"] = append([]string(nil), set.RequiredGates...)
	}
	if keys := sortedSupplementalMetadataKeys(set.Metadata, ciRunSetReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalMetadata(metadata, set.Metadata, ciRunSetReservedMetadataKey)
	return metadata
}

func ciRunGateMetadata(run CIRun, gate CIRunGate) map[string]any {
	metadata := map[string]any{
		"gate":   ciGateName(gate.Gate),
		"status": string(gate.Status),
	}
	addCIRunIdentityMetadata(metadata, run)
	if gate.Job != "" {
		metadata["job"] = gate.Job
	}
	if gate.Step != "" {
		metadata["step"] = gate.Step
	}
	if gate.URL != "" {
		metadata["url"] = gate.URL
	} else if run.URL != "" {
		metadata["url"] = run.URL
	}
	if !gate.StartedAt.IsZero() {
		metadata["started_at"] = gate.StartedAt.Format(time.RFC3339Nano)
	}
	if !gate.CompletedAt.IsZero() {
		metadata["completed_at"] = gate.CompletedAt.Format(time.RFC3339Nano)
	}
	if gate.Duration > 0 {
		metadata["duration_ms"] = gate.Duration.Milliseconds()
	}
	if keys := sortedSupplementalMetadataKeys(gate.Metadata, ciRunGateReservedMetadataKey); len(keys) > 0 {
		metadata["metadata_keys"] = keys
	}
	mergeSupplementalMetadata(metadata, gate.Metadata, ciRunGateReservedMetadataKey)
	return metadata
}

func addCIRunIdentityMetadata(metadata map[string]any, run CIRun) {
	if run.Provider != "" {
		metadata["ci_provider"] = run.Provider
	}
	if run.Repository != "" {
		metadata["repository"] = run.Repository
	}
	if run.Workflow != "" {
		metadata["workflow"] = run.Workflow
	}
	if run.RunID != "" {
		metadata["run_id"] = run.RunID
	}
	if run.URL != "" {
		metadata["url"] = run.URL
	}
	if run.HeadSHA != "" {
		metadata["head_sha"] = run.HeadSHA
	}
	if run.HeadBranch != "" {
		metadata["head_branch"] = run.HeadBranch
	}
}

func ciRunSetRepositoryCount(runs []CIRun) int {
	seen := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		repository := strings.TrimSpace(run.Repository)
		if repository == "" {
			continue
		}
		seen[repository] = struct{}{}
	}
	return len(seen)
}

func ciRunSetGateRef(repository, gate string) string {
	repository = strings.TrimSpace(repository)
	gate = ciGateName(gate)
	if repository == "" {
		return ciGateRef(gate)
	}
	return "ci-gate:" + repository + ":" + gate
}

func ciRunSetReservedMetadataKey(key string) bool {
	switch key {
	case "run_count",
		"repository_count",
		"gate_count",
		"passed_run_count",
		"failed_run_count",
		"skipped_run_count",
		"passed_gate_count",
		"failed_gate_count",
		"skipped_gate_count",
		"required_repositories",
		"required_gates",
		"metadata_keys":
		return true
	default:
		return false
	}
}

func ciRunReservedMetadataKey(key string) bool {
	switch key {
	case "gate_count",
		"passed_gate_count",
		"failed_gate_count",
		"skipped_gate_count",
		"required_gates",
		"ci_provider",
		"repository",
		"workflow",
		"run_id",
		"url",
		"head_sha",
		"head_branch",
		"status",
		"conclusion",
		"metadata_keys":
		return true
	default:
		return false
	}
}

func ciRunGateReservedMetadataKey(key string) bool {
	switch key {
	case "gate",
		"status",
		"ci_provider",
		"repository",
		"workflow",
		"run_id",
		"url",
		"head_sha",
		"head_branch",
		"job",
		"step",
		"started_at",
		"completed_at",
		"duration_ms",
		"metadata_keys":
		return true
	default:
		return false
	}
}
