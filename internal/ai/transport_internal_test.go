package ai

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderTransportDoesNotUseAmbientProxies(t *testing.T) {
	client := NewHTTPClient(TransportConfig{})
	policy, ok := client.Transport.(*policyRoundTripper)
	require.True(t, ok)
	base, ok := policy.base.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, base.Proxy)
}
