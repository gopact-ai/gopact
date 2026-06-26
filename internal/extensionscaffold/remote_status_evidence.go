package extensionscaffold

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gopact-ai/gopact"
)

var (
	ErrRemoteStatusNotReady = errors.New("extensionscaffold: remote repositories are not ready")
	ErrRemoteStatusRequired = errors.New("extensionscaffold: remote status report is required")
)

const (
	// VerificationCheckRemoteRepositories is the standard check ID prefix for external repository readiness.
	VerificationCheckRemoteRepositories = "external-repositories"

	// VerificationEvidenceTypeRemoteRepositoryReadiness is the evidence type for one external repository readiness observation.
	VerificationEvidenceTypeRemoteRepositoryReadiness = "external_repository_readiness"
)

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

func remoteStatusCheck(report RemoteStatusReport) gopact.VerificationCheck {
	organization := strings.TrimSpace(report.Organization)
	ready, missing, blockingReasons, requiredActions := remoteStatusCounts(report.Repositories)
	status := gopact.VerificationStatusPassed
	if missing > 0 {
		status = gopact.VerificationStatusFailed
	}
	return gopact.VerificationCheck{
		ID:       VerificationCheckRemoteRepositories + ":" + organization,
		Name:     "external repository readiness",
		Status:   status,
		Summary:  remoteStatusSummary(status, ready, missing),
		Evidence: remoteStatusEvidence(organization, report.Repositories),
		Metadata: map[string]any{
			"organization":          organization,
			"repository_count":      len(report.Repositories),
			"ready_count":           ready,
			"missing_count":         missing,
			"blocking_reason_count": blockingReasons,
			"required_action_count": requiredActions,
		},
	}
}

func remoteStatusCounts(repositories []RemoteRepositoryStatus) (ready, missing, blockingReasons, requiredActions int) {
	for _, repository := range repositories {
		if repository.Ready {
			ready++
		} else {
			missing++
		}
		blockingReasons += len(repository.BlockingReasons)
		requiredActions += len(repository.RequiredActions)
	}
	return ready, missing, blockingReasons, requiredActions
}

func remoteStatusSummary(status gopact.VerificationStatus, ready, missing int) string {
	if status == gopact.VerificationStatusFailed {
		return fmt.Sprintf("external repositories not ready: %d ready, %d missing", ready, missing)
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
