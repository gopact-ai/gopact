package gitdiff

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestScannerScanCapturesGitDiffSnapshotAndPatchProposal(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"index 1111111..2222222 100644",
		"--- a/README.md",
		"+++ b/README.md",
		"@@ -1 +1,2 @@",
		" hello",
		"+world",
		"diff --git a/assets/logo.png b/assets/logo.png",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/assets/logo.png",
	}, "\n")
	runner := &fakeRunner{
		outputs: map[string]string{
			commandKey("diff", "--no-ext-diff", "--binary"): diff,
			commandKey("diff", "--no-ext-diff", "--numstat"): strings.Join([]string{
				"2\t1\tREADME.md",
				"-\t-\tassets/logo.png",
			}, "\n"),
		},
	}

	scanner, err := New("/repo", WithRunner(runner.Run))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if result.Diff.ID != "git-diff" || result.Diff.Name != "git diff" || result.Diff.Ref != "git:worktree" {
		t.Fatalf("diff identity = %+v, want git worktree diff identity", result.Diff)
	}
	if result.Diff.Diff != diff {
		t.Fatalf("diff = %q, want captured diff", result.Diff.Diff)
	}
	if !reflect.DeepEqual(result.Diff.Files, []string{"README.md", "assets/logo.png"}) {
		t.Fatalf("diff files = %#v, want files from numstat", result.Diff.Files)
	}
	if result.Diff.Insertions != 2 || result.Diff.Deletions != 1 {
		t.Fatalf("diff stats = +%d -%d, want +2 -1", result.Diff.Insertions, result.Diff.Deletions)
	}
	if result.Diff.Metadata["source"] != "gitdiff" || result.Diff.Metadata["repo"] != "/repo" || result.Diff.Metadata["staged"] != false {
		t.Fatalf("diff metadata = %+v, want gitdiff source metadata", result.Diff.Metadata)
	}

	if result.Patch.ID != "git-diff" || result.Patch.Summary != "git working tree diff" || result.Patch.Diff != diff {
		t.Fatalf("patch = %+v, want patch proposal from git diff", result.Patch)
	}
	if len(result.Patch.Files) != 2 ||
		result.Patch.Files[0].Path != "README.md" ||
		result.Patch.Files[1].Path != "assets/logo.png" {
		t.Fatalf("patch files = %+v, want patch files from numstat", result.Patch.Files)
	}

	expectedCalls := []fakeCall{
		{dir: "/repo", args: []string{"diff", "--no-ext-diff", "--binary"}},
		{dir: "/repo", args: []string{"diff", "--no-ext-diff", "--numstat"}},
	}
	if !reflect.DeepEqual(runner.calls, expectedCalls) {
		t.Fatalf("git calls = %#v, want %#v", runner.calls, expectedCalls)
	}
}

func TestScannerScanUsesCachedDiffWhenStaged(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string]string{
			commandKey("diff", "--cached", "--no-ext-diff", "--binary"):  "diff --git a/go.mod b/go.mod\n",
			commandKey("diff", "--cached", "--no-ext-diff", "--numstat"): "1\t0\tgo.mod\n",
		},
	}

	scanner, err := New("/repo", WithRunner(runner.Run), WithStaged(true))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if result.Diff.Ref != "git:staged" || result.Patch.Summary != "git staged diff" {
		t.Fatalf("result = %+v, want staged identity", result)
	}
	expectedFirstCall := fakeCall{dir: "/repo", args: []string{"diff", "--cached", "--no-ext-diff", "--binary"}}
	if !reflect.DeepEqual(runner.calls[0], expectedFirstCall) {
		t.Fatalf("first git call = %#v, want %#v", runner.calls[0], expectedFirstCall)
	}
}

func TestScannerScanReturnsSkippedSnapshotForEmptyDiff(t *testing.T) {
	runner := &fakeRunner{
		outputs: map[string]string{
			commandKey("diff", "--no-ext-diff", "--binary"):  "",
			commandKey("diff", "--no-ext-diff", "--numstat"): "",
		},
	}

	scanner, err := New("/repo", WithRunner(runner.Run))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if !result.Diff.Skipped || result.Diff.Summary != "no git working tree diff" {
		t.Fatalf("diff = %+v, want skipped no-diff snapshot", result.Diff)
	}
	if result.Patch.Diff != "" || len(result.Patch.Files) != 0 {
		t.Fatalf("patch = %+v, want empty patch proposal for empty diff", result.Patch)
	}
}

func TestScannerScanReturnsFailedSnapshotWhenGitDiffFails(t *testing.T) {
	diffErr := errors.New("exit status 128")
	runner := &fakeRunner{
		errs: map[string]error{
			commandKey("diff", "--no-ext-diff", "--binary"): diffErr,
		},
		stderr: map[string]string{
			commandKey("diff", "--no-ext-diff", "--binary"): "fatal: not a git repository",
		},
	}

	scanner, err := New("/repo", WithRunner(runner.Run))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := scanner.Scan(context.Background())
	if !errors.Is(err, diffErr) {
		t.Fatalf("Scan() error = %v, want wrapped git diff error", err)
	}
	if result.Diff.Err == nil || !strings.Contains(result.Diff.Err.Error(), "fatal: not a git repository") {
		t.Fatalf("diff error = %v, want stderr in failed snapshot", result.Diff.Err)
	}
	if result.Diff.Ref != "git:worktree" || result.Patch.ID != "git-diff" {
		t.Fatalf("result = %+v, want failed worktree identity preserved", result)
	}
}

func TestNewRejectsInvalidInput(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrRepoRequired) {
		t.Fatalf("New(empty repo) error = %v, want ErrRepoRequired", err)
	}
	if _, err := New("/repo", WithRunner(nil)); !errors.Is(err, ErrRunnerRequired) {
		t.Fatalf("New(nil runner) error = %v, want ErrRunnerRequired", err)
	}
}

type fakeCall struct {
	dir  string
	args []string
}

type fakeRunner struct {
	outputs map[string]string
	stderr  map[string]string
	errs    map[string]error
	calls   []fakeCall
}

func (r *fakeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	copiedArgs := append([]string(nil), args...)
	r.calls = append(r.calls, fakeCall{dir: dir, args: copiedArgs})
	key := commandKey(args...)
	return []byte(r.outputs[key]), []byte(r.stderr[key]), r.errs[key]
}

func commandKey(args ...string) string {
	return strings.Join(args, "\x00")
}
