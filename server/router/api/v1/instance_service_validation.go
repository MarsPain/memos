package v1

import (
	"context"
	"math"
	"regexp"
	"strings"

	"github.com/lithammer/shortuuid/v4"
	"github.com/pkg/errors"
	colorpb "google.golang.org/genproto/googleapis/type/color"

	"github.com/usememos/memos/internal/ai"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
)

func validateInstanceSetting(setting *v1pb.InstanceSetting) error {
	key, err := ExtractInstanceSettingKeyFromName(setting.Name)
	if err != nil {
		return err
	}
	if key != storepb.InstanceSettingKey_TAGS.String() {
		return nil
	}
	return validateInstanceTagsSetting(setting.GetTagsSetting())
}

func (s *APIV1Service) prepareInstanceAISettingForUpdate(ctx context.Context, setting *storepb.InstanceAISetting) error {
	if setting == nil {
		return errors.New("AI setting is required")
	}

	existing, err := s.Store.GetInstanceAISetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get existing AI setting")
	}
	existingProviders := map[string]*storepb.AIProviderConfig{}
	if existing != nil {
		for _, provider := range existing.Providers {
			if provider != nil && provider.Id != "" {
				existingProviders[provider.Id] = provider
			}
		}
	}

	seenIDs := map[string]bool{}
	for _, provider := range setting.Providers {
		if provider == nil {
			return errors.New("provider cannot be nil")
		}

		provider.Id = strings.TrimSpace(provider.Id)
		if provider.Id == "" {
			provider.Id = shortuuid.New()
		}
		if seenIDs[provider.Id] {
			return errors.Errorf("duplicate provider ID %q", provider.Id)
		}
		seenIDs[provider.Id] = true

		provider.Title = strings.TrimSpace(provider.Title)
		if provider.Title == "" {
			return errors.New("provider title is required")
		}
		if provider.Type != storepb.AIProviderType_OPENAI && provider.Type != storepb.AIProviderType_GEMINI {
			return errors.Errorf("provider %q has unsupported type", provider.Id)
		}

		provider.Endpoint = strings.TrimSpace(provider.Endpoint)
		if provider.Type == storepb.AIProviderType_OPENAI && provider.Endpoint == "" {
			provider.Endpoint = "https://api.openai.com/v1"
		}
		if provider.Type == storepb.AIProviderType_GEMINI && provider.Endpoint == "" {
			provider.Endpoint = "https://generativelanguage.googleapis.com/v1beta"
		}
		if _, err := ai.ValidateEndpoint(provider.Endpoint, provider.AllowPrivateNetwork); err != nil {
			return errors.Wrapf(err, "provider %q endpoint", provider.Id)
		}

		if provider.ApiKey == "" {
			if existingProvider, ok := existingProviders[provider.Id]; ok {
				provider.ApiKey = existingProvider.ApiKey
			}
		}
		if provider.ApiKey == "" {
			return errors.Errorf("provider %q API key is required", provider.Id)
		}
	}

	if err := preparePersistedTranscriptionConfig(setting, existing); err != nil {
		return err
	}
	if err := preparePersistedCapabilityAssignments(setting, existing); err != nil {
		return err
	}
	return nil
}

func preparePersistedCapabilityAssignments(setting *storepb.InstanceAISetting, existing *storepb.InstanceAISetting) error {
	if existing != nil {
		if setting.Generation == nil {
			setting.Generation = existing.GetGeneration()
		}
		if setting.Embedding == nil {
			setting.Embedding = existing.GetEmbedding()
		}
	}
	providerIDs := make(map[string]struct{}, len(setting.Providers))
	for _, provider := range setting.Providers {
		if provider != nil {
			providerIDs[provider.Id] = struct{}{}
		}
	}
	validateAssignment := func(name, providerID, model string) error {
		providerID = strings.TrimSpace(providerID)
		model = strings.TrimSpace(model)
		if providerID == "" && model == "" {
			return nil
		}
		if providerID == "" || model == "" {
			return errors.Errorf("%s requires provider_id and model", name)
		}
		if _, ok := providerIDs[providerID]; !ok {
			return errors.Errorf("%s provider_id %q does not reference any configured provider", name, providerID)
		}
		if len(model) > maxTranscriptionConfigModelLength {
			return errors.Errorf("%s model is too long; maximum length is %d characters", name, maxTranscriptionConfigModelLength)
		}
		return nil
	}
	if generation := setting.Generation; generation != nil {
		generation.ProviderId = strings.TrimSpace(generation.ProviderId)
		generation.Model = strings.TrimSpace(generation.Model)
		if err := validateAssignment("generation", generation.ProviderId, generation.Model); err != nil {
			return err
		}
	}
	if embedding := setting.Embedding; embedding != nil {
		embedding.ProviderId = strings.TrimSpace(embedding.ProviderId)
		embedding.Model = strings.TrimSpace(embedding.Model)
		if err := validateAssignment("embedding", embedding.ProviderId, embedding.Model); err != nil {
			return err
		}
		if embedding.Dimensions < 0 || embedding.Dimensions > 65536 {
			return errors.New("embedding dimensions must be between 1 and 65536 when specified")
		}
		if embedding.ProviderId != "" && !setting.ExternalProcessingAcknowledged {
			return errors.New("external processing acknowledgement is required before assigning embeddings")
		}
	}
	setting.Readiness = readinessAfterSettingUpdate(setting, existing)
	return nil
}

func readinessAfterSettingUpdate(setting, existing *storepb.InstanceAISetting) *storepb.CapabilityReadiness {
	readiness := &storepb.CapabilityReadiness{
		TextGeneration:  storepb.CapabilityState_NOT_CONFIGURED,
		Streaming:       storepb.CapabilityState_NOT_CONFIGURED,
		StructuredTools: storepb.CapabilityState_NOT_CONFIGURED,
		Embeddings:      storepb.CapabilityState_NOT_CONFIGURED,
	}
	if setting.GetGeneration().GetProviderId() != "" {
		readiness.TextGeneration = storepb.CapabilityState_UNVALIDATED
		readiness.Streaming = storepb.CapabilityState_UNVALIDATED
		readiness.StructuredTools = storepb.CapabilityState_UNVALIDATED
	}
	if setting.GetEmbedding().GetProviderId() != "" {
		readiness.Embeddings = storepb.CapabilityState_UNVALIDATED
	}
	if existing != nil && existing.Readiness != nil &&
		setting.GetGeneration().GetProviderId() == existing.GetGeneration().GetProviderId() &&
		setting.GetGeneration().GetModel() == existing.GetGeneration().GetModel() &&
		setting.GetEmbedding().GetProviderId() == existing.GetEmbedding().GetProviderId() &&
		setting.GetEmbedding().GetModel() == existing.GetEmbedding().GetModel() &&
		setting.GetEmbedding().GetDimensions() == existing.GetEmbedding().GetDimensions() {
		return existing.Readiness
	}
	return readiness
}

func preparePersistedTranscriptionConfig(setting *storepb.InstanceAISetting, existing *storepb.InstanceAISetting) error {
	// Preserve the previously stored transcription config when the request omits it,
	// matching the same "absence == keep" semantics used for API keys. The preserved
	// config still falls through to validation below, so a stale provider_id is
	// rejected if the same update removed or renamed its referenced provider.
	if setting.Transcription == nil && existing != nil {
		setting.Transcription = existing.GetTranscription()
	}
	if setting.Transcription == nil {
		return nil
	}

	cfg := setting.Transcription
	cfg.ProviderId = strings.TrimSpace(cfg.ProviderId)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Language = strings.TrimSpace(cfg.Language)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)

	if cfg.ProviderId != "" {
		referenced := false
		for _, provider := range setting.Providers {
			if provider != nil && provider.Id == cfg.ProviderId {
				referenced = true
				break
			}
		}
		if !referenced {
			return errors.Errorf("transcription provider_id %q does not reference any configured provider", cfg.ProviderId)
		}
	}

	if len(cfg.Model) > maxTranscriptionConfigModelLength {
		return errors.Errorf("transcription model is too long; maximum length is %d characters", maxTranscriptionConfigModelLength)
	}
	if len(cfg.Language) > maxTranscriptionConfigLanguageLength {
		return errors.Errorf("transcription language is too long; maximum length is %d characters", maxTranscriptionConfigLanguageLength)
	}
	if len(cfg.Prompt) > maxTranscriptionConfigPromptLength {
		return errors.Errorf("transcription prompt is too long; maximum length is %d characters", maxTranscriptionConfigPromptLength)
	}
	return nil
}

func maskAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 8 {
		return "..."
	}
	prefixLength := min(4, len(apiKey))
	return apiKey[:prefixLength] + "..." + apiKey[len(apiKey)-4:]
}

func validateInstanceTagsSetting(setting *v1pb.InstanceSetting_TagsSetting) error {
	if setting == nil {
		return errors.New("tags setting is required")
	}
	for tag, metadata := range setting.Tags {
		if strings.TrimSpace(tag) == "" {
			return errors.New("tag key cannot be empty")
		}
		if _, err := regexp.Compile(tag); err != nil {
			return errors.Errorf("tag key %q is not a valid regex pattern: %v", tag, err)
		}
		if metadata == nil {
			return errors.Errorf("tag metadata is required for %q", tag)
		}
		if metadata.GetBackgroundColor() != nil {
			if err := validateInstanceColor(metadata.GetBackgroundColor()); err != nil {
				return errors.Wrapf(err, "background_color for %q", tag)
			}
		}
	}
	return nil
}

func validateInstanceColor(color *colorpb.Color) error {
	if err := validateInstanceColorComponent("red", color.GetRed()); err != nil {
		return err
	}
	if err := validateInstanceColorComponent("green", color.GetGreen()); err != nil {
		return err
	}
	if err := validateInstanceColorComponent("blue", color.GetBlue()); err != nil {
		return err
	}
	if alpha := color.GetAlpha(); alpha != nil {
		if err := validateInstanceColorComponent("alpha", alpha.GetValue()); err != nil {
			return err
		}
	}
	return nil
}

func validateInstanceColorComponent(name string, value float32) error {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return errors.Errorf("%s must be a finite number", name)
	}
	if value < 0 || value > 1 {
		return errors.Errorf("%s must be between 0 and 1", name)
	}
	return nil
}
