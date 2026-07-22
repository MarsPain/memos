package ai

import (
	"cmp"
	"slices"
	"strings"

	"github.com/usememos/memos/store"
)

// TODO(issue 06): replace naive substring retrieval with versioned search
// documents and lexical/fuzzy retrieval.

const (
	// minWordMatchRunes is the minimum length of a query word eligible for
	// fallback word matching.
	minWordMatchRunes = 4
	// snippetContextRunes is the number of runes kept around a match.
	snippetContextRunes = 80
	// snippetFallbackRunes is the snippet length when no match is located.
	snippetFallbackRunes = 160
)

// retrievalHit pairs a matching memo with a snippet from its current content.
type retrievalHit struct {
	memo    *store.Memo
	snippet string
}

// retrieveTop returns up to limit memos whose content matches the query.
// Matching is naive and case-insensitive: a memo scores when it contains the
// full query, or any query word of at least minWordMatchRunes characters.
// Full-query matches rank first, then higher word-match counts, then the most
// recently updated memos.
func retrieveTop(memos []*store.Memo, query string, limit int) []retrievalHit {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit <= 0 {
		return nil
	}
	words := make([]string, 0)
	for _, word := range strings.Fields(query) {
		if len([]rune(word)) >= minWordMatchRunes {
			words = append(words, word)
		}
	}

	type scoredMemo struct {
		memo        *store.Memo
		fullMatch   bool
		wordMatches int
		bestTerm    string
	}
	scored := make([]scoredMemo, 0, len(memos))
	for _, memo := range memos {
		if memo == nil {
			continue
		}
		content := strings.ToLower(memo.Content)
		fullMatch := strings.Contains(content, query)
		wordMatches := 0
		bestTerm := ""
		for _, word := range words {
			if strings.Contains(content, word) {
				wordMatches++
				if bestTerm == "" {
					bestTerm = word
				}
			}
		}
		if !fullMatch && wordMatches == 0 {
			continue
		}
		if fullMatch {
			bestTerm = query
		}
		scored = append(scored, scoredMemo{memo: memo, fullMatch: fullMatch, wordMatches: wordMatches, bestTerm: bestTerm})
	}

	slices.SortStableFunc(scored, func(a, b scoredMemo) int {
		if a.fullMatch != b.fullMatch {
			if a.fullMatch {
				return -1
			}
			return 1
		}
		if a.wordMatches != b.wordMatches {
			return cmp.Compare(b.wordMatches, a.wordMatches)
		}
		return cmp.Compare(b.memo.UpdatedTs, a.memo.UpdatedTs)
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}
	hits := make([]retrievalHit, 0, len(scored))
	for _, scoredMemo := range scored {
		hits = append(hits, retrievalHit{
			memo:    scoredMemo.memo,
			snippet: buildSnippet(scoredMemo.memo.Content, scoredMemo.bestTerm),
		})
	}
	return hits
}

// buildSnippet extracts a window of the memo's current content around the
// first case-insensitive match of term, marking truncated edges with an
// ellipsis. When term does not match, it falls back to the memo's leading
// runes.
func buildSnippet(content, term string) string {
	runes := []rune(content)
	if len(runes) == 0 {
		return ""
	}
	termRunes := []rune(term)
	index := indexCaseInsensitive(content, term)
	if index < 0 {
		end := min(len(runes), snippetFallbackRunes)
		snippet := string(runes[:end])
		if end < len(runes) {
			snippet += "…"
		}
		return snippet
	}

	start := max(0, index-snippetContextRunes)
	end := min(len(runes), index+len(termRunes)+snippetContextRunes)
	var builder strings.Builder
	if start > 0 {
		builder.WriteString("…")
	}
	builder.WriteString(string(runes[start:end]))
	if end < len(runes) {
		builder.WriteString("…")
	}
	return builder.String()
}

// indexCaseInsensitive returns the rune index of the first case-insensitive
// occurrence of term in content, or -1 when absent.
func indexCaseInsensitive(content, term string) int {
	contentRunes := []rune(strings.ToLower(content))
	termRunes := []rune(strings.ToLower(term))
	if len(termRunes) == 0 || len(termRunes) > len(contentRunes) {
		return -1
	}
	for i := 0; i+len(termRunes) <= len(contentRunes); i++ {
		if slices.Equal(contentRunes[i:i+len(termRunes)], termRunes) {
			return i
		}
	}
	return -1
}
