package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

func TestRetrieveTopRanking(t *testing.T) {
	memos := []*store.Memo{
		// Word matches only: contains "morning", "routine", and "checklist" scattered.
		{UID: "word-match", Content: "In the morning I stretch. My routine is slow. I never keep a checklist.", UpdatedTs: 300},
		// Full-query match: contains the exact phrase.
		{UID: "full-match", Content: "my morning routine checklist is short", UpdatedTs: 100},
		// No match at all.
		{UID: "no-match", Content: "completely unrelated note", UpdatedTs: 400},
	}

	hits := retrieveTop(memos, "morning routine checklist", 3)
	require.Len(t, hits, 2)
	require.Equal(t, "full-match", hits[0].memo.UID)
	require.Equal(t, "word-match", hits[1].memo.UID)
}

func TestRetrieveTopWordCountThenUpdatedTs(t *testing.T) {
	memos := []*store.Memo{
		// One matching word, newest.
		{UID: "one-word-new", Content: "apples are great", UpdatedTs: 300},
		// Two matching words, oldest.
		{UID: "two-words-old", Content: "apples and bananas", UpdatedTs: 100},
		// One matching word, older.
		{UID: "one-word-old", Content: "apples are red", UpdatedTs: 200},
	}

	hits := retrieveTop(memos, "apples bananas", 3)
	require.Len(t, hits, 3)
	require.Equal(t, "two-words-old", hits[0].memo.UID)
	require.Equal(t, "one-word-new", hits[1].memo.UID)
	require.Equal(t, "one-word-old", hits[2].memo.UID)
}

func TestRetrieveTopRespectsLimit(t *testing.T) {
	memos := []*store.Memo{
		{UID: "a", Content: "hiking trails", UpdatedTs: 1},
		{UID: "b", Content: "hiking boots", UpdatedTs: 2},
		{UID: "c", Content: "hiking poles", UpdatedTs: 3},
		{UID: "d", Content: "hiking maps", UpdatedTs: 4},
	}

	hits := retrieveTop(memos, "hiking", 3)
	require.Len(t, hits, 3)
	// Newest first among equal scores.
	require.Equal(t, "d", hits[0].memo.UID)
	require.Equal(t, "c", hits[1].memo.UID)
	require.Equal(t, "b", hits[2].memo.UID)
}

func TestRetrieveTopNoMatch(t *testing.T) {
	memos := []*store.Memo{
		{UID: "a", Content: "nothing relevant here"},
	}
	require.Empty(t, retrieveTop(memos, "astrophysics", 3))
	require.Empty(t, retrieveTop(memos, "", 3))
	require.Empty(t, retrieveTop(nil, "astrophysics", 3))
}

func TestRetrieveTopIsCaseInsensitive(t *testing.T) {
	memos := []*store.Memo{
		{UID: "a", Content: "I love APPLE pie"},
	}
	hits := retrieveTop(memos, "apple pie", 3)
	require.Len(t, hits, 1)
	require.Contains(t, hits[0].snippet, "APPLE")
}

func TestRetrieveTopIgnoresShortWords(t *testing.T) {
	memos := []*store.Memo{
		{UID: "a", Content: "the cat sat on a mat"},
	}
	// Every query word is shorter than 4 characters, so only a full-query
	// match could score; the phrase itself is absent.
	require.Empty(t, retrieveTop(memos, "cat dog", 3))
}

func TestRetrieveTopSnippet(t *testing.T) {
	t.Run("window around match with ellipses", func(t *testing.T) {
		content := strings.Repeat("lorem ipsum dolor sit amet ", 20) + "NEEDLE" + strings.Repeat(" consectetur adipiscing elit", 20)
		memos := []*store.Memo{{UID: "a", Content: content}}
		hits := retrieveTop(memos, "needle", 3)
		require.Len(t, hits, 1)
		snippet := hits[0].snippet
		require.Contains(t, snippet, "NEEDLE")
		require.True(t, strings.HasPrefix(snippet, "…"))
		require.True(t, strings.HasSuffix(snippet, "…"))
		require.Less(t, len([]rune(snippet)), len([]rune(content)))
	})

	t.Run("no ellipses when the whole memo fits the window", func(t *testing.T) {
		memos := []*store.Memo{{UID: "a", Content: "short note about hiking"}}
		hits := retrieveTop(memos, "hiking", 3)
		require.Len(t, hits, 1)
		require.Equal(t, "short note about hiking", hits[0].snippet)
	})
}
