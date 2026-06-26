package gopacttest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gopact-ai/gopact"
)

// ErrGitHubActionsCIRunRequired reports missing or invalid observed GitHub Actions data.
var ErrGitHubActionsCIRunRequired = errors.New("gopacttest: github actions ci run is required")

// GitHubActionsCIRunInput is already-observed GitHub Actions run data.
//
// The helper only parses caller-supplied JSON. It does not call GitHub, read gh
// CLI state, or load tokens/configuration.
type GitHubActionsCIRunInput struct {
	Repository    string
	RunJSON       []byte
	RequiredGates []string
	GateNames     map[string]string
	Metadata      map[string]any
}

// ParseGitHubActionsCIRun converts observed GitHub Actions run/job/step JSON
// into the provider-neutral CIRun verification input.
func ParseGitHubActionsCIRun(input GitHubActionsCIRunInput) (CIRun, error) {
	if len(bytes.TrimSpace(input.RunJSON)) == 0 {
		return CIRun{}, ErrGitHubActionsCIRunRequired
	}

	var raw githubActionsRawRun
	if err := decodeGitHubActionsJSON(input.RunJSON, &raw); err != nil {
		return CIRun{}, fmt.Errorf("%w: decode run json: %w", ErrGitHubActionsCIRunRequired, err)
	}
	run := CIRun{
		Name:          strings.TrimSpace(raw.Name),
		Provider:      "github-actions",
		Repository:    strings.TrimSpace(input.Repository),
		Workflow:      firstGitHubActionsString(raw.WorkflowName, raw.WorkflowNameSnake, raw.Workflow),
		RunID:         firstGitHubActionsString(raw.DatabaseID.String(), raw.ID.String(), raw.RunID.String()),
		URL:           strings.TrimSpace(raw.URL),
		HeadSHA:       firstGitHubActionsString(raw.HeadSHA, raw.HeadSHASnake),
		HeadBranch:    firstGitHubActionsString(raw.HeadBranch, raw.HeadBranchSnake),
		Status:        strings.TrimSpace(raw.Status),
		Conclusion:    strings.TrimSpace(raw.Conclusion),
		RequiredGates: append([]string(nil), input.RequiredGates...),
		Gates:         githubActionsCIRunGates(raw.Jobs, input.GateNames),
		Metadata:      copyGitHubActionsMetadata(input.Metadata),
	}
	if err := validateCIRun(run); err != nil {
		return CIRun{}, err
	}
	return run, nil
}

type githubActionsRawRun struct {
	DatabaseID        githubActionsString   `json:"databaseId"`
	ID                githubActionsString   `json:"id"`
	RunID             githubActionsString   `json:"run_id"`
	Name              string                `json:"name"`
	WorkflowName      string                `json:"workflowName"`
	WorkflowNameSnake string                `json:"workflow_name"`
	Workflow          string                `json:"workflow"`
	Status            string                `json:"status"`
	Conclusion        string                `json:"conclusion"`
	URL               string                `json:"url"`
	HeadSHA           string                `json:"headSha"`
	HeadSHASnake      string                `json:"head_sha"`
	HeadBranch        string                `json:"headBranch"`
	HeadBranchSnake   string                `json:"head_branch"`
	Jobs              []githubActionsRawJob `json:"jobs"`
}

type githubActionsRawJob struct {
	ID               githubActionsString    `json:"id"`
	Name             string                 `json:"name"`
	Status           string                 `json:"status"`
	Conclusion       string                 `json:"conclusion"`
	URL              string                 `json:"url"`
	StartedAt        githubActionsTime      `json:"startedAt"`
	StartedAtSnake   githubActionsTime      `json:"started_at"`
	CompletedAt      githubActionsTime      `json:"completedAt"`
	CompletedAtSnake githubActionsTime      `json:"completed_at"`
	Steps            []githubActionsRawStep `json:"steps"`
}

type githubActionsRawStep struct {
	Number           githubActionsString `json:"number"`
	Name             string              `json:"name"`
	Status           string              `json:"status"`
	Conclusion       string              `json:"conclusion"`
	StartedAt        githubActionsTime   `json:"startedAt"`
	StartedAtSnake   githubActionsTime   `json:"started_at"`
	CompletedAt      githubActionsTime   `json:"completedAt"`
	CompletedAtSnake githubActionsTime   `json:"completed_at"`
}

type githubActionsString string

func (s *githubActionsString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = githubActionsString(strings.TrimSpace(text))
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*s = githubActionsString(number.String())
	return nil
}

func (s githubActionsString) String() string {
	return strings.TrimSpace(string(s))
}

type githubActionsTime struct {
	time.Time
}

func (t *githubActionsTime) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func decodeGitHubActionsJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(out)
}

func githubActionsCIRunGates(jobs []githubActionsRawJob, gateNames map[string]string) []CIRunGate {
	gates := make([]CIRunGate, 0)
	for _, job := range jobs {
		if len(job.Steps) == 0 {
			gate, ok := githubActionsGateName(job.Name, gateNames)
			if !ok {
				continue
			}
			gates = append(gates, githubActionsCIRunGate(gate, job, githubActionsRawStep{}))
			continue
		}
		for _, step := range job.Steps {
			gate, ok := githubActionsGateName(step.Name, gateNames)
			if !ok {
				continue
			}
			gates = append(gates, githubActionsCIRunGate(gate, job, step))
		}
	}
	return gates
}

func githubActionsGateName(name string, gateNames map[string]string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if len(gateNames) == 0 {
		return name, true
	}
	if gate := strings.TrimSpace(gateNames[name]); gate != "" {
		return gate, true
	}
	for source, gate := range gateNames {
		if strings.TrimSpace(source) == name {
			gate = strings.TrimSpace(gate)
			return gate, gate != ""
		}
	}
	return "", false
}

func githubActionsCIRunGate(gate string, job githubActionsRawJob, step githubActionsRawStep) CIRunGate {
	startedAt := firstGitHubActionsTime(step.StartedAt, step.StartedAtSnake)
	completedAt := firstGitHubActionsTime(step.CompletedAt, step.CompletedAtSnake)
	status := githubActionsStatus(step.Status, step.Conclusion)
	metadata := map[string]any{
		"github_job_status":     strings.TrimSpace(job.Status),
		"github_job_conclusion": strings.TrimSpace(job.Conclusion),
	}
	name := strings.TrimSpace(step.Name)
	if name == "" {
		name = strings.TrimSpace(job.Name)
		startedAt = firstGitHubActionsTime(job.StartedAt, job.StartedAtSnake)
		completedAt = firstGitHubActionsTime(job.CompletedAt, job.CompletedAtSnake)
		status = githubActionsStatus(job.Status, job.Conclusion)
	} else {
		metadata["github_step_status"] = strings.TrimSpace(step.Status)
		metadata["github_step_conclusion"] = strings.TrimSpace(step.Conclusion)
		if number := step.Number.String(); number != "" {
			metadata["github_step_number"] = number
		}
	}
	if id := job.ID.String(); id != "" {
		metadata["github_job_id"] = id
	}
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return CIRunGate{
			Gate:     gate,
			Status:   status,
			Job:      strings.TrimSpace(job.Name),
			Step:     name,
			URL:      strings.TrimSpace(job.URL),
			Metadata: metadata,
		}
	}
	return CIRunGate{
		Gate:        gate,
		Status:      status,
		Job:         strings.TrimSpace(job.Name),
		Step:        name,
		URL:         strings.TrimSpace(job.URL),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		Duration:    completedAt.Sub(startedAt),
		Metadata:    metadata,
	}
}

func githubActionsStatus(status, conclusion string) gopact.VerificationStatus {
	status = strings.ToLower(strings.TrimSpace(status))
	conclusion = strings.ToLower(strings.TrimSpace(conclusion))
	switch conclusion {
	case "success":
		return gopact.VerificationStatusPassed
	case "skipped", "neutral":
		return gopact.VerificationStatusSkipped
	case "":
		if status == "completed" {
			return gopact.VerificationStatusPassed
		}
		return gopact.VerificationStatusSkipped
	default:
		return gopact.VerificationStatusFailed
	}
}

func firstGitHubActionsTime(values ...githubActionsTime) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.Time
		}
	}
	return time.Time{}
}

func copyGitHubActionsMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstGitHubActionsString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
