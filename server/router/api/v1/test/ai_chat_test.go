package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/store"
)

// fakeChatStream is a grpc.ServerStreamingServer that collects sent events.
type fakeChatStream struct {
	ctx    context.Context
	events []*v1pb.SendChatMessageEvent
}

func (s *fakeChatStream) Send(event *v1pb.SendChatMessageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (*fakeChatStream) SetHeader(metadata.MD) error  { return nil }
func (*fakeChatStream) SendHeader(metadata.MD) error { return nil }
func (*fakeChatStream) SetTrailer(metadata.MD)       {}
func (s *fakeChatStream) Context() context.Context   { return s.ctx }
func (*fakeChatStream) SendMsg(any) error            { return nil }
func (*fakeChatStream) RecvMsg(any) error            { return nil }

// sendChat streams a SendChatMessage call and returns the collected events.
func sendChat(ctx context.Context, ts *TestService, request *v1pb.SendChatMessageRequest) (*fakeChatStream, error) {
	stream := &fakeChatStream{ctx: ctx}
	err := ts.Service.SendChatMessage(request, stream)
	return stream, err
}

func configureChatGeneration(ctx context.Context, t *testing.T, ts *TestService) {
	t.Helper()
	_, err := ts.Store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{
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

func createChatConversation(ctx context.Context, t *testing.T, ts *TestService, userCtx context.Context) *v1pb.ChatConversation {
	t.Helper()
	conversation, err := ts.Service.CreateChatConversation(userCtx, &v1pb.CreateChatConversationRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, conversation.GetName())
	return conversation
}

func TestSendChatMessage(t *testing.T) {
	ctx := context.Background()

	t.Run("requires authentication", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		_, err := sendChat(ctx, ts, &v1pb.SendChatMessageRequest{Conversation: "ai/conversations/abc", Content: "hello", RequestId: "req-1"})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("fails when generation is not configured", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)
		conversation := createChatConversation(ctx, t, ts, userCtx)

		_, err = sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{Conversation: conversation.GetName(), Content: "hello", RequestId: "req-1"})
		require.Equal(t, codes.FailedPrecondition, status.Code(err))

		fetched, err := ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{Name: conversation.GetName()})
		require.NoError(t, err)
		require.False(t, fetched.GetGenerationAvailable())
		require.Empty(t, fetched.GetMessages())
	})

	t.Run("rejects invalid requests", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)
		conversation := createChatConversation(ctx, t, ts, userCtx)

		_, err = sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{Conversation: conversation.GetName(), Content: "  ", RequestId: "req-1"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{Conversation: conversation.GetName(), Content: "hello"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{Conversation: "memos/abc", Content: "hello", RequestId: "req-1"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{Conversation: "ai/conversations/missing", Content: "hello", RequestId: "req-1"})
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("streams grounded answer with citations", func(t *testing.T) {
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
		// Chat grounds on the derived search documents; close the index lag
		// for the memo just written.
		ts.Service.SearchService().RunOnce(ctx)
		configureChatGeneration(ctx, t, ts)

		// AIModelFactory is overridden after struct-literal construction; the
		// chat service must pick it up through lazy initialization.
		fake := &aitest.Model{StreamEvents: []ai.StreamEvent{
			{Delta: "The alpine lakes trail "},
			{Delta: "is your favorite hiking spot."},
		}}
		ts.Service.AIModelFactory = func(ai.ProviderConfig, *http.Client) (ai.Model, error) {
			return fake, nil
		}

		conversation := createChatConversation(ctx, t, ts, userCtx)
		stream, err := sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{
			Conversation: conversation.GetName(),
			Content:      "Where should I go hiking",
			RequestId:    "req-1",
		})
		require.NoError(t, err)
		// The stream is start, deltas, then the authoritative stored attempt.
		require.Len(t, stream.events, 4)
		start := stream.events[0].GetStart()
		require.NotNil(t, start)
		require.Equal(t, v1pb.ChatMessage_USER, start.GetUserMessage().GetRole())
		require.Equal(t, v1pb.ChatMessage_COMPLETE, start.GetUserMessage().GetStatus())
		require.Equal(t, "Where should I go hiking", start.GetUserMessage().GetContent())
		require.Equal(t, "req-1", start.GetUserMessage().GetClientRequestId())
		require.NotNil(t, start.GetUserMessage().GetCreateTime())
		require.Equal(t, v1pb.ChatMessage_ASSISTANT, start.GetAssistantMessage().GetRole())
		require.Equal(t, v1pb.ChatMessage_STREAMING, start.GetAssistantMessage().GetStatus())
		require.Equal(t, int32(1), start.GetAssistantMessage().GetAttempt())
		require.Equal(t, "The alpine lakes trail ", stream.events[1].GetDelta())
		require.Equal(t, "is your favorite hiking spot.", stream.events[2].GetDelta())
		complete := stream.events[3].GetComplete()
		require.NotNil(t, complete)
		require.Equal(t, v1pb.ChatMessage_COMPLETE, complete.GetStatus())
		require.Equal(t, "The alpine lakes trail is your favorite hiking spot.", complete.GetContent())
		require.Len(t, complete.GetCitations(), 1)
		require.Equal(t, "memos/"+memo.UID, complete.GetCitations()[0].GetMemo())
		require.NotEmpty(t, complete.GetCitations()[0].GetSnippet())

		fetched, err := ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{Name: conversation.GetName()})
		require.NoError(t, err)
		require.True(t, fetched.GetGenerationAvailable())
		require.Equal(t, "Where should I go hiking", fetched.GetTitle())
		require.Len(t, fetched.GetMessages(), 2)
		require.Equal(t, v1pb.ChatMessage_USER, fetched.GetMessages()[0].GetRole())
		require.Equal(t, v1pb.ChatMessage_ASSISTANT, fetched.GetMessages()[1].GetRole())

		// A repeated request ID replays the stored pair without regenerating.
		duplicate, err := sendChat(userCtx, ts, &v1pb.SendChatMessageRequest{
			Conversation: conversation.GetName(),
			Content:      "Where should I go hiking",
			RequestId:    "req-1",
		})
		require.NoError(t, err)
		require.Len(t, duplicate.events, 2)
		require.NotNil(t, duplicate.events[0].GetStart())
		require.Equal(t, complete.GetContent(), duplicate.events[1].GetComplete().GetContent())
		require.Len(t, fake.StreamRequests, 1)
	})

	t.Run("conversation requires authentication", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		_, err := ts.Service.GetChatConversation(ctx, &v1pb.GetChatConversationRequest{Name: "ai/conversations/abc"})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestChatConversations(t *testing.T) {
	ctx := context.Background()

	t.Run("create list and delete", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		first := createChatConversation(ctx, t, ts, userCtx)
		second, err := ts.Service.CreateChatConversation(userCtx, &v1pb.CreateChatConversationRequest{Title: "trip planning"})
		require.NoError(t, err)
		require.Equal(t, "trip planning", second.GetTitle())

		list, err := ts.Service.ListChatConversations(userCtx, &v1pb.ListChatConversationsRequest{})
		require.NoError(t, err)
		require.Len(t, list.GetConversations(), 2)
		require.False(t, list.GetGenerationAvailable())
		require.Empty(t, list.GetConversations()[0].GetMessages())

		_, err = ts.Service.DeleteChatConversation(userCtx, &v1pb.DeleteChatConversationRequest{Name: first.GetName()})
		require.NoError(t, err)

		_, err = ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{Name: first.GetName()})
		require.Equal(t, codes.NotFound, status.Code(err))

		list, err = ts.Service.ListChatConversations(userCtx, &v1pb.ListChatConversationsRequest{})
		require.NoError(t, err)
		require.Len(t, list.GetConversations(), 1)
		require.Equal(t, second.GetName(), list.GetConversations()[0].GetName())
	})

	t.Run("rejects invalid names", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		user, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		userCtx := ts.CreateUserContext(ctx, user.ID)

		_, err = ts.Service.GetChatConversation(userCtx, &v1pb.GetChatConversationRequest{Name: "memos/abc"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		_, err = ts.Service.DeleteChatConversation(userCtx, &v1pb.DeleteChatConversationRequest{Name: "ai/conversations/"})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("conversations are owner scoped", func(t *testing.T) {
		ts := NewTestService(t)
		defer ts.Cleanup()

		alice, err := ts.CreateRegularUser(ctx, "alice")
		require.NoError(t, err)
		aliceCtx := ts.CreateUserContext(ctx, alice.ID)
		bob, err := ts.CreateRegularUser(ctx, "bob")
		require.NoError(t, err)
		bobCtx := ts.CreateUserContext(ctx, bob.ID)

		conversation := createChatConversation(ctx, t, ts, aliceCtx)

		_, err = ts.Service.GetChatConversation(bobCtx, &v1pb.GetChatConversationRequest{Name: conversation.GetName()})
		require.Equal(t, codes.NotFound, status.Code(err))
		_, err = ts.Service.DeleteChatConversation(bobCtx, &v1pb.DeleteChatConversationRequest{Name: conversation.GetName()})
		require.Equal(t, codes.NotFound, status.Code(err))

		list, err := ts.Service.ListChatConversations(bobCtx, &v1pb.ListChatConversationsRequest{})
		require.NoError(t, err)
		require.Empty(t, list.GetConversations())
	})
}
