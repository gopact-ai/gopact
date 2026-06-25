package gopact

import (
	"encoding/json"
	"testing"
)

func TestArtifactRefJSONRoundTrip(t *testing.T) {
	ref := ArtifactRef{
		ID:       "artifact-1",
		Name:     "trace.json",
		URI:      "file:///tmp/trace.json",
		MIMEType: "application/json",
		Scope:    ArtifactScopeRun,
		Metadata: map[string]any{"kind": "trace"},
	}

	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got ArtifactRef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.ID != ref.ID || got.Scope != ArtifactScopeRun || got.Metadata["kind"] != "trace" {
		t.Fatalf("round trip = %+v, want %+v", got, ref)
	}
}

func TestArtifactCarriesPayloadAndRef(t *testing.T) {
	artifact := Artifact{
		Ref:     ArtifactRef{ID: "artifact-1", Scope: ArtifactScopeThread},
		Content: []byte("payload"),
	}

	if artifact.Ref.ID != "artifact-1" || string(artifact.Content) != "payload" {
		t.Fatalf("artifact = %+v", artifact)
	}
}
