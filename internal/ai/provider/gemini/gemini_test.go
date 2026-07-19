package gemini_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/provider/gemini"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestAdapterGeneratesStreamsAndEmbeds(t *testing.T) {
	client := ai.NewHTTPClient(ai.TransportConfig{
		LookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
		Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "secret", request.Header.Get("x-goog-api-key"))
			switch {
			case strings.HasSuffix(request.URL.Path, ":generateContent"):
				return response(request, http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`), nil
			case strings.HasSuffix(request.URL.Path, ":streamGenerateContent"):
				return response(request, http.StatusOK, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}]}\n\n"), nil
			case strings.HasSuffix(request.URL.Path, ":batchEmbedContents"):
				return response(request, http.StatusOK, `{"embeddings":[{"values":[0.5,0.25]}]}`), nil
			default:
				t.Fatalf("unexpected path %s", request.URL.Path)
				return nil, nil
			}
		}),
	})
	adapter := gemini.New(ai.ProviderConfig{Endpoint: "https://public.example/v1beta", APIKey: "secret"}, client)

	generated, err := adapter.Generate(context.Background(), ai.GenerationRequest{Model: "gemini", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	require.Equal(t, "answer", generated.Content)
	require.Equal(t, 3, generated.Usage.TotalTokens)

	stream, err := adapter.Stream(context.Background(), ai.GenerationRequest{Model: "gemini", Messages: []ai.Message{{Role: ai.RoleUser, Content: "hello"}}})
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
