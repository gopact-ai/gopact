package gopacttest

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
)

func TestParseGitHubActionsCIRunMapsObservedRunJobStepsToCIGates(t *testing.T) {
	const runJSON = `{
		"databaseId": 28257255825,
		"name": "feat: expose trace redaction boundary",
		"workflowName": "ci",
		"status": "completed",
		"conclusion": "success",
		"url": "https://github.com/gopact-ai/gopact/actions/runs/28257255825",
		"headSha": "4eb441c7b9e8",
		"headBranch": "main",
		"jobs": [
			{
				"name": "test",
				"status": "completed",
				"conclusion": "success",
				"url": "https://github.com/gopact-ai/gopact/actions/runs/28257255825/job/83723359815",
				"startedAt": "2026-06-26T18:23:35Z",
				"completedAt": "2026-06-26T18:29:09Z",
				"steps": [
					{
						"name": "Check whitespace",
						"status": "completed",
						"conclusion": "success",
						"startedAt": "2026-06-26T18:23:40Z",
						"completedAt": "2026-06-26T18:23:41Z"
					},
					{"name": "Test", "status": "completed", "conclusion": "success"},
					{"name": "Security", "status": "completed", "conclusion": "success"}
				]
			}
		]
	}`

	run, err := ParseGitHubActionsCIRun(GitHubActionsCIRunInput{
		Repository:    "gopact-ai/gopact",
		RunJSON:       []byte(runJSON),
		RequiredGates: []string{"whitespace", "unit", "security"},
		GateNames: map[string]string{
			"Check whitespace": "whitespace",
			"Test":             "unit",
			"Security":         "security",
		},
		Metadata: map[string]any{"profile": "release"},
	})
	if err != nil {
		t.Fatalf("ParseGitHubActionsCIRun() error = %v", err)
	}

	if run.Provider != "github-actions" ||
		run.Repository != "gopact-ai/gopact" ||
		run.Workflow != "ci" ||
		run.RunID != "28257255825" ||
		run.URL != "https://github.com/gopact-ai/gopact/actions/runs/28257255825" ||
		run.HeadSHA != "4eb441c7b9e8" ||
		run.HeadBranch != "main" ||
		run.Status != "completed" ||
		run.Conclusion != "success" ||
		run.Metadata["profile"] != "release" {
		t.Fatalf("run = %+v, want GitHub Actions run identity and metadata", run)
	}
	if !reflect.DeepEqual(run.RequiredGates, []string{"whitespace", "unit", "security"}) {
		t.Fatalf("required gates = %#v, want copied required gates", run.RequiredGates)
	}
	if len(run.Gates) != 3 {
		t.Fatalf("gates = %+v, want mapped workflow steps only", run.Gates)
	}

	whitespace := run.Gates[0]
	if whitespace.Gate != "whitespace" ||
		whitespace.Status != gopact.VerificationStatusPassed ||
		whitespace.Job != "test" ||
		whitespace.Step != "Check whitespace" ||
		whitespace.URL != "https://github.com/gopact-ai/gopact/actions/runs/28257255825/job/83723359815" ||
		whitespace.StartedAt.Format(time.RFC3339) != "2026-06-26T18:23:40Z" ||
		whitespace.CompletedAt.Format(time.RFC3339) != "2026-06-26T18:23:41Z" ||
		whitespace.Duration != time.Second {
		t.Fatalf("first gate = %+v, want whitespace step evidence", whitespace)
	}

	recorder := gopact.NewVerificationRecorder()
	if err := RecordCIRunCheck(recorder, run); err != nil {
		t.Fatalf("RecordCIRunCheck() error = %v", err)
	}
	check := recorder.Checks()[0]
	if check.Status != gopact.VerificationStatusPassed ||
		check.Metadata["ci_provider"] != "github-actions" ||
		check.Metadata["run_id"] != "28257255825" ||
		check.Evidence[0].Metadata["gate"] != "whitespace" ||
		check.Evidence[0].Metadata["step"] != "Check whitespace" {
		t.Fatalf("recorded check = %+v, want GitHub Actions CI gate evidence", check)
	}
}

func TestParseGitHubActionsCIRunMapsFailuresAndSkips(t *testing.T) {
	const runJSON = `{
		"id": 99,
		"workflow_name": "ci",
		"status": "completed",
		"conclusion": "failure",
		"jobs": [
			{
				"name": "test",
				"url": "https://github.com/gopact-ai/gopact/actions/runs/99/job/1",
				"steps": [
					{"name": "Lint", "status": "completed", "conclusion": "failure"},
					{"name": "Coverage", "status": "completed", "conclusion": "skipped"}
				]
			}
		]
	}`

	run, err := ParseGitHubActionsCIRun(GitHubActionsCIRunInput{
		Repository:    "gopact-ai/gopact",
		RunJSON:       []byte(runJSON),
		RequiredGates: []string{"lint", "coverage"},
		GateNames: map[string]string{
			"Lint":     "lint",
			"Coverage": "coverage",
		},
	})
	if err != nil {
		t.Fatalf("ParseGitHubActionsCIRun() error = %v", err)
	}
	if len(run.Gates) != 2 {
		t.Fatalf("gates = %+v, want lint and coverage gates", run.Gates)
	}
	if run.Gates[0].Status != gopact.VerificationStatusFailed ||
		run.Gates[1].Status != gopact.VerificationStatusSkipped {
		t.Fatalf("gates = %+v, want failed lint and skipped coverage", run.Gates)
	}

	recorder := gopact.NewVerificationRecorder()
	err = RecordCIRunCheck(recorder, run)
	if !errors.Is(err, ErrCIGateFailed) {
		t.Fatalf("RecordCIRunCheck() error = %v, want ErrCIGateFailed", err)
	}
	check := recorder.Checks()[0]
	if check.Metadata["failed_gate_count"] != 1 || check.Metadata["skipped_gate_count"] != 1 {
		t.Fatalf("metadata = %+v, want failed and skipped counts", check.Metadata)
	}
}

func TestParseGitHubActionsCIRunRejectsMissingObservedData(t *testing.T) {
	_, err := ParseGitHubActionsCIRun(GitHubActionsCIRunInput{})
	if !errors.Is(err, ErrGitHubActionsCIRunRequired) {
		t.Fatalf("ParseGitHubActionsCIRun() error = %v, want ErrGitHubActionsCIRunRequired", err)
	}

	_, err = ParseGitHubActionsCIRun(GitHubActionsCIRunInput{
		RunJSON: []byte(`{"databaseId": 1, "workflowName": "ci"}`),
		GateNames: map[string]string{
			"Test": "unit",
		},
	})
	if !errors.Is(err, ErrCIGateRequired) {
		t.Fatalf("ParseGitHubActionsCIRun() error = %v, want ErrCIGateRequired", err)
	}
}
