package ai_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	internalai "github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/internal/ai/gateway"
	"github.com/usememos/memos/internal/markdown"
	storepb "github.com/usememos/memos/proto/gen/store"
	serverai "github.com/usememos/memos/server/ai"
	"github.com/usememos/memos/server/ai/search"
	"github.com/usememos/memos/server/memo"
	"github.com/usememos/memos/store"
	teststore "github.com/usememos/memos/store/test"
)

type chatTestFixture struct {
	service *serverai.Service
	store   *store.Store
	model   *aitest.Model
	factory func() gateway.ModelFactory
	search  *search.Service
}

func newChatTestFixture(ctx context.Context, t *testing.T, model internalai.Model, factoryErr error) *chatTestFixture {
	return newChatTestFixtureWithLimits(ctx, t, model, factoryErr, serverai.DefaultLimits())
}

func newChatTestFixtureWithLimits(ctx context.Context, t *testing.T, model internalai.Model, factoryErr error, limits serverai.Limits) *chatTestFixture {
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
	markdownService := markdown.NewService(markdown.WithTagExtension(), markdown.WithMentionExtension())
	service := serverai.NewServiceWithLimits(st, memo.NewService(st), factory, limits)
	return &chatTestFixture{
		service: service,
		store:   st,
		model:   fake,
		factory: factory,
		search:  search.NewService(st, memo.NewService(st), markdownService),
	}
}

// syncSearch rebuilds the derived search documents, closing the index lag
// for the memos a test just wrote.
func (f *chatTestFixture) syncSearch(ctx context.Context) {
	f.search.RunOnce(ctx)
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

// getStoredMessages reads the raw stored messages of a conversation so tests
// can assert on persisted payloads such as the normalized error category.
func (f *chatTestFixture) getStoredMessages(ctx context.Context, t *testing.T, user *store.User, uid string) []*store.AIMessage {
	t.Helper()
	conversation, err := f.store.GetAIConversation(ctx, &store.FindAIConversation{UID: &uid, UserID: &user.ID})
	require.NoError(t, err)
	require.NotNil(t, conversation)
	messages, err := f.store.ListAIMessages(ctx, &store.FindAIMessage{ConversationID: &conversation.ID})
	require.NoError(t, err)
	return messages
}

// streamCollector gathers emitted stream events.
type streamCollector struct {
	events []serverai.StreamEvent
}

func (c *streamCollector) emit(event serverai.StreamEvent) error {
	c.events = append(c.events, event)
	return nil
}

// send runs a streaming send and returns the collected events.
func (f *chatTestFixture) send(ctx context.Context, user *store.User, conversationUID, content, requestID string) (*streamCollector, error) {
	collector := &streamCollector{}
	err := f.service.SendMessageStream(ctx, user, conversationUID, content, requestID, collector.emit)
	return collector, err
}

// requireCompleteEvent extracts the terminal complete event, requiring the
// event sequence to be start, zero or more deltas, then complete.
func requireCompleteEvent(t *testing.T, collector *streamCollector) serverai.Message {
	t.Helper()
	require.NotEmpty(t, collector.events)
	require.NotNil(t, collector.events[0].Start)
	last := collector.events[len(collector.events)-1]
	require.NotNil(t, last.Complete)
	for _, event := range collector.events[1 : len(collector.events)-1] {
		require.Nil(t, event.Start)
		require.Nil(t, event.Complete)
		require.NotEmpty(t, event.Delta)
	}
	return *last.Complete
}

// requireDeltas concatenates the delta payloads of a collected stream.
func requireDeltas(t *testing.T, collector *streamCollector) string {
	t.Helper()
	require.NotEmpty(t, collector.events)
	deltas := ""
	for _, event := range collector.events[1 : len(collector.events)-1] {
		deltas += event.Delta
	}
	return deltas
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

		_, err := fixture.send(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		// Nothing is persisted when generation is unavailable.
		require.Empty(t, fixture.getMessages(ctx, t, user, conversation.UID))
	})

	t.Run("rejects empty content", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, err := fixture.send(ctx, user, conversation.UID, "   ", "req-1")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects empty request id", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		_, err := fixture.send(ctx, user, conversation.UID, "hello", " ")
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("rejects unknown conversation", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")

		_, err := fixture.send(ctx, user, "missing", "hello", "req-1")
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("requires authentication", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{}, nil)
		fixture.configureGeneration(ctx, t)

		_, err := fixture.send(ctx, nil, "any", "hello", "req-1")
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestSendMessageStreamsGroundedAnswer(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{
			{Delta: "Alpine lakes "},
			{Delta: "are great for hiking.", Usage: &internalai.Usage{InputTokens: 10, OutputTokens: 8, TotalTokens: 18}},
		},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")

	aliceMemo := fixture.createMemo(ctx, t, alice.ID, "alice-hiking", "My favorite hiking spot is the alpine lakes trail.", store.Private)
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)
	// Bob's memo matches the query but must never be cited in Alice's answer.
	fixture.createMemo(ctx, t, bob.ID, "bob-hiking", "Bob's secret hiking spots.", store.Private)
	fixture.syncSearch(ctx)

	conversation := fixture.createConversation(ctx, t, alice, "")
	collector, err := fixture.send(ctx, alice, conversation.UID, "Where should I go hiking", "req-1")
	require.NoError(t, err)

	// The stream starts with the persisted pair before any delta.
	start := collector.events[0].Start
	require.Equal(t, serverai.RoleUser, start.UserMessage.Role)
	require.Equal(t, serverai.StatusComplete, start.UserMessage.Status)
	require.Equal(t, "Where should I go hiking", start.UserMessage.Content)
	require.Equal(t, "req-1", start.UserMessage.ClientRequestID)
	require.Equal(t, serverai.RoleAssistant, start.AssistantMessage.Role)
	require.Equal(t, serverai.StatusStreaming, start.AssistantMessage.Status)
	require.Equal(t, int32(1), start.AssistantMessage.Attempt)

	// Deltas stream incrementally and the terminal event is the authoritative
	// stored attempt whose content is exactly the concatenated deltas.
	require.Equal(t, "Alpine lakes are great for hiking.", requireDeltas(t, collector))
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, serverai.StatusComplete, complete.Status)
	require.Equal(t, "Alpine lakes are great for hiking.", complete.Content)
	require.Len(t, complete.Citations, 1)
	require.Equal(t, aliceMemo.UID, complete.Citations[0].MemoUID)
	require.NotEmpty(t, complete.Citations[0].Snippet)

	// The generation request carries instructions plus the question and the
	// quoted memo context.
	require.Len(t, fixture.model.StreamRequests, 1)
	request := fixture.model.StreamRequests[0]
	require.Equal(t, "chat-model", request.Model)
	require.Len(t, request.Messages, 2)
	require.Equal(t, internalai.RoleSystem, request.Messages[0].Role)
	require.Equal(t, internalai.RoleUser, request.Messages[1].Role)
	require.Contains(t, request.Messages[1].Content, "Where should I go hiking")
	require.Contains(t, request.Messages[1].Content, "<memo name=\"memos/"+aliceMemo.UID+"\">")
	require.NotContains(t, request.Messages[1].Content, "bob-hiking")

	// Both messages are persisted with their terminal states, usage is
	// recorded, and the conversation title comes from the first user message.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.RoleUser, messages[0].Role)
	require.Equal(t, serverai.RoleAssistant, messages[1].Role)
	stored := fixture.getStoredMessages(ctx, t, alice, conversation.UID)
	require.Equal(t, int32(18), stored[1].Payload.GetTotalTokens())
	storedConversation, err := fixture.store.GetAIConversation(ctx, &store.FindAIConversation{UID: &conversation.UID})
	require.NoError(t, err)
	require.Equal(t, "Where should I go hiking", storedConversation.Title)
}

func TestSendMessageWithoutMatchingMemos(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "I don't know."}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	fixture.createMemo(ctx, t, alice.ID, "alice-cooking", "Risotto recipe notes.", store.Public)
	fixture.syncSearch(ctx)
	conversation := fixture.createConversation(ctx, t, alice, "")

	collector, err := fixture.send(ctx, alice, conversation.UID, "What is the capital of France?", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Empty(t, complete.Citations)
	require.NotContains(t, fixture.model.StreamRequests[0].Messages[1].Content, "<memo name=")
}

func TestSendMessageFactoryAndStreamErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("factory error maps to failed precondition", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, nil, errors.New("unknown provider type"))
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")
		_, err := fixture.send(ctx, user, conversation.UID, "hello", "req-1")
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("stream error persists a failed attempt with the normalized category", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{
			StreamError: internalai.NewProviderError(internalai.ErrorRateLimit, "AI provider rate limit exceeded", nil),
		}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		collector, err := fixture.send(ctx, user, conversation.UID, "hello", "req-1")
		require.NoError(t, err)
		complete := requireCompleteEvent(t, collector)
		require.Equal(t, serverai.StatusFailed, complete.Status)
		require.Empty(t, complete.Content)

		stored := fixture.getStoredMessages(ctx, t, user, conversation.UID)
		require.Len(t, stored, 2)
		require.Equal(t, store.AIMessageStatusFailed, stored[1].Status)
		require.Equal(t, "rate_limit", stored[1].Payload.GetError())
	})

	t.Run("mid-stream error discards the partial answer", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{
			StreamEvents: []internalai.StreamEvent{
				{Delta: "partial"},
				{Err: internalai.NewProviderError(internalai.ErrorUnavailable, "AI provider is unavailable", nil)},
			},
		}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		collector, err := fixture.send(ctx, user, conversation.UID, "hello", "req-1")
		require.NoError(t, err)
		require.Equal(t, "partial", requireDeltas(t, collector))
		complete := requireCompleteEvent(t, collector)
		require.Equal(t, serverai.StatusFailed, complete.Status)

		stored := fixture.getStoredMessages(ctx, t, user, conversation.UID)
		require.Empty(t, stored[1].Content)
		require.Equal(t, "unavailable", stored[1].Payload.GetError())
	})

	t.Run("empty answer persists a failed attempt", func(t *testing.T) {
		fixture := newChatTestFixture(ctx, t, &aitest.Model{
			StreamEvents: []internalai.StreamEvent{{Delta: "  "}},
		}, nil)
		fixture.configureGeneration(ctx, t)
		user := fixture.createUser(ctx, t, "alice")
		conversation := fixture.createConversation(ctx, t, user, "")

		collector, err := fixture.send(ctx, user, conversation.UID, "hello", "req-1")
		require.NoError(t, err)
		complete := requireCompleteEvent(t, collector)
		require.Equal(t, serverai.StatusFailed, complete.Status)
	})
}

func TestSendMessageDuplicateRequestID(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	collector, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	requireCompleteEvent(t, collector)

	// A repeated request ID replays the stored pair without regenerating.
	duplicate, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	require.Len(t, duplicate.events, 2)
	require.NotNil(t, duplicate.events[0].Start)
	complete := requireCompleteEvent(t, duplicate)
	require.Equal(t, "answer", complete.Content)
	require.Equal(t, serverai.StatusComplete, complete.Status)
	require.Len(t, fixture.model.StreamRequests, 1)

	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)

	// A different request ID on the same conversation is a new message.
	_, err = fixture.send(ctx, alice, conversation.UID, "question", "req-2")
	require.NoError(t, err)
	require.Len(t, fixture.getMessages(ctx, t, alice, conversation.UID), 4)
}

func TestSendMessageDuplicateWhileActive(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendCtx, cancelSend := context.WithCancel(ctx)
	t.Cleanup(cancelSend)
	sendErr := make(chan error, 1)
	go func() {
		_, err := fixture.send(sendCtx, alice, conversation.UID, "question", "req-1")
		sendErr <- err
	}()
	<-model.started

	// A repeated request ID while the attempt is still generating replays the
	// stored pair only; the stored state stays authoritative.
	duplicate, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	require.Len(t, duplicate.events, 1)
	start := duplicate.events[0].Start
	require.NotNil(t, start)
	require.Equal(t, "question", start.UserMessage.Content)
	require.Equal(t, serverai.StatusStreaming, start.AssistantMessage.Status)
	require.Equal(t, 1, model.StreamRequestCount())

	cancelSend()
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))
}

func TestSendMessageRetryAfterFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{StreamError: errors.New("provider down")}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)

	// Retrying with the same request ID streams a new attempt on the same
	// user message instead of duplicating it.
	fixture.model.StreamError = nil
	fixture.model.StreamEvents = []internalai.StreamEvent{{Delta: "recovered"}}
	collector, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	start := collector.events[0].Start
	require.Equal(t, "question", start.UserMessage.Content)
	require.Equal(t, int32(2), start.AssistantMessage.Attempt)
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, serverai.StatusComplete, complete.Status)
	require.Equal(t, "recovered", complete.Content)

	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 3)
	require.Equal(t, serverai.RoleUser, messages[0].Role)
	require.Equal(t, int32(1), messages[1].Attempt)
	require.Equal(t, serverai.StatusFailed, messages[1].Status)
	require.Equal(t, int32(2), messages[2].Attempt)
	require.Equal(t, serverai.StatusComplete, messages[2].Status)
}

// blockingStreamModel blocks in Stream until its context is cancelled, then
// reports the context error as the terminal stream event. Setting
// staticEvents switches it to replaying those events like aitest.Model.
type blockingStreamModel struct {
	aitest.Model
	started chan struct{}
	once    sync.Once

	mu           sync.Mutex
	streamCalls  int
	staticEvents []internalai.StreamEvent
}

func (m *blockingStreamModel) Stream(ctx context.Context, _ internalai.GenerationRequest) (<-chan internalai.StreamEvent, error) {
	m.once.Do(func() { close(m.started) })
	m.mu.Lock()
	m.streamCalls++
	static := m.staticEvents
	m.mu.Unlock()
	if static != nil {
		stream := make(chan internalai.StreamEvent, len(static))
		for _, event := range static {
			stream <- event
		}
		close(stream)
		return stream, nil
	}
	stream := make(chan internalai.StreamEvent, 1)
	go func() {
		<-ctx.Done()
		stream <- internalai.StreamEvent{Err: ctx.Err()}
		close(stream)
	}()
	return stream, nil
}

// StreamRequestCount returns how many Stream calls the blocking fake served.
func (m *blockingStreamModel) StreamRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamCalls
}

func TestSendMessageCancelledByCaller(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendCtx, cancelSend := context.WithCancel(ctx)
	sendErr := make(chan error, 1)
	go func() {
		_, err := fixture.send(sendCtx, alice, conversation.UID, "hello", "req-1")
		sendErr <- err
	}()
	<-model.started
	cancelSend()
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))

	// The cancellation is persisted: the attempt is CANCELLED.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.StatusCancelled, messages[1].Status)
	stored := fixture.getStoredMessages(ctx, t, alice, conversation.UID)
	require.Equal(t, "cancelled", stored[1].Payload.GetError())
}

func TestSendMessageDisconnectDuringDeltas(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "first"}, {Delta: "second"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	// The client goes away when the first delta arrives.
	emitErr := errors.New("client disconnected")
	failOnDelta := func(event serverai.StreamEvent) error {
		if event.Delta != "" {
			return emitErr
		}
		return nil
	}
	err := fixture.service.SendMessageStream(ctx, alice, conversation.UID, "hello", "req-1", failOnDelta)
	require.ErrorIs(t, err, emitErr)

	// The disconnect propagates to the provider and is persisted as CANCELLED.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.StatusCancelled, messages[1].Status)
}

func TestReconnectReadsStoredStateAfterDisconnect(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	// The client disconnects mid-generation.
	sendCtx, cancelSend := context.WithCancel(ctx)
	sendErr := make(chan error, 1)
	go func() {
		_, err := fixture.send(sendCtx, alice, conversation.UID, "question", "req-1")
		sendErr <- err
	}()
	<-model.started
	cancelSend()
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))

	// A reconnecting client reads the authoritative stored state: the attempt
	// is CANCELLED, not assumed from the broken stream.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Equal(t, serverai.StatusCancelled, messages[1].Status)

	// Retrying the same request ID streams a fresh attempt on the stored user
	// message instead of duplicating it.
	model.mu.Lock()
	model.staticEvents = []internalai.StreamEvent{{Delta: "recovered"}}
	model.mu.Unlock()
	collector, err := fixture.send(ctx, alice, conversation.UID, "question", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, int32(2), complete.Attempt)
	require.Equal(t, serverai.StatusComplete, complete.Status)
	require.Equal(t, "recovered", complete.Content)

	messages = fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 3)
}

func TestSendMessageAttemptTimeout(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	limits := serverai.DefaultLimits()
	limits.AttemptTimeout = 50 * time.Millisecond
	fixture := newChatTestFixtureWithLimits(ctx, t, model, nil, limits)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	collector, err := fixture.send(ctx, alice, conversation.UID, "hello", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, serverai.StatusFailed, complete.Status)

	// The attempt deadline persists FAILED with the timeout category.
	stored := fixture.getStoredMessages(ctx, t, alice, conversation.UID)
	require.Len(t, stored, 2)
	require.Equal(t, store.AIMessageStatusFailed, stored[1].Status)
	require.Equal(t, "timeout", stored[1].Payload.GetError())
}

func TestSendMessageRateLimited(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixtureWithLimits(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil, serverai.Limits{
		MaxConcurrentAttempts: 8,
		AttemptTimeout:        time.Minute,
		MaxAnswerRunes:        1024,
		SendRateBurst:         1,
		SendRateInterval:      time.Hour,
	})
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, err := fixture.send(ctx, alice, conversation.UID, "first", "req-1")
	require.NoError(t, err)

	// The burst is exhausted: the next send is rejected before persisting.
	_, err = fixture.send(ctx, alice, conversation.UID, "second", "req-2")
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Len(t, fixture.getMessages(ctx, t, alice, conversation.UID), 2)
}

func TestSendMessageConcurrencyLimited(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	fixture := newChatTestFixtureWithLimits(ctx, t, model, nil, serverai.Limits{
		MaxConcurrentAttempts: 1,
		AttemptTimeout:        time.Minute,
		MaxAnswerRunes:        1024,
		SendRateBurst:         10,
		SendRateInterval:      time.Second,
	})
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendCtx, cancelSend := context.WithCancel(ctx)
	t.Cleanup(cancelSend)
	sendErr := make(chan error, 1)
	go func() {
		_, err := fixture.send(sendCtx, alice, conversation.UID, "first", "req-1")
		sendErr <- err
	}()
	<-model.started

	// The single in-flight permit is held: a second send is rejected before
	// persisting anything.
	_, err := fixture.send(ctx, alice, conversation.UID, "second", "req-2")
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Len(t, fixture.getMessages(ctx, t, alice, conversation.UID), 2)

	cancelSend()
	require.Equal(t, codes.Canceled, status.Code(<-sendErr))
}

func TestSendMessageResponseSizeLimited(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixtureWithLimits(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "hello"}, {Delta: " world"}},
	}, nil, serverai.Limits{
		MaxConcurrentAttempts: 8,
		AttemptTimeout:        time.Minute,
		MaxAnswerRunes:        5,
		SendRateBurst:         10,
		SendRateInterval:      time.Second,
	})
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	collector, err := fixture.send(ctx, alice, conversation.UID, "hello", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, serverai.StatusFailed, complete.Status)

	// The oversized answer is discarded and persisted as failed.
	stored := fixture.getStoredMessages(ctx, t, alice, conversation.UID)
	require.Len(t, stored, 2)
	require.Empty(t, stored[1].Content)
	require.Equal(t, "response_too_large", stored[1].Payload.GetError())
}

func TestDeleteConversationCancelsActiveAttempt(t *testing.T) {
	ctx := context.Background()
	model := &blockingStreamModel{started: make(chan struct{})}
	fixture := newChatTestFixture(ctx, t, model, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	sendErr := make(chan error, 1)
	go func() {
		_, err := fixture.send(ctx, alice, conversation.UID, "hello", "req-1")
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
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, err := fixture.send(ctx, alice, conversation.UID, "first question", "req-1")
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
		StreamEvents: []internalai.StreamEvent{{Delta: "recovered"}},
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

	collector := &streamCollector{}
	err = restarted.SendMessageStream(ctx, alice, conversation.UID, "question", "req-1", collector.emit)
	require.NoError(t, err)
	start := collector.events[0].Start
	require.Equal(t, int32(2), start.AssistantMessage.Attempt)
	complete := requireCompleteEvent(t, collector)
	require.Equal(t, "recovered", complete.Content)
}

func TestConversationsAreOwnerScoped(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	bob := fixture.createUser(ctx, t, "bob")
	conversation := fixture.createConversation(ctx, t, alice, "")

	// Bob has no read, send, or delete path to Alice's conversation.
	_, _, err := fixture.service.GetConversation(ctx, bob, conversation.UID)
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = fixture.send(ctx, bob, conversation.UID, "hello", "req-1")
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, codes.NotFound, status.Code(fixture.service.DeleteConversation(ctx, bob, conversation.UID)))

	conversations, err := fixture.service.ListConversations(ctx, bob)
	require.NoError(t, err)
	require.Empty(t, conversations)
}

func TestConversationAccumulatesMessages(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	conversation := fixture.createConversation(ctx, t, alice, "")

	_, err := fixture.send(ctx, alice, conversation.UID, "first question", "req-1")
	require.NoError(t, err)
	_, err = fixture.send(ctx, alice, conversation.UID, "second question", "req-2")
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

func TestSendMessageCitesTypoTolerantMatch(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	memo := fixture.createMemo(ctx, t, alice.ID, "alice-hiking", "My favorite hiking spot is the alpine lakes trail.", store.Private)
	fixture.syncSearch(ctx)
	conversation := fixture.createConversation(ctx, t, alice, "")

	// "hikxng" is one substitution away from "hiking": the real retrieval
	// path matches it typo-tolerantly.
	collector, err := fixture.send(ctx, alice, conversation.UID, "hikxng", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Len(t, complete.Citations, 1)
	require.Equal(t, memo.UID, complete.Citations[0].MemoUID)
}

func TestSendMessageBoundsProviderContextByTokens(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	// Thirty matching memos of about 4,000 runes each cannot all fit the
	// 8,000-token context budget; the budget favors diverse source memos
	// over depth.
	for i := 0; i < 30; i++ {
		fixture.createMemo(ctx, t, alice.ID, fmt.Sprintf("memo-%02d", i), "hiking "+strings.Repeat("trail notes ", 340), store.Private)
	}
	fixture.syncSearch(ctx)
	conversation := fixture.createConversation(ctx, t, alice, "")

	collector, err := fixture.send(ctx, alice, conversation.UID, "hiking", "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	// Many memos are cited — breadth over depth — and the quoted context
	// stays within the token budget.
	require.Greater(t, len(complete.Citations), 3)
	prompt := fixture.model.StreamRequests[0].Messages[1].Content
	require.LessOrEqual(t, len([]rune(prompt)), 8000*4+200)
}

func TestSendMessageCarriesRetrievalReasons(t *testing.T) {
	ctx := context.Background()
	fixture := newChatTestFixture(ctx, t, &aitest.Model{
		StreamEvents: []internalai.StreamEvent{{Delta: "answer"}},
	}, nil)
	fixture.configureGeneration(ctx, t)
	alice := fixture.createUser(ctx, t, "alice")
	fixture.createMemo(ctx, t, alice.ID, "alice-hiking", "My favorite hiking spot is the alpine lakes trail.", store.Private)
	fixture.syncSearch(ctx)
	conversation := fixture.createConversation(ctx, t, alice, "")

	// A question beyond the normalized-query budget is truncated, and the
	// attempt honestly carries the machine-readable reason.
	collector, err := fixture.send(ctx, alice, conversation.UID, "hiking "+strings.Repeat("trail ", 300), "req-1")
	require.NoError(t, err)
	complete := requireCompleteEvent(t, collector)
	require.Contains(t, complete.RetrievalReasons, "query_truncated")

	// The reason persists on the stored attempt.
	messages := fixture.getMessages(ctx, t, alice, conversation.UID)
	require.Len(t, messages, 2)
	require.Contains(t, messages[1].RetrievalReasons, "query_truncated")
}
