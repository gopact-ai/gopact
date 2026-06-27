package extensionscaffold

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

var (
	ErrRemoteStatusNotReady = errors.New("extensionscaffold: remote repositories are not ready")
	ErrRemoteStatusRequired = errors.New("extensionscaffold: remote status report is required")
	ErrRemoteCINotReady     = errors.New("extensionscaffold: remote ci run set is not ready")
)

const (
	// VerificationCheckRemoteRepositories is the standard check ID prefix for external repository readiness.
	VerificationCheckRemoteRepositories = "external-repositories"

	// VerificationCheckRemoteCI is the standard check ID prefix for external repository CI readiness.
	VerificationCheckRemoteCI = "external-ci"

	// VerificationEvidenceTypeRemoteRepositoryReadiness is the evidence type for one external repository readiness observation.
	VerificationEvidenceTypeRemoteRepositoryReadiness = "external_repository_readiness"
)

var defaultRemoteCIGates = []string{"whitespace", "unit", "vet"}

// DefaultRemoteCIGates returns the standard external scaffold CI gate names.
func DefaultRemoteCIGates() []string {
	return append([]string(nil), defaultRemoteCIGates...)
}

// RecordRemoteStatusCheck records an already-observed external repository readiness report.
func RecordRemoteStatusCheck(recorder *gopact.VerificationRecorder, report RemoteStatusReport) error {
	if recorder == nil {
		return errors.New("extensionscaffold: verification recorder is nil")
	}
	if err := validateRemoteStatusReport(report); err != nil {
		return err
	}

	check := remoteStatusCheck(report)
	if err := recorder.Record(check); err != nil {
		return err
	}
	if check.Status == gopact.VerificationStatusFailed {
		return ErrRemoteStatusNotReady
	}
	return nil
}

// RecordRemoteCIRunSetCheck records already-observed external repository CI run status as one cross-repository check.
func RecordRemoteCIRunSetCheck(recorder *gopact.VerificationRecorder, report RemoteStatusReport, requiredGates []string) error {
	if recorder == nil {
		return errors.New("extensionscaffold: verification recorder is nil")
	}
	if err := validateRemoteStatusReport(report); err != nil {
		return err
	}
	gates, err := normalizeRemoteCIGates(requiredGates)
	if err != nil {
		return err
	}

	before := len(recorder.Checks())
	err = gopacttest.RecordCIRunSetCheck(recorder, remoteCIRunSet(report, gates))
	if err != nil {
		if errors.Is(err, gopacttest.ErrCIGateFailed) {
			return ErrRemoteCINotReady
		}
		return err
	}
	checks := recorder.Checks()
	if len(checks) > before && checks[len(checks)-1].Status != gopact.VerificationStatusPassed {
		return ErrRemoteCINotReady
	}
	return nil
}

func validateRemoteStatusReport(report RemoteStatusReport) error {
	organization := strings.TrimSpace(report.Organization)
	if organization == "" {
		return fmt.Errorf("%w: organization is required", ErrRemoteStatusRequired)
	}
	if len(report.Repositories) == 0 {
		return fmt.Errorf("%w: repositories are required", ErrRemoteStatusRequired)
	}
	for i, repository := range report.Repositories {
		if remoteRepositoryRef(organization, repository) == "" {
			return fmt.Errorf("%w: repository %d remote is required", ErrRemoteStatusRequired, i)
		}
	}
	return nil
}

func normalizeRemoteCIGates(requiredGates []string) ([]string, error) {
	if len(requiredGates) == 0 {
		return nil, fmt.Errorf("%w: required CI gates are required", ErrRemoteStatusRequired)
	}
	gates := make([]string, 0, len(requiredGates))
	seen := make(map[string]bool, len(requiredGates))
	for _, gate := range requiredGates {
		gate = strings.TrimSpace(gate)
		if gate == "" {
			return nil, fmt.Errorf("%w: required CI gate is empty", ErrRemoteStatusRequired)
		}
		if seen[gate] {
			return nil, fmt.Errorf("%w: required CI gate %s is duplicated", ErrRemoteStatusRequired, gate)
		}
		seen[gate] = true
		gates = append(gates, gate)
	}
	return gates, nil
}

func remoteCIRunSet(report RemoteStatusReport, requiredGates []string) gopacttest.CIRunSet {
	organization := strings.TrimSpace(report.Organization)
	requiredRepositories := make([]string, 0, len(report.Repositories))
	runs := make([]gopacttest.CIRun, 0, len(report.Repositories))
	for _, repository := range report.Repositories {
		remote := remoteRepositoryRef(organization, repository)
		requiredRepositories = append(requiredRepositories, remote)
		runs = append(runs, remoteRepositoryCIRun(repository, remote, requiredGates))
	}
	return gopacttest.CIRunSet{
		ID:                   VerificationCheckRemoteCI + ":" + organization,
		Name:                 "external repository CI",
		RequiredRepositories: requiredRepositories,
		RequiredGates:        append([]string(nil), requiredGates...),
		Runs:                 runs,
		Metadata: map[string]any{
			"organization": organization,
			"source":       "remote_status_report",
		},
	}
}

func remoteRepositoryCIRun(repository RemoteRepositoryStatus, remote string, requiredGates []string) gopacttest.CIRun {
	workflow := strings.TrimSpace(repository.CIRunWorkflowName)
	if workflow == "" && repository.CIWorkflowPresent {
		workflow = "ci"
	}
	gates := make([]gopacttest.CIRunGate, 0, len(requiredGates))
	for _, gate := range requiredGates {
		gates = append(gates, gopacttest.CIRunGate{
			Gate:     gate,
			Status:   remoteRepositoryCIGateStatus(repository),
			Job:      "test",
			Step:     remoteCIGateStep(gate),
			URL:      strings.TrimSpace(repository.CIRunURL),
			Metadata: remoteCIGateMetadata(repository),
		})
	}
	return gopacttest.CIRun{
		Provider:      "github-actions",
		Repository:    remote,
		Workflow:      workflow,
		RunID:         strings.TrimSpace(repository.CIRunID),
		URL:           strings.TrimSpace(repository.CIRunURL),
		HeadBranch:    strings.TrimSpace(repository.CIRunHeadBranch),
		Status:        strings.TrimSpace(repository.CIRunStatus),
		Conclusion:    strings.TrimSpace(repository.CIRunConclusion),
		RequiredGates: append([]string(nil), requiredGates...),
		Gates:         gates,
		Metadata: map[string]any{
			"source":                  "remote_status_report",
			"ci_workflow_present":     repository.CIWorkflowPresent,
			"ci_workflow_run_seen":    repository.CIWorkflowRunSeen,
			"ci_run_passed":           repository.CIRunPassed,
			"ready":                   repository.Ready,
			"private_sdk_secret_seen": repository.PrivateSDKSecretPresent,
		},
	}
}

func remoteRepositoryCIGateStatus(repository RemoteRepositoryStatus) gopact.VerificationStatus {
	switch {
	case repository.CIRunPassed:
		return gopact.VerificationStatusPassed
	case repository.CIWorkflowRunSeen:
		return gopact.VerificationStatusFailed
	default:
		return gopact.VerificationStatusSkipped
	}
}

func remoteCIGateStep(gate string) string {
	switch gate {
	case "whitespace":
		return "Check formatting whitespace"
	case "unit":
		return "Test"
	case "vet":
		return "Vet"
	default:
		return gate
	}
}

func remoteCIGateMetadata(repository RemoteRepositoryStatus) map[string]any {
	metadata := map[string]any{
		"source":                  "remote_status_report",
		"ci_workflow_present":     repository.CIWorkflowPresent,
		"ci_workflow_run_seen":    repository.CIWorkflowRunSeen,
		"ci_run_passed":           repository.CIRunPassed,
		"ready":                   repository.Ready,
		"private_sdk_secret_seen": repository.PrivateSDKSecretPresent,
	}
	addRemoteRepositoryStringMetadata(metadata, "ci_run_conclusion", repository.CIRunConclusion)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_event", repository.CIRunEvent)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_error", repository.CIRunError)
	return metadata
}

func remoteStatusCheck(report RemoteStatusReport) gopact.VerificationCheck {
	organization := strings.TrimSpace(report.Organization)
	ready, notReady, blockingReasons, requiredActions := remoteStatusCounts(report.Repositories)
	status := gopact.VerificationStatusPassed
	if notReady > 0 {
		status = gopact.VerificationStatusFailed
	}
	return gopact.VerificationCheck{
		ID:       VerificationCheckRemoteRepositories + ":" + organization,
		Name:     "external repository readiness",
		Status:   status,
		Summary:  remoteStatusSummary(status, ready, notReady),
		Evidence: remoteStatusEvidence(organization, report.Repositories),
		Metadata: map[string]any{
			"organization":          organization,
			"repository_count":      len(report.Repositories),
			"ready_count":           ready,
			"not_ready_count":       notReady,
			"missing_count":         notReady,
			"blocking_reason_count": blockingReasons,
			"required_action_count": requiredActions,
		},
	}
}

func remoteStatusCounts(repositories []RemoteRepositoryStatus) (ready, notReady, blockingReasons, requiredActions int) {
	for _, repository := range repositories {
		if repository.Ready {
			ready++
		} else {
			notReady++
		}
		blockingReasons += len(repository.BlockingReasons)
		requiredActions += len(repository.RequiredActions)
	}
	return ready, notReady, blockingReasons, requiredActions
}

func remoteStatusSummary(status gopact.VerificationStatus, ready, notReady int) string {
	if status == gopact.VerificationStatusFailed {
		return fmt.Sprintf("external repositories not ready: %d ready, %d not ready", ready, notReady)
	}
	return fmt.Sprintf("external repositories ready: %d ready", ready)
}

func remoteStatusEvidence(organization string, repositories []RemoteRepositoryStatus) []gopact.VerificationEvidence {
	evidence := make([]gopact.VerificationEvidence, 0, len(repositories))
	for _, repository := range repositories {
		status := gopact.VerificationStatusPassed
		if !repository.Ready {
			status = gopact.VerificationStatusFailed
		}
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:     VerificationEvidenceTypeRemoteRepositoryReadiness,
			Ref:      "external-repository:" + remoteRepositoryRef(organization, repository),
			Summary:  remoteRepositorySummary(repository, status),
			Metadata: remoteRepositoryMetadata(organization, repository, status),
		})
	}
	return evidence
}

func remoteRepositorySummary(repository RemoteRepositoryStatus, status gopact.VerificationStatus) string {
	name := remoteRepositoryName(repository)
	if name == "" {
		name = strings.TrimSpace(repository.Remote)
	}
	return fmt.Sprintf("%s repository %s", name, status)
}

func remoteRepositoryMetadata(organization string, repository RemoteRepositoryStatus, status gopact.VerificationStatus) map[string]any {
	metadata := map[string]any{
		"organization":                     organization,
		"repository":                       remoteRepositoryName(repository),
		"remote":                           remoteRepositoryRef(organization, repository),
		"exists":                           repository.Exists,
		"private":                          repository.Private,
		"ci_workflow_present":              repository.CIWorkflowPresent,
		"ci_workflow_run_seen":             repository.CIWorkflowRunSeen,
		"ci_run_passed":                    repository.CIRunPassed,
		"private_sdk_token_secret_present": repository.PrivateSDKSecretPresent,
		"ready":                            repository.Ready,
		"status":                           string(status),
	}
	addRemoteRepositoryStringMetadata(metadata, "expected_visibility", repository.ExpectedVisibility)
	addRemoteRepositoryStringMetadata(metadata, "visibility", repository.Visibility)
	addRemoteRepositoryStringMetadata(metadata, "url", repository.URL)
	addRemoteRepositoryStringMetadata(metadata, "default_branch", repository.DefaultBranch)
	addRemoteRepositoryStringMetadata(metadata, "ci_workflow_path", repository.CIWorkflowPath)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_workflow_name", repository.CIRunWorkflowName)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_status", repository.CIRunStatus)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_conclusion", repository.CIRunConclusion)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_event", repository.CIRunEvent)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_head_branch", repository.CIRunHeadBranch)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_url", repository.CIRunURL)
	addRemoteRepositoryStringMetadata(metadata, "private_sdk_token_secret_name", repository.PrivateSDKSecretName)
	addRemoteRepositoryStringMetadata(metadata, "error", repository.Error)
	addRemoteRepositoryStringMetadata(metadata, "ci_workflow_error", repository.CIWorkflowError)
	addRemoteRepositoryStringMetadata(metadata, "ci_run_error", repository.CIRunError)
	addRemoteRepositoryStringMetadata(metadata, "private_sdk_token_secret_error", repository.PrivateSDKSecretError)
	if len(repository.BlockingReasons) > 0 {
		metadata["blocking_reasons"] = append([]string(nil), repository.BlockingReasons...)
	}
	if len(repository.RequiredActions) > 0 {
		metadata["required_actions"] = append([]string(nil), repository.RequiredActions...)
	}
	return metadata
}

func addRemoteRepositoryStringMetadata(metadata map[string]any, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		metadata[key] = value
	}
}

func remoteRepositoryRef(organization string, repository RemoteRepositoryStatus) string {
	remote := strings.TrimSpace(repository.Remote)
	if remote != "" {
		return remote
	}
	name := strings.TrimSpace(repository.Name)
	if name == "" {
		return ""
	}
	if strings.TrimSpace(organization) == "" {
		return name
	}
	return strings.TrimSpace(organization) + "/" + name
}

func remoteRepositoryName(repository RemoteRepositoryStatus) string {
	name := strings.TrimSpace(repository.Name)
	if name != "" {
		return name
	}
	remote := strings.TrimSpace(repository.Remote)
	if remote == "" {
		return ""
	}
	if slash := strings.LastIndex(remote, "/"); slash >= 0 && slash+1 < len(remote) {
		return remote[slash+1:]
	}
	return remote
}
