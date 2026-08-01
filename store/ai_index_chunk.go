package store

import (
	"context"
)

// AIIndexChunk is one embedded chunk of a Memo search document within a
// generation. Vector is the little-endian float32 embedding of the chunk
// (internal/ai vector encoding version 1); MemoRevision and ContentHash
// record the search document revision and content hash the chunk was built
// from, so reconciliation detects staleness from either. ContentStart/
// ContentEnd and SourceStart/SourceEnd map the chunk back to the projected
// content and the source memo content. Chunks are derived data: rebuildable
// from the source search documents and deletable without touching memos.
type AIIndexChunk struct {
	ID           int32
	GenerationID int32
	MemoID       int32
	MemoRevision int64
	ChunkOrdinal int32
	ContentStart int32
	ContentEnd   int32
	SourceStart  int32
	SourceEnd    int32
	Vector       []byte
	Dimensions   int32
	ContentHash  string
	IndexedTs    int64
}

// FindAIIndexChunk filters AI index chunks. IDGreaterThan with Limit gives
// keyset pagination in ascending ID order for bounded scans of a generation.
type FindAIIndexChunk struct {
	ID            *int32
	GenerationID  *int32
	MemoID        *int32
	IDGreaterThan *int32
	Limit         *int
}

// DeleteAIIndexChunk deletes AI index chunks matching whichever of ID,
// GenerationID, and MemoID are set.
type DeleteAIIndexChunk struct {
	ID           *int32
	GenerationID *int32
	MemoID       *int32
}

// UpsertAIIndexChunk creates or replaces the chunk identified by
// (generation_id, memo_id, memo_revision, chunk_ordinal).
func (s *Store) UpsertAIIndexChunk(ctx context.Context, upsert *AIIndexChunk) (*AIIndexChunk, error) {
	return s.driver.UpsertAIIndexChunk(ctx, upsert)
}

// ListAIIndexChunks lists AI index chunks matching the filter, in ascending
// ID order.
func (s *Store) ListAIIndexChunks(ctx context.Context, find *FindAIIndexChunk) ([]*AIIndexChunk, error) {
	return s.driver.ListAIIndexChunks(ctx, find)
}

// GetAIIndexChunk returns the first AI index chunk matching the filter, or
// nil when none exists.
func (s *Store) GetAIIndexChunk(ctx context.Context, find *FindAIIndexChunk) (*AIIndexChunk, error) {
	list, err := s.ListAIIndexChunks(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// DeleteAIIndexChunk deletes AI index chunks matching the filter. Deleting
// missing chunks is not an error.
func (s *Store) DeleteAIIndexChunk(ctx context.Context, delete *DeleteAIIndexChunk) error {
	return s.driver.DeleteAIIndexChunk(ctx, delete)
}
