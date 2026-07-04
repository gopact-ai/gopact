package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

func TestRunAgentInitWritesRunnableScaffold(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-agent")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init", "support-agent",
		"-out", out,
		"-module", "example.com/support-agent",
		"-sdk-version", "v0.0.15",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "created agent scaffold") {
		t.Fatalf("stdout missing success message:\n%s", stdout.String())
	}

	assertFileContains(t, filepath.Join(out, "go.mod"),
		"module example.com/support-agent",
		"github.com/gopact-ai/gopact v0.0.15",
	)
	assertFileContains(t, filepath.Join(out, "main.go"),
		`agentName = "support-agent"`,
		`agentRegistrarURLEnv`,
		`"GOPACT_A2A_REGISTRAR_URL"`,
		"signal.NotifyContext",
		"server.Shutdown",
		"a2a.NewHTTPHandler(agent)",
		"a2a.NewHTTPRegistryHandler",
		"maintainScaffoldRegistryLease",
		"RegisterCardWithLease",
		"HeartbeatCard",
	)
	assertFileContains(t, filepath.Join(out, "main_test.go"),
		"httptest.NewServer",
		"gopacttest/a2aconformance",
		"TestScaffoldAgentSatisfiesA2AConformance",
		"a2aconformance.RequireAgentConformance",
		"a2aconformance.RequireAgentMeshConformance",
		"a2aconformance.RequireDiscovererConformance",
		"TestScaffoldAgentServesHealthEndpoints",
		"TestScaffoldServerStopsOnContextCancel",
		"TestScaffoldAgentRegistryMeshStreamsAndCancels",
		"TestScaffoldAgentRegistersWithExternalRegistryLease",
		"a2a.NewHTTPAgent",
		"a2a.NewHTTPRegistry",
	)
	assertFileContains(t, filepath.Join(out, "README.md"),
		"# support-agent",
		"gopact agent run .",
		"gopact agent verify .",
		"GOPACT_AGENT_ADDR",
		"GOPACT_A2A_REGISTRAR_URL",
		"loads `.env`",
	)
	assertFileContains(t, filepath.Join(out, ".env.example"),
		"GOPACT_AGENT_ADDR=:8080",
		"GOPACT_AGENT_URL=http://localhost:8080",
		"GOPACT_A2A_REGISTRAR_URL=",
	)

	registry := readFile(t, filepath.Join(out, "agents.json"))
	if !strings.HasPrefix(strings.TrimSpace(registry), "[") {
		t.Fatalf("agents.json must use a bare array registry:\n%s", registry)
	}
	var cards []struct {
		Name         string   `json:"name"`
		URL          string   `json:"url"`
		Capabilities []string `json:"capabilities"`
		Streaming    bool     `json:"streaming"`
	}
	if err := json.Unmarshal([]byte(registry), &cards); err != nil {
		t.Fatalf("agents.json is not valid JSON: %v\n%s", err, registry)
	}
	if len(cards) != 1 || cards[0].Name != "support-agent" || cards[0].URL != "http://localhost:8080" || !cards[0].Streaming {
		t.Fatalf("agents.json card = %+v, want support-agent HTTP streaming card", cards)
	}
}

func TestRunAgentInitDefaultsModulePath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-agent")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init", "support-agent",
		"-out", out,
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertFileContains(t, filepath.Join(out, "go.mod"), "module example.com/support-agent")
}

func TestRunAgentInitClusterDefaultsModulePath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-cluster")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init-cluster", "support-cluster",
		"-out", out,
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	assertFileContains(t, filepath.Join(out, "go.mod"), "module example.com/support-cluster")
}

func TestRunAgentInitClusterWritesRunnableScaffold(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-cluster")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init-cluster", "support-cluster",
		"-out", out,
		"-module", "example.com/support-cluster",
		"-sdk-version", "v0.0.15",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "created agent cluster scaffold") {
		t.Fatalf("stdout missing success message:\n%s", stdout.String())
	}

	assertFileContains(t, filepath.Join(out, "go.mod"),
		"module example.com/support-cluster",
		"github.com/gopact-ai/gopact v0.0.15",
	)
	assertFileContains(t, filepath.Join(out, "main.go"),
		`clusterName = "support-cluster"`,
		`{name: "planner-agent", description: "Generated planning agent.", capabilities: []string{"planning"}}`,
		`{name: "worker-agent", description: "Generated execution agent.", capabilities: []string{"execution"}}`,
		`{name: "reviewer-agent", description: "Generated review agent.", capabilities: []string{"review"}}`,
		"a2a.NewHTTPHandler",
		"a2a.NewHTTPRegistryHandler",
		"a2a.NewHTTPRegistry",
		"a2a.NewMesh",
		`clusterRegistryURLEnv`,
		`"GOPACT_A2A_REGISTRY_URL"`,
		`clusterRegistrarURLEnv`,
		`"GOPACT_A2A_REGISTRAR_URL"`,
		"bootstrapClusterMeshFromEnv",
		"maintainClusterRegistryLease",
		"RegisterCardWithLease",
		"HeartbeatCard",
	)
	assertFileContains(t, filepath.Join(out, "main_test.go"),
		"httptest.NewServer",
		"gopacttest/a2aconformance",
		"TestClusterAgentsSatisfyA2AConformance",
		"a2aconformance.RequireAgentConformance",
		"a2aconformance.RequireAgentMeshConformance",
		"a2aconformance.RequireDiscovererConformance",
		"TestClusterRegistryBootstrapsMesh",
		"TestClusterBootstrapsMeshFromEnvRegistryURL",
		"TestClusterRegistersAgentsWithExternalRegistryLease",
		"TestClusterRoutesStreamingTasks",
		"TestClusterServerStopsOnContextCancel",
	)
	assertFileContains(t, filepath.Join(out, "README.md"),
		"# support-cluster",
		"gopact agent verify .",
		"gopact agent run .",
		"GOPACT_CLUSTER_ADDR",
		"GOPACT_CLUSTER_URL",
		"GOPACT_A2A_REGISTRY_URL",
		"GOPACT_A2A_REGISTRAR_URL",
		"mesh bootstrap helper",
	)
	assertFileContains(t, filepath.Join(out, ".env.example"),
		"GOPACT_CLUSTER_ADDR=:8080",
		"GOPACT_CLUSTER_URL=http://localhost:8080",
		"GOPACT_A2A_REGISTRAR_URL=",
	)

	registry := readFile(t, filepath.Join(out, "agents.json"))
	if !strings.HasPrefix(strings.TrimSpace(registry), "[") {
		t.Fatalf("agents.json must use a bare array registry:\n%s", registry)
	}
	var cards []struct {
		Name         string   `json:"name"`
		URL          string   `json:"url"`
		Capabilities []string `json:"capabilities"`
		Streaming    bool     `json:"streaming"`
	}
	if err := json.Unmarshal([]byte(registry), &cards); err != nil {
		t.Fatalf("agents.json is not valid JSON: %v\n%s", err, registry)
	}
	if len(cards) != 3 {
		t.Fatalf("agents.json card count = %d, want 3: %+v", len(cards), cards)
	}
	wantNames := []string{"planner-agent", "worker-agent", "reviewer-agent"}
	for i, want := range wantNames {
		if cards[i].Name != want || cards[i].URL == "" || len(cards[i].Capabilities) == 0 || !cards[i].Streaming {
			t.Fatalf("agents.json card[%d] = %+v, want streaming %s card", i, cards[i], want)
		}
	}
}

func TestRunAgentInitClusterWritesCustomDomainAgents(t *testing.T) {
	out := filepath.Join(t.TempDir(), "commerce-cluster")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init-cluster", "commerce-cluster",
		"-out", out,
		"-module", "example.com/commerce-cluster",
		"-sdk-version", "v0.0.15",
		"-agent", "catalog:catalog:Search product catalog.",
		"-agent", "pricing:pricing:Calculate current price.",
		"-agent", "support:support:Handle customer support.",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	assertFileContains(t, filepath.Join(out, "main.go"),
		`{name: "catalog", description: "Search product catalog.", capabilities: []string{"catalog"}}`,
		`{name: "pricing", description: "Calculate current price.", capabilities: []string{"pricing"}}`,
		`{name: "support", description: "Handle customer support.", capabilities: []string{"support"}}`,
	)

	registry := readFile(t, filepath.Join(out, "agents.json"))
	var cards []struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		URL          string   `json:"url"`
		Capabilities []string `json:"capabilities"`
		Streaming    bool     `json:"streaming"`
	}
	if err := json.Unmarshal([]byte(registry), &cards); err != nil {
		t.Fatalf("agents.json is not valid JSON: %v\n%s", err, registry)
	}
	if len(cards) != 3 {
		t.Fatalf("agents.json card count = %d, want 3: %+v", len(cards), cards)
	}
	for i, want := range []struct {
		name        string
		description string
		capability  string
	}{
		{name: "catalog", description: "Search product catalog.", capability: "catalog"},
		{name: "pricing", description: "Calculate current price.", capability: "pricing"},
		{name: "support", description: "Handle customer support.", capability: "support"},
	} {
		if cards[i].Name != want.name ||
			cards[i].Description != want.description ||
			cards[i].URL != "http://localhost:8080/agents/"+want.name ||
			len(cards[i].Capabilities) != 1 ||
			cards[i].Capabilities[0] != want.capability ||
			!cards[i].Streaming {
			t.Fatalf("agents.json card[%d] = %+v, want custom domain agent %+v", i, cards[i], want)
		}
	}
}

func TestRunAgentInitClusterRejectsInvalidCustomDomainAgents(t *testing.T) {
	tests := []struct {
		name      string
		agentSpec string
		wantError string
	}{
		{name: "empty name", agentSpec: ":catalog", wantError: "-agent name is required"},
		{name: "name with path separator", agentSpec: "support/api:support", wantError: "-agent name must not contain path separators"},
		{name: "capability with whitespace", agentSpec: "support:customer support", wantError: "-agent capability must not contain whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{
				"agent", "init-cluster", "commerce-cluster",
				"-out", filepath.Join(t.TempDir(), "commerce-cluster"),
				"-agent", tt.agentSpec,
			}, &stdout, &stderr)
			if code != exitUsage {
				t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitUsage, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"agent", "init-cluster", "commerce-cluster",
		"-out", filepath.Join(t.TempDir(), "commerce-cluster"),
		"-agent", "support:support",
		"-agent", "support:helpdesk",
	}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("duplicate run() code = %d, want %d\nstderr:\n%s", code, exitUsage, stderr.String())
	}
	if !strings.Contains(stderr.String(), `-agent "support" is duplicated`) {
		t.Fatalf("stderr missing duplicate error:\n%s", stderr.String())
	}
}

func TestRunAgentRunExecutesGoModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/agent-run\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "fmt"

func main() {
	fmt.Println("agent run ok")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "run", dir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "agent run ok" {
		t.Fatalf("stdout = %q, want agent output", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAgentRunLoadsDotEnvFileWithoutOverridingEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/agent-run-dotenv\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(os.Getenv("GOPACT_DOTENV_VALUE"))
	fmt.Println(os.Getenv("GOPACT_DOTENV_EXISTING"))
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
# local agent settings
GOPACT_DOTENV_VALUE=from-dotenv
GOPACT_DOTENV_EXISTING=from-dotenv
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPACT_DOTENV_EXISTING", "from-env")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "run", dir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), "from-dotenv\nfrom-env"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAgentVerifyChecksScaffoldAndRunsTests(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, validVerifyRegistry("verify-agent"))
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ok  \texample.com/verify-agent") {
		t.Fatalf("stdout missing go test output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "verified agent scaffold verify-agent") {
		t.Fatalf("stdout missing verify summary:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAgentVerifyRejectsWrappedRegistryObject(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `{"agents":[]}`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agents.json must be a bare array registry") {
		t.Fatalf("stderr missing registry error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsInvalidLaterRegistryCard(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  },
  {
    "name": "worker-agent",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agents.json card[1] missing url") {
		t.Fatalf("stderr missing later registry card error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsDuplicateRegistryCardName(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  },
  {
    "name": "verify-agent",
    "url": "http://localhost:8080/agents/verify-agent",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["review"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `agents.json card[1] duplicates name "verify-agent"`) {
		t.Fatalf("stderr missing duplicate registry card error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsRegistryCardNameWithSurroundingWhitespace(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": " verify-agent ",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agents.json first card name must not have surrounding whitespace") {
		t.Fatalf("stderr missing registry card name error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsRegistryCardNonAbsoluteHTTPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "missing scheme", url: "localhost:8080"},
		{name: "missing hostname", url: "http://:80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "`+tt.url+`",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), `agents.json first card url must be an absolute http(s) URL`) {
				t.Fatalf("stderr missing registry card url error:\n%s", stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsRegistryCardProtocolWithoutRequiredFields(t *testing.T) {
	tests := []struct {
		name      string
		protocol  string
		wantError string
	}{
		{
			name:      "missing name",
			protocol:  `{"transport": "http"}`,
			wantError: "agents.json first card protocols[0] missing name",
		},
		{
			name:      "missing transport",
			protocol:  `{"name": "a2a"}`,
			wantError: "agents.json first card protocols[0] missing transport",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [`+tt.protocol+`],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsRegistryCardWithoutA2AHTTPProtocol(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "custom", "transport": "stdio"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agents.json first card missing a2a http protocol") {
		t.Fatalf("stderr missing a2a http protocol error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsRegistryCardBlankTextFields(t *testing.T) {
	tests := []struct {
		name      string
		registry  string
		wantError string
	}{
		{
			name: "blank name",
			registry: `[
  {
    "name": "   ",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`,
			wantError: "agents.json first card missing name",
		},
		{
			name: "blank protocol name",
			registry: `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "   ", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`,
			wantError: "agents.json first card protocols[0] missing name",
		},
		{
			name: "blank protocol transport",
			registry: `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "   "}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`,
			wantError: "agents.json first card protocols[0] missing transport",
		},
		{
			name: "blank capability",
			registry: `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["   "],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`,
			wantError: "agents.json first card capabilities[0] missing value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, tt.registry)
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsRegistryCardCapabilityWithSurroundingWhitespace(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": [" chat "],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]`)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agents.json first card capabilities[0] must not have surrounding whitespace") {
		t.Fatalf("stderr missing registry card capability error:\n%s", stderr.String())
	}
}

func TestRunAgentVerifyRejectsRegistryCardInvalidHealthPaths(t *testing.T) {
	tests := []struct {
		name      string
		health    string
		wantError string
	}{
		{
			name:      "blank health path",
			health:    `"health_path": "   ", "readiness_path": "/readyz"`,
			wantError: "agents.json first card missing health path",
		},
		{
			name:      "readiness path without slash",
			health:    `"health_path": "/healthz", "readiness_path": "readyz"`,
			wantError: "agents.json first card readiness path must start with /",
		},
		{
			name:      "health path absolute url",
			health:    `"health_path": "http://localhost:8080/healthz", "readiness_path": "/readyz"`,
			wantError: "agents.json first card health path must start with /",
		},
		{
			name:      "readiness path scheme relative url",
			health:    `"health_path": "/healthz", "readiness_path": "//localhost:8080/readyz"`,
			wantError: "agents.json first card readiness path must be a local HTTP path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, `[
  {
    "name": "verify-agent",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {`+tt.health+`}
  }
]`)
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsGitignoreWithoutDotEnvBoundary(t *testing.T) {
	tests := []struct {
		name      string
		gitignore string
		wantError string
	}{
		{
			name:      "missing dot env",
			gitignore: "coverage.out\n",
			wantError: ".gitignore must ignore .env",
		},
		{
			name:      "missing env example exception",
			gitignore: ".env\n",
			wantError: ".gitignore must keep .env.example trackable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, validVerifyRegistry("verify-agent"))
			if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(tt.gitignore), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsDotEnvExampleWithoutScaffoldEnvContract(t *testing.T) {
	tests := []struct {
		name       string
		envExample string
		wantError  string
	}{
		{
			name: "missing agent registrar url",
			envExample: `GOPACT_AGENT_ADDR=:8080
GOPACT_AGENT_URL=http://localhost:8080
`,
			wantError: ".env.example missing GOPACT_A2A_REGISTRAR_URL",
		},
		{
			name: "missing cluster registry url",
			envExample: `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=http://localhost:8080
GOPACT_A2A_REGISTRAR_URL=
`,
			wantError: ".env.example missing GOPACT_A2A_REGISTRY_URL",
		},
		{
			name:       "unknown scaffold env",
			envExample: "GOPACT_OTHER=value\n",
			wantError:  ".env.example must define agent or cluster scaffold environment variables",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, validVerifyRegistry("verify-agent"))
			if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(tt.envExample), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsDotEnvExampleInvalidURLs(t *testing.T) {
	tests := []struct {
		name       string
		envExample string
		wantError  string
	}{
		{
			name: "agent url without scheme",
			envExample: `GOPACT_AGENT_ADDR=:8080
GOPACT_AGENT_URL=localhost:8080
GOPACT_A2A_REGISTRAR_URL=
`,
			wantError: ".env.example GOPACT_AGENT_URL must be an absolute http(s) URL",
		},
		{
			name: "cluster url without scheme",
			envExample: `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=localhost:8080
GOPACT_A2A_REGISTRY_URL=http://localhost:8080/agents.json
GOPACT_A2A_REGISTRAR_URL=
`,
			wantError: ".env.example GOPACT_CLUSTER_URL must be an absolute http(s) URL",
		},
		{
			name: "cluster registry url without scheme",
			envExample: `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=http://localhost:8080
GOPACT_A2A_REGISTRY_URL=localhost:8080/agents.json
GOPACT_A2A_REGISTRAR_URL=
`,
			wantError: ".env.example GOPACT_A2A_REGISTRY_URL must be an absolute http(s) URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, validVerifyRegistry("verify-agent"))
			if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(tt.envExample), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantError, stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyRejectsDotEnvExampleInvalidRegistrarURL(t *testing.T) {
	tests := []struct {
		name       string
		envExample string
	}{
		{
			name: "agent registrar url without scheme",
			envExample: `GOPACT_AGENT_ADDR=:8080
GOPACT_AGENT_URL=http://localhost:8080
GOPACT_A2A_REGISTRAR_URL=localhost:9090
`,
		},
		{
			name: "cluster registrar url without scheme",
			envExample: `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=http://localhost:8080
GOPACT_A2A_REGISTRY_URL=http://localhost:8080/agents.json
GOPACT_A2A_REGISTRAR_URL=localhost:9090
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyRuns(t *testing.T) {}
`, validVerifyRegistry("verify-agent"))
			if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(tt.envExample), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
			if code != exitError {
				t.Fatalf("run() code = %d, want %d", code, exitError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), ".env.example GOPACT_A2A_REGISTRAR_URL must be empty or an absolute http(s) URL") {
				t.Fatalf("stderr missing registrar url error:\n%s", stderr.String())
			}
		})
	}
}

func TestRunAgentVerifyReportsGoTestFailure(t *testing.T) {
	dir := t.TempDir()
	writeVerifyAgentModule(t, dir, `package main

import "testing"

func TestVerifyFails(t *testing.T) {
	t.Fatal("boom")
}
`, validVerifyRegistry("verify-agent"))
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"agent", "verify", dir}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stdout.String(), "boom") {
		t.Fatalf("stdout missing failing test output:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agent verify: go test ./...") {
		t.Fatalf("stderr missing verify command error:\n%s", stderr.String())
	}
}

func TestAgentRunEnvSupportsExportAndRejectsInvalidDotEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
export GOPACT_EXPORTED=from-dotenv
GOPACT_EXISTING=from-dotenv
`), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := agentRunEnv(dir, []string{"GOPACT_EXISTING=from-env"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(env, "\n")
	if !strings.Contains(got, "GOPACT_EXPORTED=from-dotenv") {
		t.Fatalf("env missing exported value:\n%s", got)
	}
	if strings.Contains(got, "GOPACT_EXISTING=from-dotenv") {
		t.Fatalf("env must not override existing values:\n%s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GOPACT_BROKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentRunEnv(dir, nil); err == nil || !strings.Contains(err.Error(), "missing =") {
		t.Fatalf("agentRunEnv() err = %v, want missing = parse error", err)
	}
}

func TestRunReleaseBundleBuildsSelfBootstrapBundle(t *testing.T) {
	dir := t.TempDir()
	input := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-cli-release-bundle", ThreadID: "thread-cli-release-bundle"},
		Outcome: gopact.RunCompleted,
	}
	report, err := gopacttest.BuildSelfBootstrapReleaseGateReport(input)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(dir, "run-export.json")
	writeJSONFile(t, inputPath, input)
	reportPath := filepath.Join(dir, "verification-report.json")
	writeJSONFile(t, reportPath, report)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"release-bundle", "-run-export", inputPath, "-report", reportPath}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var bundle struct {
		RunExport gopact.RunExport          `json:"run_export"`
		Report    gopact.VerificationReport `json:"report"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &bundle); err != nil {
		t.Fatalf("stdout is not release bundle JSON: %v\n%s", err, stdout.String())
	}
	if bundle.Report.Status != gopact.VerificationStatusPassed {
		t.Fatalf("report status = %q, want passed", bundle.Report.Status)
	}
	if len(bundle.RunExport.VerificationReports) != 1 {
		t.Fatalf("embedded reports = %d, want 1", len(bundle.RunExport.VerificationReports))
	}
	gopacttest.RequireSelfBootstrapReleaseGateForExport(t, bundle.RunExport, bundle.Report)
}

func TestRunReleaseBundleRequiresObservedReport(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "run-export.json")
	writeJSONFile(t, inputPath, gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-cli-release-bundle", ThreadID: "thread-cli-release-bundle"},
		Outcome: gopact.RunCompleted,
	})
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"release-bundle", "-run-export", inputPath}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "-report is required") {
		t.Fatalf("stderr missing -report requirement:\n%s", stderr.String())
	}
}

func TestRunReleaseBundleRejectsMismatchedObservedReport(t *testing.T) {
	dir := t.TempDir()
	input := gopact.RunExport{
		Version: gopact.RunExportVersion,
		IDs:     gopact.RuntimeIDs{RunID: "run-cli-release-bundle", ThreadID: "thread-cli-release-bundle"},
		Outcome: gopact.RunCompleted,
	}
	reportExport := input
	reportExport.IDs.RunID = "other-run"
	report, err := gopacttest.BuildSelfBootstrapReleaseGateReport(reportExport)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(dir, "run-export.json")
	writeJSONFile(t, inputPath, input)
	reportPath := filepath.Join(dir, "verification-report.json")
	writeJSONFile(t, reportPath, report)
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"release-bundle", "-run-export", inputPath, "-report", reportPath}, &stdout, &stderr)
	if code != exitError {
		t.Fatalf("run() code = %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "mixed run ids") {
		t.Fatalf("stderr missing run id mismatch:\n%s", stderr.String())
	}
}

func TestDefaultSDKVersionFallbackUsesCurrentReleasedTag(t *testing.T) {
	want := "v0.0.55"
	if got := defaultSDKVersion(); got != want {
		t.Fatalf("defaultSDKVersion() = %q, want %s", got, want)
	}
}

func TestGeneratedAgentScaffoldPassesGoTest(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-agent")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init", "support-agent",
		"-out", out,
		"-module", "example.com/support-agent",
		"-sdk-version", "v0.0.15",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}

	runGeneratedCommand(t, out, "go", "mod", "edit", "-replace", "github.com/gopact-ai/gopact="+repoRoot(t))
	runGeneratedCommand(t, out, "go", "mod", "tidy")
	runGeneratedCommand(t, out, "go", "test", "./...")

	code = run(context.Background(), []string{"agent", "verify", out}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("agent verify code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verified agent scaffold support-agent") {
		t.Fatalf("agent verify stdout missing summary:\n%s", stdout.String())
	}
}

func TestGeneratedAgentClusterScaffoldPassesGoTest(t *testing.T) {
	out := filepath.Join(t.TempDir(), "support-cluster")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init-cluster", "support-cluster",
		"-out", out,
		"-module", "example.com/support-cluster",
		"-sdk-version", "v0.0.15",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}

	runGeneratedCommand(t, out, "go", "mod", "edit", "-replace", "github.com/gopact-ai/gopact="+repoRoot(t))
	runGeneratedCommand(t, out, "go", "mod", "tidy")
	runGeneratedCommand(t, out, "go", "test", "./...")

	code = run(context.Background(), []string{"agent", "verify", out}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("agent verify code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verified agent scaffold planner-agent") {
		t.Fatalf("agent verify stdout missing summary:\n%s", stdout.String())
	}
}

func TestGeneratedCustomAgentClusterScaffoldPassesGoTest(t *testing.T) {
	out := filepath.Join(t.TempDir(), "commerce-cluster")
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init-cluster", "commerce-cluster",
		"-out", out,
		"-module", "example.com/commerce-cluster",
		"-sdk-version", "v0.0.15",
		"-agent", "catalog:catalog:Search product catalog.",
		"-agent", "pricing:pricing:Calculate current price.",
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("run() code = %d, want %d\nstderr:\n%s", code, exitOK, stderr.String())
	}

	runGeneratedCommand(t, out, "go", "mod", "edit", "-replace", "github.com/gopact-ai/gopact="+repoRoot(t))
	runGeneratedCommand(t, out, "go", "mod", "tidy")
	runGeneratedCommand(t, out, "go", "test", "./...")

	code = run(context.Background(), []string{"agent", "verify", out}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("agent verify code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitOK, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "verified agent scaffold catalog") {
		t.Fatalf("agent verify stdout missing summary:\n%s", stdout.String())
	}
}

func assertFileContains(t *testing.T, path string, wants ...string) {
	t.Helper()
	body := readFile(t, path)
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q:\n%s", path, want, body)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("find repo root from %s: %v", wd, err)
	}
	return root
}

func runGeneratedCommand(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPRIVATE=github.com/gopact-ai/*")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func writeVerifyAgentModule(t *testing.T, dir string, testBody string, registry string) {
	t.Helper()
	files := map[string]string{
		"go.mod":       "module example.com/verify-agent\n\ngo 1.25\n",
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": testBody,
		"README.md":    "# verify-agent\n",
		".env.example": "GOPACT_AGENT_ADDR=:8080\nGOPACT_AGENT_URL=http://localhost:8080\nGOPACT_A2A_REGISTRAR_URL=\n",
		".gitignore":   ".env\n.env.*\n!.env.example\n",
		"agents.json":  registry,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func validVerifyRegistry(name string) string {
	return `[
  {
    "name": "` + name + `",
    "url": "http://localhost:8080",
    "protocols": [{"name": "a2a", "transport": "http"}],
    "capabilities": ["chat"],
    "streaming": true,
    "health": {"health_path": "/healthz", "readiness_path": "/readyz"}
  }
]
`
}
