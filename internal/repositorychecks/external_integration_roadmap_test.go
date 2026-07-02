package repositorychecks

import (
	"encoding/json"
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

	modelEntry := entries["model-providers"]
	for _, integration := range []string{
		"OpenAI",
		"Anthropic",
		"GLM/BigModel",
		"Z.AI",
		"Volcengine Ark",
		"Alibaba DashScope/Model Studio",
		"OpenRouter",
		"OpenAI-compatible",
		"models.dev catalog",
	} {
		if !slices.Contains(modelEntry.Integrations, integration) {
			t.Fatalf("model-providers missing integration %q", integration)
		}
	}
	for _, contract := range []string{
		"provider.Provider",
		"provider.Registry",
		"provider.Router",
		"provider.Capability",
		"ModelRequest",
		"ModelResponse",
		"StreamingModel",
	} {
		if !slices.Contains(modelEntry.CoreContracts, contract) {
			t.Fatalf("model-providers missing core contract %q", contract)
		}
	}
	for _, suite := range []string{
		"provider-profile",
		"model-catalog-hints",
	} {
		if !slices.Contains(modelEntry.ConformanceSuites, suite) {
			t.Fatalf("model-providers missing conformance suite %q", suite)
		}
	}
}

func TestTemplateRoadmapEntriesRequireGraphConformance(t *testing.T) {
	roadmap := loadExternalIntegrationRoadmap(t)
	conformance := loadExtensionConformanceManifest(t)

	templateTargets := map[string]bool{}
	for _, target := range conformance.Targets {
		if target.Kind == "template" {
			templateTargets[target.Name] = true
		}
	}

	for _, entry := range roadmap.Entries {
		if entry.Area != "template" {
			continue
		}
		for _, target := range entry.ExtensionTargets {
			if !templateTargets[target] {
				continue
			}
			if !slices.Contains(entry.ConformanceSuites, "gopacttest-graph-conformance") {
				t.Fatalf("template roadmap entry %q conformance_suites = %#v, want graph conformance", entry.ID, entry.ConformanceSuites)
			}
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

	raw := readFile(t, filepath.Join("doc", "design", "external-integration-roadmap.json"))
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
