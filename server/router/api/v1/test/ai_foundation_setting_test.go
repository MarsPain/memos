package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/aitest"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

func TestInstanceAISettingCapabilityAssignmentsRoundTrip(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	admin, err := ts.CreateHostUser(ctx, "admin")
	require.NoError(t, err)
	adminCtx := ts.CreateUserContext(ctx, admin.ID)

	updated, err := ts.Service.UpdateInstanceSetting(adminCtx, &v1pb.UpdateInstanceSettingRequest{Setting: &v1pb.InstanceSetting{
		Name: "instance/settings/AI",
		Value: &v1pb.InstanceSetting_AiSetting{AiSetting: &v1pb.InstanceSetting_AISetting{
			Providers: []*v1pb.InstanceSetting_AIProviderConfig{{
				Id: "local", Title: "Local", Type: v1pb.InstanceSetting_OPENAI, Endpoint: "http://10.0.0.2/v1", ApiKey: "secret",
				AllowPrivateNetwork: true,
			}},
			Generation:                     &v1pb.InstanceSetting_GenerationConfig{ProviderId: "local", Model: "chat-model"},
			Embedding:                      &v1pb.InstanceSetting_EmbeddingConfig{ProviderId: "local", Model: "embed-model", Dimensions: 768},
			ExternalProcessingAcknowledged: true,
		}},
	}})
	require.NoError(t, err)
	aiSetting := updated.GetAiSetting()
	require.Equal(t, "chat-model", aiSetting.GetGeneration().GetModel())
	require.Equal(t, int32(768), aiSetting.GetEmbedding().GetDimensions())
	require.True(t, aiSetting.GetExternalProcessingAcknowledged())
	require.True(t, aiSetting.GetProviders()[0].GetAllowPrivateNetwork())
	require.Empty(t, aiSetting.GetProviders()[0].GetApiKey())

	stored, err := ts.Store.GetInstanceAISetting(ctx)
	require.NoError(t, err)
	require.Equal(t, "secret", stored.GetProviders()[0].GetApiKey())
	require.Equal(t, "local", stored.GetGeneration().GetProviderId())
}

func TestInstanceAIConnectivityTestIsAdminOnlyAndSanitized(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	admin, err := ts.CreateHostUser(ctx, "admin")
	require.NoError(t, err)
	user, err := ts.CreateRegularUser(ctx, "user")
	require.NoError(t, err)
	adminCtx := ts.CreateUserContext(ctx, admin.ID)
	userCtx := ts.CreateUserContext(ctx, user.ID)

	_, err = ts.Service.UpdateInstanceSetting(adminCtx, &v1pb.UpdateInstanceSettingRequest{Setting: &v1pb.InstanceSetting{
		Name: "instance/settings/AI",
		Value: &v1pb.InstanceSetting_AiSetting{AiSetting: &v1pb.InstanceSetting_AISetting{
			Providers:  []*v1pb.InstanceSetting_AIProviderConfig{{Id: "p", Title: "P", Type: v1pb.InstanceSetting_OPENAI, ApiKey: "stored-secret"}},
			Generation: &v1pb.InstanceSetting_GenerationConfig{ProviderId: "p", Model: "model"},
		}},
	}})
	require.NoError(t, err)

	fake := &aitest.Model{ProbeErrors: map[ai.Capability]error{
		ai.CapabilityTextGeneration: ai.NewProviderError(ai.ErrorAuthentication, "AI provider authentication failed", nil),
	}}
	ts.Service.AIModelFactory = func(config ai.ProviderConfig, _ *http.Client) (ai.Model, error) {
		require.Equal(t, "stored-secret", config.APIKey)
		return fake, nil
	}
	request := &v1pb.TestInstanceAISettingRequest{
		Provider:   &v1pb.InstanceSetting_AIProviderConfig{Id: "p", Title: "P", Type: v1pb.InstanceSetting_OPENAI},
		Capability: v1pb.InstanceSetting_TEXT_GENERATION,
		Model:      "model",
	}
	_, err = ts.Service.TestInstanceAISetting(userCtx, request)
	require.ErrorContains(t, err, "permission denied")

	result, err := ts.Service.TestInstanceAISetting(adminCtx, request)
	require.NoError(t, err)
	require.False(t, result.GetReady())
	require.Equal(t, string(ai.ErrorAuthentication), result.GetCategory())
	require.NotContains(t, result.GetMessage(), "stored-secret")
}

func TestInstanceAISettingRejectsUnsafeOrInvalidAssignments(t *testing.T) {
	ctx := context.Background()
	ts := NewTestService(t)
	defer ts.Cleanup()

	admin, err := ts.CreateHostUser(ctx, "admin")
	require.NoError(t, err)
	adminCtx := ts.CreateUserContext(ctx, admin.ID)

	tests := []struct {
		name       string
		provider   *v1pb.InstanceSetting_AIProviderConfig
		generation *v1pb.InstanceSetting_GenerationConfig
		embedding  *v1pb.InstanceSetting_EmbeddingConfig
		want       string
	}{
		{
			name:     "private endpoint requires opt in",
			provider: &v1pb.InstanceSetting_AIProviderConfig{Id: "p", Title: "P", Type: v1pb.InstanceSetting_OPENAI, Endpoint: "http://127.0.0.1:8080/v1", ApiKey: "secret"},
			want:     "private-network authorization",
		},
		{
			name:       "generation provider exists",
			provider:   &v1pb.InstanceSetting_AIProviderConfig{Id: "p", Title: "P", Type: v1pb.InstanceSetting_OPENAI, ApiKey: "secret"},
			generation: &v1pb.InstanceSetting_GenerationConfig{ProviderId: "missing", Model: "model"},
			want:       "generation provider_id",
		},
		{
			name:      "embedding dimensions are positive",
			provider:  &v1pb.InstanceSetting_AIProviderConfig{Id: "p", Title: "P", Type: v1pb.InstanceSetting_OPENAI, ApiKey: "secret"},
			embedding: &v1pb.InstanceSetting_EmbeddingConfig{ProviderId: "p", Model: "model", Dimensions: -1},
			want:      "embedding dimensions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ts.Service.UpdateInstanceSetting(adminCtx, &v1pb.UpdateInstanceSettingRequest{Setting: &v1pb.InstanceSetting{
				Name: "instance/settings/AI",
				Value: &v1pb.InstanceSetting_AiSetting{AiSetting: &v1pb.InstanceSetting_AISetting{
					Providers: []*v1pb.InstanceSetting_AIProviderConfig{test.provider}, Generation: test.generation, Embedding: test.embedding,
				}},
			}})
			require.ErrorContains(t, err, test.want)
		})
	}
}
