package store

import (
	"context"
)

// AIConversation is a private AI chat conversation owned by a single user.
// Conversations persist until the owner deletes them; chat history is stored
// apart from memos and is never indexed as memos.
type AIConversation struct {
	ID        int32
	UID       string
	UserID    int32
	Title     string
	CreatedTs int64
	UpdatedTs int64
}

// FindAIConversation filters AI conversations. UserID scopes reads to the
// owner; the service layer always sets it.
type FindAIConversation struct {
	ID     *int32
	UID    *string
	UserID *int32
	Limit  *int
	Offset *int
}

// UpdateAIConversation updates an AI conversation. Nil fields are untouched.
type UpdateAIConversation struct {
	ID        int32
	Title     *string
	UpdatedTs *int64
}

// DeleteAIConversation deletes an AI conversation and all of its messages.
type DeleteAIConversation struct {
	ID int32
}

// CreateAIConversation creates a new AI conversation.
func (s *Store) CreateAIConversation(ctx context.Context, create *AIConversation) (*AIConversation, error) {
	return s.driver.CreateAIConversation(ctx, create)
}

// ListAIConversations lists AI conversations matching the filter, most
// recently updated first.
func (s *Store) ListAIConversations(ctx context.Context, find *FindAIConversation) ([]*AIConversation, error) {
	return s.driver.ListAIConversations(ctx, find)
}

// GetAIConversation returns the first AI conversation matching the filter, or
// nil when none exists.
func (s *Store) GetAIConversation(ctx context.Context, find *FindAIConversation) (*AIConversation, error) {
	list, err := s.ListAIConversations(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// UpdateAIConversation updates an AI conversation.
func (s *Store) UpdateAIConversation(ctx context.Context, update *UpdateAIConversation) error {
	return s.driver.UpdateAIConversation(ctx, update)
}

// DeleteAIConversation deletes an AI conversation and all of its messages.
func (s *Store) DeleteAIConversation(ctx context.Context, delete *DeleteAIConversation) error {
	return s.driver.DeleteAIConversation(ctx, delete)
}
