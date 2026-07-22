package ai_test

import (
	"context"
	"net/http"
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
}

func newChatTestFixture(ctx context.Context, t *testing.T, model internalai.Model, factoryErr error) *chatTestFixture {
	st := teststore.NewTestingStore(ctx, t)
	t.Cleanup(func() { _ = st.Close() })
	fake, _ := model.(*aitest.Model)
	service := serverai.NewService(st, memo.NewService(st), func() gateway.ModelFactory {
		return func(internalai.ProviderConfig, *http.Client) (internalai.Model, error) {
			if factoryErr != nil {
				return nil, factoryErr
			}
			return model, nil
		}
	})
	return &chatTestFixture{service: service, store: st, model: fake}
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

func TestSendMessageNotConfigured(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
	user := fixture.createUser(ctx, t, "alice")

	_, _, err := fixture.service.SendMessage(ctx, user, "hello")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestSendMessageEmptyContent(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
	fixture.configureGeneration(ctx, t)
	user := fixture.createUser(ctx, t, "alice")

	_, _, err := fixture.service.SendMessage(ctx, user, "   ")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSendMessageGroundedAnswer(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "Alpine lakes are great for hiking."},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")

	aliceMemo := fixture.createMemo(ctx, t, alice.ID, "alice-hiking", "My favorite hiking spot is the alpine lakes trail.", store.Private)
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)
	// Bob's memo matches the query but must never be cited in Alice's answer.
	fixture.createMemo(ctx, t, bob.ID, "bob-hiking", "Bob's secret hiking spots.", store.Private)

	userMsg, assistantMsg, err := fixture.service.SendMessage(ctx, alice, "Where should I go hiking")
	require.NoError(t, err)
	require.Equal(t, serverai.RoleUser, userMsg.Role)
	require.Equal(t, "Where should I go hiking", userMsg.Content)
	require.Equal(t, serverai.RoleAssistant, assistantMsg.Role)
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
}

func TestSendMessageWithoutMatchingMemos(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "I don't know."},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)

	_, assistantMsg, err := fixture.service.SendMessage(ctx, alice, "What is the capital of France?")
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
		_, _, err := fixture.service.SendMessage(ctx, user, "hello")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("generate error maps to internal", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{GenerateError: errors.New("provider unavailable")}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		_, _, err := fixture.service.SendMessage(ctx, user, "hello")
		require.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("empty answer maps to internal", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{GenerateResponse: internalai.GenerationResponse{Content: "  "}}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		_, _, err := fixture.service.SendMessage(ctx, user, "hello")
		require.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestConversationAccumulatesPerUser(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		GenerateResponse: internalai.GenerationResponse{Content: "answer"},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")

	_, _, err := fixture.service.SendMessage(ctx, alice, "first question")
	require.NoError(t, err)
	_, _, err = fixture.service.SendMessage(ctx, alice, "second question")
	require.NoError(t, err)

	conversation := fixture.service.Conversation(alice.ID)
	require.Len(t, conversation, 4)
	require.Equal(t, serverai.RoleUser, conversation[0].Role)
	require.Equal(t, "first question", conversation[0].Content)
	require.Equal(t, serverai.RoleAssistant, conversation[1].Role)
	require.Equal(t, serverai.RoleUser, conversation[2].Role)
	require.Equal(t, "second question", conversation[2].Content)
	require.Equal(t, serverai.RoleAssistant, conversation[3].Role)
	for i := 1; i < len(conversation); i++ {
		require.False(t, conversation[i].CreateTime.Before(conversation[i-1].CreateTime))
	}

	// Conversations are isolated between users.
	require.Empty(t, fixture.service.Conversation(bob.ID))
}
