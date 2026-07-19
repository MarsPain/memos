package openai_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/provider/openai"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func newClient(t *testing.T, handler roundTripFunc) *http.Client {
	t.Helper()
	return ai.NewHTTPClient(ai.TransportConfig{
		AllowPrivateNetwork: false,
		LookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
		Base: handler,
	})
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestAdapterGeneratesStreamsAndEmbeds(t *testing.T) {
	client := newClient(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/v1/chat/completions":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			if strings.Contains(string(body), `"stream":true`) {
				return response(request, http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"), nil
			}
			return response(request, http.StatusOK, `{"choices":[{"message":{"content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`), nil
		case "/v1/embeddings":
			return response(request, http.StatusOK, `{"data":[{"index":0,"embedding":[0.5,0.25]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`), nil
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
			return nil, nil
		}
	})
	adapter := openai.New(ai.ProviderConfig{Endpoint: "https://public.example/v1", APIKey: "secret"}, client)

	generated, err := adapter.Generate(context.Background(), ai.GenerationRequest{Model: "chat", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	require.Equal(t, "answer", generated.Content)
	require.Equal(t, 5, generated.Usage.TotalTokens)

	stream, err := adapter.Stream(context.Background(), ai.GenerationRequest{Model: "chat", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	var streamed string
	for event := range stream {
		require.NoError(t, event.Err)
		streamed += event.Delta
	}
	require.Equal(t, "hello", streamed)

	embedded, err := adapter.Embed(context.Background(), ai.EmbeddingRequest{Model: "embed", Inputs: []string{"hello"}, Dimensions: 2})
	require.NoError(t, err)
	require.Equal(t, [][]float32{{0.5, 0.25}}, embedded.Vectors)
}

func TestAdapterNormalizesProviderFailures(t *testing.T) {
	client := newClient(t, func(request *http.Request) (*http.Response, error) {
		return response(request, http.StatusUnauthorized, `{"error":{"message":"secret provider payload"}}`), nil
	})
	adapter := openai.New(ai.ProviderConfig{Endpoint: "https://public.example/v1", APIKey: "secret"}, client)
	_, err := adapter.Generate(context.Background(), ai.GenerationRequest{Model: "chat", Messages: []ai.Message{{Role: ai.RoleUser, Content: "private prompt"}}})
	require.Equal(t, ai.ErrorAuthentication, ai.CategoryOf(err))
	require.NotContains(t, err.Error(), "secret provider payload")
	require.NotContains(t, err.Error(), "private prompt")
}
