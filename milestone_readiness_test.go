package gopact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMilestoneReadinessManifestCoversMStages(t *testing.T) {
	manifest := loadMilestoneReadinessManifest(t)

	if manifest.OverallStatus == "" {
		t.Fatal("milestone readiness overall_status is empty")
	}
	if manifest.OverallStatus == "complete" {
		for _, milestone := range manifest.Milestones {
			if milestone.Status != "complete" {
				t.Fatalf("overall_status is complete but milestone %q status = %q", milestone.ID, milestone.Status)
			}
		}
	}

	milestones := map[string]milestoneReadiness{}
	for _, milestone := range manifest.Milestones {
		if milestone.ID == "" {
			t.Fatal("milestone readiness id is empty")
		}
		if milestones[milestone.ID].ID != "" {
			t.Fatalf("milestone readiness %q is duplicated", milestone.ID)
		}
		if !isMilestoneReadinessStatus(milestone.Status) {
			t.Fatalf("milestone readiness %q has invalid status %q", milestone.ID, milestone.Status)
		}
		if strings.TrimSpace(milestone.Objective) == "" {
			t.Fatalf("milestone readiness %q objective is empty", milestone.ID)
		}
		if len(milestone.EvidenceDocs) == 0 {
			t.Fatalf("milestone readiness %q evidence_docs is empty", milestone.ID)
		}
		for _, path := range milestone.EvidenceDocs {
			if _, err := os.Stat(filepath.Clean(path)); err != nil {
				t.Fatalf("milestone readiness %q evidence doc %q: %v", milestone.ID, path, err)
			}
		}
		if milestone.Status == "complete" && len(milestone.OpenItems) != 0 {
			t.Fatalf("milestone readiness %q is complete but has open_items", milestone.ID)
		}
		if milestone.Status != "complete" && len(milestone.OpenItems) == 0 {
			t.Fatalf("milestone readiness %q is not complete but open_items is empty", milestone.ID)
		}
		milestones[milestone.ID] = milestone
	}

	for _, id := range []string{"M1", "M2", "M3", "M4", "M5", "M6"} {
		if milestones[id].ID == "" {
			t.Fatalf("milestone readiness missing %q", id)
		}
	}
	if manifest.OverallStatus != "complete" {
		hasOpenMilestone := false
		for _, milestone := range manifest.Milestones {
			if milestone.Status != "complete" {
				hasOpenMilestone = true
				break
			}
		}
		if !hasOpenMilestone {
			t.Fatal("overall_status is not complete but all milestones are complete")
		}
	}
}

func TestMilestoneReadinessDefinesBootstrapLevels(t *testing.T) {
	manifest := loadMilestoneReadinessManifest(t)

	levels := map[string]bootstrapLevel{}
	for _, level := range manifest.BootstrapLevels {
		if level.Level == "" {
			t.Fatal("bootstrap level is empty")
		}
		if levels[level.Level].Level != "" {
			t.Fatalf("bootstrap level %q is duplicated", level.Level)
		}
		if len(level.AllowedModes) == 0 {
			t.Fatalf("bootstrap level %q allowed_modes is empty", level.Level)
		}
		if len(level.RequiredMilestones) == 0 {
			t.Fatalf("bootstrap level %q required_milestones is empty", level.Level)
		}
		if len(level.RequiredGates) == 0 {
			t.Fatalf("bootstrap level %q required_gates is empty", level.Level)
		}
		levels[level.Level] = level
	}

	for _, level := range []string{"level-1", "level-2", "level-3"} {
		if levels[level].Level == "" {
			t.Fatalf("bootstrap levels missing %q", level)
		}
	}
	if !slices.ContainsFunc(manifest.BootstrapLevels, func(level bootstrapLevel) bool {
		return level.Level == manifest.CurrentBootstrapLevel
	}) {
		t.Fatalf("current_bootstrap_level %q is not declared", manifest.CurrentBootstrapLevel)
	}
}

func TestMilestoneReadinessManifestIsIndexed(t *testing.T) {
	for _, path := range []string{
		"README.md",
		filepath.Join("docs", "design", "index.md"),
		filepath.Join("docs", "design", "development-plan.md"),
	} {
		content := readTextFile(t, path)
		if !strings.Contains(content, "milestone-readiness.json") {
			t.Fatalf("%s does not reference milestone-readiness.json", path)
		}
	}
}

func TestMilestoneReadinessRecordsM6RemoteCIEvidence(t *testing.T) {
	manifest := loadMilestoneReadinessManifest(t)
	coreGates := loadCoreCIGatesManifest(t)
	m6, ok := findMilestoneReadiness(manifest, "M6")
	if !ok {
		t.Fatal("milestone readiness missing M6")
	}

	var coreCI milestoneEvidenceRecord
	var externalBlocker milestoneEvidenceRecord
	for _, record := range m6.EvidenceRecords {
		switch {
		case record.Type == "github_actions_run" && record.Scope == "core-ci":
			coreCI = record
		case record.Type == "remote_extension_ci_blocker" && record.Scope == "external-repositories":
			externalBlocker = record
		}
	}
	if coreCI.Type == "" {
		t.Fatal("M6 missing core GitHub Actions evidence record")
	}
	if coreCI.Repository != "gopact-ai/gopact" ||
		coreCI.Workflow != "ci" ||
		coreCI.RunID <= 0 ||
		coreCI.HeadBranch != "main" ||
		strings.TrimSpace(coreCI.HeadSHA) == "" ||
		coreCI.Conclusion != "success" ||
		strings.TrimSpace(coreCI.ObservedAt) == "" {
		t.Fatalf("M6 core CI evidence = %+v, want successful gopact-ai/gopact ci run", coreCI)
	}
	if !slices.Equal(coreCI.RequiredGates, coreGates.RequiredGates) {
		t.Fatalf("M6 core CI required_gates = %v, want core-ci-gates required_gates %v", coreCI.RequiredGates, coreGates.RequiredGates)
	}
	if externalBlocker.Type == "" {
		t.Fatal("M6 missing external repository CI blocker evidence record")
	}
	if externalBlocker.Conclusion != "blocked" ||
		!strings.Contains(externalBlocker.Notes, "GOPACT_GITHUB_TOKEN") {
		t.Fatalf("M6 external blocker evidence = %+v, want GOPACT_GITHUB_TOKEN blocker", externalBlocker)
	}
	for _, item := range m6.OpenItems {
		if strings.Contains(item, "M6 release 前仍必须在 GitHub CI") {
			t.Fatalf("M6 open item still contains stale core GitHub CI blocker: %q", item)
		}
	}
}

func TestMilestoneReadinessM5OpenItemsTrackOnlyRemainingWork(t *testing.T) {
	manifest := loadMilestoneReadinessManifest(t)
	m5, ok := findMilestoneReadiness(manifest, "M5")
	if !ok {
		t.Fatal("milestone readiness missing M5")
	}

	for _, item := range m5.OpenItems {
		if strings.Contains(item, "已覆盖：") {
			t.Fatalf("M5 open item describes completed work instead of remaining work: %q", item)
		}
	}
}

type milestoneReadinessManifest struct {
	Version               int                  `json:"version"`
	OverallStatus         string               `json:"overall_status"`
	CurrentBootstrapLevel string               `json:"current_bootstrap_level"`
	Milestones            []milestoneReadiness `json:"milestones"`
	BootstrapLevels       []bootstrapLevel     `json:"bootstrap_levels"`
}

type milestoneReadiness struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Status          string                    `json:"status"`
	Objective       string                    `json:"objective"`
	EvidenceDocs    []string                  `json:"evidence_docs"`
	EvidenceRecords []milestoneEvidenceRecord `json:"evidence_records"`
	OpenItems       []string                  `json:"open_items"`
}

type milestoneEvidenceRecord struct {
	Type          string   `json:"type"`
	Scope         string   `json:"scope"`
	Repository    string   `json:"repository"`
	Workflow      string   `json:"workflow"`
	RunID         int64    `json:"run_id"`
	HeadBranch    string   `json:"head_branch"`
	HeadSHA       string   `json:"head_sha"`
	Conclusion    string   `json:"conclusion"`
	ObservedAt    string   `json:"observed_at"`
	RequiredGates []string `json:"required_gates"`
	Notes         string   `json:"notes"`
}

type bootstrapLevel struct {
	Level              string   `json:"level"`
	AllowedModes       []string `json:"allowed_modes"`
	RequiredMilestones []string `json:"required_milestones"`
	RequiredGates      []string `json:"required_gates"`
}

func loadMilestoneReadinessManifest(t *testing.T) milestoneReadinessManifest {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "milestone-readiness.json"))
	if err != nil {
		t.Fatalf("read milestone readiness manifest: %v", err)
	}
	var manifest milestoneReadinessManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode milestone readiness manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("milestone readiness manifest version = %d, want 1", manifest.Version)
	}
	return manifest
}

func isMilestoneReadinessStatus(status string) bool {
	switch status {
	case "complete", "first-slice-complete", "partial", "in-progress":
		return true
	default:
		return false
	}
}

func findMilestoneReadiness(manifest milestoneReadinessManifest, id string) (milestoneReadiness, bool) {
	for _, milestone := range manifest.Milestones {
		if milestone.ID == id {
			return milestone, true
		}
	}
	return milestoneReadiness{}, false
}
