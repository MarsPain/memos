package search

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

// RankReasonLexical marks a result the lexical path produced. It appears only
// on results of a genuinely hybrid-served query: when the semantic path
// serves nothing, the result list keeps the Stage 2 lexical reasons alone.
const RankReasonLexical = "lexical"

// rrfK is the reciprocal-rank-fusion constant: the rank offset damping the
// contribution of lower ranks, conventional at 60.
const rrfK = 60

// fuseSemantic merges the semantic matches into the lexical candidates with
// ordinal-rank fusion, replacing the tracer's naive interleave. The second
// return value is the machine-readable reason semantic retrieval did not
// serve — the query is answered by lexical coverage alone — empty when it
// served. An error fails the query: caller cancellation propagates and a
// store failure is never hidden behind lexical coverage.
func (r *Retriever) fuseSemantic(ctx context.Context, queryText string, tagFilters []string, lexical []candidate, budgets Budgets) ([]candidate, string, error) {
	if r.semantic == nil {
		return lexical, "", nil
	}
	matches, reason, err := r.semantic.Search(ctx, queryText, semanticScanBudgets{
		maxScanBytes: budgets.MaxSemanticScanBytes,
		embedTimeout: budgets.EmbeddingTimeout,
	})
	if err != nil {
		if ctx.Err() != nil {
			if timeBudgetExpired(ctx) {
				// The wall clock fired during the semantic phase: lexical
				// answers with what it gathered, time budget disclosed.
				return lexical, ReasonTimeBudgetExhausted, nil
			}
			// The caller went away: the query fails rather than degrading.
			return nil, "", status.FromContextError(ctx.Err()).Err()
		}
		// A store failure is never hidden behind lexical coverage.
		return nil, "", status.Errorf(codes.Internal, "failed to retrieve semantic candidates: %v", err)
	}
	if len(matches) == 0 {
		// Semantic retrieval served nothing: it is rebuilding, disabled, over
		// its scan budget, the embedding call failed, or nothing in the active
		// generation shares a direction with the query. Lexical answers alone,
		// disclosed by the reason when there is one.
		return lexical, reason, nil
	}
	semantic := semanticCandidates(matches, tagFilters, lexical)
	if len(semantic) == 0 {
		// Every semantic match was excluded by the structured filters or the
		// staleness guard: lexical answers alone.
		return lexical, reason, nil
	}

	// The list is genuinely hybrid: every result discloses the path(s) that
	// produced it. Lexical candidates already carry their field and tier
	// reasons; the fusion merge adds the semantic reason to corroborated
	// memos.
	for i := range lexical {
		lexical[i].reasons = appendReason(lexical[i].reasons, RankReasonLexical)
	}
	return fuseByRank(lexical, semantic, budgets.MaxCandidates), "", nil
}

// semanticCandidates builds fusion candidates from the semantic matches,
// using the documents the scan validated them against. A match built from a
// superseded source revision or content hash never serves, and the
// structured filters narrow semantic candidates exactly as they narrow
// lexical ones. Matches on memos the lexical path already found reuse the
// lexical document and are rechecked against it; the fusion merge
// corroborates them.
func semanticCandidates(matches []semanticMatch, tagFilters []string, lexical []candidate) []candidate {
	lexicalDocs := make(map[int32]*store.AISearchDocument, len(lexical))
	for _, c := range lexical {
		lexicalDocs[c.document.MemoID] = c.document
	}
	candidates := []candidate{}
	for _, match := range matches {
		document, ok := lexicalDocs[match.memoID]
		if !ok {
			document = match.document
			if document == nil {
				continue
			}
			if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
				continue
			}
			if !tagsMatch(document.Tags, tagFilters) {
				continue
			}
		}
		if !matchCurrent(document, match) {
			// The winning chunk was built from a superseded revision or hash:
			// stale indexed text never serves; the memo returns once the
			// indexer catches up.
			continue
		}
		candidates = append(candidates, candidate{
			document: document,
			reasons:  []string{RankReasonSemantic},
			anchor:   semanticAnchor(document, match.contentPos),
		})
	}
	return candidates
}

// fusedCandidate accumulates one memo's reciprocal-rank score across the
// paths that produced it, keeping the lexical candidate as the representative
// whose snippet anchor the lexical match chose.
type fusedCandidate struct {
	candidate candidate
	score     float64
}

// fuseByRank merges the lexical and semantic candidate lists by reciprocal
// rank fusion: each path contributes 1/(k+rank) per candidate, so the lists
// combine by rank position alone and never assume lexical and cosine scores
// share a scale. A memo found by both paths appears once, disclosing both
// paths, and outranks single-path candidates of comparable rank.
func fuseByRank(lexical, semantic []candidate, maxCandidates int) []candidate {
	fused := make(map[int32]*fusedCandidate, len(lexical)+len(semantic))
	contribute := func(c candidate, rank int) {
		f, ok := fused[c.document.MemoID]
		if !ok {
			f = &fusedCandidate{candidate: c}
			fused[c.document.MemoID] = f
		} else {
			for _, reason := range c.reasons {
				f.candidate.reasons = appendReason(f.candidate.reasons, reason)
			}
		}
		f.score += 1 / (rrfK + float64(rank))
	}
	// Lexical contributes first so a memo found by both paths keeps its
	// lexical representative.
	for i, c := range lexical {
		contribute(c, i+1)
	}
	for i, c := range semantic {
		contribute(c, i+1)
	}

	merged := make([]candidate, 0, len(fused))
	for _, f := range fused {
		f.candidate.score = f.score
		merged = append(merged, f.candidate)
	}
	sortCandidates(merged)
	if len(merged) > maxCandidates {
		merged = merged[:maxCandidates]
	}
	return merged
}

// matchCurrent reports whether a semantic match was built from the document's
// current source revision and content hash.
func matchCurrent(document *store.AISearchDocument, match semanticMatch) bool {
	return document.MemoUpdatedTs == match.memoRevision && document.ContentHash == match.contentHash
}

// semanticAnchor returns the first content token at or after the chunk's
// start within the projected content, used to place the snippet near the
// matching chunk.
func semanticAnchor(document *store.AISearchDocument, contentPos int32) string {
	if contentPos < 0 || int(contentPos) >= len(document.Content) {
		return ""
	}
	words := tokenize(document.Content[contentPos:])
	if len(words) == 0 {
		return ""
	}
	return words[0]
}
