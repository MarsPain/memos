package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

func upsertTestingAIIndexGeneration(ctx context.Context, t *testing.T, ts *store.Store, fingerprint string, state string) *store.AIIndexGeneration {
	t.Helper()
	generation, err := ts.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
		Fingerprint:      fingerprint,
		ProviderID:       "provider-1",
		ProviderType:     "OPENAI",
		EndpointIdentity: "api.openai.com",
		Model:            "text-embedding-3-small",
		Dimensions:       1536,
		State:            state,
		MemoTotal:        10,
		MemoIndexed:      4,
		LastError:        "",
	})
	require.NoError(t, err)
	return generation
}

func TestAIIndexGeneration(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	generation := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-alpha", "BUILDING")
	require.NotZero(t, generation.ID)
	require.Equal(t, "fp-alpha", generation.Fingerprint)
	require.Equal(t, "provider-1", generation.ProviderID)
	require.Equal(t, "OPENAI", generation.ProviderType)
	require.Equal(t, "api.openai.com", generation.EndpointIdentity)
	require.Equal(t, "text-embedding-3-small", generation.Model)
	require.Equal(t, int32(1536), generation.Dimensions)
	require.Equal(t, "BUILDING", generation.State)
	require.Equal(t, int32(10), generation.MemoTotal)
	require.Equal(t, int32(4), generation.MemoIndexed)
	require.Equal(t, "", generation.LastError)
	require.NotZero(t, generation.CreatedTs)
	require.NotZero(t, generation.UpdatedTs)

	t.Run("get by fingerprint", func(t *testing.T) {
		found, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &generation.Fingerprint})
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, generation.ID, found.ID)

		missing := "fp-missing"
		notFound, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &missing})
		require.NoError(t, err)
		require.Nil(t, notFound)
	})

	t.Run("upsert replaces the generation of the same fingerprint", func(t *testing.T) {
		replaced, err := ts.UpsertAIIndexGeneration(ctx, &store.AIIndexGeneration{
			Fingerprint:      "fp-alpha",
			ProviderID:       "provider-2",
			ProviderType:     "GEMINI",
			EndpointIdentity: "generativelanguage.googleapis.com",
			Model:            "gemini-embedding-001",
			Dimensions:       768,
			State:            "ACTIVE",
			MemoTotal:        12,
			MemoIndexed:      12,
			LastError:        "boom",
		})
		require.NoError(t, err)
		require.Equal(t, generation.ID, replaced.ID)

		found, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &generation.Fingerprint})
		require.NoError(t, err)
		require.Equal(t, "provider-2", found.ProviderID)
		require.Equal(t, "GEMINI", found.ProviderType)
		require.Equal(t, "generativelanguage.googleapis.com", found.EndpointIdentity)
		require.Equal(t, "gemini-embedding-001", found.Model)
		require.Equal(t, int32(768), found.Dimensions)
		require.Equal(t, "ACTIVE", found.State)
		require.Equal(t, int32(12), found.MemoTotal)
		require.Equal(t, int32(12), found.MemoIndexed)
		require.Equal(t, "boom", found.LastError)

		// Still a single generation for the fingerprint.
		list, err := ts.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
		require.NoError(t, err)
		require.Len(t, list, 1)
	})

	t.Run("list filters by state and id", func(t *testing.T) {
		second := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-beta", "RETIRED")
		upsertTestingAIIndexGeneration(ctx, t, ts, "fp-gamma", "BUILDING")

		state := "BUILDING"
		building, err := ts.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &state})
		require.NoError(t, err)
		require.Len(t, building, 1)
		require.Equal(t, "fp-gamma", building[0].Fingerprint)

		found, err := ts.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{ID: &second.ID})
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.Equal(t, "fp-beta", found[0].Fingerprint)

		all, err := ts.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{})
		require.NoError(t, err)
		require.Len(t, all, 3)
		require.Less(t, all[0].ID, all[1].ID)
		require.Less(t, all[1].ID, all[2].ID)
	})

	t.Run("delete removes the generation and is idempotent", func(t *testing.T) {
		require.NoError(t, ts.DeleteAIIndexGeneration(ctx, &store.DeleteAIIndexGeneration{ID: &generation.ID}))
		found, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{Fingerprint: &generation.Fingerprint})
		require.NoError(t, err)
		require.Nil(t, found)

		// Deleting a missing generation is not an error.
		require.NoError(t, ts.DeleteAIIndexGeneration(ctx, &store.DeleteAIIndexGeneration{ID: &generation.ID}))
		missing := int32(999)
		require.NoError(t, ts.DeleteAIIndexGeneration(ctx, &store.DeleteAIIndexGeneration{ID: &missing}))
	})
}

func TestAIIndexGenerationRetiredTsRoundTrip(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	generation := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-retired", "RETIRED")
	require.Zero(t, generation.RetiredTs)

	// The retirement timestamp survives the upsert/list round trip.
	generation.RetiredTs = 1700000123
	_, err := ts.UpsertAIIndexGeneration(ctx, generation)
	require.NoError(t, err)
	found, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &generation.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1700000123), found.RetiredTs)
}

func TestPromoteAIIndexGeneration(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	t.Run("promotes the builder and retires the former active atomically", func(t *testing.T) {
		serving := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-serving", "ACTIVE")
		builder := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-builder", "BUILDING")

		promoted, err := ts.PromoteAIIndexGeneration(ctx, &store.AIIndexGenerationPromotion{
			ID:          builder.ID,
			Fingerprint: builder.Fingerprint,
			RetiredTs:   1700000000,
		})
		require.NoError(t, err)
		require.True(t, promoted)

		// Exactly one generation is active: the promoted builder.
		activeState := "ACTIVE"
		actives, err := ts.ListAIIndexGenerations(ctx, &store.FindAIIndexGeneration{State: &activeState})
		require.NoError(t, err)
		require.Len(t, actives, 1)
		require.Equal(t, builder.ID, actives[0].ID)

		// The former active retired with the recorded timestamp.
		retired, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &serving.ID})
		require.NoError(t, err)
		require.Equal(t, "RETIRED", retired.State)
		require.Equal(t, int64(1700000000), retired.RetiredTs)
	})

	t.Run("promotion is conditional on the desired fingerprint", func(t *testing.T) {
		serving := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-conditional-serving", "ACTIVE")
		stale := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-stale", "BUILDING")

		// The desired fingerprint moved on mid-build: no promotion, no cutover.
		promoted, err := ts.PromoteAIIndexGeneration(ctx, &store.AIIndexGenerationPromotion{
			ID:          stale.ID,
			Fingerprint: "fp-desired-now",
			RetiredTs:   1700000000,
		})
		require.NoError(t, err)
		require.False(t, promoted)

		unchanged, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &stale.ID})
		require.NoError(t, err)
		require.Equal(t, "BUILDING", unchanged.State)
		stillServing, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &serving.ID})
		require.NoError(t, err)
		require.Equal(t, "ACTIVE", stillServing.State)
	})

	t.Run("a generation that is not building does not promote", func(t *testing.T) {
		retiredRow := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-retired-row", "RETIRED")
		promoted, err := ts.PromoteAIIndexGeneration(ctx, &store.AIIndexGenerationPromotion{
			ID:          retiredRow.ID,
			Fingerprint: retiredRow.Fingerprint,
			RetiredTs:   1700000000,
		})
		require.NoError(t, err)
		require.False(t, promoted)

		unchanged, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &retiredRow.ID})
		require.NoError(t, err)
		require.Equal(t, "RETIRED", unchanged.State)
	})

	t.Run("promotion succeeds with no former active generation", func(t *testing.T) {
		builder := upsertTestingAIIndexGeneration(ctx, t, ts, "fp-first", "BUILDING")
		promoted, err := ts.PromoteAIIndexGeneration(ctx, &store.AIIndexGenerationPromotion{
			ID:          builder.ID,
			Fingerprint: builder.Fingerprint,
			RetiredTs:   1700000000,
		})
		require.NoError(t, err)
		require.True(t, promoted)

		found, err := ts.GetAIIndexGeneration(ctx, &store.FindAIIndexGeneration{ID: &builder.ID})
		require.NoError(t, err)
		require.Equal(t, "ACTIVE", found.State)
		require.Zero(t, found.RetiredTs)
	})
}
