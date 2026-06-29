package gopact_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestV1MigrationReleaseGateChecksDriveSDKVerificationRequirements(t *testing.T) {
	plan := loadV1ReleaseGatePlan(t)

	for _, check := range plan.ReleaseGateChecks {
		t.Run(check.ID, func(t *testing.T) {
			_, report := v1SyntheticReleaseGateReport(t, check)
			requirement := []gopacttest.VerificationEvidenceRequirement{
				{
					Name:                  check.ID,
					RequiredCheckIDs:      check.RequiredCheckIDs,
					RequiredEvidenceTypes: check.EvidenceTypes,
					RequiredCIGates:       check.RequiredCIGates,
				},
			}
			gopacttest.RequireVerificationEvidenceRequirements(t, report, requirement)
		})
	}
}

type v1ReleaseGatePlan struct {
	ReleaseGateChecks []v1ReleaseGateCheck `json:"release_gate_checks"`
}

type v1ReleaseGateCheck struct {
	ID               string   `json:"id"`
	EvidenceTypes    []string `json:"evidence_types"`
	RequiredCheckIDs []string `json:"required_check_ids"`
	RequiredCIGates  []string `json:"required_ci_gates,omitempty"`
}

func loadV1ReleaseGatePlan(t *testing.T) v1ReleaseGatePlan {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "v1-migration-plan.json"))
	if err != nil {
		t.Fatalf("read v1 migration plan: %v", err)
	}
	var plan v1ReleaseGatePlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode v1 migration plan: %v", err)
	}
	if len(plan.ReleaseGateChecks) == 0 {
		t.Fatal("v1 migration plan release_gate_checks is empty")
	}
	return plan
}

func v1SyntheticReleaseGateReport(t *testing.T, check v1ReleaseGateCheck) (gopact.RunExport, gopact.VerificationReport) {
	t.Helper()

	createdAt := time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)
	export := gopact.RunExport{
		Version:   gopact.RunExportVersion,
		IDs:       gopact.RuntimeIDs{RunID: "run-v1-" + v1GateIDSegment(check.ID), ThreadID: "thread-v1"},
		Outcome:   gopact.RunCompleted,
		CreatedAt: createdAt,
	}
	recorder := gopact.NewVerificationRecorder()
	for _, checkID := range check.RequiredCheckIDs {
		if err := recorder.Record(v1SyntheticVerificationCheck(t, checkID, check)); err != nil {
			t.Fatalf("Record(%s) error = %v", checkID, err)
		}
	}
	report, err := recorder.Report(export)
	if err != nil {
		t.Fatalf("Report(%s) error = %v", check.ID, err)
	}
	export.VerificationReports = []gopact.VerificationReport{report}
	return export, report
}

func v1SyntheticVerificationCheck(t *testing.T, id string, gate v1ReleaseGateCheck) gopact.VerificationCheck {
	t.Helper()

	if id == "external-ci:gopact-ai" {
		return v1SyntheticExternalCIRunSetCheck(t, id, gate)
	}
	evidence := v1SyntheticEvidenceForCheck(id, gate)
	return gopact.VerificationCheck{
		ID:       id,
		Name:     gate.ID,
		Status:   gopact.VerificationStatusPassed,
		Summary:  "v1 release gate evidence observed",
		Evidence: evidence,
	}
}

func v1SyntheticExternalCIRunSetCheck(t *testing.T, id string, gate v1ReleaseGateCheck) gopact.VerificationCheck {
	t.Helper()

	recorder := gopact.NewVerificationRecorder()
	err := gopacttest.RecordCIRunSetCheck(recorder, gopacttest.CIRunSet{
		ID:   id,
		Name: "external repository CI",
		RequiredRepositories: []string{
			"gopact-ai/gopact-adapters-model",
			"gopact-ai/gopact-templates-react",
		},
		RequiredGates: gate.RequiredCIGates,
		Runs: []gopacttest.CIRun{
			v1SyntheticExternalCIRun("gopact-ai/gopact-adapters-model", "1001", gate.RequiredCIGates),
			v1SyntheticExternalCIRun("gopact-ai/gopact-templates-react", "1002", gate.RequiredCIGates),
		},
		Metadata: map[string]any{"v1_release_gate_check": gate.ID},
	})
	if err != nil {
		t.Fatalf("RecordCIRunSetCheck(%s) error = %v", id, err)
	}
	checks := recorder.Checks()
	if len(checks) != 1 {
		t.Fatalf("RecordCIRunSetCheck(%s) recorded %d checks, want 1", id, len(checks))
	}
	return checks[0]
}

func v1SyntheticExternalCIRun(repository, runID string, requiredGates []string) gopacttest.CIRun {
	gates := make([]gopacttest.CIRunGate, 0, len(requiredGates))
	for _, gate := range requiredGates {
		gates = append(gates, gopacttest.CIRunGate{
			Gate:   gate,
			Status: gopact.VerificationStatusPassed,
			Job:    "test",
			Step:   v1SyntheticExternalCIStep(gate),
		})
	}
	return gopacttest.CIRun{
		Provider:   "github-actions",
		Repository: repository,
		Workflow:   "ci",
		RunID:      runID,
		Status:     "completed",
		Conclusion: "success",
		Gates:      gates,
	}
}

func v1SyntheticExternalCIStep(gate string) string {
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

func v1SyntheticEvidenceForCheck(id string, gate v1ReleaseGateCheck) []gopact.VerificationEvidence {
	var evidence []gopact.VerificationEvidence
	switch {
	case id == "ci-gates":
		for _, ciGate := range gate.RequiredCIGates {
			evidence = append(evidence, gopact.VerificationEvidence{
				Type:    gopacttest.VerificationEvidenceTypeCIGate,
				Ref:     "ci-gate:" + ciGate,
				Summary: ciGate + " gate passed",
				Metadata: map[string]any{
					"gate":   ciGate,
					"status": string(gopact.VerificationStatusPassed),
				},
			})
		}
	case strings.HasPrefix(id, "command:"):
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    gopacttest.VerificationEvidenceTypeCommand,
			Ref:     strings.TrimPrefix(id, "command:"),
			Summary: "command passed",
		})
	case strings.HasPrefix(id, "file-snapshot:"):
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    "file_snapshot",
			Ref:     strings.TrimPrefix(id, "file-snapshot:"),
			Summary: "file snapshot captured",
		})
	default:
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    v1FallbackEvidenceType(gate),
			Ref:     id,
			Summary: "gate evidence captured",
		})
	}
	for _, evidenceType := range gate.EvidenceTypes {
		if !v1EvidenceContainsType(evidence, evidenceType) {
			evidence = append(evidence, gopact.VerificationEvidence{
				Type:    evidenceType,
				Ref:     "v1-gate:" + gate.ID + ":" + evidenceType,
				Summary: "gate evidence captured",
			})
		}
	}
	return evidence
}

func v1FallbackEvidenceType(gate v1ReleaseGateCheck) string {
	for _, evidenceType := range gate.EvidenceTypes {
		if evidenceType != "" {
			return evidenceType
		}
	}
	return "requirement"
}

func v1EvidenceContainsType(evidence []gopact.VerificationEvidence, evidenceType string) bool {
	for _, item := range evidence {
		if item.Type == evidenceType {
			return true
		}
	}
	return false
}

func v1GateIDSegment(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, ":", "-")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, " ", "-")
	if id == "" {
		return "release-gate"
	}
	return id
}
