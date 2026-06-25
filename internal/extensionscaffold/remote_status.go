package extensionscaffold

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const defaultGHCommand = "gh"

// RemoteStatusOptions configures GitHub remote repository status checks.
type RemoteStatusOptions struct {
	GHPath string
}

// RemoteStatusReport summarizes GitHub status for external scaffold repositories.
type RemoteStatusReport struct {
	Organization string                   `json:"organization"`
	ReadyCount   int                      `json:"ready_count"`
	MissingCount int                      `json:"missing_count"`
	Repositories []RemoteRepositoryStatus `json:"repositories"`
}

// Repository returns the status for one remote repository by name.
func (report RemoteStatusReport) Repository(name string) *RemoteRepositoryStatus {
	for i := range report.Repositories {
		if report.Repositories[i].Name == name {
			return &report.Repositories[i]
		}
	}
	return nil
}

// RemoteRepositoryStatus records the observed GitHub state of one external repository.
type RemoteRepositoryStatus struct {
	Name               string `json:"name"`
	Remote             string `json:"remote"`
	ExpectedVisibility string `json:"expected_visibility"`
	Visibility         string `json:"visibility,omitempty"`
	URL                string `json:"url,omitempty"`
	DefaultBranch      string `json:"default_branch,omitempty"`
	CIWorkflowPath     string `json:"ci_workflow_path,omitempty"`
	CIRunWorkflowName  string `json:"ci_run_workflow_name,omitempty"`
	CIRunStatus        string `json:"ci_run_status,omitempty"`
	CIRunConclusion    string `json:"ci_run_conclusion,omitempty"`
	CIRunEvent         string `json:"ci_run_event,omitempty"`
	CIRunHeadBranch    string `json:"ci_run_head_branch,omitempty"`
	CIRunURL           string `json:"ci_run_url,omitempty"`
	Error              string `json:"error,omitempty"`
	CIWorkflowError    string `json:"ci_workflow_error,omitempty"`
	CIRunError         string `json:"ci_run_error,omitempty"`
	Exists             bool   `json:"exists"`
	Private            bool   `json:"private"`
	CIWorkflowPresent  bool   `json:"ci_workflow_present"`
	CIWorkflowRunSeen  bool   `json:"ci_workflow_run_seen"`
	CIRunPassed        bool   `json:"ci_run_passed"`
	Ready              bool   `json:"ready"`
}

// CheckRemoteRepositories checks GitHub repository existence and CI workflow presence.
func CheckRemoteRepositories(ctx context.Context, root string, options RemoteStatusOptions) (RemoteStatusReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RemoteStatusReport{}, err
	}
	plan, err := RenderSyncPlanFromDesign(root)
	if err != nil {
		return RemoteStatusReport{}, err
	}

	ghPath := strings.TrimSpace(options.GHPath)
	if ghPath == "" {
		ghPath = defaultGHCommand
	}

	report := RemoteStatusReport{
		Organization: plan.Organization,
		Repositories: make([]RemoteRepositoryStatus, 0, len(plan.Repositories)),
	}
	for _, repo := range plan.Repositories {
		status := RemoteRepositoryStatus{
			Name:               repo.Name,
			Remote:             plan.Organization + "/" + repo.Name,
			ExpectedVisibility: repo.Visibility,
		}
		fillRepositoryView(ctx, ghPath, &status)
		if status.Exists {
			fillRepositoryWorkflow(ctx, ghPath, &status)
		}
		if status.CIWorkflowPresent {
			fillRepositoryWorkflowRun(ctx, ghPath, &status)
		}
		status.Ready = status.Exists &&
			status.CIWorkflowPresent &&
			status.CIRunPassed &&
			visibilityMatches(status.ExpectedVisibility, status.Private)
		if status.Ready {
			report.ReadyCount++
		} else {
			report.MissingCount++
		}
		report.Repositories = append(report.Repositories, status)
	}
	return report, nil
}

func fillRepositoryView(ctx context.Context, ghPath string, status *RemoteRepositoryStatus) {
	output, err := runGH(ctx, ghPath, "repo", "view", status.Remote, "--json", "name,visibility,isPrivate,url,defaultBranchRef")
	if err != nil {
		status.Error = commandError(err, output)
		return
	}
	var view struct {
		Name             string `json:"name"`
		Visibility       string `json:"visibility"`
		IsPrivate        bool   `json:"isPrivate"`
		URL              string `json:"url"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	}
	if err := json.Unmarshal(output, &view); err != nil {
		status.Error = err.Error()
		return
	}
	status.Exists = true
	status.Visibility = view.Visibility
	status.Private = view.IsPrivate
	status.URL = view.URL
	status.DefaultBranch = view.DefaultBranchRef.Name
}

func fillRepositoryWorkflow(ctx context.Context, ghPath string, status *RemoteRepositoryStatus) {
	output, err := runGH(ctx, ghPath, "api", "repos/"+status.Remote+"/contents/.github/workflows/ci.yml")
	if err != nil {
		status.CIWorkflowError = commandError(err, output)
		return
	}
	var workflow struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(output, &workflow); err != nil {
		status.CIWorkflowError = err.Error()
		return
	}
	if workflow.Path == ".github/workflows/ci.yml" {
		status.CIWorkflowPath = workflow.Path
		status.CIWorkflowPresent = true
		return
	}
	status.CIWorkflowError = fmt.Sprintf("unexpected workflow path %q", workflow.Path)
}

func fillRepositoryWorkflowRun(ctx context.Context, ghPath string, status *RemoteRepositoryStatus) {
	output, err := runGH(ctx, ghPath, "run", "list", "-R", status.Remote, "--limit", "1", "--json", "databaseId,status,conclusion,workflowName,event,headBranch,createdAt,url")
	if err != nil {
		status.CIRunError = commandError(err, output)
		return
	}
	var runs []struct {
		WorkflowName string `json:"workflowName"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		Event        string `json:"event"`
		HeadBranch   string `json:"headBranch"`
		URL          string `json:"url"`
	}
	if err := json.Unmarshal(output, &runs); err != nil {
		status.CIRunError = err.Error()
		return
	}
	if len(runs) == 0 {
		status.CIRunError = "no workflow runs observed"
		return
	}
	run := runs[0]
	status.CIWorkflowRunSeen = true
	status.CIRunWorkflowName = run.WorkflowName
	status.CIRunStatus = run.Status
	status.CIRunConclusion = run.Conclusion
	status.CIRunEvent = run.Event
	status.CIRunHeadBranch = run.HeadBranch
	status.CIRunURL = run.URL
	status.CIRunPassed = run.Status == "completed" && run.Conclusion == "success"
}

func runGH(ctx context.Context, ghPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, ghPath, args...)
	return cmd.CombinedOutput()
}

func commandError(err error, output []byte) string {
	outputText := strings.TrimSpace(string(output))
	if outputText == "" {
		return err.Error()
	}
	return err.Error() + ": " + outputText
}

func visibilityMatches(expected string, private bool) bool {
	switch strings.ToLower(strings.TrimSpace(expected)) {
	case "", "private":
		return private
	case "public":
		return !private
	default:
		return false
	}
}
