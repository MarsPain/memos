package search

import (
	"context"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/store"
)

func TestIndexerSweepRepairsDroppedSignal(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "owl", "the owl hunts at dusk")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	generation := activeGeneration(ctx, t, fixture.store)
	require.Len(t, listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID), 1)

	// The memo changes with no invalidation signal at all: the derived
	// document is rewritten directly at a new revision, as if every signal
	// had been dropped.
	document := fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	document.Content = "kitchen simmer tomato sauce"
	document.ContentHash = contentHash(document.Content)
	document.MemoUpdatedTs = m.UpdatedTs + 100
	_, err := fixture.store.UpsertAISearchDocument(ctx, document)
	require.NoError(t, err)

	// The next reconciliation sweep repairs the dropped signal: the chunks
	// are rebuilt from the new revision and the superseded ones are gone.
	require.NoError(t, indexer.RunOnce(ctx))
	chunks := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Len(t, chunks, 1)
	require.Equal(t, document.MemoUpdatedTs, chunks[0].MemoRevision)
}

func TestIndexerResumeAfterRestartSkipsCommittedWork(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	for _, uid := range []string{"m1", "m2", "m3", "m4"} {
		fixture.createMemo(ctx, t, uid, "note "+uid)
	}
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)

	// The first worker dies mid-sweep: its context is cancelled after the
	// first embedding call committed one document's chunks.
	interruptCtx, cancel := context.WithCancel(ctx)
	calls := 0
	interruptModel := &aitest.Model{EmbedFunc: func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		calls++
		if calls > 1 {
			cancel()
			return internalai.EmbeddingResponse{}, interruptCtx.Err()
		}
		return bucketEmbedFunc(request)
	}}
	first := NewIndexer(fixture.store, embedModelFactory(interruptModel))
	require.ErrorIs(t, first.RunOnce(interruptCtx), context.Canceled)

	committed, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{})
	require.NoError(t, err)
	require.Len(t, committed, 1, "one document committed before the crash")

	// A fresh worker after the restart resumes from the stored chunks alone:
	// it embeds only the remaining documents, never redoing committed work.
	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	embedded := 0
	for _, request := range model.EmbeddingRequests {
		embedded += len(request.Inputs)
	}
	require.Equal(t, 3, embedded, "only the documents not committed before the crash are embedded")

	generation := activeGeneration(ctx, t, fixture.store)
	require.Equal(t, int32(4), generation.MemoTotal)
	require.Equal(t, int32(4), generation.MemoIndexed)
	chunks, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generation.ID})
	require.NoError(t, err)
	require.Len(t, chunks, 4, "every memo indexed exactly once, with no duplicate work")
}

func TestIndexerPoisonMemoBacksOff(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	good1 := fixture.createMemo(ctx, t, "good-owl", "the owl hunts at dusk")
	poison := fixture.createMemo(ctx, t, "poison", "this poison pill never embeds")
	good2 := fixture.createMemo(ctx, t, "good-kitchen", "simmer the tomato sauce")
	fixture.service.RunOnce(ctx)

	poisonAttempts := 0
	model := &aitest.Model{EmbedFunc: func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		for _, input := range request.Inputs {
			if strings.Contains(input, "poison") {
				poisonAttempts++
				return internalai.EmbeddingResponse{}, errors.New("provider rejects the input")
			}
		}
		return bucketEmbedFunc(request)
	}}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	// The poison memo backs off alone: the healthy corpus is indexed, the
	// generation records the normalized failure category, and the partial
	// index stays building (Scaffold C never serves a partial index).
	generations, err := fixture.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
	require.NoError(t, err)
	require.Len(t, generations, 1)
	generation := generations[0]
	require.Equal(t, generationStateBuilding, generation.State)
	require.Equal(t, int32(3), generation.MemoTotal)
	require.Equal(t, int32(2), generation.MemoIndexed)
	require.NotEmpty(t, generation.LastError)
	require.NotEmpty(t, listMemoChunks(ctx, t, fixture.store, generation.ID, good1.ID))
	require.NotEmpty(t, listMemoChunks(ctx, t, fixture.store, generation.ID, good2.ID))
	require.Empty(t, listMemoChunks(ctx, t, fixture.store, generation.ID, poison.ID))
	require.Equal(t, 1, poisonAttempts)

	// An immediate second sweep does not hot-loop on the poison memo: its
	// backoff window is still open, so it is not retried, while the healthy
	// corpus stays current without re-embedding.
	require.NoError(t, indexer.RunOnce(ctx))
	require.Equal(t, 1, poisonAttempts)
}

func TestIndexerSyncMemosDeletesChunksAcrossGenerations(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	keep := fixture.createMemo(ctx, t, "keep", "the owl hunts at dusk")
	drop := fixture.createMemo(ctx, t, "drop", "simmer the tomato sauce")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	generation := activeGeneration(ctx, t, fixture.store)
	require.NotEmpty(t, listMemoChunks(ctx, t, fixture.store, generation.ID, drop.ID))

	// A second generation also holds chunks of the dropped memo (for example
	// a generation that has not been swept yet).
	other, err := fixture.store.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
		Fingerprint: "other-fingerprint",
		ProviderID:  "p",
		Model:       "embed-model",
		Dimensions:  semanticTestDimensions,
		State:       generationStateActive,
	})
	require.NoError(t, err)
	_, err = fixture.store.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
		GenerationID: other.ID,
		MemoID:       drop.ID,
		MemoRevision: drop.UpdatedTs,
		ChunkOrdinal: 0,
		Vector:       []byte{1, 2, 3, 4},
		Dimensions:   1,
	})
	require.NoError(t, err)

	// The memo leaves the corpus: the reconciler removed its document.
	require.NoError(t, fixture.store.DeleteMemo(ctx, &store.DeleteMemo{ID: drop.ID}))
	require.NoError(t, fixture.store.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: drop.ID}))
	require.NoError(t, indexer.SyncMemos(ctx, []int32{drop.ID}))

	// The dropped memo's derived chunks are gone from every generation; the
	// kept memo's chunks are untouched.
	chunks, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &drop.ID})
	require.NoError(t, err)
	require.Empty(t, chunks)
	require.NotEmpty(t, listMemoChunks(ctx, t, fixture.store, generation.ID, keep.ID))
}

func TestIndexerDetectsContentHashDriftWithoutRevisionBump(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "owl", "the owl hunts at dusk")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))
	embedCalls := len(model.EmbeddingRequests)
	require.NotZero(t, embedCalls)

	// The document is rewritten in place: same revision, new content hash.
	// Comparing revisions alone would leave the stale chunks forever.
	document := fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	document.Content = "kitchen simmer tomato sauce"
	document.ContentHash = contentHash(document.Content)
	_, err := fixture.store.UpsertAISearchDocument(ctx, document)
	require.NoError(t, err)

	require.NoError(t, indexer.RunOnce(ctx))
	require.Greater(t, len(model.EmbeddingRequests), embedCalls, "hash drift alone re-embeds the document")
	generation := activeGeneration(ctx, t, fixture.store)
	chunks := listMemoChunks(ctx, t, fixture.store, generation.ID, m.ID)
	require.Len(t, chunks, 1)
	require.Equal(t, document.ContentHash, chunks[0].ContentHash)
}

func TestSemanticPathDropsStaleMatchesUntilReindexed(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	m := fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	indexer := NewIndexer(fixture.store, embedModelFactory(model))
	require.NoError(t, indexer.RunOnce(ctx))

	// The memo is edited and its document refreshed, but the indexer has not
	// caught up: the stored chunks are built from the superseded revision.
	updated := "# Kitchen\n\nSimmer the tomato sauce slowly."
	updatedTs := m.UpdatedTs + 100
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, Content: &updated, UpdatedTs: &updatedTs}))
	fixture.service.RunOnce(ctx)

	// In the interim the semantic path must not serve the stale chunks: the
	// meaning-based query matches nothing instead of ranking on outdated
	// vectors.
	matches, err := NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "nocturnal predator")
	require.NoError(t, err)
	require.Empty(t, matches)

	// Once the runner catches up, the fresh chunks serve the query.
	require.NoError(t, indexer.RunOnce(ctx))
	matches, err = NewSemanticSearcher(fixture.store, embedModelFactory(model)).Search(ctx, "nocturnal predator")
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	require.Equal(t, m.ID, matches[0].memoID)
}

func TestIndexerProviderOutageLeavesLexicalSearchUnaffected(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	model := &aitest.Model{EmbeddingError: errors.New("provider is down")}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The outage is recorded as a normalized error category on the
	// generation, which stays building and never serves queries.
	generations, err := fixture.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
	require.NoError(t, err)
	require.Len(t, generations, 1)
	require.Equal(t, generationStateBuilding, generations[0].State)
	require.NotEmpty(t, generations[0].LastError)

	// Lexical search is unaffected by the embedding provider outage.
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
}
