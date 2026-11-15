package knowledge

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestBuildKnowledgeRetrieveChain_Compile(t *testing.T) {
	r := NewRetriever(nil, nil, &RetrievalConfig{TopK: 3, SimilarityThreshold: 0.5}, zap.NewNop())
	_, err := BuildKnowledgeRetrieveChain(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildKnowledgeRetrieveChain_NilRetriever(t *testing.T) {
	_, err := BuildKnowledgeRetrieveChain(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil retriever")
	}
}
