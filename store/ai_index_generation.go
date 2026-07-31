package store

import (
	"context"
)

// AIIndexGeneration is one versioned embedding index generation. The
// Fingerprint identifies the embedding endpoint configuration (provider,
// endpoint identity, model, dimensions) the generation was built with, so
// index rows built by another configuration are isolated and can be rebuilt.
// State is BUILDING, ACTIVE, or RETIRED; MemoTotal and MemoIndexed track build
// progress; LastError records the most recent build failure.
type AIIndexGeneration struct {
	ID               int32
	Fingerprint      string
	ProviderID       string
	ProviderType     string
	EndpointIdentity string
	Model            string
	Dimensions       int32
	State            string
	MemoTotal        int32
	MemoIndexed      int32
	LastError        string
	CreatedTs        int64
	UpdatedTs        int64
}

// FindAIIndexGeneration filters AI index generations.
type FindAIIndexGeneration struct {
	ID          *int32
	Fingerprint *string
	State       *string
}

// DeleteAIIndexGeneration deletes an AI index generation by ID.
type DeleteAIIndexGeneration struct {
	ID *int32
}

// UpsertAIIndexGeneration creates or replaces the index generation identified
// by its fingerprint.
func (s *Store) UpsertAIIndexGeneration(ctx context.Context, upsert *AIIndexGeneration) (*AIIndexGeneration, error) {
	return s.driver.UpsertAIIndexGeneration(ctx, upsert)
}

// ListAIIndexGenerations lists AI index generations matching the filter, in
// ascending ID order.
func (s *Store) ListAIIndexGenerations(ctx context.Context, find *FindAIIndexGeneration) ([]*AIIndexGeneration, error) {
	return s.driver.ListAIIndexGenerations(ctx, find)
}

// GetAIIndexGeneration returns the first AI index generation matching the
// filter, or nil when none exists.
func (s *Store) GetAIIndexGeneration(ctx context.Context, find *FindAIIndexGeneration) (*AIIndexGeneration, error) {
	list, err := s.ListAIIndexGenerations(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// DeleteAIIndexGeneration deletes an AI index generation. Deleting a missing
// generation is not an error.
func (s *Store) DeleteAIIndexGeneration(ctx context.Context, delete *DeleteAIIndexGeneration) error {
	return s.driver.DeleteAIIndexGeneration(ctx, delete)
}
