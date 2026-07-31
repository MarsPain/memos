package search

import (
	"context"
	"hash/fnv"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/internal/markdown"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
)

// semanticTestDimensions is the embedding size every semantic test uses.
const semanticTestDimensions = 8

// semanticTestBuckets maps words to a shared semantic bucket so the
// deterministic fake treats bucket-sharing words as related: "owl" and
// "nocturnal predator" share bucket 0 without sharing a single token.
var semanticTestBuckets = map[string]int{
	"owl":       0,
	"hunts":     0,
	"nocturnal": 0,
	"predator":  0,
	"kitchen":   1,
	"simmer":    1,
	"tomato":    1,
	"sauce":     1,
}

// bucketEmbedFunc deterministically embeds texts as normalized bag-of-words
// vectors over the semantic buckets; unmapped words hash into a bucket.
func bucketEmbedFunc(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
	vectors := make([][]float32, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		vector := make([]float32, semanticTestDimensions)
		for _, word := range tokenize(normalize(input)) {
			bucket, ok := semanticTestBuckets[word]
			if !ok {
				h := fnv.New32a()
				_, _ = h.Write([]byte(word))
				bucket = int(h.Sum32() % semanticTestDimensions)
			}
			vector[bucket]++
		}
		vectors = append(vectors, vector)
	}
	return internalai.EmbeddingResponse{Vectors: vectors, Dimensions: semanticTestDimensions}, nil
}

// embedModelFactory returns a model-factory indirection that always builds
// the given model.
func embedModelFactory(model internalai.Model) func() gateway.ModelFactory {
	return func() gateway.ModelFactory {
		return func(internalai.ProviderConfig, *http.Client) (internalai.Model, error) {
			return model, nil
		}
	}
}

// configureEmbeddingSetting assigns the embedding capability.
func configureEmbeddingSetting(ctx context.Context, t *testing.T, st *store.Store, dimensions int32) {
	t.Helper()
	_, err := st.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI,
		Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
			Providers: []*storepb.AIProviderConfig{{
				Id: "p", Title: "P", Type: storepb.AIProviderType_OPENAI,
				Endpoint: "https://embed.example.com/v1", ApiKey: "sk-test",
			}},
			Embedding: &storepb.EmbeddingConfig{ProviderId: "p", Model: "embed-model", Dimensions: dimensions},
		}},
	})
	require.NoError(t, err)
}

func (f *searchTestFixture) newTestRetriever() *Retriever {
	retriever := NewRetriever(f.store, memo.NewService(f.store), markdown.NewService(markdown.WithTagExtension()), DefaultBudgets())
	return retriever
}

func TestSemanticRetrievalEndToEnd(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.createMemo(ctx, t, "cooking", "# Kitchen\n\nSimmer the tomato sauce slowly.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)

	// Before indexing there is no callable generation: the meaning-based
	// query finds nothing, and search behaves exactly as Stage 2 lexical.
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)

	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	// The one-shot pass completed: the generation is active with progress.
	activeState := generationStateActive
	generation, err := fixture.store.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{State: &activeState})
	require.NoError(t, err)
	require.NotNil(t, generation)
	require.Equal(t, int32(2), generation.MemoTotal)
	require.Equal(t, int32(2), generation.MemoIndexed)
	require.Equal(t, int32(semanticTestDimensions), generation.Dimensions)
	require.Equal(t, "embed-model", generation.Model)
	require.Equal(t, "https://embed.example.com/v1", generation.EndpointIdentity)
	require.Empty(t, generation.LastError)

	// The meaning-based query shares no token with the memo, yet the memo
	// ranks first through the fused semantic path.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
	// The citation guarantee holds for semantic hits: revision and hash come
	// from the current source memo.
	require.Equal(t, contentHash("# Field Notes\n\nThe owl hunts field mice at dusk."), outcome.Hits[0].SourceHash)
}

func TestSemanticFusionCorroboratesLexicalMatch(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.createMemo(ctx, t, "cooking", "# Kitchen\n\nSimmer the tomato sauce slowly.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))

	// "owl" matches lexically and semantically: one fused result carrying
	// both paths' rank reasons, not a duplicate.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
	require.Contains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
	for _, hit := range outcome.Hits[1:] {
		require.NotEqual(t, "owl", hit.MemoUID)
	}
}

func TestSemanticDisabledWithoutEmbeddingAssignment(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}

	// No embedding assignment: indexing is a no-op and creates nothing.
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	generations, err := fixture.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
	require.NoError(t, err)
	require.Empty(t, generations)

	// Retrieval is unchanged Stage 2 behavior; the provider is never called.
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
	require.NotContains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
	require.Empty(t, model.EmbeddingRequests)
}

func TestIndexerRunOnceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))

	require.NoError(t, indexer.RunOnce(ctx))
	embedCalls := len(model.EmbeddingRequests)
	require.NotZero(t, embedCalls)

	// A second pass finds every chunk current and embeds nothing again.
	require.NoError(t, indexer.RunOnce(ctx))
	require.Len(t, model.EmbeddingRequests, embedCalls)
}

func TestIndexerRejectsInvalidBatchBeforeCommit(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	// The provider answers with the wrong dimensions.
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	model := &aitest.Model{EmbeddingResponse: internalai.EmbeddingResponse{
		Vectors:    [][]float32{{0.5, 0.5}},
		Dimensions: 2,
	}}

	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The batch was rejected: no chunks committed, the generation stays
	// building (a partial index never serves queries) and records the
	// failure category instead of raw provider payloads.
	chunks, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{})
	require.NoError(t, err)
	require.Empty(t, chunks)
	generations, err := fixture.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
	require.NoError(t, err)
	require.Len(t, generations, 1)
	generation := generations[0]
	require.Equal(t, generationStateBuilding, generation.State)
	require.Equal(t, int32(0), generation.MemoIndexed)
	require.NotEmpty(t, generation.LastError)

	// Semantic retrieval is not callable on the building generation.
	matches, err := NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "owl")
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestSemanticSearcherIgnoresForeignFingerprint(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The embedding model changed: the stored generation no longer matches
	// the desired fingerprint, so semantic retrieval is not callable.
	_, err := fixture.store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI,
		Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
			Providers: []*storepb.AIProviderConfig{{
				Id: "p", Title: "P", Type: storepb.AIProviderType_OPENAI,
				Endpoint: "https://embed.example.com/v1", ApiKey: "sk-test",
			}},
			Embedding: &storepb.EmbeddingConfig{ProviderId: "p", Model: "other-model", Dimensions: semanticTestDimensions},
		}},
	})
	require.NoError(t, err)

	matches, err := NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "nocturnal predator")
	require.NoError(t, err)
	require.Empty(t, matches)
}
