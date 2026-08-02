package search

import (
	"context"
	"errors"
	"slices"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
)

// Machine-readable partial/degraded reasons returned when coverage falls
// short of complete — a budget cut the scan short, or semantic retrieval
// could not serve and lexical answered alone. Their presence means the
// searchable corpus was not covered completely; an empty reason list always
// means complete coverage.
const (
	// ReasonQueryTruncated marks a normalized query cut down to the query
	// budgets before matching.
	ReasonQueryTruncated = "query_truncated"
	// ReasonScanBudgetExhausted marks the lexical scan stopped after the
	// search-document byte budget ran out; unscanned memos may match.
	ReasonScanBudgetExhausted = "scan_budget_exhausted"
	// ReasonCandidateBudgetExhausted marks matching documents dropped because
	// the candidate budget was full.
	ReasonCandidateBudgetExhausted = "candidate_budget_exhausted"
	// ReasonTimeBudgetExhausted marks retrieval cut short by the wall clock.
	ReasonTimeBudgetExhausted = "time_budget_exhausted"
	// ReasonSemanticRebuilding marks lexical-only coverage because no complete
	// embedding generation is callable: the semantic index is building (or its
	// provider left the pool) and lexical retrieval serves the query.
	ReasonSemanticRebuilding = "semantic_rebuilding"
	// ReasonSemanticDisabled marks lexical-only coverage because the embedding
	// capability was disabled after index generations existed.
	ReasonSemanticDisabled = "semantic_disabled"
	// ReasonSemanticBudgetExceeded marks lexical-only coverage because the
	// semantic scan could not finish within its chunk-vector byte budget; the
	// incomplete semantic candidate set was discarded rather than ranked as
	// though it represented the corpus.
	ReasonSemanticBudgetExceeded = "semantic_budget_exceeded"
	// ReasonEmbeddingFailed marks lexical-only coverage because the query
	// embedding call failed.
	ReasonEmbeddingFailed = "embedding_failed"
	// ReasonEmbeddingTimeout marks lexical-only coverage because the bounded
	// query embedding call exceeded its own deadline.
	ReasonEmbeddingTimeout = "embedding_timeout"
)

// Budgets caps the work one retrieval query may do. The defaults are the
// spec's provisional budgets.
type Budgets struct {
	// MaxQueryRunes caps the normalized query text.
	MaxQueryRunes int
	// MaxQueryWords caps the query words used for matching.
	MaxQueryWords int
	// MaxScanBytes caps the search-document bytes scanned per query.
	MaxScanBytes int
	// ScanBatchSize bounds every document list read.
	ScanBatchSize int
	// MaxCandidates caps the candidates kept before final ranking.
	MaxCandidates int
	// MaxResults caps the final results.
	MaxResults int
	// MaxSemanticScanBytes caps the chunk-vector bytes the semantic scan
	// reads per query.
	MaxSemanticScanBytes int
	// EmbeddingTimeout bounds the single query embedding call, counted within
	// the wall clock.
	EmbeddingTimeout time.Duration
	// WallClock is the per-query wall-clock deadline.
	WallClock time.Duration
}

// DefaultBudgets returns the spec's provisional retrieval budgets: a
// 1,024-character normalized query, a 32 MB scan, 200 candidates, 20 final
// results, a 128 MB semantic scan of chunk-vector bytes, one query embedding
// call bounded at 2 seconds, and a 5-second wall clock.
func DefaultBudgets() Budgets {
	return Budgets{
		MaxQueryRunes:        1024,
		MaxQueryWords:        32,
		MaxScanBytes:         32 << 20,
		ScanBatchSize:        100,
		MaxCandidates:        200,
		MaxResults:           20,
		MaxSemanticScanBytes: 128 << 20,
		EmbeddingTimeout:     2 * time.Second,
		WallClock:            5 * time.Second,
	}
}

// normalize fills zero or negative fields with the defaults.
func (b Budgets) normalize() Budgets {
	defaults := DefaultBudgets()
	if b.MaxQueryRunes <= 0 {
		b.MaxQueryRunes = defaults.MaxQueryRunes
	}
	if b.MaxQueryWords <= 0 {
		b.MaxQueryWords = defaults.MaxQueryWords
	}
	if b.MaxScanBytes <= 0 {
		b.MaxScanBytes = defaults.MaxScanBytes
	}
	if b.ScanBatchSize <= 0 {
		b.ScanBatchSize = defaults.ScanBatchSize
	}
	if b.MaxCandidates <= 0 {
		b.MaxCandidates = defaults.MaxCandidates
	}
	if b.MaxResults <= 0 {
		b.MaxResults = defaults.MaxResults
	}
	if b.MaxSemanticScanBytes <= 0 {
		b.MaxSemanticScanBytes = defaults.MaxSemanticScanBytes
	}
	if b.EmbeddingTimeout <= 0 {
		b.EmbeddingTimeout = defaults.EmbeddingTimeout
	}
	if b.WallClock <= 0 {
		b.WallClock = defaults.WallClock
	}
	return b
}

// Filters narrows search candidates by structured product intent. Every set
// field must hold for a memo to appear in the results.
type Filters struct {
	// Tags requires the memo to carry all of these tags.
	Tags []string
	// CreatedAfter and CreatedBefore bound the memo's create time,
	// inclusive.
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Visibility requires the memo to have exactly this visibility.
	Visibility *store.Visibility
	// CreatorID requires the memo to be created by this user.
	CreatorID *int32
}

// Query is one retrieval request: the free-text intent plus structured
// filters and an optional per-query budget override.
type Query struct {
	Text    string
	Filters Filters
	// AnyWord broadens matching from every query word must match (the
	// search default) to at least one query word must match, which suits
	// natural-language chat questions.
	AnyWord bool
	// Budgets overrides the retriever's budgets when set.
	Budgets *Budgets
}

// Hit is one matching memo: its snippet quoted from the current source after
// reauthorization and revision checking, and the machine-readable reasons it
// ranked. SourceRevision, SourceHash, and the SourceStart/SourceEnd byte
// range identify exactly what was quoted for traceability. Source is the
// current, reauthorized memo the snippet was quoted from; callers building
// provider context quote it without rereading.
type Hit struct {
	MemoUID        string
	Snippet        string
	RankReasons    []string
	SourceRevision int64
	SourceHash     string
	SourceStart    int
	SourceEnd      int
	Source         *store.Memo
}

// ScanStats records the coverage one query achieved against the budgets, so
// benchmarks and operators can see the work a query did rather than inferring
// it from the results. BytesScanned counts the search-document bytes (title,
// tags, and content) the scan consumed, DocumentsScanned the documents those
// bytes came from, and Candidates the matching documents kept before final
// ranking.
type ScanStats struct {
	BytesScanned     int
	DocumentsScanned int
	Candidates       int
}

// Outcome is the result of one retrieval query. PartialReasons empty means
// the searchable corpus was covered completely.
type Outcome struct {
	Hits           []Hit
	PartialReasons []string
	Stats          ScanStats
}

// Retriever runs bounded lexical, partial, and typo-tolerant retrieval over
// the derived search documents. Every candidate is reauthorized and
// revision-checked against the current source memo before its snippet
// leaves: a memo whose permission changed between indexing and retrieval is
// never returned, and stale indexed text never leaves Memos.
type Retriever struct {
	store    *store.Store
	memos    *memo.Service
	markdown markdown.Service
	budgets  Budgets
	// semantic is the optional semantic query path; nil keeps retrieval
	// purely lexical (Stage 2 behavior).
	semantic *SemanticSearcher
}

// NewRetriever creates a Retriever with the given budgets; zero fields fall
// back to the spec's provisional budgets.
func NewRetriever(s *store.Store, memos *memo.Service, markdownService markdown.Service, budgets Budgets) *Retriever {
	return &Retriever{
		store:    s,
		memos:    memos,
		markdown: markdownService,
		budgets:  budgets.normalize(),
	}
}

// SetSemanticSearcher attaches the semantic query path. A nil searcher keeps
// retrieval purely lexical.
func (r *Retriever) SetSemanticSearcher(semantic *SemanticSearcher) {
	r.semantic = semantic
}

// Search runs one retrieval query for the user. Retrieval is read-only and
// works with no generation or embedding configuration.
func (r *Retriever) Search(ctx context.Context, user *store.User, query Query) (*Outcome, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	budgets := r.budgets
	if query.Budgets != nil {
		budgets = query.Budgets.normalize()
	}
	parsed, truncated := parseQuery(query.Text, budgets)
	if len(parsed.words) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "query is required")
	}
	parsed.anyWord = query.AnyWord
	tagFilters := make([]string, 0, len(query.Filters.Tags))
	for _, tag := range query.Filters.Tags {
		if normalized := normalize(tag); normalized != "" {
			tagFilters = append(tagFilters, normalized)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, budgets.WallClock)
	defer cancel()

	outcome := &Outcome{Hits: []Hit{}, PartialReasons: []string{}}
	if truncated {
		outcome.PartialReasons = append(outcome.PartialReasons, ReasonQueryTruncated)
	}

	candidates, scanReasons, stats, err := r.scan(ctx, parsed, tagFilters, budgets)
	if err != nil {
		return nil, err
	}
	outcome.PartialReasons = append(outcome.PartialReasons, scanReasons...)
	outcome.Stats = stats

	// Fuse the semantic candidates with the lexical ones by ordinal rank.
	// When semantic retrieval cannot serve this query, lexical coverage
	// answers it, disclosed by the machine-readable reason.
	candidates, semanticReason, err := r.fuseSemantic(ctx, parsed.text, tagFilters, candidates, budgets)
	if err != nil {
		return nil, err
	}
	if semanticReason != "" {
		outcome.PartialReasons = appendReason(outcome.PartialReasons, semanticReason)
	}
	outcome.Stats.Candidates = len(candidates)

	for _, candidate := range candidates {
		if len(outcome.Hits) >= budgets.MaxResults {
			break
		}
		hit, err := r.hydrate(ctx, user, candidate, parsed, &query.Filters)
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			if !timeBudgetExpired(ctx) {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			outcome.PartialReasons = appendReason(outcome.PartialReasons, ReasonTimeBudgetExhausted)
			break
		}
		if hit != nil {
			outcome.Hits = append(outcome.Hits, *hit)
		}
	}
	return outcome, nil
}

// timeBudgetExpired reports whether the context ended on a deadline — the
// query's own wall clock — rather than a caller cancellation.
func timeBudgetExpired(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// ctxFailure classifies an expired context for the semantic path: the query's
// own wall clock firing is the time-budget degraded reason, while a caller
// cancellation propagates as the context error.
func ctxFailure(ctx context.Context) (string, error) {
	if timeBudgetExpired(ctx) {
		return ReasonTimeBudgetExhausted, nil
	}
	return "", status.FromContextError(ctx.Err()).Err()
}

// appendReason appends a reason once, preserving first-occurrence order.
func appendReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

// scan walks the search documents in ascending ID order with keyset
// pagination, keeping the top candidates under the scan, candidate, and
// wall-clock budgets. Documents from another projection or normalization
// version are excluded; reconciliation rebuilds them.
func (r *Retriever) scan(ctx context.Context, query *parsedQuery, tagFilters []string, budgets Budgets) ([]candidate, []string, ScanStats, error) {
	reasons := []string{}
	candidates := []candidate{}
	stats := ScanStats{}
	candidateDropped := false
	var afterID int32

	for {
		if ctx.Err() != nil {
			if !timeBudgetExpired(ctx) {
				return nil, nil, ScanStats{}, status.FromContextError(ctx.Err()).Err()
			}
			reasons = appendReason(reasons, ReasonTimeBudgetExhausted)
			break
		}
		limit := budgets.ScanBatchSize
		documents, err := r.store.ListAISearchDocuments(ctx, &store.FindAISearchDocument{IDGreaterThan: &afterID, Limit: &limit})
		if err != nil {
			if ctx.Err() != nil {
				// The wall clock expired mid-scan, or the caller went away.
				if timeBudgetExpired(ctx) {
					reasons = appendReason(reasons, ReasonTimeBudgetExhausted)
					break
				}
				return nil, nil, ScanStats{}, status.FromContextError(ctx.Err()).Err()
			}
			// A store failure mid-scan fails the query rather than silently
			// narrowing coverage.
			return nil, nil, ScanStats{}, status.Errorf(codes.Internal, "failed to scan search documents: %v", err)
		}
		stop := false
		for _, document := range documents {
			afterID = document.ID
			documentBytes := len(document.Title) + len(document.Content)
			for _, tag := range document.Tags {
				documentBytes += len(tag)
			}
			if stats.BytesScanned+documentBytes > budgets.MaxScanBytes {
				reasons = appendReason(reasons, ReasonScanBudgetExhausted)
				stop = true
				break
			}
			stats.BytesScanned += documentBytes
			stats.DocumentsScanned++

			if document.ProjectionVersion != ProjectionVersion || document.NormalizationVersion != NormalizationVersion {
				continue
			}
			if !tagsMatch(document.Tags, tagFilters) {
				continue
			}
			scored, ok := scoreDocument(document, indexDocument(document), query)
			if !ok {
				continue
			}
			candidates = append(candidates, scored)
			if len(candidates) > budgets.MaxCandidates {
				sortCandidates(candidates)
				candidates = candidates[:budgets.MaxCandidates]
				candidateDropped = true
			}
		}
		if stop || len(documents) < budgets.ScanBatchSize {
			break
		}
	}
	if candidateDropped {
		reasons = appendReason(reasons, ReasonCandidateBudgetExhausted)
	}
	sortCandidates(candidates)
	stats.Candidates = len(candidates)
	return candidates, reasons, stats, nil
}

// tagsMatch reports whether the document carries every required tag. The
// document tags and the filter tags are both normalized.
func tagsMatch(documentTags, filterTags []string) bool {
	for _, filterTag := range filterTags {
		found := false
		for _, documentTag := range documentTags {
			if documentTag == filterTag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// hydrate rereads the candidate's source memo, reauthorizes it against the
// caller's current read permission, applies the memo-level filters, and
// quotes the snippet from the current source. A memo that left the corpus,
// lost read permission, or no longer matches after reprojection is skipped;
// a store failure fails the query rather than hiding it, while the wall
// clock expiring mid-hydration ends the query with what is already
// collected.
func (r *Retriever) hydrate(ctx context.Context, user *store.User, scored candidate, query *parsedQuery, filters *Filters) (*Hit, error) {
	m, err := r.memos.GetSearchableMemo(ctx, scored.document.MemoID)
	if err != nil {
		if ctx.Err() != nil {
			if timeBudgetExpired(ctx) {
				return nil, nil
			}
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Errorf(codes.Internal, "failed to reread memo: %v", err)
	}
	if m == nil {
		// The memo left the corpus since indexing (deleted, archived, or a
		// comment); reconciliation removes its document.
		return nil, nil
	}
	if err := r.memos.CheckReadAccess(user, m); err != nil {
		// The permission changed between indexing and retrieval: the memo is
		// never returned.
		return nil, nil
	}
	if !memoFiltersMatch(m, filters) {
		return nil, nil
	}

	document := scored.document
	if document.ContentHash != contentHash(m.Content) || document.MemoUID != m.UID {
		// The document is stale: reproject the current source and rescore.
		// The snippet either comes from the current source or the citation
		// is discarded; stale indexed text never leaves Memos.
		fresh, err := ProjectDocument(r.markdown, m)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to reproject memo: %v", err)
		}
		rescored, ok := scoreDocument(fresh, indexDocument(fresh), query)
		if !ok {
			// A semantic candidate does not need a lexical match after
			// reprojection; its snippet falls back to the source's leading
			// runes. A purely lexical candidate that no longer matches is
			// discarded.
			if !slices.Contains(scored.reasons, RankReasonSemantic) {
				return nil, nil
			}
			rescored = candidate{document: fresh, reasons: []string{RankReasonSemantic}}
		}
		scored = rescored
		document = fresh
	}

	snippet, snippetStart, snippetEnd := buildSourceSnippet(document, m.Content, scored.anchor)
	return &Hit{
		MemoUID:        m.UID,
		Snippet:        snippet,
		RankReasons:    scored.reasons,
		SourceRevision: m.UpdatedTs,
		SourceHash:     contentHash(m.Content),
		SourceStart:    snippetStart,
		SourceEnd:      snippetEnd,
		Source:         m,
	}, nil
}

// memoFiltersMatch applies the memo-level structured filters against the
// current source memo.
func memoFiltersMatch(m *store.Memo, filters *Filters) bool {
	if filters.CreatorID != nil && m.CreatorID != *filters.CreatorID {
		return false
	}
	if filters.Visibility != nil && m.Visibility != *filters.Visibility {
		return false
	}
	if filters.CreatedAfter != nil && m.CreatedTs < filters.CreatedAfter.Unix() {
		return false
	}
	if filters.CreatedBefore != nil && m.CreatedTs > filters.CreatedBefore.Unix() {
		return false
	}
	return true
}
