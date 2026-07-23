package store

import (
	"context"

	storepb "github.com/usememos/memos/proto/gen/store"
)

// AIMessageRole identifies the author of an AI chat message.
type AIMessageRole string

const (
	// AIMessageRoleUser is a message authored by the user.
	AIMessageRoleUser AIMessageRole = "USER"
	// AIMessageRoleAssistant is a message authored by the assistant.
	AIMessageRoleAssistant AIMessageRole = "ASSISTANT"
)

// String returns the string representation of the role.
func (r AIMessageRole) String() string {
	return string(r)
}

// AIMessageStatus is the lifecycle state of an AI chat message.
type AIMessageStatus string

const (
	// AIMessageStatusStreaming marks an assistant attempt still generating.
	AIMessageStatusStreaming AIMessageStatus = "STREAMING"
	// AIMessageStatusComplete marks a fully stored message.
	AIMessageStatusComplete AIMessageStatus = "COMPLETE"
	// AIMessageStatusFailed marks an attempt whose generation failed.
	AIMessageStatusFailed AIMessageStatus = "FAILED"
	// AIMessageStatusCancelled marks an attempt cancelled before completion.
	AIMessageStatusCancelled AIMessageStatus = "CANCELLED"
)

// String returns the string representation of the status.
func (s AIMessageStatus) String() string {
	return string(s)
}

// AIMessage is a single message in an AI chat conversation. Assistant
// messages are attempts: ParentID links an attempt to the user message it
// answers and Attempt numbers attempts per user message from 1. User
// messages carry a client request ID unique within their conversation.
// Hidden provider reasoning is neither requested nor stored.
type AIMessage struct {
	ID              int32
	ConversationID  int32
	ParentID        *int32
	Attempt         int32
	Role            AIMessageRole
	Content         string
	Status          AIMessageStatus
	ClientRequestID *string
	Payload         *storepb.AIMessagePayload
	CreatedTs       int64
	UpdatedTs       int64
}

// FindAIMessage filters AI messages.
type FindAIMessage struct {
	ID              *int32
	ConversationID  *int32
	ParentID        *int32
	ClientRequestID *string
	Status          *AIMessageStatus
	Limit           *int
	Offset          *int
}

// UpdateAIMessage updates an AI message. Nil fields are untouched.
type UpdateAIMessage struct {
	ID        int32
	Content   *string
	Status    *AIMessageStatus
	Payload   *storepb.AIMessagePayload
	UpdatedTs *int64
}

// CreateAIMessageWithAttempt atomically persists a user message and its first
// assistant attempt.
func (s *Store) CreateAIMessageWithAttempt(ctx context.Context, message *AIMessage, attempt *AIMessage) (*AIMessage, *AIMessage, error) {
	return s.driver.CreateAIMessageWithAttempt(ctx, message, attempt)
}

// CreateAIMessageAttempt persists a new assistant attempt for an existing
// user message, assigning the next attempt number.
func (s *Store) CreateAIMessageAttempt(ctx context.Context, attempt *AIMessage) (*AIMessage, error) {
	return s.driver.CreateAIMessageAttempt(ctx, attempt)
}

// ListAIMessages lists AI messages matching the filter in chronological order.
func (s *Store) ListAIMessages(ctx context.Context, find *FindAIMessage) ([]*AIMessage, error) {
	return s.driver.ListAIMessages(ctx, find)
}

// GetAIMessage returns the first AI message matching the filter, or nil when
// none exists.
func (s *Store) GetAIMessage(ctx context.Context, find *FindAIMessage) (*AIMessage, error) {
	list, err := s.ListAIMessages(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return list[0], nil
}

// UpdateAIMessage updates an AI message. Updating a deleted row is a no-op.
func (s *Store) UpdateAIMessage(ctx context.Context, update *UpdateAIMessage) error {
	return s.driver.UpdateAIMessage(ctx, update)
}
