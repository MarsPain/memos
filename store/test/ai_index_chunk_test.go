package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

func upsertTestingAIIndexChunk(ctx context.Context, t *testing.T, ts *store.Store, generationID, memoID int32, chunkOrdinal int32) *store.AIIndexChunk {
	t.Helper()
	chunk, err := ts.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
		GenerationID: generationID,
		MemoID:       memoID,
		MemoRevision: 100,
		ChunkOrdinal: chunkOrdinal,
		ContentStart: 0,
		ContentEnd:   16,
		SourceStart:  2,
		SourceEnd:    18,
		Vector:       []byte{1, 2, 3, 4},
		Dimensions:   1,
	})
	require.NoError(t, err)
	return chunk
}

func TestAIIndexChunk(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	chunk := upsertTestingAIIndexChunk(ctx, t, ts, 1, 101, 0)
	require.NotZero(t, chunk.ID)
	require.Equal(t, int32(1), chunk.GenerationID)
	require.Equal(t, int32(101), chunk.MemoID)
	require.Equal(t, int64(100), chunk.MemoRevision)
	require.Equal(t, int32(0), chunk.ChunkOrdinal)
	require.Equal(t, int32(0), chunk.ContentStart)
	require.Equal(t, int32(16), chunk.ContentEnd)
	require.Equal(t, int32(2), chunk.SourceStart)
	require.Equal(t, int32(18), chunk.SourceEnd)
	require.Equal(t, []byte{1, 2, 3, 4}, chunk.Vector)
	require.Equal(t, int32(1), chunk.Dimensions)
	require.NotZero(t, chunk.IndexedTs)

	t.Run("get by id and miss returns nil", func(t *testing.T) {
		found, err := ts.GetAIIndexChunk(ctx, &store.FindAIIndexChunk{ID: &chunk.ID})
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, chunk.ID, found.ID)
		require.Equal(t, chunk.Vector, found.Vector)

		missing := int32(999)
		notFound, err := ts.GetAIIndexChunk(ctx, &store.FindAIIndexChunk{ID: &missing})
		require.NoError(t, err)
		require.Nil(t, notFound)
	})

	t.Run("upsert replaces the chunk of the same conflict key", func(t *testing.T) {
		replaced, err := ts.UpsertAIIndexChunk(ctx, &store.AIIndexChunk{
			GenerationID: 1,
			MemoID:       101,
			MemoRevision: 100,
			ChunkOrdinal: 0,
			ContentStart: 4,
			ContentEnd:   20,
			SourceStart:  6,
			SourceEnd:    22,
			Vector:       []byte{5, 6, 7, 8, 9, 10, 11, 12},
			Dimensions:   2,
		})
		require.NoError(t, err)
		require.Equal(t, chunk.ID, replaced.ID)

		found, err := ts.GetAIIndexChunk(ctx, &store.FindAIIndexChunk{ID: &chunk.ID})
		require.NoError(t, err)
		require.Equal(t, int32(4), found.ContentStart)
		require.Equal(t, int32(20), found.ContentEnd)
		require.Equal(t, int32(6), found.SourceStart)
		require.Equal(t, int32(22), found.SourceEnd)
		require.Equal(t, []byte{5, 6, 7, 8, 9, 10, 11, 12}, found.Vector)
		require.Equal(t, int32(2), found.Dimensions)

		// Still a single chunk for the conflict key.
		generationID, memoID := int32(1), int32(101)
		list, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, MemoID: &memoID})
		require.NoError(t, err)
		require.Len(t, list, 1)
	})

	t.Run("list paginates by keyset in ascending id order", func(t *testing.T) {
		second := upsertTestingAIIndexChunk(ctx, t, ts, 1, 101, 1)
		third := upsertTestingAIIndexChunk(ctx, t, ts, 1, 102, 0)

		generationID := int32(1)
		limit := 2
		first, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, Limit: &limit})
		require.NoError(t, err)
		require.Len(t, first, 2)
		require.Equal(t, chunk.ID, first[0].ID)

		rest, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID, IDGreaterThan: &first[1].ID})
		require.NoError(t, err)
		require.Len(t, rest, 1)
		require.Equal(t, third.ID, rest[0].ID)
		require.Equal(t, second.ID, first[1].ID)
	})

	t.Run("list filters by memo id", func(t *testing.T) {
		memoID := int32(101)
		list, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &memoID})
		require.NoError(t, err)
		require.Len(t, list, 2)
	})

	t.Run("delete by memo id removes only that memo's chunks", func(t *testing.T) {
		memoID := int32(102)
		require.NoError(t, ts.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{MemoID: &memoID}))

		list, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &memoID})
		require.NoError(t, err)
		require.Empty(t, list)

		remainingMemoID := int32(101)
		remaining, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{MemoID: &remainingMemoID})
		require.NoError(t, err)
		require.Len(t, remaining, 2)
	})

	t.Run("delete by generation id and idempotent delete", func(t *testing.T) {
		other := upsertTestingAIIndexChunk(ctx, t, ts, 2, 201, 0)

		generationID := int32(1)
		require.NoError(t, ts.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{GenerationID: &generationID}))
		list, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &generationID})
		require.NoError(t, err)
		require.Empty(t, list)

		// Deleting missing chunks is not an error.
		require.NoError(t, ts.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{GenerationID: &generationID}))

		// Chunks of other generations are untouched.
		otherGenerationID := int32(2)
		remaining, err := ts.ListAIIndexChunks(ctx, &store.FindAIIndexChunk{GenerationID: &otherGenerationID})
		require.NoError(t, err)
		require.Len(t, remaining, 1)
		require.Equal(t, other.ID, remaining[0].ID)

		require.NoError(t, ts.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{ID: &other.ID}))
		require.NoError(t, ts.DeleteAIIndexChunk(ctx, &store.DeleteAIIndexChunk{ID: &other.ID}))
	})
}
