// Package main implements the gopact extension scaffold command.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gopact-ai/gopact/internal/extensionscaffold"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var root string
	var out string
	var dryRun bool
	var planJSON bool
	var planSH bool
	var remoteStatusJSON bool
	var verify bool

	fs := flag.NewFlagSet("gopact-extscaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&root, "root", ".", "gopact repository root containing docs/design manifests")
	fs.StringVar(&out, "out", "", "output directory for external repository scaffolds")
	fs.BoolVar(&dryRun, "dry-run", false, "print scaffold plan without writing files")
	fs.BoolVar(&planJSON, "plan-json", false, "print remote bootstrap sync plan as JSON without writing files")
	fs.BoolVar(&planSH, "plan-sh", false, "print remote bootstrap sync shell script without writing files")
	fs.BoolVar(&remoteStatusJSON, "remote-status-json", false, "print GitHub remote repository status as JSON without writing files")
	fs.BoolVar(&verify, "verify", false, "run required CI commands in each generated repository after writing")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	if dryRun && planJSON {
		_, _ = fmt.Fprintln(stderr, "-dry-run and -plan-json cannot be used together")
		return exitUsage
	}
	if dryRun && planSH {
		_, _ = fmt.Fprintln(stderr, "-dry-run and -plan-sh cannot be used together")
		return exitUsage
	}
	if planJSON && planSH {
		_, _ = fmt.Fprintln(stderr, "-plan-json and -plan-sh cannot be used together")
		return exitUsage
	}
	if remoteStatusJSON && (dryRun || planJSON || planSH) {
		_, _ = fmt.Fprintln(stderr, "-remote-status-json cannot be used with -dry-run, -plan-json, or -plan-sh")
		return exitUsage
	}
	if !dryRun && !planJSON && !planSH && !remoteStatusJSON && strings.TrimSpace(out) == "" {
		_, _ = fmt.Fprintln(stderr, "-out is required unless -dry-run, -plan-json, -plan-sh, or -remote-status-json is set")
		return exitUsage
	}

	if remoteStatusJSON {
		report, err := extensionscaffold.CheckRemoteRepositories(ctx, root, extensionscaffold.RemoteStatusOptions{})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "check remote repositories: %v\n", err)
			return exitError
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			_, _ = fmt.Fprintf(stderr, "encode remote status: %v\n", err)
			return exitError
		}
		return exitOK
	}

	if planJSON {
		plan, err := extensionscaffold.RenderSyncPlanFromDesign(root)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "render sync plan: %v\n", err)
			return exitError
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			_, _ = fmt.Fprintf(stderr, "encode sync plan: %v\n", err)
			return exitError
		}
		return exitOK
	}
	if planSH {
		file, err := extensionscaffold.RenderSyncScriptFromDesign(root)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "render sync script: %v\n", err)
			return exitError
		}
		_, _ = io.WriteString(stdout, file.Body)
		return exitOK
	}

	if dryRun {
		scaffolds, err := extensionscaffold.RenderRepositoriesFromDesign(root)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "render scaffold plan: %v\n", err)
			return exitError
		}
		_, _ = fmt.Fprintf(stdout, "dry-run: %d repositories\n", len(scaffolds))
		for _, scaffold := range scaffolds {
			_, _ = fmt.Fprintf(stdout, "%s\t%d files\n", scaffold.Directory, len(scaffold.Files))
		}
		return exitOK
	}

	workspace, err := extensionscaffold.WriteBootstrapWorkspace(ctx, root, out)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "write scaffold workspace: %v\n", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d repositories, %s, %s, and %s to %s\n", len(workspace.Scaffolds), workspace.GoWork.Path, workspace.SyncPlan.Path, workspace.SyncScript.Path, out)
	for _, scaffold := range workspace.Scaffolds {
		_, _ = fmt.Fprintf(stdout, "%s\t%d files\n", scaffold.Directory, len(scaffold.Files))
	}
	if verify {
		report, err := extensionscaffold.VerifyBootstrapWorkspace(ctx, out, workspace)
		for _, result := range report.Results {
			status := "ok"
			if !result.Passed {
				status = "failed"
			}
			_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\n", result.Repository, result.CommandLine, status)
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "verify scaffold workspace: %v\n", err)
			return exitError
		}
		_, _ = fmt.Fprintf(stdout, "verified %d checks across %d repositories\n", len(report.Results), len(workspace.Scaffolds))
	}
	return exitOK
}
