package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

func TestSendChatMessage(t *testing.T) {
	ctx := context.Background()

	t.Run("requires authentication", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		_, err := ts.Service.SendChatMessage(ctx, &v1pb.SendChatMessageRequest{Content: "hello"})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("fails when generation is not configured", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Service.SendChatMessage(userCtx, &v1pb.SendChatMessageRequest{Content: "hello"})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))

		conversation, err := ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{})
		require.NoError(t, err)
		require.False(t, conversation.GetGenerationAvailable())
		require.Empty(t, conversation.GetMessages())
	})

	t.Run("rejects empty content", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Service.SendChatMessage(userCtx, &v1pb.SendChatMessageRequest{Content: "  "})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("returns grounded answer with citations", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		memo, err := ts.Store.CreateMemo(ctx, &store.Memo{
			UID:        "alice-hiking",
			CreatorID:  user.ID,
			Content:    "My favorite hiking spot is the alpine lakes trail.",
			Visibility: store.Public,
		})
		require.NoError(t, err)

		_, err = ts.Store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
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

		// AIModelFactory is overridden after struct-literal construction; the
		// chat service must pick it up through lazy initialization.
		fake := &aitest.Model{GenerateResponse: ai.GenerationResponse{Content: "The alpine lakes trail is your favorite hiking spot."}}
		ts.Service.AIModelFactory = func(ai.ProviderConfig, *http.Client) (ai.Model, error) {
			return fake, nil
		}

		response, err := ts.Service.SendChatMessage(userCtx, &v1pb.SendChatMessageRequest{Content: "Where should I go hiking"})
		require.NoError(t, err)
		require.Equal(t, v1pb.ChatMessage_USER, response.GetUserMessage().GetRole())
		require.Equal(t, "Where should I go hiking", response.GetUserMessage().GetContent())
		require.NotNil(t, response.GetUserMessage().GetCreateTime())
		require.Equal(t, v1pb.ChatMessage_ASSISTANT, response.GetAssistantMessage().GetRole())
		require.Equal(t, "The alpine lakes trail is your favorite hiking spot.", response.GetAssistantMessage().GetContent())
		require.Len(t, response.GetAssistantMessage().GetCitations(), 1)
		require.Equal(t, "memos/"+memo.UID, response.GetAssistantMessage().GetCitations()[0].GetMemo())
		require.NotEmpty(t, response.GetAssistantMessage().GetCitations()[0].GetSnippet())

		conversation, err := ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{})
		require.NoError(t, err)
		require.True(t, conversation.GetGenerationAvailable())
		require.Len(t, conversation.GetMessages(), 2)
		require.Equal(t, v1pb.ChatMessage_USER, conversation.GetMessages()[0].GetRole())
		require.Equal(t, v1pb.ChatMessage_ASSISTANT, conversation.GetMessages()[1].GetRole())
	})

	t.Run("conversation requires authentication", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		_, err := ts.Service.GetChatConversation(ctx, &v1pb.GetChatConversationRequest{})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}
