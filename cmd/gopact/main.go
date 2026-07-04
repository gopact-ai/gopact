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

	"github.com/gopact-ai/gopact"
	"github.com/gopact-ai/gopact/gopacttest"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	fallbackSDKVersion = "v0.0.55"
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
	Agents             []clusterAgentScaffoldSpec
}

type releaseBundleOutput struct {
	RunExport gopact.RunExport          `json:"run_export"`
	Report    gopact.VerificationReport `json:"report"`
}

type clusterAgentScaffoldSpec struct {
	Name               string
	NameLiteral        string
	Description        string
	DescriptionLiteral string
	Capability         string
	CapabilityLiteral  string
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
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
	case "release-bundle":
		return runReleaseBundle(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func runReleaseBundle(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var runExportPath string
	var reportPath string

	fs := flag.NewFlagSet("gopact release-bundle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&runExportPath, "run-export", "", "path to a gopact RunExport JSON file")
	fs.StringVar(&reportPath, "report", "", "path to an observed gopact VerificationReport JSON file")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	runExportPath = strings.TrimSpace(runExportPath)
	if runExportPath == "" {
		_, _ = fmt.Fprintln(stderr, "-run-export is required")
		return exitUsage
	}
	reportPath = strings.TrimSpace(reportPath)
	if reportPath == "" {
		_, _ = fmt.Fprintln(stderr, "-report is required")
		return exitUsage
	}

	bundle, err := buildSelfBootstrapReleaseBundle(ctx, runExportPath, reportPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "release-bundle: %v\n", err)
		return exitError
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		_, _ = fmt.Fprintf(stderr, "release-bundle: write JSON: %v\n", err)
		return exitError
	}
	return exitOK
}

func buildSelfBootstrapReleaseBundle(ctx context.Context, runExportPath, reportPath string) (releaseBundleOutput, error) {
	if err := ctx.Err(); err != nil {
		return releaseBundleOutput{}, err
	}

	body, err := os.ReadFile(runExportPath)
	if err != nil {
		return releaseBundleOutput{}, fmt.Errorf("read run export: %w", err)
	}
	var export gopact.RunExport
	if err := json.Unmarshal(body, &export); err != nil {
		return releaseBundleOutput{}, fmt.Errorf("parse run export: %w", err)
	}

	body, err = os.ReadFile(reportPath)
	if err != nil {
		return releaseBundleOutput{}, fmt.Errorf("read verification report: %w", err)
	}
	var report gopact.VerificationReport
	if err := json.Unmarshal(body, &report); err != nil {
		return releaseBundleOutput{}, fmt.Errorf("parse verification report: %w", err)
	}

	bundled, err := gopact.EmbedVerificationReport(export, report)
	if err != nil {
		return releaseBundleOutput{}, err
	}
	for _, result := range gopacttest.CheckSelfBootstrapReleaseGate(ctx, bundled, report) {
		if !result.Passed {
			return releaseBundleOutput{}, fmt.Errorf("release gate case %q failed: %w", result.Case, result.Err)
		}
	}
	return releaseBundleOutput{RunExport: bundled, Report: report}, nil
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
	fs.StringVar(&modulePath, "module", "", "Go module path for the generated agent (default example.com/<name>)")
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
	if modulePath == "" {
		modulePath = defaultAgentModulePath(name)
	}
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
	var agentSpecs repeatedStringFlag
	sdkVersion := defaultSDKVersion()
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("gopact agent init-cluster", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&out, "out", "", "output directory for the generated agent cluster")
	fs.StringVar(&modulePath, "module", "", "Go module path for the generated agent cluster (default example.com/<name>)")
	fs.StringVar(&sdkVersion, "sdk-version", sdkVersion, "github.com/gopact-ai/gopact version required by the generated agent cluster")
	fs.Var(&agentSpecs, "agent", "domain agent spec name[:capability[:description]]; repeat for multiple agents")
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
	if modulePath == "" {
		modulePath = defaultAgentModulePath(name)
	}
	if err := validateAgentInit(name, modulePath, sdkVersion); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	agents, err := parseClusterAgentSpecs(agentSpecs)
	if err != nil {
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
		Agents:             agents,
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
	if err := verifyAgentGitignore(filepath.Join(dir, ".gitignore")); err != nil {
		return agentVerifyReport{}, err
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
	for i, card := range cards {
		label := agentRegistryCardLabel(i)
		if card.Name == "" {
			return "", fmt.Errorf("agents.json %s missing name", label)
		}
		if card.URL == "" {
			return "", fmt.Errorf("agents.json %s missing url", label)
		}
		if len(card.Protocols) == 0 {
			return "", fmt.Errorf("agents.json %s missing protocols", label)
		}
		if len(card.Capabilities) == 0 {
			return "", fmt.Errorf("agents.json %s missing capabilities", label)
		}
		if !card.Streaming {
			return "", fmt.Errorf("agents.json %s must enable streaming", label)
		}
		if card.Health == nil || card.Health.HealthPath == "" || card.Health.ReadinessPath == "" {
			return "", fmt.Errorf("agents.json %s missing health and readiness paths", label)
		}
	}
	return cards[0].Name, nil
}

func verifyAgentGitignore(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := map[string]bool{}
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines[line] = true
	}
	if !lines[".env"] {
		return errors.New(".gitignore must ignore .env")
	}
	if !lines["!.env.example"] {
		return errors.New(".gitignore must keep .env.example trackable")
	}
	return nil
}

func agentRegistryCardLabel(index int) string {
	if index == 0 {
		return "first card"
	}
	return fmt.Sprintf("card[%d]", index)
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
	if strings.ContainsAny(modulePath, " \t\r\n") {
		return errors.New("-module must not contain whitespace")
	}
	if sdkVersion == "" {
		return errors.New("-sdk-version is required")
	}
	return nil
}

func parseClusterAgentSpecs(raw []string) ([]clusterAgentScaffoldSpec, error) {
	if len(raw) == 0 {
		return defaultClusterAgentSpecs(), nil
	}
	agents := make([]clusterAgentScaffoldSpec, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		spec, err := parseClusterAgentSpec(item)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[spec.Name]; ok {
			return nil, fmt.Errorf("-agent %q is duplicated", spec.Name)
		}
		seen[spec.Name] = struct{}{}
		agents = append(agents, spec)
	}
	return agents, nil
}

func parseClusterAgentSpec(raw string) (clusterAgentScaffoldSpec, error) {
	parts := strings.SplitN(raw, ":", 3)
	name := strings.TrimSpace(parts[0])
	if err := validateClusterAgentField("-agent name", name); err != nil {
		return clusterAgentScaffoldSpec{}, err
	}
	capability := name
	if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		capability = strings.TrimSpace(parts[1])
	}
	if err := validateClusterAgentField("-agent capability", capability); err != nil {
		return clusterAgentScaffoldSpec{}, err
	}
	description := "Generated " + strings.ReplaceAll(name, "-", " ") + " agent."
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		description = strings.TrimSpace(parts[2])
	}
	return clusterAgentScaffoldSpec{
		Name:               name,
		NameLiteral:        fmt.Sprintf("%q", name),
		Description:        description,
		DescriptionLiteral: fmt.Sprintf("%q", description),
		Capability:         capability,
		CapabilityLiteral:  fmt.Sprintf("%q", capability),
	}, nil
}

func validateClusterAgentField(label, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s must not contain path separators", label)
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("%s must not contain whitespace", label)
	}
	return nil
}

func defaultClusterAgentSpecs() []clusterAgentScaffoldSpec {
	return []clusterAgentScaffoldSpec{
		mustClusterAgentSpec("planner-agent:planning:Generated planning agent."),
		mustClusterAgentSpec("worker-agent:execution:Generated execution agent."),
		mustClusterAgentSpec("reviewer-agent:review:Generated review agent."),
	}
}

func mustClusterAgentSpec(raw string) clusterAgentScaffoldSpec {
	spec, err := parseClusterAgentSpec(raw)
	if err != nil {
		panic(err)
	}
	return spec
}

func defaultAgentModulePath(name string) string {
	return "example.com/" + name
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

	registry, err := renderAgentClusterRegistry(data)
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

func renderAgentClusterRegistry(data agentClusterScaffoldData) ([]byte, error) {
	cards := make([]map[string]any, 0, len(data.Agents))
	for _, agent := range data.Agents {
		cards = append(cards, agentClusterRegistryCard(
			agent.Name,
			agent.Description,
			"http://localhost:8080/agents/"+agent.Name,
			agent.Capability,
		))
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
  gopact agent init <name> [-module <module>] [-out <dir>] [-sdk-version <version>]
  gopact agent init-cluster <name> [-module <module>] [-out <dir>] [-sdk-version <version>] [-agent name[:capability[:description]]]...
  gopact agent run [dir]
  gopact agent verify [dir]
  gopact release-bundle -run-export <file> -report <file>

Commands:
  agent init         Create a runnable A2A HTTP agent scaffold.
  agent init-cluster Create a runnable local A2A agent cluster scaffold.
  agent run          Run an agent module with go run.
  agent verify       Verify an agent scaffold with local mock-only checks.
  release-bundle     Bundle observed self-bootstrap release evidence.
`)
}

func printAgentUsage(w io.Writer) {
	_, _ = io.WriteString(w, `Usage:
  gopact agent init <name> [-module <module>] [-out <dir>] [-sdk-version <version>]
  gopact agent init-cluster <name> [-module <module>] [-out <dir>] [-sdk-version <version>] [-agent name[:capability[:description]]]...
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
	"strings"
	"syscall"
	"time"

	"github.com/gopact-ai/gopact/a2a"
)

const agentName = {{ .AgentNameLiteral }}

const (
	agentAddrEnv          = "GOPACT_AGENT_ADDR"
	agentURLEnv           = "GOPACT_AGENT_URL"
	agentRegistrarURLEnv  = "GOPACT_A2A_REGISTRAR_URL"
	defaultURL             = "http://localhost:8080"
	agentRegistryLeaseTTL = 30 * time.Second
	agentHeartbeatEvery   = 15 * time.Second
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

func maintainScaffoldRegistryLease(ctx context.Context, registrarURL string, client *http.Client, ttl, interval time.Duration) error {
	registrarURL = strings.TrimSpace(registrarURL)
	if registrarURL == "" {
		return nil
	}
	if interval <= 0 {
		return a2a.ErrSyncIntervalRequired
	}
	if client == nil {
		client = http.DefaultClient
	}
	registry, err := a2a.NewHTTPRegistry(registrarURL, a2a.WithHTTPClient(client))
	if err != nil {
		return err
	}
	registered, err := registry.RegisterCardWithLease(ctx, scaffoldAgent{}.Card(), ttl)
	if err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := registry.HeartbeatCard(ctx, registered.Name, ttl); err != nil && ctx.Err() == nil {
					log.Printf("a2a registry heartbeat failed: %v", err)
				}
			}
		}
	}()
	return nil
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
	if err := maintainScaffoldRegistryLease(ctx, os.Getenv(agentRegistrarURLEnv), http.DefaultClient, agentRegistryLeaseTTL, agentHeartbeatEvery); err != nil {
		_ = server.Close()
		<-errs
		return err
	}

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
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestScaffoldAgentSatisfiesA2AConformance(t *testing.T) {
	server := httptest.NewServer(newScaffoldHTTPHandler())
	defer server.Close()
	t.Setenv(agentURLEnv, server.URL)

	expected := scaffoldAgent{}.Card()

	a2aconformance.RequireAgentConformance(t, a2aconformance.AgentConformanceHarness{
		Agent:            scaffoldAgent{},
		Task:             a2a.Task{ID: "task-conformance", Input: "hello"},
		RequireStreaming: true,
	})

	remote, err := a2a.NewHTTPAgent(server.URL, a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPAgent() error = %v", err)
	}
	a2aconformance.RequireAgentMeshConformance(t, a2aconformance.AgentMeshConformanceHarness{
		Agent:            remote,
		Query:            a2a.DiscoveryQuery{URL: server.URL},
		ExpectedCard:     expected,
		Task:             a2a.Task{ID: "task-mesh-conformance", Input: "mesh hello"},
		RequireStreaming: true,
	})

	registry, err := a2a.NewHTTPRegistry(server.URL+"/agents.json", a2a.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}
	a2aconformance.RequireDiscovererConformance(t, a2aconformance.DiscovererConformanceHarness{
		Discoverer: registry,
		Query: a2a.DiscoveryQuery{
			Name:    agentName,
			Require: []string{"chat"},
		},
		ExpectedCard:     expected,
		RequireListCards: true,
	})
}

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

func TestScaffoldAgentRegistersWithExternalRegistryLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := a2a.NewRegistry()
	registryServer := httptest.NewServer(a2a.NewHTTPRegistryHandler(store))
	defer registryServer.Close()
	agentServer := httptest.NewServer(newScaffoldHTTPHandler())
	defer agentServer.Close()
	defer cancel()
	t.Setenv(agentURLEnv, agentServer.URL)

	if err := maintainScaffoldRegistryLease(ctx, registryServer.URL, registryServer.Client(), 100*time.Millisecond, 5*time.Millisecond); err != nil {
		t.Fatalf("maintainScaffoldRegistryLease() error = %v", err)
	}
	card, err := store.Card(ctx, agentName)
	if err != nil {
		t.Fatalf("registry card error = %v", err)
	}
	if card.URL != agentServer.URL || card.ExpiresAt.IsZero() {
		t.Fatalf("registered card = %+v, want agent URL and lease expiry", card)
	}
	requireRenewedRegistryCard(t, ctx, store, agentName, card.ExpiresAt)
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

func requireRenewedRegistryCard(t *testing.T, ctx context.Context, registry *a2a.Registry, name string, firstExpiry time.Time) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(250 * time.Millisecond)
	for {
		select {
		case <-timeout:
			t.Fatalf("registry card %q lease was not renewed after %v", name, firstExpiry)
		case <-ticker.C:
			card, err := registry.Card(ctx, name)
			if err == nil && card.ExpiresAt.After(firstExpiry) {
				return
			}
		}
	}
}
`

const envExampleTemplate = `GOPACT_AGENT_ADDR=:8080
GOPACT_AGENT_URL=http://localhost:8080
GOPACT_A2A_REGISTRAR_URL=
`

const readmeTemplate = `# {{ .AgentName }}

Generated gopact A2A HTTP agent scaffold.

## Run

` + "```bash" + `
go test ./...
gopact agent verify .
GOPACT_AGENT_ADDR=:8080 gopact agent run .
` + "```" + `

The local registry is stored in ` + "`agents.json`" + ` as a bare A2A agent-card array. The running agent also serves a registry document at ` + "`/agents.json`" + `. Set ` + "`GOPACT_A2A_REGISTRAR_URL`" + ` to a writable A2A registry root to register this agent with a renewable lease. ` + "`gopact agent verify`" + ` checks the scaffold files, registry shape, and ` + "`go test ./...`" + ` without loading local provider credentials. Copy ` + "`.env.example`" + ` to ` + "`.env`" + ` when local address or public URL overrides are needed; ` + "`gopact agent run`" + ` loads ` + "`.env`" + ` from this directory without overriding existing environment variables.
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
	clusterAddrEnv        = "GOPACT_CLUSTER_ADDR"
	clusterURLEnv         = "GOPACT_CLUSTER_URL"
	clusterRegistryURLEnv = "GOPACT_A2A_REGISTRY_URL"
	clusterRegistrarURLEnv = "GOPACT_A2A_REGISTRAR_URL"
	defaultClusterBaseURL = "http://localhost:8080"
	clusterRegistryLeaseTTL = 30 * time.Second
	clusterHeartbeatEvery   = 15 * time.Second
)

type clusterAgent struct {
	name         string
	description  string
	capabilities []string
}

func clusterAgents() []clusterAgent {
	return []clusterAgent{
		{{- range .Agents }}
		{name: {{ .NameLiteral }}, description: {{ .DescriptionLiteral }}, capabilities: []string{ {{ .CapabilityLiteral }} }},
		{{- end }}
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
	return clusterBaseURL() + clusterAgentPath(name)
}

func clusterBaseURL() string {
	base := os.Getenv(clusterURLEnv)
	if base == "" {
		return defaultClusterBaseURL
	}
	return strings.TrimRight(base, "/")
}

func clusterRegistryURL() string {
	if url := os.Getenv(clusterRegistryURLEnv); url != "" {
		return url
	}
	return clusterBaseURL() + "/agents.json"
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
		if err := ctx.Err(); err != nil {
			yield(a2a.TaskEvent{
				TaskID: task.ID,
				IDs:    task.IDs,
				Status: a2a.TaskStatusFailed,
				Err:    err,
			}, err)
			return
		}
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

func bootstrapClusterMeshFromEnv(ctx context.Context, client *http.Client) (*a2a.Mesh, error) {
	return bootstrapClusterMesh(ctx, clusterRegistryURL(), client)
}

func maintainClusterRegistryLease(ctx context.Context, registrarURL string, client *http.Client, ttl, interval time.Duration) error {
	registrarURL = strings.TrimSpace(registrarURL)
	if registrarURL == "" {
		return nil
	}
	if interval <= 0 {
		return a2a.ErrSyncIntervalRequired
	}
	if client == nil {
		client = http.DefaultClient
	}
	registry, err := a2a.NewHTTPRegistry(registrarURL, a2a.WithHTTPClient(client))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(clusterAgents()))
	for _, agent := range clusterAgents() {
		registered, err := registry.RegisterCardWithLease(ctx, agent.Card(), ttl)
		if err != nil {
			return err
		}
		names = append(names, registered.Name)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, name := range names {
					if _, err := registry.HeartbeatCard(ctx, name, ttl); err != nil && ctx.Err() == nil {
						log.Printf("a2a registry heartbeat failed for %s: %v", name, err)
					}
				}
			}
		}
	}()
	return nil
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
	if err := maintainClusterRegistryLease(ctx, os.Getenv(clusterRegistrarURLEnv), http.DefaultClient, clusterRegistryLeaseTTL, clusterHeartbeatEvery); err != nil {
		_ = server.Close()
		<-errs
		return err
	}

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
	"github.com/gopact-ai/gopact/gopacttest/a2aconformance"
)

func TestClusterAgentsSatisfyA2AConformance(t *testing.T) {
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()
	t.Setenv(clusterURLEnv, server.URL)

	registry, err := a2a.NewHTTPRegistry(server.URL+"/agents.json", a2a.WithHTTPClient(server.Client()), a2a.WithHTTPReadinessCheck())
	if err != nil {
		t.Fatalf("NewHTTPRegistry() error = %v", err)
	}

	for _, agent := range clusterAgents() {
		agent := agent
		t.Run(agent.name, func(t *testing.T) {
			expected := agent.Card()
			require := append([]string(nil), agent.capabilities...)

			a2aconformance.RequireAgentConformance(t, a2aconformance.AgentConformanceHarness{
				Agent:            agent,
				Task:             a2a.Task{ID: "task-conformance", Input: "hello"},
				RequireStreaming: true,
			})

			remote, err := a2a.NewHTTPAgent(server.URL+clusterAgentPath(agent.name), a2a.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("NewHTTPAgent() error = %v", err)
			}
			a2aconformance.RequireAgentMeshConformance(t, a2aconformance.AgentMeshConformanceHarness{
				Agent:            remote,
				Query:            a2a.DiscoveryQuery{URL: expected.URL},
				ExpectedCard:     expected,
				Task:             a2a.Task{ID: "task-mesh-conformance", Input: "mesh hello"},
				RequireStreaming: true,
			})

			a2aconformance.RequireDiscovererConformance(t, a2aconformance.DiscovererConformanceHarness{
				Discoverer: registry,
				Query: a2a.DiscoveryQuery{
					Name:    agent.name,
					Require: require,
				},
				ExpectedCard:     expected,
				RequireListCards: true,
			})
		})
	}
}

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
	agents := clusterAgents()
	if len(cards) != len(agents) {
		t.Fatalf("ListCards() count = %d, want %d: %+v", len(cards), len(agents), cards)
	}
	for i, agent := range agents {
		if cards[i].Name != agent.name {
			t.Fatalf("ListCards()[%d] = %+v, want %s", i, cards[i], agent.name)
		}
	}

	first := agents[0]
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: first.capabilities,
		Task:    a2a.Task{ID: "task-plan", Input: "ship a support agent"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != first.name+" handled: ship a support agent" {
		t.Fatalf("Route() = %+v, want first agent response", result)
	}
}

func TestClusterRegistryURLDefaultsToClusterURL(t *testing.T) {
	t.Setenv(clusterURLEnv, "http://cluster.local/root/")

	if got, want := clusterRegistryURL(), "http://cluster.local/root/agents.json"; got != want {
		t.Fatalf("clusterRegistryURL() = %q, want %q", got, want)
	}
}

func TestClusterRegistryURLUsesRegistryOverride(t *testing.T) {
	t.Setenv(clusterRegistryURLEnv, "http://registry.local/cards.json")

	if got, want := clusterRegistryURL(), "http://registry.local/cards.json"; got != want {
		t.Fatalf("clusterRegistryURL() = %q, want %q", got, want)
	}
}

func TestClusterBootstrapsMeshFromEnvRegistryURL(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()
	t.Setenv(clusterURLEnv, server.URL)
	t.Setenv(clusterRegistryURLEnv, server.URL+"/agents.json")

	mesh, err := bootstrapClusterMeshFromEnv(ctx, server.Client())
	if err != nil {
		t.Fatalf("bootstrapClusterMeshFromEnv() error = %v", err)
	}
	first := clusterAgents()[0]
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: first.capabilities,
		Task:    a2a.Task{ID: "task-execute", Input: "use env registry"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != first.name+" handled: use env registry" {
		t.Fatalf("Route() = %+v, want env registry response", result)
	}
}

func TestClusterRegistersAgentsWithExternalRegistryLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := a2a.NewRegistry()
	registryServer := httptest.NewServer(a2a.NewHTTPRegistryHandler(store))
	defer registryServer.Close()
	clusterServer := httptest.NewServer(newClusterHTTPHandler())
	defer clusterServer.Close()
	defer cancel()
	t.Setenv(clusterURLEnv, clusterServer.URL)

	if err := maintainClusterRegistryLease(ctx, registryServer.URL, registryServer.Client(), 100*time.Millisecond, 5*time.Millisecond); err != nil {
		t.Fatalf("maintainClusterRegistryLease() error = %v", err)
	}
	cards, err := store.ListCards(ctx)
	if err != nil {
		t.Fatalf("ListCards() error = %v", err)
	}
	agents := clusterAgents()
	if len(cards) != len(agents) {
		t.Fatalf("registered card count = %d, want %d: %+v", len(cards), len(agents), cards)
	}
	for i, agent := range agents {
		if cards[i].Name != agent.name {
			t.Fatalf("registered card[%d] = %+v, want %s", i, cards[i], agent.name)
		}
	}
	first := agents[0]
	last := agents[len(agents)-1]
	if cards[0].URL != clusterServer.URL+clusterAgentPath(first.name) || cards[0].ExpiresAt.IsZero() {
		t.Fatalf("registered first card = %+v, want cluster URL and lease expiry", cards[0])
	}

	mesh, err := bootstrapClusterMesh(ctx, registryServer.URL, registryServer.Client())
	if err != nil {
		t.Fatalf("bootstrapClusterMesh(external) error = %v", err)
	}
	result, err := mesh.Route(ctx, a2a.RouteQuery{
		Require: last.capabilities,
		Task:    a2a.Task{ID: "task-external-registry", Input: "external registry"},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Output != last.name+" handled: external registry" {
		t.Fatalf("Route() = %+v, want last agent response", result)
	}
	requireRenewedRegistryCard(t, ctx, store, first.name, cards[0].ExpiresAt)
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
	last := clusterAgents()[len(clusterAgents())-1]

	statuses := []a2a.TaskStatus{}
	var output string
	for event, streamErr := range mesh.RouteStream(ctx, a2a.RouteQuery{
		Require: last.capabilities,
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
	if output != last.name+" handled: candidate patch" {
		t.Fatalf("RouteStream() output = %q, want last agent response", output)
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
	first := clusterAgents()[0]
	canceled, err := mesh.Cancel(ctx, first.name, "task-cancel")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if canceled.TaskID != "task-cancel" {
		t.Fatalf("Cancel() = %+v, want task-cancel", canceled)
	}
}

func TestClusterAgentRejectsEmptyCancelID(t *testing.T) {
	err := clusterAgent{name: clusterAgents()[0].name}.Cancel(context.Background(), "")
	if !errors.Is(err, a2a.ErrTaskIDRequired) {
		t.Fatalf("Cancel() error = %v, want ErrTaskIDRequired", err)
	}
}

func TestClusterServesHealthEndpoints(t *testing.T) {
	server := httptest.NewServer(newClusterHTTPHandler())
	defer server.Close()

	first := clusterAgents()[0]
	for _, path := range []string{"/healthz", "/readyz", clusterAgentPath(first.name) + "/healthz", clusterAgentPath(first.name) + "/readyz"} {
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
	agents := clusterAgents()
	if len(cards) != len(agents) {
		t.Fatalf("agents.json card count = %d, want %d: %+v", len(cards), len(agents), cards)
	}
	for i, agent := range agents {
		if cards[i].Name != agent.name {
			t.Fatalf("agents.json card[%d] = %+v, want %s", i, cards[i], agent.name)
		}
	}
	for _, card := range cards {
		if card.URL == "" || len(card.Capabilities) == 0 || !card.Streaming || card.Health == nil {
			t.Fatalf("agents.json card = %+v, want routable streaming card with health", card)
		}
	}
}

func requireRenewedRegistryCard(t *testing.T, ctx context.Context, registry *a2a.Registry, name string, firstExpiry time.Time) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(250 * time.Millisecond)
	for {
		select {
		case <-timeout:
			t.Fatalf("registry card %q lease was not renewed after %v", name, firstExpiry)
		case <-ticker.C:
			card, err := registry.Card(ctx, name)
			if err == nil && card.ExpiresAt.After(firstExpiry) {
				return
			}
		}
	}
}
`

const clusterEnvExampleTemplate = `GOPACT_CLUSTER_ADDR=:8080
GOPACT_CLUSTER_URL=http://localhost:8080
GOPACT_A2A_REGISTRY_URL=http://localhost:8080/agents.json
GOPACT_A2A_REGISTRAR_URL=
`

const clusterReadmeTemplate = `# {{ .ClusterName }}

Generated gopact local A2A agent cluster scaffold.

## Run

` + "```bash" + `
go test ./...
gopact agent verify .
GOPACT_CLUSTER_ADDR=:8080 gopact agent run .
` + "```" + `

The cluster runs configured A2A HTTP agents under ` + "`/agents/<agent-name>`" + `:
{{- range .Agents }}
- ` + "`{{ .Name }}`" + ` (` + "`{{ .Capability }}`" + `): {{ .Description }}
{{- end }}

The local registry is stored in ` + "`agents.json`" + ` as a bare A2A agent-card array, and the running cluster serves an HTTP registry document at ` + "`/agents.json`" + `. Generated mesh bootstrap helper code reads ` + "`GOPACT_A2A_REGISTRY_URL`" + `, defaulting to ` + "`GOPACT_CLUSTER_URL`" + ` plus ` + "`/agents.json`" + `. Set ` + "`GOPACT_A2A_REGISTRAR_URL`" + ` to a writable A2A registry root to register all cluster agents with renewable leases. ` + "`gopact agent verify`" + ` checks required scaffold files, registry shape, and ` + "`go test ./...`" + ` without loading local provider credentials. Copy ` + "`.env.example`" + ` to ` + "`.env`" + ` when ` + "`GOPACT_CLUSTER_ADDR`" + `, ` + "`GOPACT_CLUSTER_URL`" + `, ` + "`GOPACT_A2A_REGISTRY_URL`" + `, or ` + "`GOPACT_A2A_REGISTRAR_URL`" + ` overrides are needed; ` + "`gopact agent run`" + ` loads ` + "`.env`" + ` from this directory without overriding existing environment variables. Pass repeated ` + "`-agent name[:capability[:description]]`" + ` flags to generate a cluster around your own domain agents.
`
