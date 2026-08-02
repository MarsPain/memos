package search

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/store"
)

// fuseTestDimensions is the embedding size the fusion tests use: two
// dimensions keep the cosine relationships between the test vectors obvious.
const fuseTestDimensions = 2

// textVectorEmbedFunc deterministically embeds the exact input texts the test
// maps, so lexical and semantic ranks are fully controlled. Chunk texts are
// the projected document contents (one chunk per short memo); the query text
// is the normalized query.
func textVectorEmbedFunc(t *testing.T, vectors map[string][]float32) func(internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
	return func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		out := make([][]float32, 0, len(request.Inputs))
		for _, input := range request.Inputs {
			vector, ok := vectors[input]
			require.True(t, ok, "unexpected embed input %q", input)
			out = append(out, vector)
		}
		return internalai.EmbeddingResponse{Vectors: out, Dimensions: fuseTestDimensions}, nil
	}
}

// newFusedTestRetriever builds a retriever with the semantic path wired to
// the given model.
func (f *searchTestFixture) newFusedTestRetriever(model internalai.Model) *Retriever {
	retriever := f.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(f.store, embedModelFactory(model)))
	return retriever
}

// rankReasonsByUID indexes the outcome's rank reasons by memo UID.
func rankReasonsByUID(outcome *Outcome) map[string][]string {
	reasons := map[string][]string{}
	for _, hit := range outcome.Hits {
		reasons[hit.MemoUID] = hit.RankReasons
	}
	return reasons
}

func TestHybridFusionRanksByOrdinalRankNotScore(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	// The lexical and semantic scales disagree deliberately: "titled" is the
	// strongest lexical match but semantically unrelated, "both" is second
	// lexically and first semantically, "middle" matches only semantically.
	titled := fixture.createMemo(ctx, t, "titled", "# Owl diary\n\nKitchen notes.")
	both := fixture.createMemo(ctx, t, "both", "The owl hunts at dusk.")
	middle := fixture.createMemo(ctx, t, "middle", "Hunts at dawn.")
	fixture.service.RunOnce(ctx)

	vectors := map[string][]float32{
		"owl": {1, 0},
		fixture.getDocument(ctx, t, titled.ID).Content: {0, 1},
		fixture.getDocument(ctx, t, both.ID).Content:   {1, 0},
		fixture.getDocument(ctx, t, middle.ID).Content: {0.6, 0.8},
	}
	model := &aitest.Model{EmbedFunc: textVectorEmbedFunc(t, vectors)}
	configureEmbeddingSetting(ctx, t, fixture.store, fuseTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// Ordinal-rank fusion rewards "both" for ranking on both paths: it leads
	// even though "titled" holds the higher lexical score. Naive interleaving
	// would keep "titled" first.
	outcome, err := fixture.newFusedTestRetriever(model).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.PartialReasons)
	require.Len(t, outcome.Hits, 3)
	require.Equal(t, "both", outcome.Hits[0].MemoUID)
	require.Equal(t, "titled", outcome.Hits[1].MemoUID)
	require.Equal(t, "middle", outcome.Hits[2].MemoUID)

	// Every result discloses the path(s) that produced it.
	reasons := rankReasonsByUID(outcome)
	require.Contains(t, reasons["both"], "content_exact")
	require.Contains(t, reasons["both"], RankReasonLexical)
	require.Contains(t, reasons["both"], RankReasonSemantic)
	require.Contains(t, reasons["titled"], "title_exact")
	require.Contains(t, reasons["titled"], RankReasonLexical)
	require.NotContains(t, reasons["titled"], RankReasonSemantic)
	require.Contains(t, reasons["middle"], RankReasonSemantic)
	require.NotContains(t, reasons["middle"], RankReasonLexical)
}

func TestHybridFusionSemanticOnlyAndLexicalOnlyShareOneList(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	both := fixture.createMemo(ctx, t, "both", "The owl hunts at dusk.")
	lexicalOnly := fixture.createMemo(ctx, t, "lexical-only", "Owl sketches and field drawings.")
	semanticOnly := fixture.createMemo(ctx, t, "semantic-only", "A nocturnal predator watches the field.")
	fixture.service.RunOnce(ctx)

	vectors := map[string][]float32{
		"owl": {1, 0},
		fixture.getDocument(ctx, t, both.ID).Content:         {1, 0},
		fixture.getDocument(ctx, t, lexicalOnly.ID).Content:  {0, 1},
		fixture.getDocument(ctx, t, semanticOnly.ID).Content: {0.8, 0.6},
	}
	model := &aitest.Model{EmbedFunc: textVectorEmbedFunc(t, vectors)}
	configureEmbeddingSetting(ctx, t, fixture.store, fuseTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	outcome, err := fixture.newFusedTestRetriever(model).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.PartialReasons)

	// The memo found by both paths appears exactly once.
	count := 0
	for _, hit := range outcome.Hits {
		if hit.MemoUID == "both" {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.Equal(t, "both", outcome.Hits[0].MemoUID)

	reasons := rankReasonsByUID(outcome)
	require.Contains(t, reasons["lexical-only"], RankReasonLexical)
	require.NotContains(t, reasons["lexical-only"], RankReasonSemantic)
	require.Contains(t, reasons["semantic-only"], RankReasonSemantic)
	require.NotContains(t, reasons["semantic-only"], RankReasonLexical)
	require.Contains(t, reasons["both"], RankReasonLexical)
	require.Contains(t, reasons["both"], RankReasonSemantic)
}

func TestSemanticBudgetExceededDegradesToLexical(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "The owl hunts at dusk.")
	fixture.createMemo(ctx, t, "cooking", "Simmer the tomato sauce slowly.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// A semantic scan budget below one chunk vector can never finish: the
	// incomplete semantic candidate set is discarded outright, never ranked
	// as though it represented the corpus.
	budgets := DefaultBudgets()
	budgets.MaxSemanticScanBytes = 1
	retriever := fixture.newFusedTestRetriever(model)

	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator", Budgets: &budgets})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits, "the discarded semantic set contributes nothing")
	require.Contains(t, outcome.PartialReasons, ReasonSemanticBudgetExceeded)
	require.NotContains(t, outcome.PartialReasons, ReasonTimeBudgetExhausted)

	// Lexical coverage still answers, with Stage 2 rank reasons only.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
	for _, hit := range outcome.Hits {
		require.NotContains(t, hit.RankReasons, RankReasonSemantic)
		require.NotContains(t, hit.RankReasons, RankReasonLexical)
	}
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
	require.Contains(t, outcome.PartialReasons, ReasonSemanticBudgetExceeded)
}

func TestEmbeddingFailureDegradesToLexical(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	// Index with a healthy provider, then take the provider down for queries.
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(&aitest.Model{EmbedFunc: bucketEmbedFunc})).RunOnce(ctx))
	failing := &aitest.Model{EmbeddingError: errors.New("provider is down")}
	retriever := fixture.newFusedTestRetriever(failing)

	// The lexical match still surfaces; the failure is disclosed, never
	// silently presented as complete coverage.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
	require.NotContains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
	require.Contains(t, outcome.PartialReasons, ReasonEmbeddingFailed)

	// A semantic-only query finds nothing while the provider is down.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonEmbeddingFailed)
}

// blockingEmbedModel answers Embed only when its context ends, so the query
// embedding call always runs into its own deadline.
type blockingEmbedModel struct {
	*aitest.Model
}

func (m blockingEmbedModel) Embed(ctx context.Context, _ internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
	<-ctx.Done()
	return internalai.EmbeddingResponse{}, ctx.Err()
}

func TestEmbeddingTimeoutDegradesToLexical(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(&aitest.Model{EmbedFunc: bucketEmbedFunc})).RunOnce(ctx))

	budgets := DefaultBudgets()
	budgets.EmbeddingTimeout = 20 * time.Millisecond
	retriever := fixture.newFusedTestRetriever(blockingEmbedModel{&aitest.Model{}})

	// The bounded embedding call gives up inside the wall clock; lexical
	// results serve with the timeout disclosed.
	start := time.Now()
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.Less(t, time.Since(start), budgets.WallClock)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "owl", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.PartialReasons, ReasonEmbeddingTimeout)
	require.NotContains(t, outcome.PartialReasons, ReasonTimeBudgetExhausted)
}

func TestSemanticPhaseTimeBudgetExhaustionDegradesGracefully(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// A wall clock already expired when the semantic phase would run degrades
	// to the time-budget reason instead of failing the query.
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Hour))
	defer cancel()
	outcome, err := fixture.newFusedTestRetriever(model).Search(expired, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonTimeBudgetExhausted)
}

func TestSemanticCandidatePermissionChangeBetweenIndexAndRetrieval(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	other, err := fixture.store.CreateUser(ctx, &store.User{Username: "other", Role: store.RoleUser, Email: "other@example.com"})
	require.NoError(t, err)
	shared, err := fixture.store.CreateMemo(ctx, &store.Memo{UID: "shared", CreatorID: other.ID, Content: "The owl hunts field mice at dusk.", Visibility: store.Public})
	require.NoError(t, err)
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	retriever := fixture.newFusedTestRetriever(model)

	// Readable at indexing and retrieval time: the semantic-only query finds it.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "shared", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)

	// Flipped to private after indexing: the semantic candidate is
	// reauthorized through the memo service and never returned, while its
	// chunks still exist.
	private := store.Private
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: shared.ID, Visibility: &private}))
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)

	// The creator keeps their read access and still finds it.
	outcome, err = retriever.Search(ctx, other, Query{Text: "nocturnal predator"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "shared", outcome.Hits[0].MemoUID)
}
