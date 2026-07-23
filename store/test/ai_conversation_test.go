package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

func createTestingAIConversation(ctx context.Context, t *testing.T, ts *store.Store, userID int32, uid string) *store.AIConversation {
	t.Helper()
	conversation, err := ts.CreateAIConversation(ctx, &store.AIConversation{UID: uid, UserID: userID})
	require.NoError(t, err)
	return conversation
}

func createTestingUserMessage(ctx context.Context, t *testing.T, ts *store.Store, conversationID int32, requestID string) (*store.AIMessage, *store.AIMessage) {
	t.Helper()
	message, attempt, err := ts.CreateAIMessageWithAttempt(ctx, &store.AIMessage{
		ConversationID:  conversationID,
		Role:            store.AIMessageRoleUser,
		Content:         "question",
		Status:          store.AIMessageStatusComplete,
		ClientRequestID: &requestID,
		Payload:         &storepb.AIMessagePayload{},
	}, &store.AIMessage{
		ConversationID: conversationID,
		Role:           store.AIMessageRoleAssistant,
		Status:         store.AIMessageStatusStreaming,
		Payload: &storepb.AIMessagePayload{
			ProviderId: "p",
			Model:      "chat-model",
			Citations:  []*storepb.AIMessagePayload_Citation{{MemoUid: "m1", Snippet: "snippet"}},
		},
	})
	require.NoError(t, err)
	return message, attempt
}

func TestAIConversation(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	user, err := createTestingHostUser(ctx, ts)
	require.NoError(t, err)
	other, err := createTestingUserWithRole(ctx, ts, "other", store.RoleUser)
	require.NoError(t, err)

	conversation := createTestingAIConversation(ctx, t, ts, user.ID, "conv-1")
	require.Equal(t, "conv-1", conversation.UID)
	require.Equal(t, user.ID, conversation.UserID)
	require.NotZero(t, conversation.CreatedTs)
	require.NotZero(t, conversation.UpdatedTs)

	t.Run("get is owner scoped", func(t *testing.T) {
		found, err := ts.GetAIConversation(ctx, &store.FindAIConversation{UID: &conversation.UID, UserID: &user.ID})
		require.NoError(t, err)
		require.NotNil(t, found)
		require.Equal(t, conversation.ID, found.ID)

		// The same UID under a different owner never matches.
		notFound, err := ts.GetAIConversation(ctx, &store.FindAIConversation{UID: &conversation.UID, UserID: &other.ID})
		require.NoError(t, err)
		require.Nil(t, notFound)
	})

	t.Run("duplicate uid is rejected", func(t *testing.T) {
		_, err := ts.CreateAIConversation(ctx, &store.AIConversation{UID: "conv-1", UserID: user.ID})
		require.Error(t, err)
	})

	t.Run("list orders most recently updated first", func(t *testing.T) {
		older := createTestingAIConversation(ctx, t, ts, user.ID, "conv-2")
		// Bump conv-1 so it outranks the newer conversation.
		later := conversation.UpdatedTs + 100
		require.NoError(t, ts.UpdateAIConversation(ctx, &store.UpdateAIConversation{ID: conversation.ID, UpdatedTs: &later}))

		list, err := ts.ListAIConversations(ctx, &store.FindAIConversation{UserID: &user.ID})
		require.NoError(t, err)
		require.Len(t, list, 2)
		require.Equal(t, conversation.ID, list[0].ID)
		require.Equal(t, older.ID, list[1].ID)

		// Other users see nothing.
		otherList, err := ts.ListAIConversations(ctx, &store.FindAIConversation{UserID: &other.ID})
		require.NoError(t, err)
		require.Empty(t, otherList)
	})

	t.Run("update sets title", func(t *testing.T) {
		title := "trip planning"
		require.NoError(t, ts.UpdateAIConversation(ctx, &store.UpdateAIConversation{ID: conversation.ID, Title: &title}))
		found, err := ts.GetAIConversation(ctx, &store.FindAIConversation{ID: &conversation.ID})
		require.NoError(t, err)
		require.Equal(t, title, found.Title)
	})
}

func TestAIMessage(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	user, err := createTestingHostUser(ctx, ts)
	require.NoError(t, err)
	conversation := createTestingAIConversation(ctx, t, ts, user.ID, "conv-1")

	t.Run("create with attempt persists both atomically", func(t *testing.T) {
		message, attempt := createTestingUserMessage(ctx, t, ts, conversation.ID, "req-1")
		require.Equal(t, int32(0), message.Attempt)
		require.Nil(t, message.ParentID)
		require.Equal(t, store.AIMessageRoleUser, message.Role)
		require.Equal(t, store.AIMessageStatusComplete, message.Status)
		require.NotNil(t, message.ClientRequestID)
		require.Equal(t, "req-1", *message.ClientRequestID)
		require.NotZero(t, message.CreatedTs)

		require.Equal(t, int32(1), attempt.Attempt)
		require.NotNil(t, attempt.ParentID)
		require.Equal(t, message.ID, *attempt.ParentID)
		require.Equal(t, store.AIMessageStatusStreaming, attempt.Status)
		// The payload round-trips.
		require.Equal(t, "p", attempt.Payload.GetProviderId())
		require.Equal(t, "chat-model", attempt.Payload.GetModel())
		require.Len(t, attempt.Payload.GetCitations(), 1)
		require.Equal(t, "m1", attempt.Payload.GetCitations()[0].GetMemoUid())

		messages, err := ts.ListAIMessages(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
		require.NoError(t, err)
		require.Len(t, messages, 2)
	})

	t.Run("duplicate client request id is rejected", func(t *testing.T) {
		requestID := "req-1"
		_, _, err := ts.CreateAIMessageWithAttempt(ctx, &store.AIMessage{
			ConversationID:  conversation.ID,
			Role:            store.AIMessageRoleUser,
			Content:         "question again",
			Status:          store.AIMessageStatusComplete,
			ClientRequestID: &requestID,
			Payload:         &storepb.AIMessagePayload{},
		}, &store.AIMessage{
			ConversationID: conversation.ID,
			Role:           store.AIMessageRoleAssistant,
			Status:         store.AIMessageStatusStreaming,
			Payload:        &storepb.AIMessagePayload{},
		})
		require.Error(t, err)

		// Nothing was duplicated.
		messages, listErr := ts.ListAIMessages(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
		require.NoError(t, listErr)
		require.Len(t, messages, 2)
	})

	t.Run("attempts number sequentially per user message", func(t *testing.T) {
		message, err := ts.GetAIMessage(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
		require.NoError(t, err)
		require.NotNil(t, message)

		second, err := ts.CreateAIMessageAttempt(ctx, &store.AIMessage{
			ConversationID: conversation.ID,
			ParentID:       &message.ID,
			Role:           store.AIMessageRoleAssistant,
			Status:         store.AIMessageStatusStreaming,
			Payload:        &storepb.AIMessagePayload{},
		})
		require.NoError(t, err)
		require.Equal(t, int32(2), second.Attempt)

		third, err := ts.CreateAIMessageAttempt(ctx, &store.AIMessage{
			ConversationID: conversation.ID,
			ParentID:       &message.ID,
			Role:           store.AIMessageRoleAssistant,
			Status:         store.AIMessageStatusStreaming,
			Payload:        &storepb.AIMessagePayload{},
		})
		require.NoError(t, err)
		require.Equal(t, int32(3), third.Attempt)
	})

	t.Run("update sets terminal state", func(t *testing.T) {
		message, err := ts.GetAIMessage(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
		require.NoError(t, err)
		attempts, err := ts.ListAIMessages(ctx, &store.FindAIMessage{ParentID: &message.ID})
		require.NoError(t, err)
		require.NotEmpty(t, attempts)
		attempt := attempts[len(attempts)-1]

		status := store.AIMessageStatusComplete
		content := "answer"
		payload := &storepb.AIMessagePayload{ProviderId: "p", Model: "chat-model", TotalTokens: 18}
		require.NoError(t, ts.UpdateAIMessage(ctx, &store.UpdateAIMessage{ID: attempt.ID, Status: &status, Content: &content, Payload: payload}))

		updated, err := ts.GetAIMessage(ctx, &store.FindAIMessage{ID: &attempt.ID})
		require.NoError(t, err)
		require.Equal(t, store.AIMessageStatusComplete, updated.Status)
		require.Equal(t, "answer", updated.Content)
		require.Equal(t, int32(18), updated.Payload.GetTotalTokens())

		// Updating a deleted row is not an error.
		require.NoError(t, ts.UpdateAIMessage(ctx, &store.UpdateAIMessage{ID: attempt.ID + 1000, Status: &status}))
	})
}

func TestAIConversationDeleteCascadesMessages(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	defer ts.Close()

	user, err := createTestingHostUser(ctx, ts)
	require.NoError(t, err)
	conversation := createTestingAIConversation(ctx, t, ts, user.ID, "conv-1")
	createTestingUserMessage(ctx, t, ts, conversation.ID, "req-1")

	require.NoError(t, ts.DeleteAIConversation(ctx, &store.DeleteAIConversation{ID: conversation.ID}))

	messages, err := ts.ListAIMessages(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
	require.NoError(t, err)
	require.Empty(t, messages)

	found, err := ts.GetAIConversation(ctx, &store.FindAIConversation{ID: &conversation.ID})
	require.NoError(t, err)
	require.Nil(t, found)

	// Deleting a missing conversation is not an error.
	require.NoError(t, ts.DeleteAIConversation(ctx, &store.DeleteAIConversation{ID: conversation.ID}))
}
