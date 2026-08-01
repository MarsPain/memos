package store

import (
	"context"
)

// AI index generation lifecycle states. A building generation never serves
// queries; at most one generation is active at a time; a retired generation
// is removed after a bounded grace period.
const (
	// AIIndexGenerationBuilding marks a generation whose indexing pass has
	// not completed. It never serves queries.
	AIIndexGenerationBuilding = "BUILDING"
	// AIIndexGenerationActive marks the generation semantic queries scan.
	AIIndexGenerationActive = "ACTIVE"
	// AIIndexGenerationRetired marks a generation replaced by a newer active
	// one. It never serves queries and is removed after the grace period.
	AIIndexGenerationRetired = "RETIRED"
)

// AIIndexGeneration is one versioned embedding index generation. The
// Fingerprint identifies the embedding endpoint configuration (provider,
// endpoint identity, model, dimensions) the generation was built with, so
// index rows built by another configuration are isolated and can be rebuilt.
// State is BUILDING, ACTIVE, or RETIRED; MemoTotal and MemoIndexed track build
// progress; LastError records the most recent build failure. RetiredTs records
// when the generation retired (zero while it has not), bounding the grace
// period before removal.
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
	RetiredTs        int64
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

// AIIndexGenerationPromotion is the conditional atomic cutover of a verified
// building generation: the generation identified by ID promotes to ACTIVE
// only if it is still BUILDING and still matches the desired embedding
// assignment (Fingerprint), and every other ACTIVE generation retires in the
// same transaction with RetiredTs recorded. The cutover is atomic: queries
// never observe zero or two ACTIVE generations partway through.
type AIIndexGenerationPromotion struct {
	ID          int32
	Fingerprint string
	RetiredTs   int64
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

// PromoteAIIndexGeneration promotes a verified building generation to ACTIVE
// and retires the former ACTIVE generation in a single conditional
// transaction. It reports whether the promotion happened: it does not when
// the generation is no longer building or no longer matches the desired
// embedding fingerprint, and no state changes in that case.
func (s *Store) PromoteAIIndexGeneration(ctx context.Context, promote *AIIndexGenerationPromotion) (bool, error) {
	return s.driver.PromoteAIIndexGeneration(ctx, promote)
}

// DeleteAIIndexGeneration deletes an AI index generation. Deleting a missing
// generation is not an error.
func (s *Store) DeleteAIIndexGeneration(ctx context.Context, delete *DeleteAIIndexGeneration) error {
	return s.driver.DeleteAIIndexGeneration(ctx, delete)
}
