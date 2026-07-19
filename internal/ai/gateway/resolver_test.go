package gateway_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	"github.com/usememos/memos/internal/ai/gateway"
)

func TestResolverReturnsProviderNeutralCapability(t *testing.T) {
	fake := &aitest.Model{}
	resolver := gateway.NewResolver(
		[]ai.ProviderConfig{{ID: "provider", Type: ai.ProviderOpenAI}},
		func(ai.ProviderConfig) *http.Client { return &http.Client{} },
		func(ai.ProviderConfig, *http.Client) (ai.Model, error) { return fake, nil },
	)
	resolved, err := resolver.Resolve(gateway.Assignment{ProviderID: "provider", Model: "model", Dimensions: 768})
	require.NoError(t, err)
	require.Same(t, fake, resolved.Model)
	require.Equal(t, "model", resolved.ModelName)
	require.Equal(t, 768, resolved.Dimensions)
}
