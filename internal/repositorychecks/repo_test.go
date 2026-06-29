package repositorychecks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repositorychecks helper")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func repoPath(t *testing.T, path string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), filepath.FromSlash(path))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(repoPath(t, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	return string(readFile(t, path))
}
