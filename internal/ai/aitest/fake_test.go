package aitest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
)

func TestFakeModelUsesProviderNeutralInterface(t *testing.T) {
	fake := &aitest.Model{
		GenerateResponse:  ai.GenerationResponse{Content: "answer", FinishReason: ai.FinishStop},
		EmbeddingResponse: ai.EmbeddingResponse{Vectors: [][]float32{{1, 0}}, Dimensions: 2},
	}
	var model ai.Model = fake

	generated, err := model.Generate(context.Background(), ai.GenerationRequest{Model: "model", Messages: []ai.Message{{Role: ai.RoleUser, Content: "question"}}})
	require.NoError(t, err)
	require.Equal(t, "answer", generated.Content)

	embedded, err := model.Embed(context.Background(), ai.EmbeddingRequest{Model: "embedding", Inputs: []string{"text"}, Dimensions: 2})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{1, 0}}, embedded.Vectors)
	require.Len(t, fake.GenerationRequests, 1)
	require.Len(t, fake.EmbeddingRequests, 1)
}
