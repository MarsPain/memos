package ai

import (
	"context"
	"encoding/json"
	"math"

	"github.com/pkg/errors"
)

// Capability identifies independently testable model behavior.
type Capability string

const (
	CapabilityTextGeneration  Capability = "text_generation"
	CapabilityStreaming       Capability = "streaming"
	CapabilityStructuredTools Capability = "structured_tools"
	CapabilityEmbeddings      Capability = "embeddings"
)

// Role identifies a provider-neutral message role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one provider-neutral generation message.
type Message struct {
	Role       Role
	Content    string
	ToolCallID string
}

// Tool describes a structured tool available to a generation request.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is a validated provider-neutral tool request.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// GenerationRequest is a bounded provider-neutral generation request.
type GenerationRequest struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	RequireTool bool
	MaxTokens   int
}

// Usage reports provider token counts where available.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// FinishReason is a normalized generation completion reason.
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishLength    FinishReason = "length"
	FinishToolCalls FinishReason = "tool_calls"
	FinishOther     FinishReason = "other"
)

// GenerationResponse is a provider-neutral generation result.
type GenerationResponse struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason FinishReason
}

// StreamEvent is one provider-neutral stream update.
type StreamEvent struct {
	Delta        string
	ToolCalls    []ToolCall
	Usage        *Usage
	FinishReason FinishReason
	Err          error
}

// EmbeddingRequest requests vectors for one or more inputs.
type EmbeddingRequest struct {
	Model      string
	Inputs     []string
	Dimensions int
}

// EmbeddingResponse contains validated vectors in input order.
type EmbeddingResponse struct {
	Vectors    [][]float32
	Dimensions int
	Usage      Usage
}

// Capabilities reports independently validated adapter support.
type Capabilities struct {
	TextGeneration  bool
	Streaming       bool
	StructuredTools bool
	Embeddings      bool
}

// Model is the provider-neutral interface consumed by application code.
type Model interface {
	Generate(context.Context, GenerationRequest) (GenerationResponse, error)
	Stream(context.Context, GenerationRequest) (<-chan StreamEvent, error)
	Embed(context.Context, EmbeddingRequest) (EmbeddingResponse, error)
	Probe(context.Context, Capability, string, int) error
}

// ValidateEmbeddingResponse validates vector count, shape, dimensions, and finite values.
func ValidateEmbeddingResponse(response EmbeddingResponse, inputCount, requestedDimensions int) error {
	if len(response.Vectors) != inputCount {
		return errors.Wrapf(ErrCapabilityUnsupported, "embedding response returned %d vectors for %d inputs", len(response.Vectors), inputCount)
	}
	dimensions := response.Dimensions
	if dimensions <= 0 && len(response.Vectors) > 0 {
		dimensions = len(response.Vectors[0])
	}
	if requestedDimensions > 0 && dimensions != requestedDimensions {
		return errors.Wrapf(ErrCapabilityUnsupported, "embedding dimensions %d do not match requested dimensions %d", dimensions, requestedDimensions)
	}
	for _, vector := range response.Vectors {
		if len(vector) != dimensions {
			return errors.Wrap(ErrCapabilityUnsupported, "embedding vectors have inconsistent dimensions")
		}
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return errors.Wrap(ErrCapabilityUnsupported, "embedding vector contains a non-finite value")
			}
		}
	}
	response.Dimensions = dimensions
	return nil
}
