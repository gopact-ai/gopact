package devagent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// EntropyInput contains already-observed patch and verification context for entropy auditing.
type EntropyInput struct {
	ID        string                    `json:"id,omitempty"`
	IDs       gopact.RuntimeIDs         `json:"ids,omitempty"`
	Patch     PatchProposal             `json:"patch,omitempty"`
	Report    gopact.VerificationReport `json:"report,omitempty"`
	CreatedAt time.Time                 `json:"created_at,omitempty"`
	Metadata  map[string]any            `json:"metadata,omitempty"`
}

// BuildEntropyAudit builds an entropy audit from already-observed patch metadata.
func BuildEntropyAudit(input EntropyInput) (gopact.EntropyAudit, error) {
	if input.ID == "" {
		return gopact.EntropyAudit{}, errors.New("devagent: entropy audit id is required")
	}
	files := normalizedPatchFiles(input.Patch)
	findings := make([]gopact.EntropyFinding, 0, 3)
	findings = append(findings, securityFindings(input.ID, files)...)
	if dependencyFiles := filesByKind(files, isDependencyPath); len(dependencyFiles) > 0 && !hasDependencyVerification(input.Report) {
		findings = append(findings, entropyFinding(
			input.ID+":dependency-review",
			gopact.EntropyDependency,
			gopact.EntropySeverityMedium,
			"dependency files changed without dependency verification evidence",
			dependencyFiles,
		))
	}
	if sourceFiles := filesByKind(files, isSourcePath); len(sourceFiles) > 0 && !hasDocsPath(files) {
		findings = append(findings, entropyFinding(
			input.ID+":stale-docs",
			gopact.EntropyStaleDocs,
			gopact.EntropySeverityLow,
			"source files changed without documentation updates",
			sourceFiles,
		))
	}

	audit := gopact.EntropyAudit{
		ID:        input.ID,
		Status:    entropyAuditStatus(findings),
		IDs:       input.IDs,
		Findings:  findings,
		CreatedAt: input.CreatedAt,
		Metadata:  copyDevAgentMetadata(input.Metadata),
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now()
	}
	if audit.Metadata == nil {
		audit.Metadata = make(map[string]any)
	}
	audit.Metadata["source"] = "devagent"
	audit.Metadata["patch_id"] = input.Patch.ID
	audit.Metadata["file_count"] = len(files)
	audit.Metadata["finding_count"] = len(findings)
	if err := audit.Validate(); err != nil {
		return gopact.EntropyAudit{}, err
	}
	return audit, nil
}

func securityFindings(auditID string, files []PatchFile) []gopact.EntropyFinding {
	var findings []gopact.EntropyFinding
	for _, file := range files {
		if !isSensitivePath(file.Path) {
			continue
		}
		findings = append(findings, entropyFinding(
			auditID+":security:"+safeFindingID(file.Path),
			gopact.EntropySecurity,
			gopact.EntropySeverityHigh,
			fmt.Sprintf("sensitive file %q changed", file.Path),
			[]PatchFile{file},
		))
	}
	return findings
}

func entropyFinding(id string, category gopact.EntropyCategory, severity gopact.EntropySeverity, summary string, files []PatchFile) gopact.EntropyFinding {
	return gopact.EntropyFinding{
		ID:        id,
		Category:  category,
		Severity:  severity,
		Summary:   summary,
		Evidence:  entropyEvidence(files),
		CreatedAt: time.Now(),
	}
}

func entropyEvidence(files []PatchFile) []gopact.VerificationEvidence {
	evidence := make([]gopact.VerificationEvidence, 0, len(files))
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    "diff",
			Ref:     file.Path,
			Summary: file.Intent,
			Metadata: map[string]any{
				"patch_file": file.Path,
			},
		})
	}
	return evidence
}

func entropyAuditStatus(findings []gopact.EntropyFinding) gopact.VerificationStatus {
	if len(findings) == 0 {
		return gopact.VerificationStatusPassed
	}
	for _, finding := range findings {
		if compareEntropySeverity(finding.Severity, gopact.EntropySeverityHigh) >= 0 {
			return gopact.VerificationStatusFailed
		}
	}
	return gopact.VerificationStatusPartial
}

func normalizedPatchFiles(patch PatchProposal) []PatchFile {
	candidates := make([]PatchFile, 0, len(patch.Files)+4)
	candidates = append(candidates, patch.Files...)
	candidates = append(candidates, patchFilesFromDiff(patch.Diff)...)

	files := make([]PatchFile, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, file := range candidates {
		path := normalizePatchPath(file.Path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		file.Path = path
		file.Metadata = copyDevAgentMetadata(file.Metadata)
		files = append(files, file)
	}
	return files
}

func patchFilesFromDiff(diff string) []PatchFile {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	lines := strings.Split(diff, "\n")
	files := make([]PatchFile, 0, 4)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if file, ok := patchFileFromDiffGitHeader(line); ok {
				files = append(files, file)
			}
		case strings.HasPrefix(line, "+++ "):
			if file, ok := patchFileFromDiffPathLine(strings.TrimPrefix(line, "+++ ")); ok {
				files = append(files, file)
			}
		case strings.HasPrefix(line, "rename to "):
			if file, ok := patchFileFromDiffPathLine(strings.TrimPrefix(line, "rename to ")); ok {
				files = append(files, file)
			}
		}
	}
	return files
}

func patchFileFromDiffGitHeader(line string) (PatchFile, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return PatchFile{}, false
	}
	if file, ok := patchFileFromDiffPathLine(fields[3]); ok {
		return file, true
	}
	return patchFileFromDiffPathLine(fields[2])
}

func patchFileFromDiffPathLine(rawPath string) (PatchFile, bool) {
	path := normalizeDiffPath(rawPath)
	if path == "" {
		return PatchFile{}, false
	}
	return PatchFile{
		Path:   path,
		Intent: "observed diff",
	}, true
}

func filesByKind(files []PatchFile, keep func(string) bool) []PatchFile {
	out := make([]PatchFile, 0, len(files))
	for _, file := range files {
		if keep(file.Path) {
			out = append(out, file)
		}
	}
	return out
}

func hasDependencyVerification(report gopact.VerificationReport) bool {
	for _, check := range report.Checks {
		if check.Status != gopact.VerificationStatusPassed {
			continue
		}
		if strings.Contains(strings.ToLower(check.ID), "dependency") ||
			strings.Contains(strings.ToLower(check.Name), "dependency") {
			return true
		}
		for _, evidence := range check.Evidence {
			if strings.Contains(strings.ToLower(evidence.Type), "dependency") ||
				strings.Contains(strings.ToLower(evidence.Ref), "go mod") {
				return true
			}
		}
	}
	return false
}

func hasDocsPath(files []PatchFile) bool {
	for _, file := range files {
		if isDocsPath(file.Path) {
			return true
		}
	}
	return false
}

func isDependencyPath(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "requirements.txt":
		return true
	default:
		return false
	}
}

func isSensitivePath(path string) bool {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, ".env") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
		return true
	}
	switch filepath.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	default:
		return false
	}
}

func isSourcePath(path string) bool {
	lower := strings.ToLower(path)
	if isDocsPath(lower) || isDependencyPath(lower) || strings.HasSuffix(lower, "_test.go") {
		return false
	}
	switch filepath.Ext(lower) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".kt", ".rb":
		return true
	default:
		return false
	}
}

func isDocsPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "docs/") ||
		strings.HasSuffix(lower, ".md") ||
		strings.Contains(lower, "/docs/")
}

func normalizePatchPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return strings.TrimPrefix(path, "./")
}

func normalizeDiffPath(path string) string {
	path = strings.Trim(path, "\"")
	path = normalizePatchPath(path)
	if path == "/dev/null" || path == "dev/null" {
		return ""
	}
	return path
}

func safeFindingID(path string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ".", "-", "_", "-")
	id := strings.Trim(replacer.Replace(path), "-")
	if id == "" {
		return "file"
	}
	return id
}

func copyDevAgentMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
