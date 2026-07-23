package ai_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/internal/ai/gateway"
	storepb "github.com/usememos/memos/proto/gen/store"
	serverai "github.com/usememos/memos/server/ai"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

type chatTestFixture struct {
	service *serverai.Service
	store   *store.Store
	model   *aitest.Model
	factory func() gateway.ModelFactory
}

func newChatTestFixture(ctx context.Context, t *testing.T, model internalai.Model, factoryErr error) *chatTestFixture {
	st := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() { _ = st.Close() })
	fake, _ := model.(*aitest.Model)
	factory := func() gateway.ModelFactory {
		return func(internalai.ProviderConfig, *http.Client) (internalai.Model, error) {
			if factoryErr != nil {
				return nil, factoryErr
			}
			return model, nil
		}
	}
	service := serverai.NewService(st, memo.NewService(st), factory)
	return &chatTestFixture{service: service, store: st, model: fake, factory: factory}
}

func (f *chatTestFixture) configureGeneration(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := f.store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
		Key: storepb.InstanceSettingKey_AI,
		Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
			Providers: []*storepb.AIProviderConfig{{
				Id: "p", Title: "P", Type: storepb.AIProviderType_OPENAI,
				Endpoint: "https://api.example.com/v1", ApiKey: "sk-test",
			}},
			Generation: &storepb.GenerationConfig{ProviderId: "p", Model: "chat-model"},
		}},
	})
	require.NoError(t, err)
}

func (f *chatTestFixture) createUser(ctx context.Context, t *testing.T, username string) *store.User {
	t.Helper()
	user, err := f.store.CreateUser(ctx, &store.User{Username: username, Role: store.RoleUser, Email: username + "@example.com"})
	require.NoError(t, err)
	return user
}

func (f *chatTestFixture) createMemo(ctx context.Context, t *testing.T, creatorID int32, uid, content string, visibility store.Visibility) *store.Memo {
	t.Helper()
	created, err := f.store.CreateMemo(ctx, &store.Memo{UID: uid, CreatorID: creatorID, Content: content, Visibility: visibility})
	require.NoError(t, err)
	return created
}

func (f *chatTestFixture) createConversation(ctx context.Context, t *testing.T, user *store.User, title string) *serverai.Conversation {
	t.Helper()
	conversation, err := f.service.CreateConversation(ctx, user, title)
	require.NoError(t, err)
	return conversation
}

func (f *chatTestFixture) getMessages(ctx context.Context, t *testing.T, user *store.User, uid string) []serverai.Message {
	t.Helper()
	_, messages, err := f.service.GetConversation(ctx, user, uid)
	require.NoError(t, err)
	return messages
}

func TestGenerationAvailable(t *testing.T) {
	ctx := context.Background()

	t.Run("false when not configured", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		available, err := fixture.service.GenerationAvailable(ctx)
		require.NoError(t, err)
		require.False(t, available)
	})

	t.Run("false when generation provider is missing", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		_, err := fixture.store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
			Key: storepb.InstanceSettingKey_AI,
			Value: &storepb.InstanceSetting_AiSetting{AiSetting: &storepb.InstanceAISetting{
				Generation: &storepb.GenerationConfig{ProviderId: "missing", Model: "chat-model"},
			}},
		})
		require.NoError(t, err)
		available, err := fixture.service.GenerationAvailable(ctx)
		require.NoError(t, err)
		require.False(t, available)
	})

	t.Run("true when configured", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		available, err := fixture.service.GenerationAvailable(ctx)
		require.NoError(t, err)
		require.True(t, available)
	})
}

func TestSendMessageValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("fails when generation is not configured", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		// Nothing is persisted when generation is unavailable.
		require.Empty(t, fixture.getMessages(ctx, t, user, conversation.UID))
	})

	t.Run("rejects empty content", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "   ", "req-1")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects empty request id", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "hello", " ")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects unknown conversation", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")

		_, _, err := fixture.service.SendMessage(ctx, user, "missing", "hello", "req-1")
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("requires authentication", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)

		_, _, err := fixture.service.SendMessage(ctx, nil, "any", "hello", "req-1")
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestSendMessageGroundedAnswer(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{
			Content: "Alpine lakes are great for hiking.",
			Usage:   internalai.Usage{InputTokens: 10, OutputTokens: 8, TotalTokens: 18},
		},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")

	aliceMemo := fixture.createMemo(ctx, t, alice.ID, "alice-hiking", "My favorite hiking spot is the alpine lakes trail.", store.Private)
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)
	// Bob's memo matches the query but must never be cited in Alice's answer.
	fixture.createMemo(ctx, t, bob.ID, "bob-hiking", "Bob's secret hiking spots.", store.Private)

	conversation := fixture.createConversation(ctx, t, alice, "")
	userMsg, assistantMsg, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "Where should I go hiking", "req-1")
	require.NoError(t, err)
	require.Equal(t, serverai.RoleUser, userMsg.Role)
	require.Equal(t, serverai.StatusComplete, userMsg.Status)
	require.Equal(t, "Where should I go hiking", userMsg.Content)
	require.Equal(t, "req-1", userMsg.ClientRequestID)
	require.Equal(t, serverai.RoleAssistant, assistantMsg.Role)
	require.Equal(t, serverai.StatusComplete, assistantMsg.Status)
	require.Equal(t, int32(1), assistantMsg.Attempt)
	require.Equal(t, "Alpine lakes are great for hiking.", assistantMsg.Content)

	require.Len(t, assistantMsg.Citations, 1)
	require.Equal(t, aliceMemo.UID, assistantMsg.Citations[0].MemoUID)
	require.NotEmpty(t, assistantMsg.Citations[0].Snippet)

	// The generation request carries instructions plus the question and the
	// quoted memo context.
	require.Len(t, fixture.model.GenerationRequests, 1)
	request := fixture.model.GenerationRequests[0]
	require.Equal(t, "chat-model", request.Model)
	require.Len(t, request.Messages, 2)
	require.Equal(t, internalai.RoleSystem, request.Messages[0].Role)
	require.Equal(t, internalai.RoleUser, request.Messages[1].Role)
	require.Contains(t, request.Messages[1].Content, "Where should I go hiking")
	require.Contains(t, request.Messages[1].Content, "<memo name=\"memos/"+aliceMemo.UID+"\">")
	require.NotContains(t, request.Messages[1].Content, "bob-hiking")

	// Both messages are persisted with their terminal states, and the
	// conversation title comes from the first user message.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.RoleUser, messages[0].Role)
	require.Equal(t, serverai.RoleAssistant, messages[1].Role)
	stored, err := fixture.store.GetAIConversation(ctx, &store.FindAIConversation{UID: &conversation.UID})
	require.NoError(t, err)
	require.Equal(t, "Where should I go hiking", stored.Title)
}

func TestSendMessageWithoutMatchingMemos(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "I don't know."},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, assistantMsg, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "What is the capital of France?", "req-1")
	require.NoError(t, err)
	require.Empty(t, assistantMsg.Citations)
	require.NotContains(t, fixture.model.GenerationRequests[0].Messages[1].Content, "<memo name=")
}

func TestSendMessageFactoryAndGenerateErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("factory error maps to failed precondition", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, nil, errors.New("unknown provider type"))
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")
		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("generate error persists a failed attempt", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{GenerateError: errors.New("provider unavailable")}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.Internal, status.Code(err))

		messages := fixture.getMessages(ctx, t, user, conversation.UID)
		require.Len(t, messages, 2)
		require.Equal(t, serverai.StatusComplete, messages[0].Status)
		require.Equal(t, serverai.StatusFailed, messages[1].Status)
		require.Empty(t, messages[1].Content)
	})

	t.Run("empty answer persists a failed attempt", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{GenerateResponse: internalai.GenerationResponse{Content: "  "}}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, _, err := fixture.service.SendMessage(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.Internal, status.Code(err))

		messages := fixture.getMessages(ctx, t, user, conversation.UID)
		require.Len(t, messages, 2)
		require.Equal(t, serverai.StatusFailed, messages[1].Status)
	})
}

func TestSendMessageDuplicateRequestID(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "answer"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	userMsg, assistantMsg, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)

	// A repeated request ID returns the existing pair without regenerating.
	duplicateUserMsg, duplicateAssistantMsg, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	require.Equal(t, userMsg.Content, duplicateUserMsg.Content)
	require.Equal(t, assistantMsg.Content, duplicateAssistantMsg.Content)
	require.Len(t, fixture.model.GenerationRequests, 1)

	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)

	// A different request ID on the same conversation is a new message.
	_, _, err = fixture.service.SendMessage(ctx, alice, conversation.UID, "question", "req-2")
	require.NoError(t, err)
	require.Len(t, fixture.getMessages(ctx, t, alice, conversation.UID), 4)
}

func TestSendMessageRetryAfterFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{GenerateError: errors.New("provider down")}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, _, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "question", "req-1")
	require.Equal(t, codes.Internal, status.Code(err))

	// Retrying with the same request ID creates a new attempt on the same
	// user message instead of duplicating it.
	fixture.model.GenerateError = nil
	fixture.model.GenerateResponse = internalai.GenerationResponse{Content: "recovered"}
	userMsg, assistantMsg, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	require.Equal(t, "question", userMsg.Content)
	require.Equal(t, int32(2), assistantMsg.Attempt)
	require.Equal(t, serverai.StatusComplete, assistantMsg.Status)
	require.Equal(t, "recovered", assistantMsg.Content)

	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 3)
	require.Equal(t, serverai.RoleUser, messages[0].Role)
	require.Equal(t, int32(1), messages[1].Attempt)
	require.Equal(t, serverai.StatusFailed, messages[1].Status)
	require.Equal(t, int32(2), messages[2].Attempt)
	require.Equal(t, serverai.StatusComplete, messages[2].Status)
}

// blockingModel blocks in Generate until its context is cancelled.
type blockingModel struct {
	aitest.Model
	started chan struct{}
	once    sync.Once
}

func (m *blockingModel) Generate(ctx context.Context, _ internalai.GenerationRequest) (internalai.GenerationResponse, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return internalai.GenerationResponse{}, ctx.Err()
}

func TestSendMessageCancelledByCaller(t *testing.T) {
	ctx := context.Background()
	model := &blockingModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendCtx, cancelSend := context.WithCancel(ctx)
	sendErr := make(chan error, 1)
	go func() {
		_, _, err := fixture.service.SendMessage(sendCtx, alice, conversation.UID, "hello", "req-1")
		sendErr <- err
	}()
	<-model.started
	cancelSend()
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))

	// The cancellation is persisted: the attempt is CANCELLED.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.StatusCancelled, messages[1].Status)
}

func TestDeleteConversationCancelsActiveAttempt(t *testing.T) {
	ctx := context.Background()
	model := &blockingModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendErr := make(chan error, 1)
	go func() {
		_, _, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "hello", "req-1")
		sendErr <- err
	}()
	<-model.started
	require.NoError(t, fixture.service.DeleteConversation(ctx, alice, conversation.UID))
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))

	// The conversation and its messages are gone.
	_, _, err := fixture.service.GetConversation(ctx, alice, conversation.UID)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestConversationSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "answer"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, _, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "first question", "req-1")
	require.NoError(t, err)

	// A fresh service instance over the same store (a restart) sees the
	// persisted conversation and messages.
	restarted := serverai.NewService(fixture.store, memo.NewService(fixture.store), fixture.factory)
	conversations, err := restarted.ListConversations(ctx, alice)
	require.NoError(t, err)
	require.Len(t, conversations, 1)
	require.Equal(t, conversation.UID, conversations[0].UID)
	require.Equal(t, "first question", conversations[0].Title)

	_, messages, err := restarted.GetConversation(ctx, alice, conversation.UID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "first question", messages[0].Content)
	require.Equal(t, "answer", messages[1].Content)
}

func TestRestartReconcilesInterruptedAttempts(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "recovered"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	// Simulate a crash mid-generation: a user message whose attempt is stuck
	// STREAMING in the store.
	storedConversation, err := fixture.store.GetAIConversation(ctx, &store.FindAIConversation{UID: &conversation.UID, UserID: &alice.ID})
	require.NoError(t, err)
	requestID := "req-1"
	_, _, err = fixture.store.CreateAIMessageWithAttempt(ctx, &store.AIMessage{
		ConversationID:  storedConversation.ID,
		Role:            store.AIMessageRoleUser,
		Content:         "question",
		Status:          store.AIMessageStatusComplete,
		ClientRequestID: &requestID,
		Payload:         &storepb.AIMessagePayload{},
	}, &store.AIMessage{
		ConversationID: storedConversation.ID,
		Role:           store.AIMessageRoleAssistant,
		Status:         store.AIMessageStatusStreaming,
		Payload:        &storepb.AIMessagePayload{},
	})
	require.NoError(t, err)

	// The restart reconciles the interrupted attempt to FAILED, so repeating
	// the request ID retries it as a new attempt.
	restarted := serverai.NewService(fixture.store, memo.NewService(fixture.store), fixture.factory)
	_, messages, err := restarted.GetConversation(ctx, alice, conversation.UID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.StatusFailed, messages[1].Status)

	_, assistantMsg, err := restarted.SendMessage(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	require.Equal(t, int32(2), assistantMsg.Attempt)
	require.Equal(t, "recovered", assistantMsg.Content)
}

func TestConversationsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "answer"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")
	conversation := fixture.createConversation(ctx, t, alice, "")

	// Bob has no read, send, or delete path to Alice's conversation.
	_, _, err := fixture.service.GetConversation(ctx, bob, conversation.UID)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, _, err = fixture.service.SendMessage(ctx, bob, conversation.UID, "hello", "req-1")
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, codes.NotFound, status.Code(fixture.service.DeleteConversation(ctx, bob, conversation.UID)))

	conversations, err := fixture.service.ListConversations(ctx, bob)
	require.NoError(t, err)
	require.Empty(t, conversations)
}

func TestConversationAccumulatesMessages(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "answer"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, _, err := fixture.service.SendMessage(ctx, alice, conversation.UID, "first question", "req-1")
	require.NoError(t, err)
	_, _, err = fixture.service.SendMessage(ctx, alice, conversation.UID, "second question", "req-2")
	require.NoError(t, err)

	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 4)
	require.Equal(t, serverai.RoleUser, messages[0].Role)
	require.Equal(t, "first question", messages[0].Content)
	require.Equal(t, serverai.RoleAssistant, messages[1].Role)
	require.Equal(t, serverai.RoleUser, messages[2].Role)
	require.Equal(t, "second question", messages[2].Content)
	require.Equal(t, serverai.RoleAssistant, messages[3].Role)
	for i := 1; i < len(messages); i++ {
		require.False(t, messages[i].CreateTime.Before(messages[i-1].CreateTime))
	}
}
