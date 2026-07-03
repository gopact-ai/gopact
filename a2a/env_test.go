package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gopact-ai/gopact"
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

func TestMeshBootstrapEnvAppliesHTTPOptionsToRegisteredAgents(t *testing.T) {
	ctx := context.Background()
	server := newHeaderProtectedSyncEnvAgent(t)
	defer server.Close()
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, _, err := mesh.BootstrapEnv(ctx, func(key string) string {
		if key == EnvA2AEndpoints {
			return server.URL
		}
		return ""
	}, WithHTTPHeader("X-Cluster", "dev")); err != nil {
		t.Fatalf("BootstrapEnv() error = %v", err)
	}

	result, err := mesh.Call(ctx, "header-agent", Task{ID: "task-1", Input: "task"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Output != "synced: task" {
		t.Fatalf("Call() output = %q, want synced task", result.Output)
	}
}

func TestMeshEnvUsesMeshHTTPOptions(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name string
		sync func(context.Context, *Mesh, func(string) string) error
	}{
		{
			name: "BootstrapEnv",
			sync: func(ctx context.Context, mesh *Mesh, lookup func(string) string) error {
				_, _, err := mesh.BootstrapEnv(ctx, lookup)
				return err
			},
		},
		{
			name: "SyncEnv",
			sync: func(ctx context.Context, mesh *Mesh, lookup func(string) string) error {
				_, err := mesh.SyncEnv(ctx, lookup)
				return err
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := newHeaderProtectedSyncEnvAgent(t)
			defer server.Close()
			mesh, err := NewMesh(WithMeshHTTPAgentOptions(
				WithHTTPHeader("X-Cluster", "dev"),
				WithHTTPReadinessCheck(),
			))
			if err != nil {
				t.Fatalf("NewMesh() error = %v", err)
			}
			lookup := func(key string) string {
				if key == EnvA2AEndpoints {
					return server.URL
				}
				return ""
			}

			if err := tt.sync(ctx, mesh, lookup); err != nil {
				t.Fatalf("%s() error = %v", tt.name, err)
			}
			result, err := mesh.Call(ctx, "header-agent", Task{ID: "task-1", Input: "task"})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if result.Output != "synced: task" {
				t.Fatalf("Call() output = %q, want synced task", result.Output)
			}
		})
	}
}

func TestMeshSyncEnvBootstrapsSourcesAndPrunesUnreadyHTTPAgents(t *testing.T) {
	ctx := context.Background()
	stale := newSyncEnvHTTPAgent(t, AgentCard{Name: "stale-agent"}, false)
	defer stale.Close()
	ready := newSyncEnvHTTPAgent(t, AgentCard{Name: "ready-agent", Capabilities: []string{"planning"}}, true)
	defer ready.Close()
	filePath := filepath.Join(t.TempDir(), "agents.json")
	raw, err := json.Marshal([]AgentCard{{Name: "stale-agent", URL: stale.URL}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var events []gopact.Event
	mesh, err := NewMesh(WithMeshEventSink(func(ctx context.Context, event gopact.Event) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	result, err := mesh.SyncEnv(ctx, func(key string) string {
		switch key {
		case EnvA2ARegistryFile:
			return filePath
		case EnvA2AEndpoints:
			return ready.URL
		default:
			return ""
		}
	}, WithHTTPReadinessCheck())
	if err != nil {
		t.Fatalf("SyncEnv() error = %v", err)
	}

	if got, want := result.Sources, []string{"file registry", "HTTP endpoints"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncEnv() sources = %v, want %v", got, want)
	}
	if got, want := cardNames(result.Bootstrap.Cards), []string{"stale-agent", "ready-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncEnv() bootstrap cards = %v, want %v", got, want)
	}
	if got, want := cardNames(result.Eviction.Cards), []string{"stale-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncEnv() evicted cards = %v, want %v", got, want)
	}
	if got, want := cardNames(result.Cards), []string{"ready-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncEnv() final cards = %v, want %v", got, want)
	}
	wantEvents := []gopact.EventType{
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentCardFetched,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2AAgentRegistered,
		gopact.EventA2AAgentEvicted,
	}
	if got := eventTypes(result.Events); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("SyncEnv() events = %v, want %v", got, wantEvents)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("published events = %v, want %v", got, wantEvents)
	}
	if result.Eviction.Events[0].Metadata["eviction_reason"] != "readiness_failed" {
		t.Fatalf("eviction metadata = %+v, want readiness_failed", result.Eviction.Events[0].Metadata)
	}
}

func TestMeshSyncEnvAppliesHTTPOptionsToRegisteredAgents(t *testing.T) {
	ctx := context.Background()
	server := newHeaderProtectedSyncEnvAgent(t)
	defer server.Close()
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	if _, err := mesh.SyncEnv(ctx, func(key string) string {
		if key == EnvA2AEndpoints {
			return server.URL
		}
		return ""
	}, WithHTTPHeader("X-Cluster", "dev"), WithHTTPReadinessCheck()); err != nil {
		t.Fatalf("SyncEnv() error = %v", err)
	}

	result, err := mesh.Call(ctx, "header-agent", Task{ID: "task-1", Input: "task"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Output != "synced: task" {
		t.Fatalf("Call() output = %q, want synced task", result.Output)
	}
}

func TestMeshSyncEnvReturnsSourcesOnSyncError(t *testing.T) {
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	result, err := mesh.SyncEnv(context.Background(), func(key string) string {
		if key == EnvA2ARegistryFile {
			return filepath.Join(t.TempDir(), "missing-agents.json")
		}
		return ""
	})
	if err == nil {
		t.Fatal("SyncEnv() error = nil, want missing file error")
	}
	if got, want := result.Sources, []string{"file registry"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncEnv() sources on error = %v, want %v", got, want)
	}
}

func TestMeshSyncEnvEveryRunsImmediatelyAndRepeats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	filePath := filepath.Join(t.TempDir(), "agents.json")
	writeCards := func(cards []AgentCard) {
		raw, err := json.Marshal(cards)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := os.WriteFile(filePath, raw, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	writeCards([]AgentCard{{Name: "planner-agent"}})
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}

	var results []SyncResult
	for result, err := range mesh.SyncEnvEvery(ctx, time.Millisecond, func(key string) string {
		if key == EnvA2ARegistryFile {
			return filePath
		}
		return ""
	}) {
		if err != nil {
			t.Fatalf("SyncEnvEvery() error = %v", err)
		}
		results = append(results, result)
		if got, want := result.Sources, []string{"file registry"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("SyncEnvEvery() sources = %v, want %v", got, want)
		}
		switch len(results) {
		case 1:
			if got, want := cardNames(result.Cards), []string{"planner-agent"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("first SyncEnvEvery() cards = %v, want %v", got, want)
			}
			writeCards([]AgentCard{{Name: "planner-agent"}, {Name: "review-agent"}})
		case 2:
			if got, want := cardNames(result.Cards), []string{"planner-agent", "review-agent"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("second SyncEnvEvery() cards = %v, want %v", got, want)
			}
			cancel()
			return
		}
	}
	t.Fatalf("SyncEnvEvery() yielded %d results, want 2", len(results))
}

func TestMeshSyncEnvEveryRejectsNonPositiveInterval(t *testing.T) {
	mesh, err := NewMesh()
	if err != nil {
		t.Fatalf("NewMesh() error = %v", err)
	}
	for _, err := range mesh.SyncEnvEvery(context.Background(), 0, func(string) string { return "" }) {
		if !errors.Is(err, ErrSyncIntervalRequired) {
			t.Fatalf("SyncEnvEvery() error = %v, want ErrSyncIntervalRequired", err)
		}
		return
	}
	t.Fatal("SyncEnvEvery() yielded no error for zero interval")
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

func newSyncEnvHTTPAgent(t *testing.T, card AgentCard, ready bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case httpPathCard:
			card.URL = "http://" + r.Host
			writeHTTPJSON(w, http.StatusOK, card)
		case httpPathReady:
			if ready {
				writeHTTPJSON(w, http.StatusOK, httpStatusResponse{Status: "ready"})
				return
			}
			writeHTTPJSON(w, http.StatusServiceUnavailable, httpStatusResponse{Status: "not_ready"})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func newHeaderProtectedSyncEnvAgent(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Cluster") != "dev" {
			http.Error(w, "missing cluster header", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case httpPathCard:
			writeHTTPJSON(w, http.StatusOK, AgentCard{Name: "header-agent", URL: "http://" + r.Host})
		case httpPathReady:
			writeHTTPJSON(w, http.StatusOK, httpStatusResponse{Status: "ready"})
		case httpPathTaskSend:
			var task Task
			if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
				writeHTTPError(w, http.StatusBadRequest, err)
				return
			}
			writeHTTPJSON(w, http.StatusOK, Result{TaskID: task.ID, Output: "synced: " + task.Input})
		default:
			http.NotFound(w, r)
		}
	}))
}
