package search

import (
	"cmp"
	"context"
	"hash/fnv"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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
	matches, reason, err := NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "owl", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, matches)
	require.Equal(t, ReasonSemanticRebuilding, reason)
}

func TestSemanticServesPreviousGenerationAcrossModelChange(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The embedding model changed but the replacement has not built yet: the
	// previous active generation remains callable — its provider type and
	// endpoint identity are still in the pool — so it keeps serving, embedded
	// with the model the generation recorded. A building generation under the
	// new fingerprint never serves.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "other-model")

	matches, reason, err := NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "nocturnal predator", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotEmpty(t, matches)
	require.Equal(t, "embed-model", model.EmbeddingRequests[len(model.EmbeddingRequests)-1].Model)
}

// longOwlMemoContent builds a memo that projects to a multi-chunk document:
// filler paragraphs in one semantic bucket, then many owl paragraphs in
// another, so the owl text lands in the later chunks.
func longOwlMemoContent() string {
	paragraphs := []string{}
	for i := 0; i < 60; i++ {
		paragraphs = append(paragraphs, "kitchen simmer tomato sauce")
	}
	for i := 0; i < 100; i++ {
		paragraphs = append(paragraphs, "The owl hunts field mice at dusk.")
	}
	return strings.Join(paragraphs, "\n\n")
}

// activeGeneration returns the test's single active generation.
func activeGeneration(ctx context.Context, t *testing.T, st *store.Store) *store.AIIndexGeneration {
	t.Helper()
	state := generationStateActive
	generation, err := st.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{State: &state})
	require.NoError(t, err)
	require.NotNil(t, generation)
	return generation
}

// listMemoChunks returns the memo's index chunk rows in chunk-ordinal order.
func listMemoChunks(ctx context.Context, t *testing.T, st *store.Store, generationID, memoID int32) []*store.AIIndexChunk {
	t.Helper()
	chunks, err := st.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &memoID})
	require.NoError(t, err)
	slices.SortFunc(chunks, func(a, b *store.AIIndexChunk) int { return cmp.Compare(a.ChunkOrdinal, b.ChunkOrdinal) })
	return chunks
}

func TestIndexerEmbedsEveryChunkOfLongMemo(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "long", longOwlMemoContent())
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	document := fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	require.Greater(t, len(document.Content), chunkTargetContentBytes, "the memo projects to a multi-chunk document")

	generation := activeGeneration(ctx, t, fixture.store)
	chunks := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Greater(t, len(chunks), 1, "a long memo indexes as multiple chunks")

	// The stored chunks are exactly the deterministic chunker's output:
	// sequential ordinals over contiguous content ranges covering the whole
	// document, each at the memo's current revision.
	position := 0
	for i, chunk := range chunks {
		require.Equal(t, int32(i), chunk.ChunkOrdinal)
		require.Equal(t, position, int(chunk.ContentStart))
		require.Equal(t, m.UpdatedTs, chunk.MemoRevision)
		require.Equal(t, int32(semanticTestDimensions), chunk.Dimensions)
		position = int(chunk.ContentEnd)
	}
	require.Equal(t, len(document.Content), position)

	// The last chunk covers only owl paragraphs, and its stored source span
	// points at the owl region of the source memo.
	last := chunks[len(chunks)-1]
	require.Contains(t, document.Content[last.ContentStart:], "owl hunts field mice")
	owlRegionStart := strings.Index(longOwlMemoContent(), "The owl hunts field mice")
	require.GreaterOrEqual(t, last.SourceStart, int32(owlRegionStart))
	require.LessOrEqual(t, last.SourceEnd, int32(len(longOwlMemoContent())))

	// Every chunk was embedded exactly once.
	embedded := 0
	for _, request := range model.EmbeddingRequests {
		embedded += len(request.Inputs)
	}
	require.Equal(t, len(chunks), embedded)
}

func TestSemanticHitOnLaterChunkMapsToSourceSpan(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "long", longOwlMemoContent())
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The query matches only the owl passage's bucket, which lives in the
	// later chunks: the semantic hit maps back to the owl region of the
	// source for its snippet.
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Equal(t, "long", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
	require.Contains(t, outcome.Hits[0].Snippet, "owl hunts field mice")
}

func TestIndexerReplacesStaleChunksOnMemoEdit(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "long", "the owl hunts at dusk")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))
	firstRevision := m.UpdatedTs

	// The memo grows into a multi-chunk document at a new source revision
	// (the memo API bumps UpdatedTs on content updates).
	updated := longOwlMemoContent()
	updatedTs := time.Now().Unix() + 1
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, Content: &updated, UpdatedTs: &updatedTs}))
	fixture.service.RunOnce(ctx)
	require.NoError(t, indexer.RunOnce(ctx))

	// Every stored chunk comes from the new revision; no chunk of the
	// superseded revision survives to map a hit to an outdated source span.
	generation := activeGeneration(ctx, t, fixture.store)
	chunks := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Greater(t, len(chunks), 1)
	document := fixture.getDocument(ctx, t, m.ID)
	require.NotEqual(t, firstRevision, document.MemoUpdatedTs)
	for _, chunk := range chunks {
		require.Equal(t, document.MemoUpdatedTs, chunk.MemoRevision)
	}
	require.Equal(t, len(chunkDocument(document)), len(chunks))
}

func TestIndexerReRunKeepsChunkOrdinalsAndSpans(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "long", longOwlMemoContent())
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	generation := activeGeneration(ctx, t, fixture.store)
	before := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Greater(t, len(before), 1)
	embedCalls := len(model.EmbeddingRequests)

	// A repeated pass over unchanged content embeds nothing, and the chunk
	// ordinals and spans stay identical.
	require.NoError(t, indexer.RunOnce(ctx))
	require.Len(t, model.EmbeddingRequests, embedCalls)
	after := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Len(t, after, len(before))
	for i := range before {
		require.Equal(t, before[i].ChunkOrdinal, after[i].ChunkOrdinal)
		require.Equal(t, before[i].ContentStart, after[i].ContentStart)
		require.Equal(t, before[i].ContentEnd, after[i].ContentEnd)
		require.Equal(t, before[i].SourceStart, after[i].SourceStart)
		require.Equal(t, before[i].SourceEnd, after[i].SourceEnd)
	}
}
