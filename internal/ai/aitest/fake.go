// Package aitest provides deterministic fakes for provider-neutral model consumers.
package aitest

import (
	"context"
	"sync"

	"github.com/usememos/memos/internal/ai"
)

// Model is a deterministic fake implementing ai.Model.
type Model struct {
	GenerateResponse  ai.GenerationResponse
	GenerateError     error
	StreamEvents      []ai.StreamEvent
	StreamError       error
	EmbeddingResponse ai.EmbeddingResponse
	EmbeddingError    error
	// EmbedFunc, when set, computes the embedding response per request,
	// allowing deterministic per-input vectors. Its response goes through
	// the same shape validation as EmbeddingResponse.
	EmbedFunc   func(ai.EmbeddingRequest) (ai.EmbeddingResponse, error)
	ProbeErrors map[ai.Capability]error

	mu                 sync.Mutex
	GenerationRequests []ai.GenerationRequest
	StreamRequests     []ai.GenerationRequest
	EmbeddingRequests  []ai.EmbeddingRequest
	ProbeRequests      []ai.Capability
}

// Generate records the request and returns the configured result.
func (model *Model) Generate(ctx context.Context, request ai.GenerationRequest) (ai.GenerationResponse, error) {
	if err := ctx.Err(); err != nil {
		return ai.GenerationResponse{}, err
	}
	model.mu.Lock()
	model.GenerationRequests = append(model.GenerationRequests, request)
	model.mu.Unlock()
	return model.GenerateResponse, model.GenerateError
}

// Stream records the request and publishes the configured events.
func (model *Model) Stream(ctx context.Context, request ai.GenerationRequest) (<-chan ai.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	model.mu.Lock()
	model.StreamRequests = append(model.StreamRequests, request)
	events := append([]ai.StreamEvent(nil), model.StreamEvents...)
	model.mu.Unlock()
	if model.StreamError != nil {
		return nil, model.StreamError
	}
	stream := make(chan ai.StreamEvent, len(events))
	for _, event := range events {
		stream <- event
	}
	close(stream)
	return stream, nil
}

// Embed records the request and returns the configured result after shape validation.
func (model *Model) Embed(ctx context.Context, request ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	if err := ctx.Err(); err != nil {
		return ai.EmbeddingResponse{}, err
	}
	model.mu.Lock()
	model.EmbeddingRequests = append(model.EmbeddingRequests, request)
	model.mu.Unlock()
	if model.EmbeddingError != nil {
		return ai.EmbeddingResponse{}, model.EmbeddingError
	}
	response := model.EmbeddingResponse
	if model.EmbedFunc != nil {
		computed, err := model.EmbedFunc(request)
		if err != nil {
			return ai.EmbeddingResponse{}, err
		}
		response = computed
	}
	if err := ai.ValidateEmbeddingResponse(response, len(request.Inputs), request.Dimensions); err != nil {
		return ai.EmbeddingResponse{}, err
	}
	return response, nil
}

// Probe records the requested capability and returns its configured error.
func (model *Model) Probe(ctx context.Context, capability ai.Capability, _ string, _ int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	model.mu.Lock()
	model.ProbeRequests = append(model.ProbeRequests, capability)
	model.mu.Unlock()
	return model.ProbeErrors[capability]
}
