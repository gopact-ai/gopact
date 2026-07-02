package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewEnvCardListersBootstrapsConfiguredSources(t *testing.T) {
	ctx := context.Background()
	filePath := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(filePath, []byte(`[{"name":"file-agent"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeHTTPJSON(w, http.StatusOK, map[string]any{"agents": []AgentCard{{Name: "registry-agent"}}})
	}))
	defer registry.Close()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != httpPathCard {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Cluster") != "dev" {
			http.Error(w, "missing cluster header", http.StatusUnauthorized)
			return
		}
		writeHTTPJSON(w, http.StatusOK, AgentCard{Name: "endpoint-agent"})
	}))
	defer endpoint.Close()

	lookup := func(key string) string {
		switch key {
		case EnvA2ARegistryFile:
			return filePath
		case EnvA2ARegistryURL:
			return registry.URL + "/agents.json"
		case EnvA2AEndpoints:
			return " " + endpoint.URL + " "
		default:
			return ""
		}
	}
	listers, sources, err := NewEnvCardListers(lookup, WithHTTPHeader("X-Cluster", "dev"))
	if err != nil {
		t.Fatalf("NewEnvCardListers() error = %v", err)
	}
	if got, want := len(listers), 3; got != want {
		t.Fatalf("listers = %d, want %d", got, want)
	}
	if got, want := sources, []string{"file registry", "HTTP registry", "HTTP endpoints"}; !equalStrings(got, want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}

	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	bootstrap, err := mesh.Bootstrap(ctx, listers...)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	raw, _ := json.Marshal(bootstrap.Cards)
	if got, want := string(raw), `[{"name":"file-agent"},{"name":"registry-agent"},{"name":"endpoint-agent","url":"`+endpoint.URL+`"}]`; got != want {
		t.Fatalf("Bootstrap() cards = %s, want %s", got, want)
	}
}

func TestMeshBootstrapEnvBootstrapsConfiguredSources(t *testing.T) {
	ctx := context.Background()
	filePath := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(filePath, []byte(`[{"name":"file-agent"}]`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	bootstrap, sources, err := mesh.BootstrapEnv(ctx, func(key string) string {
		if key == EnvA2ARegistryFile {
			return filePath
		}
		return ""
	})
	if err != nil {
		t.Fatalf("BootstrapEnv() error = %v", err)
	}
	if got, want := sources, []string{"file registry"}; !equalStrings(got, want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}
	if got, want := cardNames(bootstrap.Cards), []string{"file-agent"}; !equalStrings(got, want) {
		t.Fatalf("BootstrapEnv() cards = %v, want %v", got, want)
	}
}

func TestMeshBootstrapEnvReturnsEmptyResultForEmptyEnv(t *testing.T) {
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	bootstrap, sources, err := mesh.BootstrapEnv(context.Background(), func(string) string { return " " })
	if err != nil {
		t.Fatalf("BootstrapEnv() error = %v", err)
	}
	if len(sources) != 0 || len(bootstrap.Cards) != 0 || len(bootstrap.Events) != 0 {
		t.Fatalf("BootstrapEnv() = %+v, %v; want empty no-op", bootstrap, sources)
	}
}

func TestNewEnvCardListersReturnsNoSourcesForEmptyEnv(t *testing.T) {
	listers, sources, err := NewEnvCardListers(func(string) string { return " " })
	if err != nil {
		t.Fatalf("NewEnvCardListers() error = %v", err)
	}
	if len(listers) != 0 || len(sources) != 0 {
		t.Fatalf("NewEnvCardListers() = %d listers, %v sources; want empty", len(listers), sources)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
