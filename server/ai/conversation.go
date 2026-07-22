package ai

import "sync"

// TODO(issue 03): replace process-local in-memory conversations with
// ai_conversation/ai_message persistence.

// conversationStore keeps per-user chat history in process memory. History is
// lost on restart, which is accepted for the tracer and documented in the
// parent issue.
type conversationStore struct {
	mu     sync.Mutex
	byUser map[int32][]Message
}

func newConversationStore() *conversationStore {
	return &conversationStore{byUser: make(map[int32][]Message)}
}

func (s *conversationStore) append(userID int32, msgs ...Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byUser[userID] = append(s.byUser[userID], msgs...)
}

func (s *conversationStore) get(userID int32) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.byUser[userID]...)
}
