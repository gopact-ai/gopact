package gopact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExternalIntegrationRoadmapKeepsProductionIntegrationsOutsideCore(t *testing.T) {
	roadmap := loadExternalIntegrationRoadmap(t)
	conformance := loadExtensionConformanceManifest(t)

	if roadmap.Scope != "production-integrations" {
		t.Fatalf("external integration roadmap scope = %q, want production-integrations", roadmap.Scope)
	}
	if len(roadmap.AllowedRoutes) == 0 {
		t.Fatal("external integration roadmap allowed_routes is empty")
	}
	if slices.Contains(roadmap.AllowedRoutes, "core-repo") {
		t.Fatal("external integration roadmap must not allow core-repo as a production integration route")
	}

	entries := map[string]externalIntegrationRoadmapEntry{}
	referencedExtensionTargets := map[string]bool{}
	conformanceTargets := map[string]bool{}
	for _, target := range conformance.Targets {
		conformanceTargets[target.Name] = true
	}

	for _, entry := range roadmap.Entries {
		if entry.ID == "" {
			t.Fatal("external integration roadmap entry id is empty")
		}
		if entries[entry.ID].ID != "" {
			t.Fatalf("external integration roadmap entry %q is duplicated", entry.ID)
		}
		if entry.Area == "" {
			t.Fatalf("external integration roadmap entry %q area is empty", entry.ID)
		}
		if !isExternalIntegrationArea(entry.Area) {
			t.Fatalf("external integration roadmap entry %q area %q is invalid", entry.ID, entry.Area)
		}
		if !slices.Contains(roadmap.AllowedRoutes, entry.Route) {
			t.Fatalf("external integration roadmap entry %q route %q is not allowed", entry.ID, entry.Route)
		}
		if entry.Route == "core-repo" {
			t.Fatalf("external integration roadmap entry %q routes production integration into core repo", entry.ID)
		}
		if !strings.HasPrefix(entry.TargetRepo, "gopact-") {
			t.Fatalf("external integration roadmap entry %q target_repo %q must use a gopact-* external repo", entry.ID, entry.TargetRepo)
		}
		if len(entry.Integrations) == 0 {
			t.Fatalf("external integration roadmap entry %q integrations is empty", entry.ID)
		}
		if len(entry.CoreContracts) == 0 {
			t.Fatalf("external integration roadmap entry %q core_contracts is empty", entry.ID)
		}
		if len(entry.ConformanceSuites) == 0 {
			t.Fatalf("external integration roadmap entry %q conformance_suites is empty", entry.ID)
		}
		if !entry.HostOwnedConfig {
			t.Fatalf("external integration roadmap entry %q must keep config host-owned", entry.ID)
		}
		if strings.TrimSpace(entry.Rationale) == "" {
			t.Fatalf("external integration roadmap entry %q rationale is empty", entry.ID)
		}
		switch entry.ScaffoldStatus {
		case "ready":
			if len(entry.ExtensionTargets) == 0 {
				t.Fatalf("external integration roadmap entry %q is scaffold ready but has no extension_targets", entry.ID)
			}
			if strings.TrimSpace(entry.ScaffoldPendingReason) != "" {
				t.Fatalf("external integration roadmap entry %q is scaffold ready but still has scaffold_pending_reason", entry.ID)
			}
		case "pending":
			t.Fatalf("external integration roadmap entry %q is still scaffold pending: %s", entry.ID, entry.ScaffoldPendingReason)
		default:
			t.Fatalf("external integration roadmap entry %q scaffold_status = %q, want ready", entry.ID, entry.ScaffoldStatus)
		}
		for _, target := range entry.ExtensionTargets {
			if !conformanceTargets[target] {
				t.Fatalf("external integration roadmap entry %q references extension target %q without extension conformance contract", entry.ID, target)
			}
			referencedExtensionTargets[target] = true
		}
		entries[entry.ID] = entry
	}

	for _, id := range []string{
		"model-providers",
		"checkpoint-backends",
		"turnloop-stores",
		"lease-backends",
		"storage-backends",
		"channel-platforms",
		"observability-exporters",
		"mcp-a2a-transports",
		"devagent-reviewers",
		"agenttool-template",
	} {
		if entries[id].ID == "" {
			t.Fatalf("external integration roadmap missing required entry %q", id)
		}
	}

	for _, integration := range []string{
		"OpenAI",
		"Anthropic",
		"Gemini",
		"OpenRouter",
		"Redis",
		"SQL",
		"S3",
		"GCS",
		"R2",
		"OSS",
		"A2UI",
		"AG-UI",
		"Lark",
		"WebSocket",
		"LangSmith",
		"LangGraph",
		"CI reviewer",
		"model reviewer",
	} {
		if !roadmapIncludesIntegration(roadmap, integration) {
			t.Fatalf("external integration roadmap missing production integration %q", integration)
		}
	}

	for _, target := range conformance.Targets {
		if !referencedExtensionTargets[target.Name] {
			t.Fatalf("external integration roadmap does not reference conformance target %q", target.Name)
		}
	}

	transportEntry := entries["mcp-a2a-transports"]
	if transportEntry.ScaffoldStatus != "ready" {
		t.Fatalf("mcp-a2a-transports scaffold_status = %q, want ready", transportEntry.ScaffoldStatus)
	}
	for _, target := range []string{
		"gopact-adapters-transport-mcp",
		"gopact-adapters-transport-a2a",
	} {
		if !slices.Contains(transportEntry.ExtensionTargets, target) {
			t.Fatalf("mcp-a2a-transports missing extension target %q", target)
		}
		if !conformanceTargets[target] {
			t.Fatalf("mcp-a2a-transports target %q has no extension conformance contract", target)
		}
	}
}

type externalIntegrationRoadmap struct {
	Version       int                               `json:"version"`
	Scope         string                            `json:"scope"`
	AllowedRoutes []string                          `json:"allowed_routes"`
	Entries       []externalIntegrationRoadmapEntry `json:"entries"`
}

type externalIntegrationRoadmapEntry struct {
	ID                    string   `json:"id"`
	Area                  string   `json:"area"`
	Route                 string   `json:"route"`
	TargetRepo            string   `json:"target_repo"`
	Integrations          []string `json:"integrations"`
	ExtensionTargets      []string `json:"extension_targets"`
	CoreContracts         []string `json:"core_contracts"`
	ConformanceSuites     []string `json:"conformance_suites"`
	HostOwnedConfig       bool     `json:"host_owned_config"`
	Rationale             string   `json:"rationale"`
	ScaffoldStatus        string   `json:"scaffold_status"`
	ScaffoldPendingReason string   `json:"scaffold_pending_reason"`
}

func loadExternalIntegrationRoadmap(t *testing.T) externalIntegrationRoadmap {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("docs", "design", "external-integration-roadmap.json"))
	if err != nil {
		t.Fatalf("read external integration roadmap: %v", err)
	}
	var roadmap externalIntegrationRoadmap
	if err := json.Unmarshal(raw, &roadmap); err != nil {
		t.Fatalf("decode external integration roadmap: %v", err)
	}
	if roadmap.Version != 1 {
		t.Fatalf("external integration roadmap version = %d, want 1", roadmap.Version)
	}
	return roadmap
}

func isExternalIntegrationArea(area string) bool {
	switch area {
	case "model",
		"checkpoint",
		"turnloop",
		"lease",
		"storage",
		"channel",
		"observability",
		"transport",
		"template":
		return true
	default:
		return false
	}
}

func roadmapIncludesIntegration(roadmap externalIntegrationRoadmap, integration string) bool {
	for _, entry := range roadmap.Entries {
		if slices.Contains(entry.Integrations, integration) {
			return true
		}
	}
	return false
}
