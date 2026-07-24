package ai

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/lithammer/shortuuid/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/store"
)

// Conversation is a private AI chat conversation owned by a single user.
// Conversations always persist until the owner deletes them.
type Conversation struct {
	UID        string
	Title      string
	CreateTime time.Time
	UpdateTime time.Time
}

// Status is the lifecycle state of a chat message.
type Status string

const (
	// StatusStreaming marks an assistant attempt still generating.
	StatusStreaming Status = "STREAMING"
	// StatusComplete marks a fully stored message.
	StatusComplete Status = "COMPLETE"
	// StatusFailed marks an attempt whose generation failed.
	StatusFailed Status = "FAILED"
	// StatusCancelled marks an attempt cancelled before completion.
	StatusCancelled Status = "CANCELLED"
)

// Message is a single chat message. Assistant messages are attempts: Attempt
// numbers the attempts answering one user message from 1, and is 0 for user
// messages.
type Message struct {
	Role            Role
	Content         string
	Status          Status
	Attempt         int32
	ClientRequestID string
	CreateTime      time.Time
	Citations       []Citation
}

// CreateConversation creates a persisted conversation owned by the user.
func (s *Service) CreateConversation(ctx context.Context, user *store.User, title string) (*Conversation, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	created, err := s.store.CreateAIConversation(ctx, &store.AIConversation{
		UID:    shortuuid.New(),
		UserID: user.ID,
		Title:  strings.TrimSpace(title),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create conversation: %v", err)
	}
	return convertConversation(created), nil
}

// ListConversations returns the user's conversations, most recently updated
// first.
func (s *Service) ListConversations(ctx context.Context, user *store.User) ([]*Conversation, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	stored, err := s.store.ListAIConversations(ctx, &store.FindAIConversation{UserID: &user.ID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list conversations: %v", err)
	}
	conversations := make([]*Conversation, 0, len(stored))
	for _, conversation := range stored {
		conversations = append(conversations, convertConversation(conversation))
	}
	return conversations, nil
}

// GetConversation returns one conversation owned by the user with its
// messages in chronological order.
func (s *Service) GetConversation(ctx context.Context, user *store.User, uid string) (*Conversation, []Message, error) {
	conversation, err := s.findConversation(ctx, user, uid)
	if err != nil {
		return nil, nil, err
	}
	stored, err := s.store.ListAIMessages(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "failed to list messages: %v", err)
	}
	messages := make([]Message, 0, len(stored))
	for _, message := range stored {
		messages = append(messages, convertMessage(message))
	}
	return convertConversation(conversation), messages, nil
}

// DeleteConversation deletes one conversation owned by the user. An attempt
// still generating is cancelled first.
func (s *Service) DeleteConversation(ctx context.Context, user *store.User, uid string) error {
	conversation, err := s.findConversation(ctx, user, uid)
	if err != nil {
		return err
	}
	s.active.cancelAll(conversation.ID)
	if err := s.store.DeleteAIConversation(ctx, &store.DeleteAIConversation{ID: conversation.ID}); err != nil {
		// The conversation survived: lift the tombstone so it accepts sends.
		s.active.restore(conversation.ID)
		return status.Errorf(codes.Internal, "failed to delete conversation: %v", err)
	}
	return nil
}

// findConversation resolves an owned conversation by UID. Reads are always
// scoped to the owner; there is no cross-user path.
func (s *Service) findConversation(ctx context.Context, user *store.User, uid string) (*store.AIConversation, error) {
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, status.Errorf(codes.InvalidArgument, "conversation is required")
	}
	conversation, err := s.store.GetAIConversation(ctx, &store.FindAIConversation{UID: &uid, UserID: &user.ID})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get conversation: %v", err)
	}
	if conversation == nil {
		return nil, status.Errorf(codes.NotFound, "conversation not found")
	}
	return conversation, nil
}

// activeAttempts tracks in-flight assistant attempts per conversation so
// deleting a conversation cancels generation that is still running. A
// conversation that entered deletion is tombstoned so late registrations
// fail instead of leaking an uncancellable generation.
type activeAttempts struct {
	mu             sync.Mutex
	byConversation map[int32]map[int32]context.CancelCauseFunc
	deleting       map[int32]struct{}
}

func newActiveAttempts() *activeAttempts {
	return &activeAttempts{
		byConversation: make(map[int32]map[int32]context.CancelCauseFunc),
		deleting:       make(map[int32]struct{}),
	}
}

// register tracks an in-flight attempt. It returns false when the
// conversation is already being deleted, closing the race where a send
// registers after deletion cancelled the active attempts.
func (a *activeAttempts) register(conversationID, attemptID int32, cancel context.CancelCauseFunc) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.deleting[conversationID]; ok {
		return false
	}
	if a.byConversation[conversationID] == nil {
		a.byConversation[conversationID] = make(map[int32]context.CancelCauseFunc)
	}
	a.byConversation[conversationID][attemptID] = cancel
	return true
}

func (a *activeAttempts) unregister(conversationID, attemptID int32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.byConversation[conversationID], attemptID)
	if len(a.byConversation[conversationID]) == 0 {
		delete(a.byConversation, conversationID)
	}
}

// cancelAll cancels every in-flight attempt of a conversation and tombstones
// it so further registrations are refused: the conversation is being deleted.
func (a *activeAttempts) cancelAll(conversationID int32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleting[conversationID] = struct{}{}
	for _, cancel := range a.byConversation[conversationID] {
		cancel(errAttemptCancelled)
	}
}

// restore lifts the deletion tombstone when the store delete failed, so the
// surviving conversation accepts sends again.
func (a *activeAttempts) restore(conversationID int32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.deleting, conversationID)
}

func convertConversation(stored *store.AIConversation) *Conversation {
	return &Conversation{
		UID:        stored.UID,
		Title:      stored.Title,
		CreateTime: time.Unix(stored.CreatedTs, 0),
		UpdateTime: time.Unix(stored.UpdatedTs, 0),
	}
}

func convertMessage(stored *store.AIMessage) Message {
	message := Message{
		Role:       Role(stored.Role.String()),
		Content:    stored.Content,
		Status:     Status(stored.Status.String()),
		Attempt:    stored.Attempt,
		CreateTime: time.Unix(stored.CreatedTs, 0),
	}
	if stored.ClientRequestID != nil {
		message.ClientRequestID = *stored.ClientRequestID
	}
	for _, citation := range stored.Payload.GetCitations() {
		message.Citations = append(message.Citations, Citation{MemoUID: citation.GetMemoUid(), Snippet: citation.GetSnippet()})
	}
	return message
}
