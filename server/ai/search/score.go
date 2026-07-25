package search

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/usememos/memos/store"
)

// Match tiers, strongest first. The tier names surface in rank reasons as
// "{field}_{tier}".
const (
	tierExact = iota
	tierPartial
	tierFuzzy
)

// Field weights per tier: title matches outrank tag matches, which outrank
// content matches; within a field exact outranks partial, which outranks
// fuzzy.
var tierFieldWeights = [3][3]float64{
	{8, 4, 2.5}, // title: exact, partial, fuzzy
	{6, 3, 2},   // tags
	{4, 2, 1},   // content
}

// phraseBonusTitle and phraseBonusContent reward the full query appearing as
// one phrase in the field text.
const (
	phraseBonusTitle   = 6
	phraseBonusContent = 4
)

// minFuzzyWordRunes is the minimum query-word length eligible for
// typo-tolerant matching; shorter words match exactly or partially only.
const minFuzzyWordRunes = 4

// minPartialWordRunes is the minimum query-word length eligible for
// substring matching; shorter words would match inside unrelated tokens.
const minPartialWordRunes = 3

// parsedQuery is the normalized, budgeted form of a query text.
type parsedQuery struct {
	// text is the normalized full query, used for phrase matching.
	text string
	// words are the normalized query words, used for token matching.
	words []string
	// anyWord broadens matching from every word must match to at least one
	// word must match.
	anyWord bool
}

// parseQuery normalizes the query text and caps it at the query budgets. It
// reports whether truncation happened.
func parseQuery(text string, budgets Budgets) (*parsedQuery, bool) {
	normalized := normalize(text)
	truncated := false
	if utf8.RuneCountInString(normalized) > budgets.MaxQueryRunes {
		normalized = string([]rune(normalized)[:budgets.MaxQueryRunes])
		truncated = true
	}
	words := tokenize(normalized)
	if len(words) > budgets.MaxQueryWords {
		words = words[:budgets.MaxQueryWords]
		truncated = true
	}
	return &parsedQuery{text: normalized, words: words}, truncated
}

// tokenize splits normalized text into words: maximal runs of Unicode
// letters and digits.
func tokenize(text string) []string {
	words := []string{}
	start := -1
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			words = append(words, text[start:i])
			start = -1
		}
	}
	if start >= 0 {
		words = append(words, text[start:])
	}
	return words
}

// candidate is one matching document before hydration.
type candidate struct {
	document *store.AISearchDocument
	score    float64
	reasons  []string
	// anchor is the strongest content term, used to place the snippet.
	anchor string
}

// documentIndex precomputes a document's token sets once per query.
type documentIndex struct {
	titleText     string
	tagText       string
	contentText   string
	titleTokens   map[string]struct{}
	tagTokens     map[string]struct{}
	contentTokens map[string]struct{}
	contentWords  []string
	titleWords    []string
	tagWords      []string
}

func indexDocument(document *store.AISearchDocument) *documentIndex {
	index := &documentIndex{
		titleText:     document.Title,
		tagText:       strings.Join(document.Tags, " "),
		contentText:   document.Content,
		titleTokens:   map[string]struct{}{},
		tagTokens:     map[string]struct{}{},
		contentTokens: map[string]struct{}{},
		titleWords:    tokenize(document.Title),
		contentWords:  tokenize(document.Content),
	}
	for _, tag := range document.Tags {
		index.tagWords = append(index.tagWords, tokenize(tag)...)
	}
	for _, word := range index.titleWords {
		index.titleTokens[word] = struct{}{}
	}
	for _, word := range index.tagWords {
		index.tagTokens[word] = struct{}{}
	}
	for _, word := range index.contentWords {
		index.contentTokens[word] = struct{}{}
	}
	return index
}

// scoreDocument scores one document against the query. In the default mode
// a document is a candidate when every query word matches some field at some
// tier; in any-word mode one matched word suffices, which suits
// natural-language questions. The score sums each matched word's best
// tier-field weight plus a phrase bonus; reasons name the best tier reached
// per field.
func scoreDocument(document *store.AISearchDocument, index *documentIndex, query *parsedQuery) (candidate, bool) {
	score := 0.0
	matched := 0
	best := [3]int{-1, -1, -1} // per field, the best tier any word reached
	anchor := ""
	anchorScore := 0.0
	for _, word := range query.words {
		tier, field, token := bestMatch(index, word)
		if tier < 0 {
			if !query.anyWord {
				return candidate{}, false
			}
			continue
		}
		matched++
		weight := tierFieldWeights[field][tier]
		score += weight
		if tier < best[field] || best[field] < 0 {
			best[field] = tier
		}
		if field == 2 && weight > anchorScore {
			anchor, anchorScore = token, weight
		}
	}
	if matched == 0 {
		return candidate{}, false
	}

	reasons := []string{}
	if query.text != "" {
		if strings.Contains(document.Title, query.text) {
			score += phraseBonusTitle
			reasons = append(reasons, "phrase_title")
		} else if strings.Contains(document.Content, query.text) {
			score += phraseBonusContent
			reasons = append(reasons, "phrase_content")
		}
	}
	for field, tier := range best {
		if tier < 0 {
			continue
		}
		reasons = append(reasons, fieldNames[field]+"_"+tierNames[tier])
	}
	return candidate{document: document, score: score, reasons: reasons, anchor: anchor}, true
}

var fieldNames = [3]string{"title", "tag", "content"}
var tierNames = [3]string{"exact", "partial", "fuzzy"}

// bestMatch returns the strongest tier at which the word matches any field,
// preferring the higher-weighted field on ties, plus the matched field
// token. It returns -1 when the word matches nothing.
func bestMatch(index *documentIndex, word string) (int, int, string) {
	bestTier, bestField := -1, -1
	bestToken := ""
	bestWeight := 0.0
	for field := range 3 {
		tier, token := matchField(index, field, word)
		if tier < 0 {
			continue
		}
		if weight := tierFieldWeights[field][tier]; weight > bestWeight {
			bestTier, bestField, bestToken, bestWeight = tier, field, token, weight
		}
	}
	return bestTier, bestField, bestToken
}

// matchField returns the strongest tier at which the word matches the field
// plus the matched field token, or -1. Exact is a token match, partial a
// substring match, fuzzy an edit-distance match against the field tokens.
func matchField(index *documentIndex, field int, word string) (int, string) {
	var text string
	var tokens map[string]struct{}
	var words []string
	switch field {
	case 0:
		text, tokens, words = index.titleText, index.titleTokens, index.titleWords
	case 1:
		text, tokens, words = index.tagText, index.tagTokens, index.tagWords
	case 2:
		text, tokens, words = index.contentText, index.contentTokens, index.contentWords
	}
	wordRunes := utf8.RuneCountInString(word)
	if _, ok := tokens[word]; ok {
		return tierExact, word
	}
	if wordRunes >= minPartialWordRunes && strings.Contains(text, word) {
		return tierPartial, word
	}
	if wordRunes < minFuzzyWordRunes {
		return -1, ""
	}
	maxDistance := maxFuzzyDistance(wordRunes)
	bestToken := ""
	bestDistance := maxDistance + 1
	for _, candidate := range words {
		if diff := utf8.RuneCountInString(candidate) - wordRunes; diff > maxDistance || diff < -maxDistance {
			continue
		}
		if distance := levenshteinDistance(word, candidate, bestDistance-1); distance < bestDistance {
			bestToken, bestDistance = candidate, distance
		}
	}
	if bestToken == "" {
		return -1, ""
	}
	return tierFuzzy, bestToken
}

// sortCandidates orders candidates by score descending, then by document ID
// ascending for determinism.
func sortCandidates(candidates []candidate) {
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.document.ID, b.document.ID)
	})
}

// levenshteinDistance returns the edit distance between a and b when it is
// at most cutoff, and cutoff+1 otherwise, using rune-level dynamic
// programming with early exit.
func levenshteinDistance(a, b string, cutoff int) int {
	if cutoff < 0 {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	if diff := len(ar) - len(br); diff > cutoff || diff < -cutoff {
		return cutoff + 1
	}
	previous := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current := make([]int, len(br)+1)
		current[0] = i
		rowMin := current[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
			rowMin = min(rowMin, current[j])
		}
		if rowMin > cutoff {
			return cutoff + 1
		}
		previous = current
	}
	if previous[len(br)] > cutoff {
		return cutoff + 1
	}
	return previous[len(br)]
}

// maxFuzzyDistance returns the edit distance tolerated for a query word of
// the given rune length: one edit for shorter words, two for longer ones.
func maxFuzzyDistance(runes int) int {
	if runes >= 8 {
		return 2
	}
	return 1
}
