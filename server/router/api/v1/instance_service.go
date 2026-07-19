package v1

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/gateway"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	storepb "github.com/usememos/memos/proto/gen/store"
	"github.com/usememos/memos/server/notification"
	"github.com/usememos/memos/store"
)

const (
	maxTranscriptionConfigModelLength    = 256
	maxTranscriptionConfigLanguageLength = 32
	maxTranscriptionConfigPromptLength   = 4096
	maxBatchGetInstanceSettings          = 100
)

type instanceSettingCaller struct {
	user   *store.User
	loaded bool
}

func (c *instanceSettingCaller) currentUser(ctx context.Context, service *APIV1Service) (*store.User, error) {
	if c.loaded {
		return c.user, nil
	}
	user, err := service.fetchCurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	c.user = user
	c.loaded = true
	return c.user, nil
}

// GetInstanceProfile returns the instance profile.
func (s *APIV1Service) GetInstanceProfile(ctx context.Context, _ *v1pb.GetInstanceProfileRequest) (*v1pb.InstanceProfile, error) {
	admin, err := s.GetInstanceAdmin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get instance admin: %v", err)
	}

	// needs_setup reflects whether the instance has any users at all, which is
	// the real signal for first-run setup. It is deliberately independent of the
	// admin lookup: an instance that has lost its admins still has users and must
	// not be treated as a fresh install.
	limitOne := 1
	users, err := s.Store.ListUsers(ctx, &store.FindUser{Limit: &limitOne})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list users: %v", err)
	}
	aiSetting, err := s.Store.GetInstanceAISetting(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get AI setting: %v", err)
	}

	instanceProfile := &v1pb.InstanceProfile{
		Version:                     s.Profile.Version,
		Demo:                        s.Profile.Demo,
		InstanceUrl:                 s.Profile.InstanceURL,
		Admin:                       admin, // for display only; may be nil even on a populated instance
		Commit:                      s.Profile.Commit,
		NeedsSetup:                  len(users) == 0,
		ExternalAiProcessingEnabled: aiSetting.GetGeneration().GetProviderId() != "" || aiSetting.GetEmbedding().GetProviderId() != "",
	}
	return instanceProfile, nil
}

func (s *APIV1Service) GetInstanceSetting(ctx context.Context, request *v1pb.GetInstanceSettingRequest) (*v1pb.InstanceSetting, error) {
	return s.getInstanceSettingByName(ctx, request.Name, &instanceSettingCaller{})
}

// BatchGetInstanceSettings returns multiple instance settings in request order.
func (s *APIV1Service) BatchGetInstanceSettings(ctx context.Context, request *v1pb.BatchGetInstanceSettingsRequest) (*v1pb.BatchGetInstanceSettingsResponse, error) {
	if len(request.Names) > maxBatchGetInstanceSettings {
		return nil, status.Errorf(codes.InvalidArgument, "too many instance setting names (max %d)", maxBatchGetInstanceSettings)
	}

	caller := &instanceSettingCaller{}
	settings := make([]*v1pb.InstanceSetting, 0, len(request.Names))
	for _, name := range request.Names {
		setting, err := s.getInstanceSettingByName(ctx, name, caller)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}

	return &v1pb.BatchGetInstanceSettingsResponse{Settings: settings}, nil
}

func (s *APIV1Service) getInstanceSettingByName(ctx context.Context, name string, caller *instanceSettingCaller) (*v1pb.InstanceSetting, error) {
	instanceSettingKeyString, err := ExtractInstanceSettingKeyFromName(name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid instance setting name: %v", err)
	}

	instanceSettingKey := storepb.InstanceSettingKey(storepb.InstanceSettingKey_value[instanceSettingKeyString])
	// Get instance setting from store with default value.
	var instanceSetting *storepb.InstanceSetting
	switch instanceSettingKey {
	case storepb.InstanceSettingKey_BASIC:
		var setting *storepb.InstanceBasicSetting
		setting, err = s.Store.GetInstanceBasicSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_BasicSetting{BasicSetting: setting}}
	case storepb.InstanceSettingKey_GENERAL:
		var setting *storepb.InstanceGeneralSetting
		setting, err = s.Store.GetInstanceGeneralSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_GeneralSetting{GeneralSetting: setting}}
	case storepb.InstanceSettingKey_MEMO_RELATED:
		var setting *storepb.InstanceMemoRelatedSetting
		setting, err = s.Store.GetInstanceMemoRelatedSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_MemoRelatedSetting{MemoRelatedSetting: setting}}
	case storepb.InstanceSettingKey_STORAGE:
		var setting *storepb.InstanceStorageSetting
		setting, err = s.Store.GetInstanceStorageSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_StorageSetting{StorageSetting: setting}}
	case storepb.InstanceSettingKey_TAGS:
		var setting *storepb.InstanceTagsSetting
		setting, err = s.Store.GetInstanceTagsSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_TagsSetting{TagsSetting: setting}}
	case storepb.InstanceSettingKey_NOTIFICATION:
		var setting *storepb.InstanceNotificationSetting
		setting, err = s.Store.GetInstanceNotificationSetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_NotificationSetting{NotificationSetting: setting}}
	case storepb.InstanceSettingKey_AI:
		var setting *storepb.InstanceAISetting
		setting, err = s.Store.GetInstanceAISetting(ctx)
		instanceSetting = &storepb.InstanceSetting{Key: instanceSettingKey, Value: &storepb.InstanceSetting_AiSetting{AiSetting: setting}}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported instance setting key: %v", instanceSettingKey)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get instance setting: %v", err)
	}

	// Storage and notification settings contain credentials; restrict to admins only.
	if instanceSetting.Key == storepb.InstanceSettingKey_STORAGE ||
		instanceSetting.Key == storepb.InstanceSettingKey_NOTIFICATION {
		user, err := caller.currentUser(ctx, s)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
		}
		if user == nil {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		if user.Role != store.RoleAdmin {
			return nil, status.Errorf(codes.PermissionDenied, "permission denied")
		}
	}
	isAdminCaller := false
	if instanceSetting.Key == storepb.InstanceSettingKey_AI {
		user, err := caller.currentUser(ctx, s)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
		}
		if user == nil {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		isAdminCaller = user.Role == store.RoleAdmin
	}

	result := convertInstanceSettingFromStore(instanceSetting)
	if instanceSetting.Key == storepb.InstanceSettingKey_AI && !isAdminCaller {
		// Non-admin callers only need transcription.provider_id to gate the
		// editor's Transcribe button. Model / language / prompt are
		// admin-entered defaults that may contain proprietary glossary terms,
		// so they are redacted from non-admin responses.
		if ai := result.GetAiSetting(); ai != nil && ai.Transcription != nil {
			ai.Transcription.Model = ""
			ai.Transcription.Language = ""
			ai.Transcription.Prompt = ""
		}
	}
	return result, nil
}

func (s *APIV1Service) UpdateInstanceSetting(ctx context.Context, request *v1pb.UpdateInstanceSettingRequest) (*v1pb.InstanceSetting, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if user.Role != store.RoleAdmin {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}
	if request.Setting == nil {
		return nil, status.Errorf(codes.InvalidArgument, "instance setting is required")
	}
	settingKeyString, err := ExtractInstanceSettingKeyFromName(request.Setting.Name)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid instance setting name: %v", err)
	}
	settingKey := storepb.InstanceSettingKey(storepb.InstanceSettingKey_value[settingKeyString])
	if s.Store.IsInstanceSettingDeploymentConfigured(settingKey) {
		return nil, status.Errorf(codes.FailedPrecondition, "instance setting %q is configured by the deployment", settingKeyString)
	}

	// TODO: Apply update_mask if specified
	_ = request.UpdateMask

	if err := validateInstanceSetting(request.Setting); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid instance setting: %v", err)
	}

	updateSetting := convertInstanceSettingToStore(request.Setting)

	// Preserve write-only credential fields when the caller sends an empty value.
	// An empty string means "no change", not "clear the credential".
	switch updateSetting.Key {
	case storepb.InstanceSettingKey_NOTIFICATION:
		if notif := updateSetting.GetNotificationSetting(); notif != nil && notif.Email != nil && notif.Email.SmtpPassword == "" {
			existing, err := s.Store.GetInstanceNotificationSetting(ctx)
			if err == nil && existing != nil && existing.Email != nil {
				if existing.Email.SmtpPassword != "" && !sameSMTPConnectionIdentity(notif.Email, existing.Email) {
					return nil, status.Errorf(codes.InvalidArgument, "smtp password is required when changing SMTP host, port, username, or encryption settings")
				}
				notif.Email.SmtpPassword = existing.Email.SmtpPassword
			}
		}
	case storepb.InstanceSettingKey_STORAGE:
		if storage := updateSetting.GetStorageSetting(); storage != nil && storage.S3Config != nil && storage.S3Config.AccessKeySecret == "" {
			existing, err := s.Store.GetInstanceStorageSetting(ctx)
			if err == nil && existing != nil && existing.S3Config != nil {
				storage.S3Config.AccessKeySecret = existing.S3Config.AccessKeySecret
			}
		}
	case storepb.InstanceSettingKey_AI:
		if err := s.prepareInstanceAISettingForUpdate(ctx, updateSetting.GetAiSetting()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid AI setting: %v", err)
		}
	default:
		// No credential preservation needed for other setting types.
	}

	var instanceSetting *storepb.InstanceSetting
	if updateSetting.Key == storepb.InstanceSettingKey_GENERAL {
		instanceSetting, err = s.Store.UpsertInstanceGeneralSettingSafely(ctx, updateSetting)
	} else {
		instanceSetting, err = s.Store.UpsertInstanceSetting(ctx, updateSetting)
	}
	if err != nil {
		if errors.Is(err, store.ErrUnsafeAuthenticationConfiguration) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to upsert instance setting: %v", err)
	}

	return convertInstanceSettingFromStore(instanceSetting), nil
}

func (s *APIV1Service) TestInstanceEmailSetting(ctx context.Context, request *v1pb.TestInstanceEmailSettingRequest) (*emptypb.Empty, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if user.Role != store.RoleAdmin {
		return nil, status.Errorf(codes.PermissionDenied, "permission denied")
	}

	emailSetting, err := s.resolveTestEmailSetting(ctx, request.Email)
	if err != nil {
		return nil, err
	}

	recipientEmail := strings.TrimSpace(request.RecipientEmail)
	if recipientEmail == "" {
		recipientEmail = strings.TrimSpace(user.Email)
	}
	if recipientEmail == "" {
		return nil, status.Errorf(codes.InvalidArgument, "recipient email is required")
	}

	if err := notification.ValidateEmailSetting(emailSetting); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid notification email setting: %v", err)
	}

	if err := notification.SendTestEmail(emailSetting, recipientEmail); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to send test email: %v. Check that the SMTP port matches encryption: Gmail uses port 587 with STARTTLS on and SSL/TLS off; port 465 requires SSL/TLS on", err)
	}

	return &emptypb.Empty{}, nil
}

// TestInstanceAISetting tests one configured AI capability through the shared provider transport.
func (s *APIV1Service) TestInstanceAISetting(ctx context.Context, request *v1pb.TestInstanceAISettingRequest) (*v1pb.TestInstanceAISettingResponse, error) {
	user, err := s.fetchCurrentUser(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current user: %v", err)
	}
	if user == nil {
		return nil, status.Error(codes.Unauthenticated, "user not authenticated")
	}
	if user.Role != store.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	if request == nil || request.Provider == nil {
		return nil, status.Error(codes.InvalidArgument, "AI provider is required")
	}
	modelName := strings.TrimSpace(request.Model)
	if modelName == "" || len(modelName) > maxTranscriptionConfigModelLength {
		return nil, status.Error(codes.InvalidArgument, "AI model is required and must not exceed 256 characters")
	}
	if request.Dimensions < 0 || request.Dimensions > 65536 {
		return nil, status.Error(codes.InvalidArgument, "embedding dimensions must be between 1 and 65536 when specified")
	}
	capability, err := convertAICapability(request.Capability)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	provider, err := s.resolveConnectivityTestProvider(ctx, request.Provider)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	client := ai.NewHTTPClient(ai.TransportConfig{AllowPrivateNetwork: provider.AllowPrivateNetwork})
	factory := s.AIModelFactory
	if factory == nil {
		factory = gateway.NewModel
	}
	model, err := factory(provider, client)
	if err != nil {
		return sanitizedConnectivityResponse(err), nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err = model.Probe(probeCtx, capability, modelName, int(request.Dimensions))
	if persistErr := s.persistCapabilityReadiness(ctx, request.Provider.Id, modelName, request.Dimensions, capability, err == nil); persistErr != nil {
		return nil, status.Error(codes.Internal, "failed to persist AI capability readiness")
	}
	if err != nil {
		return sanitizedConnectivityResponse(err), nil
	}
	return &v1pb.TestInstanceAISettingResponse{Ready: true, Message: "AI capability is ready"}, nil
}

func (s *APIV1Service) resolveConnectivityTestProvider(ctx context.Context, request *v1pb.InstanceSetting_AIProviderConfig) (ai.ProviderConfig, error) {
	provider := ai.ProviderConfig{
		ID: request.GetId(), Title: strings.TrimSpace(request.GetTitle()), Endpoint: strings.TrimSpace(request.GetEndpoint()),
		APIKey: request.GetApiKey(), AllowPrivateNetwork: request.GetAllowPrivateNetwork(),
	}
	switch request.GetType() {
	case v1pb.InstanceSetting_OPENAI:
		provider.Type = ai.ProviderOpenAI
		if provider.Endpoint == "" {
			provider.Endpoint = "https://api.openai.com/v1"
		}
	case v1pb.InstanceSetting_GEMINI:
		provider.Type = ai.ProviderGemini
		if provider.Endpoint == "" {
			provider.Endpoint = "https://generativelanguage.googleapis.com/v1beta"
		}
	default:
		return ai.ProviderConfig{}, errors.New("AI provider type is unsupported")
	}
	if provider.APIKey == "" && provider.ID != "" {
		existing, err := s.Store.GetInstanceAISetting(ctx)
		if err != nil {
			return ai.ProviderConfig{}, errors.Wrap(err, "failed to get stored AI provider")
		}
		for _, candidate := range existing.Providers {
			if candidate.GetId() != provider.ID {
				continue
			}
			if ai.ProviderType(candidate.GetType().String()) != provider.Type || candidate.GetEndpoint() != provider.Endpoint {
				return ai.ProviderConfig{}, errors.New("AI provider key is required when changing provider type or endpoint")
			}
			provider.APIKey = candidate.GetApiKey()
			break
		}
	}
	if provider.APIKey == "" {
		return ai.ProviderConfig{}, errors.New("AI provider API key is required")
	}
	if _, err := ai.ValidateEndpoint(provider.Endpoint, provider.AllowPrivateNetwork); err != nil {
		return ai.ProviderConfig{}, err
	}
	return provider, nil
}

func convertAICapability(capability v1pb.InstanceSetting_AICapability) (ai.Capability, error) {
	switch capability {
	case v1pb.InstanceSetting_TEXT_GENERATION:
		return ai.CapabilityTextGeneration, nil
	case v1pb.InstanceSetting_STREAMING:
		return ai.CapabilityStreaming, nil
	case v1pb.InstanceSetting_STRUCTURED_TOOLS:
		return ai.CapabilityStructuredTools, nil
	case v1pb.InstanceSetting_EMBEDDINGS:
		return ai.CapabilityEmbeddings, nil
	default:
		return "", errors.New("AI capability is unsupported")
	}
}

func sanitizedConnectivityResponse(err error) *v1pb.TestInstanceAISettingResponse {
	category := ai.CategoryOf(err)
	messages := map[ai.ErrorCategory]string{
		ai.ErrorConfiguration:  "AI provider configuration was rejected",
		ai.ErrorAuthentication: "AI provider authentication failed",
		ai.ErrorRateLimit:      "AI provider rate limit exceeded",
		ai.ErrorTimeout:        "AI provider request timed out",
		ai.ErrorUnavailable:    "AI provider is unavailable",
		ai.ErrorMalformed:      "AI provider returned malformed data",
		ai.ErrorInternal:       "AI provider test failed",
	}
	return &v1pb.TestInstanceAISettingResponse{Category: string(category), Message: messages[category]}
}

func (s *APIV1Service) persistCapabilityReadiness(ctx context.Context, providerID, modelName string, dimensions int32, capability ai.Capability, ready bool) error {
	setting, err := s.Store.GetInstanceAISetting(ctx)
	if err != nil || setting == nil {
		return err
	}
	matches := false
	if capability == ai.CapabilityEmbeddings {
		matches = setting.GetEmbedding().GetProviderId() == providerID && setting.GetEmbedding().GetModel() == modelName && setting.GetEmbedding().GetDimensions() == dimensions
	} else {
		matches = setting.GetGeneration().GetProviderId() == providerID && setting.GetGeneration().GetModel() == modelName
	}
	if !matches {
		return nil
	}
	updated := proto.Clone(setting).(*storepb.InstanceAISetting)
	if updated.Readiness == nil {
		updated.Readiness = &storepb.CapabilityReadiness{}
	}
	state := storepb.CapabilityState_UNAVAILABLE
	if ready {
		state = storepb.CapabilityState_READY
	}
	switch capability {
	case ai.CapabilityTextGeneration:
		updated.Readiness.TextGeneration = state
	case ai.CapabilityStreaming:
		updated.Readiness.Streaming = state
	case ai.CapabilityStructuredTools:
		updated.Readiness.StructuredTools = state
	case ai.CapabilityEmbeddings:
		updated.Readiness.Embeddings = state
	}
	_, err = s.Store.UpsertInstanceSetting(ctx, &storepb.InstanceSetting{Key: storepb.InstanceSettingKey_AI, Value: &storepb.InstanceSetting_AiSetting{AiSetting: updated}})
	return err
}

func (s *APIV1Service) resolveTestEmailSetting(ctx context.Context, requestEmail *v1pb.InstanceSetting_NotificationSetting_EmailSetting) (*storepb.InstanceNotificationSetting_EmailSetting, error) {
	if requestEmail == nil {
		existing, err := s.Store.GetInstanceNotificationSetting(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get notification setting: %v", err)
		}
		return existing.GetEmail(), nil
	}

	emailSetting := convertInstanceNotificationSettingToStore(&v1pb.InstanceSetting_NotificationSetting{Email: requestEmail}).GetEmail()
	if emailSetting.SmtpPassword != "" {
		return emailSetting, nil
	}

	existing, err := s.Store.GetInstanceNotificationSetting(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get notification setting: %v", err)
	}
	existingEmail := existing.GetEmail()
	if existingEmail == nil || existingEmail.SmtpPassword == "" {
		return emailSetting, nil
	}
	if sameSMTPConnectionIdentity(emailSetting, existingEmail) {
		emailSetting.SmtpPassword = existingEmail.SmtpPassword
		return emailSetting, nil
	}
	return nil, status.Errorf(codes.InvalidArgument, "smtp password is required when changing SMTP host, port, username, or encryption settings")
}

func sameSMTPConnectionIdentity(setting, existing *storepb.InstanceNotificationSetting_EmailSetting) bool {
	if setting == nil || existing == nil {
		return false
	}
	return strings.TrimSpace(setting.SmtpHost) == strings.TrimSpace(existing.SmtpHost) &&
		setting.SmtpPort == existing.SmtpPort &&
		strings.TrimSpace(setting.SmtpUsername) == strings.TrimSpace(existing.SmtpUsername) &&
		setting.UseTls == existing.UseTls &&
		setting.UseSsl == existing.UseSsl
}

func (s *APIV1Service) GetInstanceAdmin(ctx context.Context) (*v1pb.User, error) {
	adminUserType := store.RoleAdmin
	user, err := s.Store.GetUser(ctx, &store.FindUser{
		Role: &adminUserType,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find admin")
	}
	if user == nil {
		return nil, nil
	}

	currentUser, _ := s.fetchCurrentUser(ctx)
	return convertUserFromStore(user, currentUser), nil
}
