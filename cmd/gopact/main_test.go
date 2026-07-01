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
		"signal.NotifyContext",
		"server.Shutdown",
		"a2a.NewHTTPHandler(agent)",
		"a2a.NewHTTPRegistryHandler",
	)
	assertFileContains(t, filepath.Join(out, "main_test.go"),
		"httptest.NewServer",
		"TestScaffoldAgentServesHealthEndpoints",
		"TestScaffoldServerStopsOnContextCancel",
		"a2a.NewHTTPAgent",
		"a2a.NewHTTPRegistry",
	)
	assertFileContains(t, filepath.Join(out, "README.md"),
		"# support-agent",
		"gopact agent run .",
		"GOPACT_AGENT_ADDR",
	)
	assertFileContains(t, filepath.Join(out, ".env.example"),
		"GOPACT_AGENT_ADDR=:8080",
		"GOPACT_AGENT_URL=http://localhost:8080",
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

func TestRunAgentInitRejectsMissingModule(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{
		"agent", "init", "support-agent",
		"-out", filepath.Join(t.TempDir(), "support-agent"),
	}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() code = %d, want %d", code, exitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "-module is required") {
		t.Fatalf("stderr missing module error:\n%s", stderr.String())
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

func TestDefaultSDKVersionFallbackTracksLatestTag(t *testing.T) {
	if got := defaultSDKVersion(); got != "v0.0.27" {
		t.Fatalf("defaultSDKVersion() = %q, want v0.0.27", got)
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
