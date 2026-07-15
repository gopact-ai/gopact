package gopact

import "context"

// Embedder is the optional provider-neutral text embedding protocol.
type Embedder interface {
	Embed(context.Context, EmbeddingRequest) (EmbeddingResponse, error)
}

// EmbeddingRequest requests vectors for one or more text inputs.
type EmbeddingRequest struct {
	Model      string
	Input      []string
	Dimensions int
}

// EmbeddingResponse is one normalized embedding result.
type EmbeddingResponse struct {
	Model            string
	Embeddings       []Embedding
	Usage            Usage
	ProviderMetadata map[string]any
}

// Embedding associates one input index with its vector.
type Embedding struct {
	Index  int
	Vector []float32
}

// ModelCatalog is the optional provider-neutral model discovery protocol.
type ModelCatalog interface {
	ListModels(context.Context) (ModelList, error)
}

// ModelList is one provider model catalog snapshot.
type ModelList struct {
	Models           []ModelInfo
	ProviderMetadata map[string]any
}

// ModelInfo describes one discoverable provider model.
type ModelInfo struct {
	ID               string
	DisplayName      string
	Description      string
	OwnedBy          string
	InputModalities  []Modality
	OutputModalities []Modality
	ProviderMetadata map[string]any
}
