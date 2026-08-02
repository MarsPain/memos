package search

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

// semanticTestEndpoint is the provider endpoint every semantic test
// configures.
const semanticTestEndpoint = "https://embed.example.com/v1"

// upsertEmbeddingSetting rewrites the instance AI setting with the given
// provider endpoint and embedding model. An empty model disables the
// embedding capability while leaving the provider configured.
func upsertEmbeddingSetting(ctx context.Context, t *testing.T, st *store.Store, endpoint, model string) {
	t.Helper()
	require.NoError(t, upsertEmbeddingSettingE(ctx, st, endpoint, model))
}

// upsertEmbeddingSettingE is upsertEmbeddingSetting for use inside embedding
// fakes, where a testing.T failure cannot fail the test directly.
func upsertEmbeddingSettingE(ctx context.Context, st *store.Store, endpoint, model string) error {
	var embedding *storepb.EmbeddingConfig
	if model != "" {
		embedding = &storepb.EmbeddingConfig{ProviderId: "p", Model: model, Dimensions: semanticTestDimensions}
	}
	_, err := st.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI,
		Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
			Providers: []*storepb.AIProviderConfig{{
				Id: "p", Title: "P", Type: storepb.AIProviderType_OPENAI,
				Endpoint: endpoint, ApiKey: "sk-test",
			}},
			Embedding: embedding,
		}},
	})
	return err
}

// listGenerations returns every index generation in ascending ID order.
func listGenerations(ctx context.Context, t *testing.T, st *store.Store) []*store.AIIndexGeneration {
	t.Helper()
	generations, err := st.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
	require.NoError(t, err)
	return generations
}

// requireOnlyActiveGeneration asserts exactly one generation is ACTIVE and
// returns it: the cutover never leaves zero or two active generations.
func requireOnlyActiveGeneration(ctx context.Context, t *testing.T, st *store.Store) *store.AIIndexGeneration {
	t.Helper()
	activeState := generationStateActive
	actives, err := st.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &activeState})
	require.NoError(t, err)
	require.Len(t, actives, 1, "exactly one generation may be active")
	return actives[0]
}

// failingEmbedFunc returns an embed function that fails while disabled is
// false, letting a test fail one build phase and recover the next.
func failingEmbedFunc(failing *bool) func(internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
	return func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		if *failing {
			return internalai.EmbeddingResponse{}, errors.New("provider overloaded")
		}
		return bucketEmbedFunc(request)
	}
}

func TestCutoverKeepsPreviousGenerationServingDuringRebuild(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.createMemo(ctx, t, "cooking", "# Kitchen\n\nSimmer the tomato sauce slowly.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	first := activeGeneration(ctx, t, fixture.store)

	// The embedding model changes: the replacement generation builds under a
	// new fingerprint while the previous active generation keeps serving.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "embed-model-v2")
	failing := true
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(&aitest.Model{EmbedFunc: failingEmbedFunc(&failing)})).RunOnce(ctx))

	// The replacement never finished its verification pass: it stays building
	// with no chunks, and the previous generation is still the only active one.
	replacement := listGenerations(ctx, t, fixture.store)
	require.Len(t, replacement, 2)
	serve := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.Equal(t, first.ID, serve.ID)
	buildingState := generationStateBuilding
	builders, err := fixture.store.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &buildingState})
	require.NoError(t, err)
	require.Len(t, builders, 1)
	require.NotEqual(t, first.Fingerprint, builders[0].Fingerprint)

	// Rebuild isolation: the semantic query is served by the previous
	// generation, embedded with the model that generation recorded, never by
	// the building replacement.
	searcher := NewSemanticSearcher(fixture.store, embedModelFactory(model))
	matches, reason, err := searcher.Search(ctx, "nocturnal predator", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotEmpty(t, matches)
	lastRequest := model.EmbeddingRequests[len(model.EmbeddingRequests)-1]
	require.Equal(t, "embed-model", lastRequest.Model, "the query embeds with the serving generation's model")

	// The replacement finishes building and promotes atomically: it becomes
	// the only active generation and the former retires with a timestamp.
	failing = false
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	promoted := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.NotEqual(t, first.ID, promoted.ID)
	retired, err := fixture.store.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &first.ID})
	require.NoError(t, err)
	require.Equal(t, generationStateRetired, retired.State)
	require.NotZero(t, retired.RetiredTs)

	// Queries now embed with the new generation's model.
	matches, reason, err = searcher.Search(ctx, "nocturnal predator", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotEmpty(t, matches)
	lastRequest = model.EmbeddingRequests[len(model.EmbeddingRequests)-1]
	require.Equal(t, "embed-model-v2", lastRequest.Model)
}

func TestPromotionSkippedWhenDesiredFingerprintChangesMidBuild(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)

	// The admin changes the embedding model while the build is running.
	var flipErr error
	flipped := false
	flipModel := &aitest.Model{EmbedFunc: func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		if !flipped {
			flipped = true
			flipErr = upsertEmbeddingSettingE(ctx, fixture.store, semanticTestEndpoint, "embed-model-v2")
		}
		return bucketEmbedFunc(request)
	}}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(flipModel)).RunOnce(ctx))
	require.NoError(t, flipErr)

	// The verification pass completed but promotion did not: the desired
	// fingerprint moved on mid-build, so the stale builder was removed rather
	// than promoted. No generation is active.
	require.Empty(t, listGenerations(ctx, t, fixture.store))

	// The next pass builds the generation the configuration now desires and
	// promotes it.
	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	promoted := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.Equal(t, "embed-model-v2", promoted.Model)
}

func TestNewerFingerprintSupersedesAndRemovesOlderBuilder(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	// The first build never completes: the provider keeps failing, so the
	// building generation holds a partial index.
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	failing := true
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(&aitest.Model{EmbedFunc: failingEmbedFunc(&failing)})).RunOnce(ctx))
	stale := listGenerations(ctx, t, fixture.store)
	require.Len(t, stale, 1)
	require.Equal(t, generationStateBuilding, stale[0].State)

	// The embedding model changes before the build ever completed: the newer
	// desired fingerprint supersedes and removes the older builder outright,
	// chunks included, and its replacement builds and promotes.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "embed-model-v2")
	failing = false
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(&aitest.Model{EmbedFunc: bucketEmbedFunc})).RunOnce(ctx))

	promoted := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.NotEqual(t, stale[0].Fingerprint, promoted.Fingerprint)
	require.Len(t, listGenerations(ctx, t, fixture.store), 1, "the superseded builder is removed")
	chunks, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{})
	require.NoError(t, err)
	for _, chunk := range chunks {
		require.Equal(t, promoted.ID, chunk.GenerationID, "no chunks of the superseded builder survive")
	}
}

func TestDisableMidBuildStopsWorkAndReportsDisabled(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	failing := true
	model := &aitest.Model{EmbedFunc: failingEmbedFunc(&failing)}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	building := listGenerations(ctx, t, fixture.store)
	require.Len(t, building, 1)
	require.Equal(t, generationStateBuilding, building[0].State)

	// Embeddings are disabled mid-build: the runner does no building work and
	// the inert builder never serves.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "")
	embedCalls := len(model.EmbeddingRequests)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	require.Len(t, model.EmbeddingRequests, embedCalls, "no embedding work runs while disabled")
	building = listGenerations(ctx, t, fixture.store)
	require.Len(t, building, 1)
	require.Equal(t, generationStateBuilding, building[0].State)

	// Semantic queries report the disabled state while lexical keeps serving.
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonSemanticDisabled)

	// Re-enabling the same configuration resumes the inert builder: the build
	// completes and promotes.
	failing = false
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "embed-model")
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	promoted := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.Equal(t, building[0].ID, promoted.ID)
}

func TestDisableMidSweepLeavesBuilderInert(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)

	// Embeddings are disabled while the build's sweep is in flight: the pass
	// finishes but never promotes, and the builder stays inert.
	var disableErr error
	disabled := false
	disableModel := &aitest.Model{EmbedFunc: func(request internalai.EmbeddingRequest) (internalai.EmbeddingResponse, error) {
		if !disabled {
			disabled = true
			disableErr = upsertEmbeddingSettingE(ctx, fixture.store, semanticTestEndpoint, "")
		}
		return bucketEmbedFunc(request)
	}}
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(disableModel)).RunOnce(ctx))
	require.NoError(t, disableErr)

	generations := listGenerations(ctx, t, fixture.store)
	require.Len(t, generations, 1)
	require.Equal(t, generationStateBuilding, generations[0].State, "disabled mid-build: the sweep never promotes")
	require.Equal(t, int32(1), generations[0].MemoTotal)
	require.Equal(t, int32(1), generations[0].MemoIndexed, "the completed pass's progress survives the inert builder")
}

func TestRetiredGenerationRemovedAfterGracePeriod(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	// An expired retired generation with leftover chunks, and a freshly
	// retired one. The cleanup runs with or without an embedding assignment.
	expired, err := fixture.store.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
		Fingerprint: "fp-expired",
		ProviderID:  "p",
		Model:       "embed-model",
		Dimensions:  semanticTestDimensions,
		State:       generationStateRetired,
		RetiredTs:   time.Now().Add(-retiredGenerationGracePeriod - time.Minute).Unix(),
	})
	require.NoError(t, err)
	_, err = fixture.store.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
		GenerationID: expired.ID,
		MemoID:       1,
		MemoRevision: 1,
		ChunkOrdinal: 0,
		Vector:       []byte{1, 2, 3, 4},
		Dimensions:   1,
	})
	require.NoError(t, err)
	fresh, err := fixture.store.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
		Fingerprint: "fp-fresh",
		ProviderID:  "p",
		Model:       "embed-model",
		Dimensions:  semanticTestDimensions,
		State:       generationStateRetired,
		RetiredTs:   time.Now().Unix(),
	})
	require.NoError(t, err)

	require.NoError(t, NewIndexer(fixture.store, nil).RunOnce(ctx))

	// The expired generation is gone with its chunks; the fresh one stays.
	generations := listGenerations(ctx, t, fixture.store)
	require.Len(t, generations, 1)
	require.Equal(t, fresh.ID, generations[0].ID)
	chunks, err := fixture.store.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{})
	require.NoError(t, err)
	require.Empty(t, chunks)
}

func TestSemanticReportsRebuildingUntilFirstGenerationPromotes(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	retriever := fixture.newTestRetriever()
	retriever.SetSemanticSearcher(NewSemanticSearcher(fixture.store, embedModelFactory(model)))

	// Embeddings are configured but no complete generation exists yet: lexical
	// serves with the machine-readable rebuilding reason.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonSemanticRebuilding)
	require.NotContains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)

	// Once the generation promotes, semantic serves and the reason is gone.
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.Hits)
	require.NotContains(t, outcome.PartialReasons, ReasonSemanticRebuilding)
	require.Contains(t, outcome.Hits[0].RankReasons, RankReasonSemantic)
}

func TestSemanticNotCallableAfterProviderEndpointChange(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))

	// The provider endpoint changes: the previous generation's endpoint
	// identity is no longer in the pool, so it is not callable. Semantic
	// reports rebuilding and lexical serves until the replacement promotes.
	upsertEmbeddingSetting(ctx, t, fixture.store, "https://embed-v2.example.com/v1", "embed-model")
	searcher := NewSemanticSearcher(fixture.store, embedModelFactory(model))
	matches, reason, err := searcher.Search(ctx, "nocturnal predator", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, matches)
	require.Equal(t, ReasonSemanticRebuilding, reason)

	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	promoted := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.Equal(t, "https://embed-v2.example.com/v1", promoted.EndpointIdentity)
	matches, reason, err = searcher.Search(ctx, "nocturnal predator", semanticScanBudgets{})
	require.NoError(t, err)
	require.Empty(t, reason)
	require.NotEmpty(t, matches)
}

func TestRetiredGenerationResumesOnConfigRevert(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	fixture.createMemo(ctx, t, "owl", "# Field Notes\n\nThe owl hunts field mice at dusk.")
	fixture.service.RunOnce(ctx)

	model := &aitest.Model{EmbedFunc: bucketEmbedFunc}
	configureEmbeddingSetting(ctx, t, fixture.store, semanticTestDimensions)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	first := activeGeneration(ctx, t, fixture.store)

	// The embedding model changes and the replacement promotes; the former
	// generation retires.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "embed-model-v2")
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	requireOnlyActiveGeneration(ctx, t, fixture.store)

	// The configuration reverts inside the grace period: the retired
	// generation resumes building with its still-current chunks as a head
	// start — the revert re-embeds nothing — and promotes again.
	upsertEmbeddingSetting(ctx, t, fixture.store, semanticTestEndpoint, "embed-model")
	embedCalls := len(model.EmbeddingRequests)
	require.NoError(t, NewIndexer(fixture.store, embedModelFactory(model)).RunOnce(ctx))
	require.Len(t, model.EmbeddingRequests, embedCalls, "the reverted generation's chunks are still current")

	restored := requireOnlyActiveGeneration(ctx, t, fixture.store)
	require.Equal(t, first.ID, restored.ID)
	require.Zero(t, restored.RetiredTs)
}
