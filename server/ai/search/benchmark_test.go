package search

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
)

// BenchmarkRetrieval measures lexical retrieval latency, memory, scanned
// bytes, and candidate counts at the spec's default budgets against
// representative corpus sizes. It runs against the DRIVER-selected database:
// file-backed SQLite by default, MySQL or PostgreSQL via TestContainers when
// DRIVER is set and Docker is available (SKIP_CONTAINER_TESTS=1 skips them).
//
//	go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
//	DRIVER=mysql go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
//	DRIVER=postgres go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
//
// The corpus sizes step from a small corpus up to the largest envelope the
// 32 MB scan budget can cover and beyond it, so the run shows both the
// supported envelope and the degraded behavior past it.
func BenchmarkRetrieval(b *testing.B) {
	ctx := context.Background()
	sizes := []struct {
		name      string
		documents int
	}{
		{"small_1000_docs", 1000},
		{"medium_8000_docs", 8000},
		{"envelope_22000_docs", 22000},
		{"beyond_scan_budget_30000_docs", 30000},
	}
	queries := []struct {
		name string
		text string
	}{
		{"exact_rare_token", benchmarkRareToken},
		{"multiword_rare", "quixotic owl hunts"},
		{"multiword_common", "owl hunts dusk"},
		{"max_words_32", benchmarkMaxWordsQuery},
		{"fuzzy_typo", "quixottc"},
	}

	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			fixture := buildBenchmarkCorpus(ctx, b, size.documents)
			retriever := NewRetriever(fixture.store, memo.NewService(fixture.store), markdown.NewService(markdown.WithTagExtension()), DefaultBudgets())
			for _, query := range queries {
				b.Run(query.name, func(b *testing.B) {
					b.ReportAllocs()
					var last *Outcome
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						outcome, err := retriever.Search(ctx, fixture.owner, Query{Text: query.text})
						if err != nil {
							b.Fatalf("search failed: %v", err)
						}
						last = outcome
					}
					b.StopTimer()
					b.ReportMetric(float64(last.Stats.BytesScanned), "scan_bytes")
					b.ReportMetric(float64(last.Stats.DocumentsScanned), "docs_scanned")
					b.ReportMetric(float64(last.Stats.Candidates), "candidates")
					b.ReportMetric(float64(len(last.Hits)), "hits")
					b.Logf("partial reasons: %v", last.PartialReasons)
				})
			}
		})
	}
}

// benchmarkRareToken is planted in a fixed share of the benchmark corpus so
// the exact and fuzzy queries have realistic hit counts.
const benchmarkRareToken = "quixotic"

// benchmarkMaxWordsQuery exercises the 32-word query budget end to end: every
// word is a common vocabulary word except the planted rare token, which gates
// the AND match to the planted documents.
const benchmarkMaxWordsQuery = "quixotic the field notes owl hunts dusk forest river stone wind light garden harbor meadow shadow ember cedar willow falcon heron otter ledger margin signal anchor voyage harvest threshold pattern texture fragment"

// benchmarkVocabulary is the shared word pool benchmark memo bodies are drawn
// from. Common words collide across documents so multiword queries exercise
// real candidate filtering instead of matching nothing or everything.
var benchmarkVocabulary = []string{
	"the", "of", "and", "a", "in", "to", "is", "was", "he", "for",
	"it", "with", "as", "his", "on", "be", "at", "by", "had", "not",
	"are", "but", "from", "or", "have", "an", "they", "which", "one", "you",
	"field", "notes", "owl", "hunts", "dusk", "forest", "river", "stone", "wind", "light",
	"garden", "harbor", "meadow", "shadow", "ember", "cedar", "willow", "falcon", "heron", "otter",
	"ledger", "margin", "signal", "anchor", "voyage", "harvest", "threshold", "pattern", "texture", "fragment",
}

// buildBenchmarkCorpus creates memos with deterministic pseudo-natural
// content and builds their search documents through the real projection path.
// Content is seeded by size only, so a given corpus size is byte-identical
// across runs and databases.
func buildBenchmarkCorpus(ctx context.Context, b testing.TB, documents int) *searchTestFixture {
	b.Helper()
	fixture := newSearchTestFixture(ctx, b)

	rng := rand.New(rand.NewSource(int64(documents)))
	// Every plantedInterval-th memo carries the rare token and the exact
	// multiword phrase, giving the benchmark queries a stable hit count.
	plantedInterval := documents / 10
	if plantedInterval < 1 {
		plantedInterval = 1
	}
	for i := 0; i < documents; i++ {
		var body strings.Builder
		fmt.Fprintf(&body, "# Field notes %d\n\n", i)
		// ~220 words of shared vocabulary, roughly 1.2 KB of normalized
		// search-document bytes per memo once projected.
		for w := 0; w < 220; w++ {
			body.WriteString(benchmarkVocabulary[rng.Intn(len(benchmarkVocabulary))])
			body.WriteByte(' ')
		}
		if i%plantedInterval == 0 {
			body.WriteString("the quixotic owl hunts dusk over the meadow")
		}
		if i%2 == 0 {
			body.WriteString(" #fieldnotes")
		}
		fixture.createMemo(ctx, b, fmt.Sprintf("bench-%d", i), body.String())
	}

	b.Logf("building search documents for %d memos", documents)
	fixture.service.RunOnce(ctx)

	// Every memo must be searchable before measurements start.
	limit := 1
	latest, err := fixture.store.ListAISearchDocuments(ctx, &store.FindAISearchDocument{Limit: &limit})
	require.NoError(b, err)
	require.NotEmpty(b, latest)
	return fixture
}
