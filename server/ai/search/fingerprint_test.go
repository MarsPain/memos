package search

import (
	"testing"

	"github.com/stretchr/testify/require"

	internalai "github.com/usememos/memos/internal/ai"
)

func TestEndpointIdentity(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "host only", endpoint: "https://api.openai.com", want: "https://api.openai.com"},
		{name: "path kept", endpoint: "https://api.openai.com/v1", want: "https://api.openai.com/v1"},
		{name: "scheme and host lowercased", endpoint: "HTTPS://API.OpenAI.COM/v1", want: "https://api.openai.com/v1"},
		{name: "port kept", endpoint: "http://localhost:11434/v1", want: "http://localhost:11434/v1"},
		{name: "credentials stripped", endpoint: "https://user:secret@api.openai.com/v1", want: "https://api.openai.com/v1"},
		{name: "query stripped", endpoint: "https://api.openai.com/v1?api_key=secret", want: "https://api.openai.com/v1"},
		{name: "fragment stripped", endpoint: "https://api.openai.com/v1#frag", want: "https://api.openai.com/v1"},
		{name: "not a URL", endpoint: "not a url", want: ""},
		{name: "empty", endpoint: "", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, endpointIdentity(test.endpoint))
		})
	}
}

func TestComputeIndexFingerprint(t *testing.T) {
	base := computeIndexFingerprint(internalai.ProviderOpenAI, "https://api.openai.com", "text-embedding-3-small", 1536)
	require.Len(t, base, 64)

	// Deterministic.
	require.Equal(t, base, computeIndexFingerprint(internalai.ProviderOpenAI, "https://api.openai.com", "text-embedding-3-small", 1536))

	// Every input changes the fingerprint.
	require.NotEqual(t, base, computeIndexFingerprint(internalai.ProviderGemini, "https://api.openai.com", "text-embedding-3-small", 1536))
	require.NotEqual(t, base, computeIndexFingerprint(internalai.ProviderOpenAI, "https://other.example.com", "text-embedding-3-small", 1536))
	require.NotEqual(t, base, computeIndexFingerprint(internalai.ProviderOpenAI, "https://api.openai.com", "other-model", 1536))
	require.NotEqual(t, base, computeIndexFingerprint(internalai.ProviderOpenAI, "https://api.openai.com", "text-embedding-3-small", 768))
}
