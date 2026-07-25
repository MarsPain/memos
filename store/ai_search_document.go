package store

import (
	"context"
)

// AISearchDocumentSpan maps a byte range of the projected content back to the
// byte range of the source memo content it was projected from.
type AISearchDocumentSpan struct {
	ContentStart int `json:"content_start"`
	ContentEnd   int `json:"content_end"`
	SourceStart  int `json:"source_start"`
	SourceEnd    int `json:"source_end"`
}

// AISearchDocument is the provider-independent derived search document for one
// memo in the searchable corpus (NORMAL, top-level memos). It is derived data:
// rebuildable from the source memo and deletable without touching memos.
// MemoUpdatedTs and ContentHash record the source revision the document was
// built from; ProjectionVersion and NormalizationVersion isolate documents
// built by other projection/normalization versions so they can be excluded
// from retrieval and rebuilt.
type AISearchDocument struct {
	ID                   int32
	MemoID               int32
	MemoUID              string
	MemoUpdatedTs        int64
	ContentHash          string
	ProjectionVersion    int32
	NormalizationVersion int32
	// Title, Tags, and Content are the normalized projection of the source
	// memo: the H1-derived title, the extracted tags, and the plain-text
	// projection of the markdown content.
	Title   string
	Tags    []string
	Content string
	// Spans maps Content byte ranges back to source memo content byte ranges.
	Spans     []*AISearchDocumentSpan
	CreatedTs int64
	UpdatedTs int64
}

// FindAISearchDocument filters AI search documents. IDGreaterThan with Limit
// gives keyset pagination in ascending ID order for bounded reconciliation
// sweeps.
type FindAISearchDocument struct {
	ID            *int32
	MemoID        *int32
	IDGreaterThan *int32
	Limit         *int
}

// DeleteAISearchDocument deletes the search document of a memo.
type DeleteAISearchDocument struct {
	MemoID int32
}

// UpsertAISearchDocument creates or replaces the search document of a memo.
func (s *Store) UpsertAISearchDocument(ctx context.Context, upsert *AISearchDocument) (*AISearchDocument, error) {
	return s.driver.UpsertAISearchDocument(ctx, upsert)
}

// ListAISearchDocuments lists AI search documents matching the filter, in
// ascending ID order.
func (s *Store) ListAISearchDocuments(ctx context.Context, find *FindAISearchDocument) ([]*AISearchDocument, error) {
	return s.driver.ListAISearchDocuments(ctx, find)
}

// GetAISearchDocument returns the first AI search document matching the
// filter, or nil when none exists.
func (s *Store) GetAISearchDocument(ctx context.Context, find *FindAISearchDocument) (*AISearchDocument, error) {
	list, err := s.ListAISearchDocuments(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// DeleteAISearchDocument deletes the search document of a memo. Deleting a
// missing document is not an error.
func (s *Store) DeleteAISearchDocument(ctx context.Context, delete *DeleteAISearchDocument) error {
	return s.driver.DeleteAISearchDocument(ctx, delete)
}
