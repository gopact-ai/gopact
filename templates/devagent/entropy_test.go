package devagent

import (
	"strings"
	"testing"

	"github.com/gopact-ai/gopact"
)

func TestBuildEntropyAuditFlagsDependencyChangeWithoutDependencyVerification(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-1",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "add dependency",
			Diff:    "diff --git a/go.mod b/go.mod\n",
			Files: []PatchFile{
				{Path: "go.mod", Intent: "add dependency"},
			},
		},
		Report: verificationReport(t, gopact.VerificationStatusPassed),
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}
	if audit.Status != gopact.VerificationStatusPartial {
		t.Fatalf("audit.Status = %q, want partial", audit.Status)
	}
	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %+v, want one dependency finding", audit.Findings)
	}
	finding := audit.Findings[0]
	if finding.Category != gopact.EntropyDependency || finding.Severity != gopact.EntropySeverityMedium {
		t.Fatalf("finding = %+v, want medium dependency finding", finding)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0].Ref != "go.mod" {
		t.Fatalf("finding evidence = %+v, want go.mod evidence", finding.Evidence)
	}
}

func TestBuildEntropyAuditParsesDependencyChangeFromDiffOnlyPatch(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-diff-only",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-diff-only",
			Summary: "update module dependency",
			Diff: strings.Join([]string{
				"diff --git a/go.mod b/go.mod",
				"index 1111111..2222222 100644",
				"--- a/go.mod",
				"+++ b/go.mod",
				"@@ -1,3 +1,4 @@",
				" module github.com/gopact-ai/gopact",
				"+require example.com/new v1.2.3",
			}, "\n"),
		},
		Report: verificationReport(t, gopact.VerificationStatusPassed),
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}

	if audit.Status != gopact.VerificationStatusPartial {
		t.Fatalf("audit.Status = %q, want partial", audit.Status)
	}
	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %+v, want one dependency finding", audit.Findings)
	}
	finding := audit.Findings[0]
	if finding.Category != gopact.EntropyDependency || finding.Severity != gopact.EntropySeverityMedium {
		t.Fatalf("finding = %+v, want medium dependency finding", finding)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0].Ref != "go.mod" {
		t.Fatalf("finding evidence = %+v, want go.mod evidence parsed from diff", finding.Evidence)
	}
	if audit.Metadata["file_count"] != 1 {
		t.Fatalf("file_count = %#v, want 1", audit.Metadata["file_count"])
	}
}

func TestBuildEntropyAuditParsesRenamedPathFromDiffOnlyPatch(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-rename",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-rename",
			Summary: "move source file",
			Diff: strings.Join([]string{
				"diff --git a/old/runner.go b/runner.go",
				"similarity index 80%",
				"rename from old/runner.go",
				"rename to runner.go",
				"@@ -1 +1 @@",
				"-package old",
				"+package gopact",
			}, "\n"),
		},
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}

	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %+v, want stale docs finding", audit.Findings)
	}
	finding := audit.Findings[0]
	if finding.Category != gopact.EntropyStaleDocs || finding.Severity != gopact.EntropySeverityLow {
		t.Fatalf("finding = %+v, want low stale docs finding", finding)
	}
	if len(finding.Evidence) != 1 || finding.Evidence[0].Ref != "runner.go" {
		t.Fatalf("finding evidence = %+v, want renamed runner.go evidence", finding.Evidence)
	}
}

func TestBuildEntropyAuditSkipsDependencyFindingWhenDependencyVerificationExists(t *testing.T) {
	report := verificationReport(t, gopact.VerificationStatusPassed)
	report.Checks = append(report.Checks, gopact.VerificationCheck{
		ID:       "dependency-audit",
		Name:     "dependency audit",
		Status:   gopact.VerificationStatusPassed,
		Evidence: []gopact.VerificationEvidence{{Type: "dependency", Ref: "go mod tidy", Summary: "dependencies reviewed"}},
	})
	report.PassedCount++
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-1",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "add dependency",
			Diff:    "diff --git a/go.mod b/go.mod\n",
			Files: []PatchFile{
				{Path: "go.mod", Intent: "add dependency"},
			},
		},
		Report: report,
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}
	if len(audit.Findings) != 0 {
		t.Fatalf("findings = %+v, want none after dependency evidence", audit.Findings)
	}
	if audit.Status != gopact.VerificationStatusPassed {
		t.Fatalf("audit.Status = %q, want passed", audit.Status)
	}
}

func TestBuildEntropyAuditFlagsSensitiveFileAsHighSecurityEntropy(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-1",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "touch secrets",
			Diff:    "diff --git a/.env b/.env\n",
			Files: []PatchFile{
				{Path: ".env", Intent: "configure local secret"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}
	if audit.Status != gopact.VerificationStatusFailed {
		t.Fatalf("audit.Status = %q, want failed", audit.Status)
	}
	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %+v, want one security finding", audit.Findings)
	}
	finding := audit.Findings[0]
	if finding.Category != gopact.EntropySecurity || finding.Severity != gopact.EntropySeverityHigh {
		t.Fatalf("finding = %+v, want high security finding", finding)
	}
}

func TestBuildEntropyAuditFlagsSourceChangeWithoutDocs(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-1",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "change runtime behavior",
			Diff:    "diff --git a/runner.go b/runner.go\n",
			Files: []PatchFile{
				{Path: "runner.go", Intent: "change runtime behavior"},
				{Path: "runner_test.go", Intent: "cover runtime behavior"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}
	if len(audit.Findings) != 1 {
		t.Fatalf("findings = %+v, want stale docs finding", audit.Findings)
	}
	finding := audit.Findings[0]
	if finding.Category != gopact.EntropyStaleDocs || finding.Severity != gopact.EntropySeverityLow {
		t.Fatalf("finding = %+v, want low stale docs finding", finding)
	}
}

func TestBuildEntropyAuditPassesCleanDocsOnlyPatch(t *testing.T) {
	audit, err := BuildEntropyAudit(EntropyInput{
		ID:  "entropy-1",
		IDs: gopact.RuntimeIDs{RunID: "run-1"},
		Patch: PatchProposal{
			ID:      "patch-1",
			Summary: "update docs",
			Files: []PatchFile{
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildEntropyAudit() error = %v", err)
	}
	if audit.Status != gopact.VerificationStatusPassed {
		t.Fatalf("audit.Status = %q, want passed", audit.Status)
	}
	if len(audit.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", audit.Findings)
	}
}
