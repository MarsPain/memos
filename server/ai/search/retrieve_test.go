package search

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
)

func newTestRetriever(fixture *searchTestFixture) *Retriever {
	return NewRetriever(fixture.store, memo.NewService(fixture.store), markdown.NewService(markdown.WithTagExtension()), DefaultBudgets())
}

func TestSearchRanksExactMatches(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "owls", "# Birding\n\nThe owl hunts at dusk. #birds")
	fixture.createMemo(ctx, t, "bats", "Bats navigate by echo.")
	fixture.service.RunOnce(ctx)

	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.PartialReasons)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "owls", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].Snippet, "owl")
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
}

func TestSearchMatchesPartialAndFuzzy(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "substring", "The owls hunt at dusk.")
	fixture.createMemo(ctx, t, "typo", "Field notes on barn owl behavior.")
	fixture.createMemo(ctx, t, "unrelated", "Bats navigate by echo.")
	fixture.service.RunOnce(ctx)

	retriever := newTestRetriever(fixture)

	// "owl" appears only inside the token "owls": a partial match.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.PartialReasons)
	require.Len(t, outcome.Hits, 2)
	require.Equal(t, "typo", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "content_exact")
	require.Equal(t, "substring", outcome.Hits[1].MemoUID)
	require.Contains(t, outcome.Hits[1].RankReasons, "content_partial")

	// "behaviyr" is one substitution away from the token "behavior" and not
	// a substring of it: a fuzzy match.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "behaviyr"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "typo", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "content_fuzzy")
	require.Contains(t, outcome.Hits[0].Snippet, "behavior")
}

func TestSearchRanksTitleAboveContent(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "content-only", "Notes about the owl.")
	fixture.createMemo(ctx, t, "titled", "# Owl diary\n\nField notes.")
	fixture.service.RunOnce(ctx)

	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 2)
	require.Equal(t, "titled", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "title_exact")
	require.Equal(t, "content-only", outcome.Hits[1].MemoUID)
}

func TestSearchMatchesTags(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "tagged", "Field notes. #birds/owls")
	fixture.createMemo(ctx, t, "untagged", "Field notes.")
	fixture.service.RunOnce(ctx)

	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "birds/owls"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "tagged", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.Hits[0].RankReasons, "tag_exact")
}

func TestSearchAppliesStructuredFilters(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	other, err := fixture.store.CreateUser(ctx, &store.User{Username: "other", Role: store.RoleUser, Email: "other@example.com"})
	require.NoError(t, err)
	create := func(uid, content string, visibility store.Visibility, creatorID int32) {
		_, err := fixture.store.CreateMemo(ctx, &store.Memo{UID: uid, CreatorID: creatorID, Content: content, Visibility: visibility})
		require.NoError(t, err)
	}
	create("own-tagged", "The owl hunts. #birds", store.Private, fixture.owner.ID)
	create("own-untagged", "The owl hunts.", store.Private, fixture.owner.ID)
	create("other-public", "The owl hunts.", store.Public, other.ID)
	fixture.service.RunOnce(ctx)

	retriever := newTestRetriever(fixture)

	// Tag filter: only the tagged memo qualifies.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{Tags: []string{"birds"}}})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "own-tagged", outcome.Hits[0].MemoUID)

	// Visibility filter: only the public memo qualifies.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{Visibility: ptr(store.Public)}})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "other-public", outcome.Hits[0].MemoUID)

	// Creator filter: only the other user's memo qualifies.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{CreatorID: &other.ID}})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "other-public", outcome.Hits[0].MemoUID)

	// Time filter: everything was just created, so a past bound matches all
	// three and a future bound matches none.
	past := time.Now().Add(-time.Hour)
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{CreatedAfter: &past}})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 3)
	future := time.Now().Add(time.Hour)
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{CreatedBefore: &future}})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 3)
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Filters: Filters{CreatedAfter: &future}})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
}

func TestSearchNeverReturnsMemoWhosePermissionChanged(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	other, err := fixture.store.CreateUser(ctx, &store.User{Username: "other", Role: store.RoleUser, Email: "other@example.com"})
	require.NoError(t, err)
	memo, err := fixture.store.CreateMemo(ctx, &store.Memo{UID: "shared", CreatorID: other.ID, Content: "The owl hunts.", Visibility: store.Public})
	require.NoError(t, err)
	fixture.service.RunOnce(ctx)

	retriever := newTestRetriever(fixture)

	// Readable at indexing and retrieval time: returned.
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)

	// Flipped to private after indexing: never returned to another user,
	// while the stale document still exists.
	private := store.Private
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: memo.ID, Visibility: &private}))
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)

	// The creator still finds their own memo.
	outcome, err = retriever.Search(ctx, other, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)

	// Archived after indexing: never returned, not even to the creator.
	archived := store.Archived
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: memo.ID, RowStatus: &archived}))
	outcome, err = retriever.Search(ctx, other, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
}

func TestSearchExcludesOtherProjectionVersions(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	m := fixture.createMemo(ctx, t, "versioned", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	document := fixture.getDocument(ctx, t, m.ID)
	require.NotNil(t, document)
	document.ProjectionVersion = ProjectionVersion + 1
	_, err := fixture.store.UpsertAISearchDocument(ctx, document)
	require.NoError(t, err)

	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
}

func TestSearchRegeneratesOrDropsStaleCitations(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	m := fixture.createMemo(ctx, t, "stale", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	// Edit the memo without resyncing its document: the indexed text is
	// stale. The snippet must come from the current source.
	require.NoError(t, fixture.store.UpdateMemo(ctx, &store.UpdateMemo{ID: m.ID, Content: ptr("At dawn the owl stretches. The forest wakes.")}))
	retriever := newTestRetriever(fixture)
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Contains(t, outcome.Hits[0].Snippet, "owl")
	require.NotContains(t, outcome.Hits[0].Snippet, "dusk")

	// A term only present in the stale indexed text no longer matches: the
	// citation is discarded.
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "dusk"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
}

func ptr[T any](v T) *T { return &v }

func TestSearchTruncatesOverlongQueries(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "owls", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	retriever := newTestRetriever(fixture)

	// The normalized query exceeds the rune budget: truncated, and the
	// truncation is honestly reported.
	budgets := DefaultBudgets()
	budgets.MaxQueryRunes = 3
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owls", Budgets: &budgets})
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonQueryTruncated)
	require.Len(t, outcome.Hits, 1)

	// The word budget keeps only the leading query words.
	budgets = DefaultBudgets()
	budgets.MaxQueryWords = 1
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl hunts", Budgets: &budgets})
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonQueryTruncated)
	require.Len(t, outcome.Hits, 1)
}

func TestSearchScanBudgetExhaustion(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "first", "The owl hunts at dusk.")
	fixture.createMemo(ctx, t, "second", "The owl sleeps at dawn.")
	fixture.service.RunOnce(ctx)

	retriever := newTestRetriever(fixture)

	// A scan budget below one document scans nothing.
	budgets := DefaultBudgets()
	budgets.MaxScanBytes = 1
	outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonScanBudgetExhausted)

	// A scan budget covering exactly the first document (documents scan in
	// ascending ID order) returns it and reports the uncovered rest.
	var first *store.AISearchDocument
	for _, uid := range []string{"first", "second"} {
		m, err := fixture.store.GetMemo(ctx, &store.FindMemo{UID: &uid})
		require.NoError(t, err)
		document := fixture.getDocument(ctx, t, m.ID)
		require.NotNil(t, document)
		if first == nil || document.ID < first.ID {
			first = document
		}
	}
	budgets.MaxScanBytes = len(first.Title) + len(first.Content)
	for _, tag := range first.Tags {
		budgets.MaxScanBytes += len(tag)
	}
	outcome, err = retriever.Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "first", outcome.Hits[0].MemoUID)
	require.Contains(t, outcome.PartialReasons, ReasonScanBudgetExhausted)
}

func TestSearchCandidateBudgetExhaustion(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "one", "The owl hunts at dusk.")
	fixture.createMemo(ctx, t, "two", "The owl sleeps at dawn.")
	fixture.createMemo(ctx, t, "three", "# Owl diary\n\nField notes.")
	fixture.service.RunOnce(ctx)

	budgets := DefaultBudgets()
	budgets.MaxCandidates = 1
	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonCandidateBudgetExhausted)
	// The single kept candidate is the strongest: the title match.
	require.Len(t, outcome.Hits, 1)
	require.Equal(t, "three", outcome.Hits[0].MemoUID)
}

func TestSearchResultBudgetCapsHits(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "one", "The owl hunts at dusk.")
	fixture.createMemo(ctx, t, "two", "The owl sleeps at dawn.")
	fixture.createMemo(ctx, t, "three", "The owl eats at noon.")
	fixture.service.RunOnce(ctx)

	budgets := DefaultBudgets()
	budgets.MaxResults = 2
	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl", Budgets: &budgets})
	require.NoError(t, err)
	require.Len(t, outcome.Hits, 2)
	require.Empty(t, outcome.PartialReasons)
}

func TestSearchTimeBudgetExhaustion(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "owls", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	// A deadline already passed exhausts the wall clock before the scan
	// starts; the query reports the time budget rather than failing.
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Hour))
	defer cancel()
	outcome, err := newTestRetriever(fixture).Search(expired, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.Hits)
	require.Contains(t, outcome.PartialReasons, ReasonTimeBudgetExhausted)

	// A caller cancellation is not a budget exhaustion: it propagates.
	cancelled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	_, err = newTestRetriever(fixture).Search(cancelled, fixture.owner, Query{Text: "owl"})
	require.Error(t, err)
}
