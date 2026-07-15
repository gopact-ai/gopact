package gopact

import (
	"context"
	"testing"
)

type embeddingFunc func(context.Context, EmbeddingRequest) (EmbeddingResponse, error)

func (fn embeddingFunc) Embed(ctx context.Context, request EmbeddingRequest) (EmbeddingResponse, error) {
	return fn(ctx, request)
}

type modelCatalogFunc func(context.Context) (ModelList, error)

func (fn modelCatalogFunc) ListModels(ctx context.Context) (ModelList, error) {
	return fn(ctx)
}

func TestOptionalModelCapabilitiesAreConsumerCheckable(t *testing.T) {
	var embedder any = embeddingFunc(func(context.Context, EmbeddingRequest) (EmbeddingResponse, error) {
		return EmbeddingResponse{Embeddings: []Embedding{{Index: 0, Vector: []float32{1, 2}}}}, nil
	})
	if _, ok := embedder.(Embedder); !ok {
		t.Fatal("embedding implementation does not satisfy Embedder")
	}

	var catalog any = modelCatalogFunc(func(context.Context) (ModelList, error) {
		return ModelList{Models: []ModelInfo{{ID: "model-1"}}}, nil
	})
	if _, ok := catalog.(ModelCatalog); !ok {
		t.Fatal("model catalog implementation does not satisfy ModelCatalog")
	}
}
