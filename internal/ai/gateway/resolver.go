// Package gateway resolves provider-neutral model assignments to adapters.
package gateway

import (
	"net/http"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/ai"
	"github.com/usememos/memos/internal/ai/provider/gemini"
	"github.com/usememos/memos/internal/ai/provider/openai"
)

// Assignment identifies a configured provider and model.
type Assignment struct {
	ProviderID string
	Model      string
	Dimensions int
}

// ResolvedCapability combines a provider-neutral model with assignment metadata.
type ResolvedCapability struct {
	Model      ai.Model
	ModelName  string
	Dimensions int
}

// ModelFactory constructs one provider-neutral adapter.
type ModelFactory func(ai.ProviderConfig, *http.Client) (ai.Model, error)

// NewModel constructs the supported adapter without exposing it to callers.
func NewModel(config ai.ProviderConfig, client *http.Client) (ai.Model, error) {
	switch config.Type {
	case ai.ProviderOpenAI:
		return openai.New(config, client), nil
	case ai.ProviderGemini:
		return gemini.New(config, client), nil
	default:
		return nil, errors.Wrapf(ai.ErrCapabilityUnsupported, "provider type %q", config.Type)
	}
}

// Resolver resolves assignments against a provider pool.
type Resolver struct {
	providers []ai.ProviderConfig
	clientFor func(ai.ProviderConfig) *http.Client
	factory   ModelFactory
}

// NewResolver creates a provider-neutral capability resolver.
func NewResolver(providers []ai.ProviderConfig, clientFor func(ai.ProviderConfig) *http.Client, factory ModelFactory) *Resolver {
	if factory == nil {
		factory = NewModel
	}
	return &Resolver{providers: append([]ai.ProviderConfig(nil), providers...), clientFor: clientFor, factory: factory}
}

// Resolve returns the model assigned to one capability.
func (resolver *Resolver) Resolve(assignment Assignment) (*ResolvedCapability, error) {
	provider, err := ai.FindProvider(resolver.providers, assignment.ProviderID)
	if err != nil {
		return nil, err
	}
	if assignment.Model == "" {
		return nil, errors.Wrap(ai.ErrCapabilityUnsupported, "model is required")
	}
	client := resolver.clientFor(*provider)
	model, err := resolver.factory(*provider, client)
	if err != nil {
		return nil, err
	}
	return &ResolvedCapability{Model: model, ModelName: assignment.Model, Dimensions: assignment.Dimensions}, nil
}
