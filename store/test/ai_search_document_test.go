package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

func upsertTestingAISearchDocument(ctx context.Context, t *testing.T, ts *store.Store, memoID int32, content string) *store.AISearchDocument {
	t.Helper()
	document, err := ts.UpsertAISearchDocument(ctx, &store.AISearchDocument{
		MemoID:               memoID,
		MemoUID:              "memo-uid",
		MemoUpdatedTs:        100,
		ContentHash:          "hash-" + content,
		ProjectionVersion:    1,
		NormalizationVersion: 1,
		Title:                "title " + content,
		Tags:                 []string{"tag-a", "tag-b"},
		Content:              content,
		Spans: []*store.AISearchDocumentSpan{
			{ContentStart: 0, ContentEnd: 5, SourceStart: 2, SourceEnd: 7},
		},
	})
	require.NoError(t, err)
	return document
}

func TestAISearchDocument(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	document := upsertTestingAISearchDocument(ctx, t, ts, 101, "alpha")
	require.NotZero(t, document.ID)
	require.Equal(t, int32(101), document.MemoID)
	require.Equal(t, "memo-uid", document.MemoUID)
	require.Equal(t, int64(100), document.MemoUpdatedTs)
	require.Equal(t, "hash-alpha", document.ContentHash)
	require.Equal(t, int32(1), document.ProjectionVersion)
	require.Equal(t, int32(1), document.NormalizationVersion)
	require.Equal(t, "title alpha", document.Title)
	require.Equal(t, []string{"tag-a", "tag-b"}, document.Tags)
	require.Equal(t, "alpha", document.Content)
	require.Len(t, document.Spans, 1)
	require.Equal(t, &store.AISearchDocumentSpan{ContentStart: 0, ContentEnd: 5, SourceStart: 2, SourceEnd: 7}, document.Spans[0])
	require.NotZero(t, document.CreatedTs)
	require.NotZero(t, document.UpdatedTs)

	t.Run("get by memo id", func(t *testing.T) {
		found, err := ts.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &document.MemoID})
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, document.ID, found.ID)
		require.Equal(t, document.Tags, found.Tags)
		require.Equal(t, document.Spans, found.Spans)

		missing := int32(999)
		notFound, err := ts.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &missing})
		require.NoError(t, err)
		require.Nil(t, notFound)
	})

	t.Run("upsert replaces the document of the same memo", func(t *testing.T) {
		replaced, err := ts.UpsertAISearchDocument(ctx, &store.AISearchDocument{
			MemoID:               101,
			MemoUID:              "memo-uid",
			MemoUpdatedTs:        200,
			ContentHash:          "hash-beta",
			ProjectionVersion:    2,
			NormalizationVersion: 1,
			Title:                "title beta",
			Tags:                 []string{"tag-c"},
			Content:              "beta",
			Spans:                []*store.AISearchDocumentSpan{},
		})
		require.NoError(t, err)
		require.Equal(t, document.ID, replaced.ID)

		found, err := ts.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &document.MemoID})
		require.NoError(t, err)
		require.Equal(t, int64(200), found.MemoUpdatedTs)
		require.Equal(t, "hash-beta", found.ContentHash)
		require.Equal(t, int32(2), found.ProjectionVersion)
		require.Equal(t, []string{"tag-c"}, found.Tags)
		require.Equal(t, "beta", found.Content)
		require.Empty(t, found.Spans)

		// Still a single document for the memo.
		list, err := ts.ListAISearchDocuments(ctx, &store.FindAISearchDocument{})
		require.NoError(t, err)
		require.Len(t, list, 1)
	})

	t.Run("nil tags and spans round trip as empty", func(t *testing.T) {
		bare, err := ts.UpsertAISearchDocument(ctx, &store.AISearchDocument{
			MemoID:               102,
			MemoUID:              "memo-bare",
			MemoUpdatedTs:        1,
			ContentHash:          "hash-bare",
			ProjectionVersion:    1,
			NormalizationVersion: 1,
		})
		require.NoError(t, err)
		require.Empty(t, bare.Title)
		require.Empty(t, bare.Tags)
		require.Empty(t, bare.Content)
		require.Empty(t, bare.Spans)
	})

	t.Run("list paginates by keyset in ascending id order", func(t *testing.T) {
		third := upsertTestingAISearchDocument(ctx, t, ts, 103, "gamma")
		fourth := upsertTestingAISearchDocument(ctx, t, ts, 104, "delta")

		limit := 2
		first, err := ts.ListAISearchDocuments(ctx, &store.FindAISearchDocument{Limit: &limit})
		require.NoError(t, err)
		require.Len(t, first, 2)
		require.Equal(t, document.ID, first[0].ID)

		rest, err := ts.ListAISearchDocuments(ctx, &store.FindAISearchDocument{IDGreaterThan: &first[1].ID})
		require.NoError(t, err)
		require.Len(t, rest, 2)
		require.Equal(t, third.ID, rest[0].ID)
		require.Equal(t, fourth.ID, rest[1].ID)
	})

	t.Run("delete removes the document and is idempotent", func(t *testing.T) {
		require.NoError(t, ts.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: 101}))
		found, err := ts.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &document.MemoID})
		require.NoError(t, err)
		require.Nil(t, found)

		// Deleting a missing document is not an error.
		require.NoError(t, ts.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: 101}))
		require.NoError(t, ts.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: 999}))
	})
}
