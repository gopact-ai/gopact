package extensionscaffold

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestRecordRemoteStatusCheckRecordsPassedCheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordRemoteStatusCheck(recorder, RemoteStatusReport{
		Organization: "gopact-ai",
		Repositories: []RemoteRepositoryStatus{
			{
				Name:                    "gopact-adapters-model",
				Remote:                  "gopact-ai/gopact-adapters-model",
				ExpectedVisibility:      "private",
				Visibility:              "PRIVATE",
				URL:                     "https://github.com/gopact-ai/gopact-adapters-model",
				DefaultBranch:           "main",
				CIWorkflowPath:          ".github/workflows/ci.yml",
				CIRunWorkflowName:       "ci",
				CIRunStatus:             "completed",
				CIRunConclusion:         "success",
				CIRunEvent:              "push",
				CIRunHeadBranch:         "main",
				CIRunURL:                "https://github.com/gopact-ai/gopact-adapters-model/actions/runs/123",
				PrivateSDKSecretName:    "GOPACT_GITHUB_TOKEN",
				Exists:                  true,
				Private:                 true,
				CIWorkflowPresent:       true,
				CIWorkflowRunSeen:       true,
				CIRunPassed:             true,
				PrivateSDKSecretPresent: true,
				Ready:                   true,
			},
		},
	}); err != nil {
		t.Fatalf("RecordRemoteStatusCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "external-repositories:gopact-ai" ||
		check.Name != "external repository readiness" ||
		check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed external repository readiness", check)
	}
	if check.Metadata["organization"] != "gopact-ai" ||
		check.Metadata["repository_count"] != 1 ||
		check.Metadata["ready_count"] != 1 ||
		check.Metadata["missing_count"] != 0 {
		t.Fatalf("metadata = %+v, want readiness counts", check.Metadata)
	}
	if len(check.Evidence) != 1 {
		t.Fatalf("evidence = %+v, want one evidence item per repository", check.Evidence)
	}
	evidence := check.Evidence[0]
	if evidence.Type != VerificationEvidenceTypeRemoteRepositoryReadiness ||
		evidence.Ref != "external-repository:gopact-ai/gopact-adapters-model" {
		t.Fatalf("evidence = %+v, want external repository evidence", evidence)
	}
	if evidence.Metadata["repository"] != "gopact-adapters-model" ||
		evidence.Metadata["remote"] != "gopact-ai/gopact-adapters-model" ||
		evidence.Metadata["ci_run_url"] != "https://github.com/gopact-ai/gopact-adapters-model/actions/runs/123" ||
		evidence.Metadata["ready"] != true ||
		evidence.Metadata["status"] != string(gopact.VerificationStatusPassed) {
		t.Fatalf("evidence metadata = %+v, want canonical repository readiness fields", evidence.Metadata)
	}
}

func TestRecordRemoteStatusCheckRecordsFailedCheckBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordRemoteStatusCheck(recorder, RemoteStatusReport{
		Organization: "gopact-ai",
		Repositories: []RemoteRepositoryStatus{
			{
				Name:                    "gopact-adapters-model",
				Remote:                  "gopact-ai/gopact-adapters-model",
				ExpectedVisibility:      "private",
				Visibility:              "PRIVATE",
				URL:                     "https://github.com/gopact-ai/gopact-adapters-model",
				DefaultBranch:           "main",
				CIWorkflowPath:          ".github/workflows/ci.yml",
				CIRunWorkflowName:       "ci",
				CIRunStatus:             "completed",
				CIRunConclusion:         "failure",
				CIRunEvent:              "push",
				CIRunHeadBranch:         "main",
				CIRunURL:                "https://github.com/gopact-ai/gopact-adapters-model/actions/runs/123",
				PrivateSDKSecretName:    "GOPACT_GITHUB_TOKEN",
				BlockingReasons:         []string{"GOPACT_GITHUB_TOKEN secret is missing", "latest ci workflow run did not pass"},
				RequiredActions:         []string{"configure GOPACT_GITHUB_TOKEN with sync-secrets.sh", "rerun ci workflow with rerun-ci.sh after fixing blockers"},
				Exists:                  true,
				Private:                 true,
				CIWorkflowPresent:       true,
				CIWorkflowRunSeen:       true,
				CIRunPassed:             false,
				PrivateSDKSecretPresent: false,
				Ready:                   false,
			},
		},
	})
	if !errors.Is(err, ErrRemoteStatusNotReady) {
		t.Fatalf("RecordRemoteStatusCheck() error = %v, want ErrRemoteStatusNotReady", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Status != gopact.VerificationStatusFailed {
		t.Fatalf("check status = %q, want failed", check.Status)
	}
	if check.Metadata["repository_count"] != 1 ||
		check.Metadata["ready_count"] != 0 ||
		check.Metadata["missing_count"] != 1 ||
		check.Metadata["blocking_reason_count"] != 2 ||
		check.Metadata["required_action_count"] != 2 {
		t.Fatalf("metadata = %+v, want missing repository counts", check.Metadata)
	}
	metadata := check.Evidence[0].Metadata
	if metadata["private_sdk_token_secret_present"] != false ||
		metadata["ci_run_passed"] != false ||
		metadata["status"] != string(gopact.VerificationStatusFailed) {
		t.Fatalf("evidence metadata = %+v, want failed repository status", metadata)
	}
	if got, want := metadata["blocking_reasons"], []string{"GOPACT_GITHUB_TOKEN secret is missing", "latest ci workflow run did not pass"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocking reasons = %#v, want %#v", got, want)
	}
	if got, want := metadata["required_actions"], []string{"configure GOPACT_GITHUB_TOKEN with sync-secrets.sh", "rerun ci workflow with rerun-ci.sh after fixing blockers"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required actions = %#v, want %#v", got, want)
	}
}

func TestRecordRemoteStatusCheckRejectsInvalidInputWithoutRecording(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordRemoteStatusCheck(nil, RemoteStatusReport{
		Organization: "gopact-ai",
		Repositories: []RemoteRepositoryStatus{{Name: "gopact-adapters-model", Remote: "gopact-ai/gopact-adapters-model", Ready: true}},
	}); err == nil {
		t.Fatal("RecordRemoteStatusCheck(nil) error = nil, want error")
	}
	if err := RecordRemoteStatusCheck(recorder, RemoteStatusReport{}); !errors.Is(err, ErrRemoteStatusRequired) {
		t.Fatalf("RecordRemoteStatusCheck(empty report) error = %v, want ErrRemoteStatusRequired", err)
	}
	if len(recorder.Checks()) != 0 {
		t.Fatalf("recorded checks = %+v, want none for invalid input", recorder.Checks())
	}
}
