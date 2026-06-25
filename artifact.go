package gopact

// ArtifactScope describes the lifetime and visibility of an artifact.
type ArtifactScope string

const (
	// ArtifactScope values describe the lifetime and visibility of an artifact.
	ArtifactScopeRun     ArtifactScope = "run"
	ArtifactScopeThread  ArtifactScope = "thread"
	ArtifactScopeSession ArtifactScope = "session"
	ArtifactScopeUser    ArtifactScope = "user"
)

// ArtifactRef is a stable reference to an artifact produced or consumed by a run.
type ArtifactRef struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	URI      string         `json:"uri,omitempty"`
	MIMEType string         `json:"mime_type,omitempty"`
	Size     int64          `json:"size,omitempty"`
	SHA256   string         `json:"sha256,omitempty"`
	Scope    ArtifactScope  `json:"scope,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Artifact is an in-memory artifact payload plus its reference metadata.
type Artifact struct {
	Ref      ArtifactRef    `json:"ref"`
	Content  []byte         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
