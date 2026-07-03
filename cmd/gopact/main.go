// Package main implements the gopact development CLI.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"text/template"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	fallbackSDKVersion = "v0.0.46"
	scaffoldGoVersion  = "1.25"
)

type agentScaffoldData struct {
	AgentName        string
	AgentNameLiteral string
	ModulePath       string
	SDKVersion       string
	GoVersion        string
}

type agentClusterScaffoldData struct {
	ClusterName        string
	ClusterNameLiteral string
	ModulePath         string
	SDKVersion         string
	GoVersion          string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "context canceled: %v\n", err)
		return exitError
	}
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "agent":
		return runAgent(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func runAgent(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := ctx.Err(); err != nil {
		_, _ = fmt.Fprintf(stderr, "context canceled: %v\n", err)
		return exitError
	}
	if len(args) == 0 {
		printAgentUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "init":
		return runAgentInit(ctx, args[1:], stdout, stderr)
	case "init-cluster":
		return runAgentInitCluster(ctx, args[1:], stdout, stderr)
	case "run":
		return runAgentRun(ctx, args[1:], stdout, stderr)
	case "verify":
		return runAgentVerify(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printAgentUsage(stdout)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "unknown agent command: %s\n", args[0])
		printAgentUsage(stderr)
		return exitUsage
	}
}

func runAgentInit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var out string
	var modulePath string
	sdkVersion := defaultSDKVersion()
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("gopact agent init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&out, "out", "", "output directory for the generated agent")
	fs.StringVar(&modulePath, "module", "", "Go module path for the generated agent")
	fs.StringVar(&sdkVersion, "sdk-version", sdkVersion, "github.com/gopact-ai/gopact version required by the generated agent")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if name == "" {
		if fs.NArg() != 1 {
			_, _ = fmt.Fprintln(stderr, "agent name is required")
			return exitUsage
		}
		name = fs.Arg(0)
	} else if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	name = strings.TrimSpace(name)
	modulePath = strings.TrimSpace(modulePath)
	sdkVersion = strings.TrimSpace(sdkVersion)
	out = strings.TrimSpace(out)
	if err := validateAgentInit(name, modulePath, sdkVersion); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if out == "" {
		out = name
	}

	files, err := renderAgentScaffold(agentScaffoldData{
		AgentName:        name,
		AgentNameLiteral: fmt.Sprintf("%q", name),
		ModulePath:       modulePath,
		SDKVersion:       sdkVersion,
		GoVersion:        scaffoldGoVersion,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "render agent scaffold: %v\n", err)
		return exitError
	}
	if err := writeAgentScaffold(ctx, out, files); err != nil {
		_, _ = fmt.Fprintf(stderr, "write agent scaffold: %v\n", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "created agent scaffold %s in %s\n", name, out)
	return exitOK
}

func runAgentInitCluster(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var out string
	var modulePath string
	sdkVersion := defaultSDKVersion()
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("gopact agent init-cluster", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&out, "out", "", "output directory for the generated agent cluster")
	fs.StringVar(&modulePath, "module", "", "Go module path for the generated agent cluster")
	fs.StringVar(&sdkVersion, "sdk-version", sdkVersion, "github.com/gopact-ai/gopact version required by the generated agent cluster")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if name == "" {
		if fs.NArg() != 1 {
			_, _ = fmt.Fprintln(stderr, "agent cluster name is required")
			return exitUsage
		}
		name = fs.Arg(0)
	} else if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	name = strings.TrimSpace(name)
	modulePath = strings.TrimSpace(modulePath)
	sdkVersion = strings.TrimSpace(sdkVersion)
	out = strings.TrimSpace(out)
	if err := validateAgentInit(name, modulePath, sdkVersion); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if out == "" {
		out = name
	}

	files, err := renderAgentClusterScaffold(agentClusterScaffoldData{
		ClusterName:        name,
		ClusterNameLiteral: fmt.Sprintf("%q", name),
		ModulePath:         modulePath,
		SDKVersion:         sdkVersion,
		GoVersion:          scaffoldGoVersion,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "render agent cluster scaffold: %v\n", err)
		return exitError
	}
	if err := writeAgentScaffold(ctx, out, files); err != nil {
		_, _ = fmt.Fprintf(stderr, "write agent cluster scaffold: %v\n", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "created agent cluster scaffold %s in %s\n", name, out)
	return exitOK
}

func runAgentRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gopact agent run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args()[1:], " "))
		return exitUsage
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	env, err := agentRunEnv(dir, os.Environ())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent run: %v\n", err)
		return exitError
	}
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(stderr, "agent run: %v\n", err)
		return exitError
	}
	return exitOK
}

func runAgentVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gopact agent verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args()[1:], " "))
		return exitUsage
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}

	report, err := verifyAgentScaffold(ctx, dir, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agent verify: %v\n", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "verified agent scaffold %s checks=%d\n", report.AgentName, report.Checks)
	return exitOK
}

type agentVerifyReport struct {
	AgentName string
	Checks    int
}

var agentVerifyRequiredFiles = []string{
	"go.mod",
	"main.go",
	"main_test.go",
	"agents.json",
	".env.example",
	".gitignore",
	"README.md",
}

func verifyAgentScaffold(ctx context.Context, dir string, stdout, stderr io.Writer) (agentVerifyReport, error) {
	if err := ctx.Err(); err != nil {
		return agentVerifyReport{}, err
	}
	for _, name := range agentVerifyRequiredFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return agentVerifyReport{}, fmt.Errorf("missing %s", name)
			}
			return agentVerifyReport{}, err
		}
	}

	agentName, err := verifyAgentRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		return agentVerifyReport{}, err
	}

	cmd := exec.CommandContext(ctx, "go", "test", "./...")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return agentVerifyReport{}, fmt.Errorf("go test ./...: %w", err)
	}
	return agentVerifyReport{
		AgentName: agentName,
		Checks:    len(agentVerifyRequiredFiles) + 2,
	}, nil
}

type agentRegistryCard struct {
	Name         string                  `json:"name"`
	URL          string                  `json:"url"`
	Protocols    []agentRegistryProtocol `json:"protocols"`
	Capabilities []string                `json:"capabilities"`
	Streaming    bool                    `json:"streaming"`
	Health       *agentRegistryHealth    `json:"health"`
}

type agentRegistryProtocol struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
}

type agentRegistryHealth struct {
	HealthPath    string `json:"health_path"`
	ReadinessPath string `json:"readiness_path"`
}

func verifyAgentRegistry(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		return "", errors.New("agents.json must be a bare array registry")
	}

	var cards []agentRegistryCard
	if err := json.Unmarshal(body, &cards); err != nil {
		return "", fmt.Errorf("parse agents.json: %w", err)
	}
	if len(cards) == 0 {
		return "", errors.New("agents.json must contain at least one agent card")
	}
	card := cards[0]
	if card.Name == "" {
		return "", errors.New("agents.json first card missing name")
	}
	if card.URL == "" {
		return "", errors.New("agents.json first card missing url")
	}
	if len(card.Protocols) == 0 {
		return "", errors.New("agents.json first card missing protocols")
	}
	if len(card.Capabilities) == 0 {
		return "", errors.New("agents.json first card missing capabilities")
	}
	if !card.Streaming {
		return "", errors.New("agents.json first card must enable streaming")
	}
	if card.Health == nil || card.Health.HealthPath == "" || card.Health.ReadinessPath == "" {
		return "", errors.New("agents.json first card missing health and readiness paths")
	}
	return card.Name, nil
}

func agentRunEnv(dir string, env []string) ([]string, error) {
	body, err := os.ReadFile(filepath.Join(dir, ".env"))
	if errors.Is(err, os.ErrNotExist) {
		return env, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	seen := make(map[string]struct{}, len(env))
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			seen[key] = struct{}{}
		}
	}
	for lineNo, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("parse .env line %d: missing =", lineNo+1)
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t\r\n") {
			return nil, fmt.Errorf("parse .env line %d: invalid key", lineNo+1)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		env = append(env, key+"="+strings.TrimSpace(value))
		seen[key] = struct{}{}
	}
	return env, nil
}

func validateAgentInit(name, modulePath, sdkVersion string) error {
	if name == "" {
		return errors.New("agent name is required")
	}
	if strings.ContainsAny(name, `/\`) {
		return errors.New("agent name must not contain path separators")
	}
	if modulePath == "" {
		return errors.New("-module is required")
	}
	if strings.ContainsAny(modulePath, " \t\r\n") {
		return errors.New("-module must not contain whitespace")
	}
	if sdkVersion == "" {
		return errors.New("-sdk-version is required")
	}
	return nil
}

func defaultSDKVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return fallbackSDKVersion
	}
	return info.Main.Version
}

func renderAgentScaffold(data agentScaffoldData) (map[string][]byte, error) {
	files := make(map[string][]byte)
	rendered, err := renderTextTemplate("go.mod", goModTemplate, data)
	if err != nil {
		return nil, err
	}
	files["go.mod"] = []byte(rendered)

	for name, tpl := range map[string]string{
		"main.go":      mainGoTemplate,
		"main_test.go": mainTestGoTemplate,
	} {
		rendered, err := renderTextTemplate(name, tpl, data)
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source([]byte(rendered))
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", name, err)
		}
		files[name] = formatted
	}

	registry, err := renderAgentRegistry(data)
	if err != nil {
		return nil, err
	}
	files["agents.json"] = registry

	readme, err := renderTextTemplate("README.md", readmeTemplate, data)
	if err != nil {
		return nil, err
	}
	files["README.md"] = []byte(readme)
	files[".env.example"] = []byte(envExampleTemplate)
	files[".gitignore"] = []byte(".env\n.env.*\n!.env.example\n")
	return files, nil
}

func renderAgentClusterScaffold(data agentClusterScaffoldData) (map[string][]byte, error) {
	files := make(map[string][]byte)
	rendered, err := renderTextTemplate("go.mod", goModTemplate, data)
	if err != nil {
		return nil, err
	}
	files["go.mod"] = []byte(rendered)

	for name, tpl := range map[string]string{
		"main.go":      clusterMainGoTemplate,
		"main_test.go": clusterMainTestGoTemplate,
	} {
		rendered, err := renderTextTemplate(name, tpl, data)
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source([]byte(rendered))
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", name, err)
		}
		files[name] = formatted
	}

	registry, err := renderAgentClusterRegistry()
	if err != nil {
		return nil, err
	}
	files["agents.json"] = registry

	readme, err := renderTextTemplate("README.md", clusterReadmeTemplate, data)
	if err != nil {
		return nil, err
	}
	files["README.md"] = []byte(readme)
	files[".env.example"] = []byte(clusterEnvExampleTemplate)
	files[".gitignore"] = []byte(".env\n.env.*\n!.env.example\n")
	return files, nil
}

func renderTextTemplate(name, body string, data any) (string, error) {
	tpl, err := template.New(name).Parse(body)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func renderAgentRegistry(data agentScaffoldData) ([]byte, error) {
	cards := []map[string]any{{
		"name":         data.AgentName,
		"description":  "Generated gopact A2A agent scaffold.",
		"url":          "http://localhost:8080",
		"protocols":    []map[string]string{{"name": "a2a", "transport": "http"}},
		"capabilities": []string{"chat"},
		"streaming":    true,
		"health": map[string]string{
			"health_path":    "/healthz",
			"readiness_path": "/readyz",
		},
	}}
	body, err := json.MarshalIndent(cards, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func renderAgentClusterRegistry() ([]byte, error) {
	cards := []map[string]any{
		agentClusterRegistryCard("planner-agent", "Generated planning agent.", "http://localhost:8080/agents/planner-agent", "planning"),
		agentClusterRegistryCard("worker-agent", "Generated execution agent.", "http://localhost:8080/agents/worker-agent", "execution"),
		agentClusterRegistryCard("reviewer-agent", "Generated review agent.", "http://localhost:8080/agents/reviewer-agent", "review"),
	}
	body, err := json.MarshalIndent(cards, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func agentClusterRegistryCard(name, description, url, capability string) map[string]any {
	return map[string]any{
		"name":         name,
		"description":  description,
		"url":          url,
		"protocols":    []map[string]string{{"name": "a2a", "transport": "http"}},
		"capabilities": []string{capability},
		"streaming":    true,
		"health": map[string]string{
			"health_path":    "/healthz",
			"readiness_path": "/readyz",
		},
	}
}

func writeAgentScaffold(ctx context.Context, out string, files map[string][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for name := range files {
		path := filepath.Join(out, name)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for name, body := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func printUsage(w io.Writer) {
	_, _ = io.WriteString(w, `Usage:
  gopact agent init <name> -module <module> [-out <dir>] [-sdk-version <version>]
  gopact agent init-cluster <name> -module <module> [-out <dir>] [-sdk-version <version>]
  gopact agent run [dir]
  gopact agent verify [dir]

Commands:
  agent init         Create a runnable A2A HTTP agent scaffold.
  agent init-cluster Create a runnable local A2A agent cluster scaffold.
  agent run          Run an agent module with go run.
  agent verify       Verify an agent scaffold with local mock-only checks.
`)
}

func printAgentUsage(w io.Writer) {
	_, _ = io.WriteString(w, `Usage:
  gopact agent init <name> -module <module> [-out <dir>] [-sdk-version <version>]
  gopact agent init-cluster <name> -module <module> [-out <dir>] [-sdk-version <version>]
  gopact agent run [dir]
  gopact agent verify [dir]
`)
}

const goModTemplate = `module {{ .ModulePath }}

go {{ .GoVersion }}

require github.com/gopact-ai/gopact {{ .SDKVersion }}
`

const mainGoTemplate = `package main

import (
	"context"
	"errors"
	"iter"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

const agentName = {{ .AgentNameLiteral }}

const (
	agentAddrEnv = "GOPACT_AGENT_ADDR"
	agentURLEnv  = "GOPACT_AGENT_URL"
	defaultURL   = "http://localhost:8080"
)

type scaffoldAgent struct{}

func (scaffoldAgent) Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:         agentName,
		Description:  "Generated gopact A2A agent scaffold.",
		URL:          scaffoldAgentURL(),
		Protocols: []a2a.ProtocolBinding{
			{Name: "a2a", Transport: "http"},
		},
		Capabilities: []string{"chat"},
		Streaming:    true,
		Health: &a2a.HealthHints{
			HealthPath:    "/healthz",
			ReadinessPath: "/readyz",
		},
	}
}

func scaffoldAgentURL() string {
	if url := os.Getenv(agentURLEnv); url != "" {
		return url
	}
	return defaultURL
}

func (scaffoldAgent) Send(ctx context.Context, task a2a.Task) (a2a.Result, error) {
	if err := ctx.Err(); err != nil {
		return a2a.Result{}, err
	}
	return a2a.Result{
		TaskID: task.ID,
		Output: agentName + " handled: " + task.Input,
	}, nil
}

func (agent scaffoldAgent) Stream(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
	return func(yield func(a2a.TaskEvent, error) bool) {
		result, err := agent.Send(ctx, task)
		if err != nil {
			yield(a2a.TaskEvent{
				TaskID: task.ID,
				IDs:    task.IDs,
				Status: a2a.TaskStatusFailed,
				Err:    err,
			}, err)
			return
		}
		yield(a2a.TaskEvent{
			TaskID: task.ID,
			IDs:    task.IDs,
			Status: a2a.TaskStatusCompleted,
			Result: &result,
		}, nil)
	}
}

func (scaffoldAgent) Cancel(ctx context.Context, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if taskID == "" {
		return a2a.ErrTaskIDRequired
	}
	return nil
}

type scaffoldRegistry struct {
	agent scaffoldAgent
}

func (r scaffoldRegistry) ListCards(ctx context.Context) ([]a2a.AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []a2a.AgentCard{r.agent.Card()}, nil
}

func newScaffoldHTTPHandler() http.Handler {
	agent := scaffoldAgent{}
	mux := http.NewServeMux()
	mux.Handle("/", a2a.NewHTTPHandler(agent))
	mux.Handle("/agents.json", a2a.NewHTTPRegistryHandler(scaffoldRegistry{agent: agent}))
	return mux
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, os.Getenv(agentAddrEnv)); err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           newScaffoldHTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		log.Printf("serving %s on %s", agentName, listener.Addr())
		errs <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errs
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
`

const mainTestGoTemplate = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

func TestScaffoldAgentServesA2A(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newScaffoldHTTPHandler())
	defer server.Close()
	t.Setenv(agentURLEnv, server.URL)

	remote, err := a2a.NewHTTPAgent(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	discovered, err := remote.Discover(ctx, a2a.DiscoveryQuery{Name: agentName})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if discovered.Card.Name != agentName || !discovered.Card.Streaming {
		t.Fatalf("Discover() card = %+v, want streaming %s", discovered.Card, agentName)
	}
	if discovered.Card.URL != server.URL {
		t.Fatalf("Discover() card url = %q, want %q", discovered.Card.URL, server.URL)
	}

	result, err := remote.Send(ctx, a2a.Task{ID: "task-1", Input: "hello"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.TaskID != "task-1" || result.Output != agentName+" handled: hello" {
		t.Fatalf("Send() = %+v, want scaffold response", result)
	}
}

func TestScaffoldAgentServesHealthEndpoints(t *testing.T) {
	server := httptest.NewServer(newScaffoldHTTPHandler())
	defer server.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestScaffoldAgentServesHTTPRegistry(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newScaffoldHTTPHandler())
	defer server.Close()
	t.Setenv(agentURLEnv, server.URL)

	registry, err := a2a.NewHTTPRegistry(server.URL+"/agents.json", a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}
	cards, err := registry.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 1 || cards[0].Name != agentName || cards[0].URL != server.URL || !cards[0].Streaming {
		t.Fatalf("ListCards() = %+v, want generated agent card", cards)
	}

	mesh, err := a2a.NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Bootstrap(ctx, registry); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"chat"},
		Task:    a2a.Task{ID: "task-2", Input: "registry hello"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != agentName+" handled: registry hello" {
		t.Fatalf("Route() = %+v, want scaffold response", result)
	}
}

func TestScaffoldAgentRegistryMeshStreamsAndCancels(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newScaffoldHTTPHandler())
	defer server.Close()
	t.Setenv(agentURLEnv, server.URL)

	registry, err := a2a.NewHTTPRegistry(server.URL+"/agents.json", a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}
	mesh, err := a2a.NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.Bootstrap(ctx, registry); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	var streamed a2a.TaskEvent
	for event, streamErr := range mesh.RouteStream(ctx, a2a.RouteQuery{
		Require: []string{"chat"},
		Task:    a2a.Task{ID: "task-stream", Input: "stream hello"},
	}) {
		if streamErr != nil {
			t.Fatalf("RouteStream() error = %v", streamErr)
		}
		streamed = event
	}
	if streamed.Status != a2a.TaskStatusCompleted || streamed.Result == nil || streamed.Result.Output != agentName+" handled: stream hello" {
		t.Fatalf("RouteStream() event = %+v, want completed scaffold response", streamed)
	}

	canceled, err := mesh.Cancel(ctx, agentName, "task-cancel")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled.TaskID != "task-cancel" {
		t.Fatalf("Cancel() = %+v, want task-cancel", canceled)
	}
}

func TestScaffoldAgentRejectsEmptyCancelID(t *testing.T) {
	err := scaffoldAgent{}.Cancel(context.Background(), "")
	if !errors.Is(err, a2a.ErrTaskIDRequired) {
		t.Fatalf("Cancel() error = %v, want ErrTaskIDRequired", err)
	}
}

func TestScaffoldServerStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		errs <- serve(ctx, "127.0.0.1:0")
	}()
	cancel()

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("serve() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not stop after context cancel")
	}
}

func TestScaffoldRegistryFile(t *testing.T) {
	body, err := os.ReadFile("agents.json")
	if err != nil {
		t.Fatal(err)
	}
	var cards []a2a.AgentCard
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("unmarshal agents.json: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != agentName || cards[0].URL != "http://localhost:8080" || !cards[0].Streaming {
		t.Fatalf("agents.json = %+v, want local streaming card", cards)
	}
}
`

const envExampleTemplate = `GOPACT_AGENT_ADDR=:8080
GOPACT_AGENT_URL=http://localhost:8080
`

const readmeTemplate = `# {{ .AgentName }}

Generated gopact A2A HTTP agent scaffold.

## Run

` + "```bash" + `
go test ./...
gopact agent verify .
GOPACT_AGENT_ADDR=:8080 gopact agent run .
` + "```" + `

The local registry is stored in ` + "`agents.json`" + ` as a bare A2A agent-card array. The running agent also serves a registry document at ` + "`/agents.json`" + `. ` + "`gopact agent verify`" + ` checks the scaffold files, registry shape, and ` + "`go test ./...`" + ` without loading local provider credentials. Copy ` + "`.env.example`" + ` to ` + "`.env`" + ` when local address or public URL overrides are needed; ` + "`gopact agent run`" + ` loads ` + "`.env`" + ` from this directory without overriding existing environment variables.
`

const clusterMainGoTemplate = `package main

import (
	"context"
	"errors"
	"iter"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

const clusterName = {{ .ClusterNameLiteral }}

const (
	plannerAgentName  = "planner-agent"
	workerAgentName   = "worker-agent"
	reviewerAgentName = "reviewer-agent"
)

const (
	clusterAddrEnv        = "GOPACT_CLUSTER_ADDR"
	clusterURLEnv         = "GOPACT_CLUSTER_URL"
	defaultClusterBaseURL = "http://localhost:8080"
)

type clusterAgent struct {
	name         string
	description  string
	capabilities []string
}

func clusterAgents() []clusterAgent {
	return []clusterAgent{
		{name: plannerAgentName, description: "Generated planning agent.", capabilities: []string{"planning"}},
		{name: workerAgentName, description: "Generated execution agent.", capabilities: []string{"execution"}},
		{name: reviewerAgentName, description: "Generated review agent.", capabilities: []string{"review"}},
	}
}

func (agent clusterAgent) Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        agent.name,
		Description: agent.description,
		URL:         clusterAgentURL(agent.name),
		Protocols: []a2a.ProtocolBinding{
			{Name: "a2a", Transport: "http"},
		},
		Capabilities: append([]string(nil), agent.capabilities...),
		Streaming:    true,
		Health: &a2a.HealthHints{
			HealthPath:    "/healthz",
			ReadinessPath: "/readyz",
		},
	}
}

func clusterAgentURL(name string) string {
	base := os.Getenv(clusterURLEnv)
	if base == "" {
		base = defaultClusterBaseURL
	}
	return strings.TrimRight(base, "/") + clusterAgentPath(name)
}

func clusterAgentPath(name string) string {
	return "/agents/" + name
}

func (agent clusterAgent) Send(ctx context.Context, task a2a.Task) (a2a.Result, error) {
	if err := ctx.Err(); err != nil {
		return a2a.Result{}, err
	}
	return a2a.Result{
		TaskID: task.ID,
		Output: agent.name + " handled: " + task.Input,
	}, nil
}

func (agent clusterAgent) Stream(ctx context.Context, task a2a.Task) iter.Seq2[a2a.TaskEvent, error] {
	return func(yield func(a2a.TaskEvent, error) bool) {
		if !yield(a2a.TaskEvent{
			TaskID:  task.ID,
			IDs:     task.IDs,
			Status:  a2a.TaskStatusRunning,
			Message: agent.name + " started",
		}, nil) {
			return
		}
		result, err := agent.Send(ctx, task)
		if err != nil {
			yield(a2a.TaskEvent{
				TaskID: task.ID,
				IDs:    task.IDs,
				Status: a2a.TaskStatusFailed,
				Err:    err,
			}, err)
			return
		}
		yield(a2a.TaskEvent{
			TaskID: task.ID,
			IDs:    task.IDs,
			Status: a2a.TaskStatusCompleted,
			Result: &result,
		}, nil)
	}
}

func (clusterAgent) Cancel(ctx context.Context, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if taskID == "" {
		return a2a.ErrTaskIDRequired
	}
	return nil
}

type clusterRegistry struct {
	agents []clusterAgent
}

func (r clusterRegistry) ListCards(ctx context.Context) ([]a2a.AgentCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cards := make([]a2a.AgentCard, 0, len(r.agents))
	for _, agent := range r.agents {
		cards = append(cards, agent.Card())
	}
	return cards, nil
}

func newClusterHTTPHandler() http.Handler {
	agents := clusterAgents()
	mux := http.NewServeMux()
	mux.Handle("/agents.json", a2a.NewHTTPRegistryHandler(clusterRegistry{agents: agents}))
	mux.HandleFunc("/healthz", statusHandler("ok"))
	mux.HandleFunc("/readyz", statusHandler("ready"))
	for _, agent := range agents {
		prefix := clusterAgentPath(agent.name)
		mux.Handle(prefix+"/", http.StripPrefix(prefix, a2a.NewHTTPHandler(agent)))
	}
	return mux
}

func statusHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`" + `{"status":"` + "`" + ` + status + ` + "`" + `"}` + "`" + `))
	}
}

func bootstrapClusterMesh(ctx context.Context, registryURL string, client *http.Client) (*a2a.Mesh, error) {
	registry, err := a2a.NewHTTPRegistry(registryURL, a2a.WithHTTPClient(client), a2a.WithHTTPReadinessCheck())
	if err != nil {
		return nil, err
	}
	mesh, err := a2a.NewMesh(a2a.WithMeshHTTPAgentOptions(a2a.WithHTTPReadinessCheck()))
	if err != nil {
		return nil, err
	}
	if _, err := mesh.Bootstrap(ctx, registry); err != nil {
		return nil, err
	}
	return mesh, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := serve(ctx, os.Getenv(clusterAddrEnv)); err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, addr string) error {
	if addr == "" {
		addr = ":8080"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           newClusterHTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		log.Printf("serving %s on %s", clusterName, listener.Addr())
		errs <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errs
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
`

const clusterMainTestGoTemplate = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

func TestClusterRegistryBootstrapsMesh(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()
	t.Setenv(clusterURLEnv, server.URL)

	mesh, err := bootstrapClusterMesh(ctx, server.URL+"/agents.json", server.Client())
	if err != nil {
		t.Fatalf("bootstrapClusterMesh() error = %v", err)
	}
	cards, err := mesh.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("ListCards() count = %d, want 3: %+v", len(cards), cards)
	}
	if cards[0].Name != plannerAgentName || cards[1].Name != workerAgentName || cards[2].Name != reviewerAgentName {
		t.Fatalf("ListCards() = %+v, want planner, worker, reviewer order", cards)
	}

	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: []string{"planning"},
		Task:    a2a.Task{ID: "task-plan", Input: "ship a support agent"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != plannerAgentName+" handled: ship a support agent" {
		t.Fatalf("Route() = %+v, want planner response", result)
	}
}

func TestClusterRoutesStreamingTasks(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()
	t.Setenv(clusterURLEnv, server.URL)

	mesh, err := bootstrapClusterMesh(ctx, server.URL+"/agents.json", server.Client())
	if err != nil {
		t.Fatalf("bootstrapClusterMesh() error = %v", err)
	}

	statuses := []a2a.TaskStatus{}
	var output string
	for event, streamErr := range mesh.RouteStream(ctx, a2a.RouteQuery{
		Require: []string{"review"},
		Task:    a2a.Task{ID: "task-review", Input: "candidate patch"},
	}) {
		if streamErr != nil {
			t.Fatalf("RouteStream() error = %v", streamErr)
		}
		statuses = append(statuses, event.Status)
		if event.Result != nil {
			output = event.Result.Output
		}
	}
	if len(statuses) != 2 || statuses[0] != a2a.TaskStatusRunning || statuses[1] != a2a.TaskStatusCompleted {
		t.Fatalf("RouteStream() statuses = %+v, want running then completed", statuses)
	}
	if output != reviewerAgentName+" handled: candidate patch" {
		t.Fatalf("RouteStream() output = %q, want reviewer response", output)
	}
}

func TestClusterCancelsTasks(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()
	t.Setenv(clusterURLEnv, server.URL)

	mesh, err := bootstrapClusterMesh(ctx, server.URL+"/agents.json", server.Client())
	if err != nil {
		t.Fatalf("bootstrapClusterMesh() error = %v", err)
	}
	canceled, err := mesh.Cancel(ctx, workerAgentName, "task-cancel")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled.TaskID != "task-cancel" {
		t.Fatalf("Cancel() = %+v, want task-cancel", canceled)
	}
}

func TestClusterAgentRejectsEmptyCancelID(t *testing.T) {
	err := clusterAgent{name: workerAgentName}.Cancel(context.Background(), "")
	if !errors.Is(err, a2a.ErrTaskIDRequired) {
		t.Fatalf("Cancel() error = %v, want ErrTaskIDRequired", err)
	}
}

func TestClusterServesHealthEndpoints(t *testing.T) {
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()

	for _, path := range []string{"/healthz", "/readyz", "/agents/planner-agent/healthz", "/agents/planner-agent/readyz"} {
		resp, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestClusterServerStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		errs <- serve(ctx, "127.0.0.1:0")
	}()
	cancel()

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("serve() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not stop after context cancel")
	}
}

func TestClusterRegistryFile(t *testing.T) {
	body, err := os.ReadFile("agents.json")
	if err != nil {
		t.Fatal(err)
	}
	var cards []a2a.AgentCard
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("unmarshal agents.json: %v", err)
	}
	if len(cards) != 3 || cards[0].Name != plannerAgentName || cards[1].Name != workerAgentName || cards[2].Name != reviewerAgentName {
		t.Fatalf("agents.json = %+v, want planner, worker, reviewer cards", cards)
	}
	for _, card := range cards {
		if card.URL == "" || len(card.Capabilities) == 0 || !card.Streaming || card.Health == nil {
			t.Fatalf("agents.json card = %+v, want routable streaming card with health", card)
		}
	}
}
`

const clusterEnvExampleTemplate = `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=http://localhost:8080
GOPACT_A2A_REGISTRY_URL=http://localhost:8080/agents.json
`

const clusterReadmeTemplate = `# {{ .ClusterName }}

Generated gopact local A2A agent cluster scaffold.

## Run

` + "```bash" + `
go test ./...
gopact agent verify .
GOPACT_CLUSTER_ADDR=:8080 gopact agent run .
` + "```" + `

The cluster runs three A2A HTTP agents under ` + "`/agents/planner-agent`" + `, ` + "`/agents/worker-agent`" + `, and ` + "`/agents/reviewer-agent`" + `. The local registry is stored in ` + "`agents.json`" + ` as a bare A2A agent-card array, and the running cluster serves an HTTP registry document at ` + "`/agents.json`" + `. ` + "`gopact agent verify`" + ` checks required scaffold files, registry shape, and ` + "`go test ./...`" + ` without loading local provider credentials. Copy ` + "`.env.example`" + ` to ` + "`.env`" + ` when ` + "`GOPACT_CLUSTER_ADDR`" + `, ` + "`GOPACT_CLUSTER_URL`" + `, or ` + "`GOPACT_A2A_REGISTRY_URL`" + ` overrides are needed; ` + "`gopact agent run`" + ` loads ` + "`.env`" + ` from this directory without overriding existing environment variables.
`
