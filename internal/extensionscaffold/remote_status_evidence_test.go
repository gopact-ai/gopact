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
		check.Metadata["not_ready_count"] != 0 ||
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

func TestRecordRemoteCIRunSetCheckRecordsPassedExternalCICheck(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	if err := RecordRemoteCIRunSetCheck(recorder, RemoteStatusReport{
		Organization: "gopact-ai",
		Repositories: []RemoteRepositoryStatus{
			{
				Name:                    "gopact-adapters-model",
				Remote:                  "gopact-ai/gopact-adapters-model",
				CIWorkflowPath:          ".github/workflows/ci.yml",
				CIRunWorkflowName:       "ci",
				CIRunID:                 "123",
				CIRunStatus:             "completed",
				CIRunConclusion:         "success",
				CIRunEvent:              "push",
				CIRunHeadBranch:         "main",
				CIRunURL:                "https://github.com/gopact-ai/gopact-adapters-model/actions/runs/123",
				Exists:                  true,
				CIWorkflowPresent:       true,
				CIWorkflowRunSeen:       true,
				CIRunPassed:             true,
				PrivateSDKSecretPresent: true,
				Ready:                   true,
			},
			{
				Name:                    "gopact-templates-react",
				Remote:                  "gopact-ai/gopact-templates-react",
				CIWorkflowPath:          ".github/workflows/ci.yml",
				CIRunWorkflowName:       "ci",
				CIRunID:                 "456",
				CIRunStatus:             "completed",
				CIRunConclusion:         "success",
				CIRunEvent:              "push",
				CIRunHeadBranch:         "main",
				CIRunURL:                "https://github.com/gopact-ai/gopact-templates-react/actions/runs/456",
				Exists:                  true,
				CIWorkflowPresent:       true,
				CIWorkflowRunSeen:       true,
				CIRunPassed:             true,
				PrivateSDKSecretPresent: true,
				Ready:                   true,
			},
		},
	}, []string{"whitespace", "unit", "vet"}); err != nil {
		t.Fatalf("RecordRemoteCIRunSetCheck() error = %v", err)
	}

	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "external-ci:gopact-ai" ||
		check.Name != "external repository CI" ||
		check.Status != gopact.VerificationStatusPassed {
		t.Fatalf("check = %+v, want passed external CI check", check)
	}
	if check.Metadata["run_count"] != 2 ||
		check.Metadata["repository_count"] != 2 ||
		check.Metadata["gate_count"] != 6 ||
		check.Metadata["passed_gate_count"] != 6 {
		t.Fatalf("metadata = %+v, want external CI run set counts", check.Metadata)
	}
	if got, want := check.Metadata["required_gates"], []string{"whitespace", "unit", "vet"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required gates = %#v, want %#v", got, want)
	}
	if len(check.Evidence) != 6 {
		t.Fatalf("evidence count = %d, want one evidence item per repo gate", len(check.Evidence))
	}
	evidence := check.Evidence[0]
	if evidence.Type != "ci_gate" ||
		evidence.Ref != "ci-gate:gopact-ai/gopact-adapters-model:whitespace" ||
		evidence.Metadata["repository"] != "gopact-ai/gopact-adapters-model" ||
		evidence.Metadata["run_id"] != "123" ||
		evidence.Metadata["gate"] != "whitespace" ||
		evidence.Metadata["status"] != string(gopact.VerificationStatusPassed) {
		t.Fatalf("first evidence = %+v, want repository-qualified external CI gate evidence", evidence)
	}
}

func TestRecordRemoteCIRunSetCheckRecordsFailedExternalCIBeforeReturningError(t *testing.T) {
	recorder := gopact.NewVerificationRecorder()

	err := RecordRemoteCIRunSetCheck(recorder, RemoteStatusReport{
		Organization: "gopact-ai",
		Repositories: []RemoteRepositoryStatus{
			{
				Name:                 "gopact-adapters-model",
				Remote:               "gopact-ai/gopact-adapters-model",
				CIRunWorkflowName:    "ci",
				CIRunID:              "123",
				CIRunStatus:          "completed",
				CIRunConclusion:      "failure",
				CIWorkflowRunSeen:    true,
				CIWorkflowPresent:    true,
				PrivateSDKSecretName: "GOPACT_GITHUB_TOKEN",
			},
		},
	}, []string{"whitespace", "unit", "vet"})
	if !errors.Is(err, ErrRemoteCINotReady) {
		t.Fatalf("RecordRemoteCIRunSetCheck() error = %v, want ErrRemoteCINotReady", err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("check count = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.ID != "external-ci:gopact-ai" ||
		check.Status != gopact.VerificationStatusFailed ||
		check.Metadata["failed_gate_count"] != 3 {
		t.Fatalf("check = %+v, want failed external CI check", check)
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
	if check.Summary != "external repositories not ready: 0 ready, 1 not ready" {
		t.Fatalf("check summary = %q, want not ready count", check.Summary)
	}
	if check.Metadata["repository_count"] != 1 ||
		check.Metadata["ready_count"] != 0 ||
		check.Metadata["not_ready_count"] != 1 ||
		check.Metadata["missing_count"] != 1 ||
		check.Metadata["blocking_reason_count"] != 2 ||
		check.Metadata["required_action_count"] != 2 {
		t.Fatalf("metadata = %+v, want not ready repository counts", check.Metadata)
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
