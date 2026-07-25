// Package search builds and maintains the derived ai_search_document corpus:
// provider-independent search documents projected from the searchable memo
// corpus (NORMAL, top-level memos). Projection stays outside the memo write
// path: writes emit a lightweight post-commit invalidation signal, and
// periodic reconciliation repairs dropped signals and resume state after
// restart without a durable queue, so a fresh memo eventually appears in
// fuzzy results.
package search

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/usememos/memos/internal/markdown"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
)

const (
	// reconcileInterval is how often a full reconciliation sweep runs.
	reconcileInterval = 5 * time.Minute
	// reconcileBatchSize bounds every list query reconciliation makes.
	reconcileBatchSize = 100
	// backoffBase and backoffMax bound the per-item exponential backoff
	// applied to memos whose sync keeps failing.
	backoffBase = 5 * time.Second
	backoffMax  = 10 * time.Minute
)

// Service maintains the derived search documents for the searchable corpus.
type Service struct {
	store    *store.Store
	memos    *memo.Service
	markdown markdown.Service

	mu       sync.Mutex
	dirty    map[int32]struct{}
	failures map[int32]*failure
	wake     chan struct{}
}

// failure records the per-item backoff state of a memo whose sync failed.
type failure struct {
	count     int
	nextRetry time.Time
}

// NewService creates the search document service.
func NewService(s *store.Store, memos *memo.Service, markdownService markdown.Service) *Service {
	return &Service{
		store:    s,
		memos:    memos,
		markdown: markdownService,
		dirty:    map[int32]struct{}{},
		failures: map[int32]*failure{},
		wake:     make(chan struct{}, 1),
	}
}

// Invalidate emits the post-commit invalidation signal for a memo. It never
// blocks and never fails: the projection is rebuilt outside the write path.
func (s *Service) Invalidate(memoID int32) {
	s.mu.Lock()
	s.dirty[memoID] = struct{}{}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// PendingInvalidations returns how many memos currently hold an unprocessed
// invalidation signal. It is an observability hook for the signal backlog.
func (s *Service) PendingInvalidations() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.dirty)
}

// Run reconciles the corpus until the context is cancelled: an initial pass
// repairs anything missed while the server was down, a full sweep runs every
// reconcileInterval, and invalidation signals trigger targeted syncs.
func (s *Service) Run(ctx context.Context) {
	s.RunOnce(ctx)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.wake:
			s.syncDirty(ctx)
		case <-ticker.C:
			s.RunOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// RunOnce runs one full reconciliation: targeted sync of signaled memos, a
// keyset-paginated sweep over the searchable corpus, and removal of documents
// whose memo left the corpus. The removal only runs when the corpus sweep
// completed — deleting against a partial sweep would drop healthy documents.
func (s *Service) RunOnce(ctx context.Context) {
	s.syncDirty(ctx)
	corpusIDs, complete := s.sweepMemos(ctx)
	if complete {
		s.sweepDocuments(ctx, corpusIDs)
	}
}

// syncDirty syncs the memos named by pending invalidation signals in bounded
// batches. Memos that left the corpus (deleted, archived, or comments) have
// their derived document removed.
func (s *Service) syncDirty(ctx context.Context) {
	for {
		ids := s.takeDirty(reconcileBatchSize)
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			m, err := s.memos.GetSearchableMemo(ctx, id)
			if err != nil {
				s.recordResult(id, err)
				continue
			}
			if m == nil {
				s.recordResult(id, s.store.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: id}))
				continue
			}
			s.syncMemo(ctx, m)
		}
	}
}

// sweepMemos walks the searchable corpus in ascending ID order with keyset
// pagination and syncs every memo whose document is missing or stale. It
// returns the IDs of the whole corpus and whether the sweep completed; the
// document sweep deletes against that set, so a partial sweep must not be
// treated as complete.
func (s *Service) sweepMemos(ctx context.Context) (map[int32]struct{}, bool) {
	corpusIDs := map[int32]struct{}{}
	var afterID int32
	for {
		if ctx.Err() != nil {
			return nil, false
		}
		memos, err := s.memos.ListSearchableMemos(ctx, afterID, reconcileBatchSize)
		if err != nil {
			slog.Warn("search document reconciliation failed to list searchable memos", slog.Any("err", err))
			return nil, false
		}
		for _, m := range memos {
			s.syncMemo(ctx, m)
			corpusIDs[m.ID] = struct{}{}
			afterID = m.ID
		}
		if len(memos) < reconcileBatchSize {
			return corpusIDs, true
		}
	}
}

// sweepDocuments removes documents whose memo left the corpus (deleted or
// archived): any document keyed by a memo ID outside the completed corpus
// sweep. Delete failures are retried by the next sweep.
func (s *Service) sweepDocuments(ctx context.Context, corpusIDs map[int32]struct{}) {
	var afterID int32
	for {
		if ctx.Err() != nil {
			return
		}
		limit := reconcileBatchSize
		documents, err := s.store.ListAISearchDocuments(ctx, &store.FindAISearchDocument{IDGreaterThan: &afterID, Limit: &limit})
		if err != nil {
			slog.Warn("search document reconciliation failed to list documents", slog.Any("err", err))
			return
		}
		for _, document := range documents {
			afterID = document.ID
			if _, ok := corpusIDs[document.MemoID]; ok {
				continue
			}
			if err := s.store.DeleteAISearchDocument(ctx, &store.DeleteAISearchDocument{MemoID: document.MemoID}); err != nil {
				slog.Warn("search document reconciliation failed to delete document", slog.Int64("memo_id", int64(document.MemoID)), slog.Any("err", err))
			}
		}
		if len(documents) < reconcileBatchSize {
			return
		}
	}
}

// syncMemo syncs one corpus memo's document, honoring the per-item backoff.
func (s *Service) syncMemo(ctx context.Context, m *store.Memo) {
	if !s.retryAllowed(m.ID) {
		return
	}
	s.recordResult(m.ID, s.syncMemoOnce(ctx, m))
}

// syncMemoOnce rebuilds a memo's document when its content hash or the
// projection/normalization versions drifted, refreshes the recorded source
// revision and UID when only they changed, and does nothing when the document
// is current.
func (s *Service) syncMemoOnce(ctx context.Context, m *store.Memo) error {
	existing, err := s.store.GetAISearchDocument(ctx, &store.FindAISearchDocument{MemoID: &m.ID})
	if err != nil {
		return err
	}
	if existing != nil &&
		existing.ContentHash == contentHash(m.Content) &&
		existing.ProjectionVersion == ProjectionVersion &&
		existing.NormalizationVersion == NormalizationVersion {
		if existing.MemoUpdatedTs == m.UpdatedTs && existing.MemoUID == m.UID {
			return nil
		}
		// Only the source revision or UID drifted (for example a pin or a
		// visibility change): refresh them without a rebuild.
		existing.MemoUpdatedTs = m.UpdatedTs
		existing.MemoUID = m.UID
		_, err := s.store.UpsertAISearchDocument(ctx, existing)
		return err
	}
	document, err := s.buildDocument(m)
	if err != nil {
		return err
	}
	_, err = s.store.UpsertAISearchDocument(ctx, document)
	return err
}

// takeDirty drains up to limit pending invalidation signals.
func (s *Service) takeDirty(limit int) []int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int32, 0, min(limit, len(s.dirty)))
	for id := range s.dirty {
		if len(ids) >= limit {
			break
		}
		ids = append(ids, id)
		delete(s.dirty, id)
	}
	return ids
}

// retryAllowed reports whether the memo's sync may run now under its backoff.
func (s *Service) retryAllowed(memoID int32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[memoID]
	return !ok || !time.Now().Before(f.nextRetry)
}

// recordResult clears the memo's backoff on success and backs off
// exponentially on failure, so a poisoned memo cannot stall reconciliation.
func (s *Service) recordResult(memoID int32, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.failures, memoID)
		return
	}
	f := s.failures[memoID]
	if f == nil {
		f = &failure{}
		s.failures[memoID] = f
	}
	f.count++
	delay := backoffBase
	for i := 1; i < f.count && delay < backoffMax; i++ {
		delay *= 2
	}
	if delay > backoffMax {
		delay = backoffMax
	}
	f.nextRetry = time.Now().Add(delay)
	slog.Warn("search document sync failed; backing off",
		slog.Int64("memo_id", int64(memoID)),
		slog.Int("failures", f.count),
		slog.Time("next_retry", f.nextRetry),
		slog.Any("err", err))
}
