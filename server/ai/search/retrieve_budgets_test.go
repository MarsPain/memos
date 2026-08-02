package search

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/store"
)

// TestDefaultBudgetsMatchSpecDefaults pins the retrieval budget defaults to
// the values the product spec records (1,024 query characters, 32 MB scan,
// 200 candidates, 20 results, 5-second wall clock, 128 MB of chunk vector
// bytes per semantic scan, one bounded query embedding call) plus the
// implementation budgets derived from them (32 query words, 100-document
// scan batches), so none of them drifts silently. If a benchmark-driven
// tightening changes a default, change the spec and this fixture together.
func TestDefaultBudgetsMatchSpecDefaults(t *testing.T) {
	budgets := DefaultBudgets()
	require.Equal(t, 1024, budgets.MaxQueryRunes)
	require.Equal(t, 32, budgets.MaxQueryWords)
	require.Equal(t, 32<<20, budgets.MaxScanBytes)
	require.Equal(t, 100, budgets.ScanBatchSize)
	require.Equal(t, 200, budgets.MaxCandidates)
	require.Equal(t, 20, budgets.MaxResults)
	require.Equal(t, 5*time.Second, budgets.WallClock)
	require.Equal(t, 128<<20, budgets.MaxSemanticScanBytes)
	require.Equal(t, 2*time.Second, budgets.EmbeddingTimeout)
}

// TestSearchQueryTruncationAtSpecDefaults proves the spec's 1,024-character
// query budget is enforced at the defaults: an overlong query is cut down and
// the truncation is honestly reported rather than silently searched.
func TestSearchQueryTruncationAtSpecDefaults(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	fixture.createMemo(ctx, t, "owls", "The owl hunts at dusk.")
	fixture.service.RunOnce(ctx)

	overlong := strings.Repeat("owl ", 300) // 1,200 characters > 1,024 runes
	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: overlong})
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonQueryTruncated)
	require.Len(t, outcome.Hits, 1)
}

// TestSearchScanBudgetExhaustionAtSpecDefaults proves the spec's 32 MB scan
// budget is enforced at the defaults: a corpus past the benchmarked envelope
// yields an explicit scan_budget_exhausted degradation with the scanned bytes
// and candidates still inside the budgets, never unbounded work.
func TestSearchScanBudgetExhaustionAtSpecDefaults(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)

	// Six 6 MB search documents: five fit the 32 MB scan budget, the sixth
	// pushes the scan past it. The matching memos deliberately do not exist;
	// the scan exhausts before hydration, and hydration drops the phantom
	// candidates.
	documentBytes := 6 << 20
	for i := 0; i < 6; i++ {
		content := strings.Repeat("lorem ", documentBytes/6) + "quixotic"
		_, err := fixture.store.UpsertAISearchDocument(ctx, &store.AISearchDocument{
			MemoID:               int32(10_000 + i),
			MemoUID:              fmt.Sprintf("phantom-%d", i),
			MemoUpdatedTs:        1,
			ContentHash:          fmt.Sprintf("hash-%d", i),
			ProjectionVersion:    ProjectionVersion,
			NormalizationVersion: NormalizationVersion,
			Content:              content,
		})
		require.NoError(t, err)
	}

	budgets := DefaultBudgets()
	start := time.Now()
	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "quixotic"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonScanBudgetExhausted)
	require.NotContains(t, outcome.PartialReasons, ReasonTimeBudgetExhausted)
	require.Less(t, elapsed, budgets.WallClock)

	// The scan stopped inside the byte budget after exactly five documents.
	require.LessOrEqual(t, outcome.Stats.BytesScanned, budgets.MaxScanBytes)
	require.Equal(t, 5, outcome.Stats.DocumentsScanned)
	require.LessOrEqual(t, outcome.Stats.Candidates, budgets.MaxCandidates)
	require.Empty(t, outcome.Hits)
}

// TestSearchCandidateBudgetExhaustionAtSpecDefaults proves the spec's
// 200-candidate budget is enforced at the defaults: a query matching more
// than 200 documents keeps the 200 strongest and reports the dropped rest
// rather than ranking an unbounded candidate set. The time-budget mechanism
// is covered by TestSearchTimeBudgetExhaustion; the 5-second default itself
// is measured by BenchmarkRetrieval, which shows the 32 MB scan budget
// binding long before it.
func TestSearchCandidateBudgetExhaustionAtSpecDefaults(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	for i := 0; i < 250; i++ {
		fixture.createMemo(ctx, t, fmt.Sprintf("owl-%d", i), fmt.Sprintf("The owl hunts at dusk, note %d.", i))
	}
	fixture.service.RunOnce(ctx)

	budgets := DefaultBudgets()
	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Contains(t, outcome.PartialReasons, ReasonCandidateBudgetExhausted)
	require.Equal(t, budgets.MaxCandidates, outcome.Stats.Candidates)
	require.Len(t, outcome.Hits, budgets.MaxResults)
}

// TestSearchScanStatsReportCoverage pins the scan-statistics contract
// benchmarks and operators rely on: on a fully covered corpus the stats count
// every document's bytes and every kept candidate.
func TestSearchScanStatsReportCoverage(t *testing.T) {
	ctx := context.Background()
	fixture := newSearchTestFixture(ctx, t)
	first := fixture.createMemo(ctx, t, "first", "The owl hunts at dusk.")
	second := fixture.createMemo(ctx, t, "second", "The owl sleeps at dawn.")
	third := fixture.createMemo(ctx, t, "third", "Bats navigate by echo.")
	fixture.service.RunOnce(ctx)

	expectedBytes := 0
	for _, m := range []*store.Memo{first, second, third} {
		document := fixture.getDocument(ctx, t, m.ID)
		require.NotNil(t, document)
		expectedBytes += len(document.Title) + len(document.Content)
		for _, tag := range document.Tags {
			expectedBytes += len(tag)
		}
	}

	outcome, err := newTestRetriever(fixture).Search(ctx, fixture.owner, Query{Text: "owl"})
	require.NoError(t, err)
	require.Empty(t, outcome.PartialReasons)
	require.Len(t, outcome.Hits, 2)
	require.Equal(t, 3, outcome.Stats.DocumentsScanned)
	require.Equal(t, expectedBytes, outcome.Stats.BytesScanned)
	require.Equal(t, 2, outcome.Stats.Candidates)
}
