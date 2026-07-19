// Package openai implements OpenAI-compatible generation and embedding APIs.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/ai"
)

// Adapter implements ai.Model using an OpenAI-compatible endpoint.
type Adapter struct {
	config ai.ProviderConfig
	client *http.Client
}

// New creates an OpenAI-compatible model adapter.
func New(config ai.ProviderConfig, client *http.Client) *Adapter {
	return &Adapter{config: config, client: client}
}

type message struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type tool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatRequest struct {
	Model      string    `json:"model"`
	Messages   []message `json:"messages"`
	Tools      []tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Stream     bool      `json:"stream"`
}

type toolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Generate performs a non-streaming generation request.
func (adapter *Adapter) Generate(ctx context.Context, request ai.GenerationRequest) (ai.GenerationResponse, error) {
	var response chatResponse
	if err := adapter.postJSON(ctx, "chat/completions", buildChatRequest(request, false), &response); err != nil {
		return ai.GenerationResponse{}, err
	}
	if len(response.Choices) == 0 {
		return ai.GenerationResponse{}, ai.NewProviderError(ai.ErrorMalformed, "AI provider returned a malformed generation response", nil)
	}
	choice := response.Choices[0]
	return ai.GenerationResponse{
		Content:      choice.Message.Content,
		ToolCalls:    convertToolCalls(choice.Message.ToolCalls),
		Usage:        convertUsage(response.Usage),
		FinishReason: normalizeFinishReason(choice.FinishReason),
	}, nil
}

// Stream performs a streaming generation request without exposing SSE events.
func (adapter *Adapter) Stream(ctx context.Context, request ai.GenerationRequest) (<-chan ai.StreamEvent, error) {
	body, err := json.Marshal(buildChatRequest(request, true))
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode AI provider request")
	}
	endpoint, err := ai.EndpointWithPath(adapter.config.Endpoint, "chat/completions")
	if err != nil {
		return nil, ai.NewProviderError(ai.ErrorConfiguration, "AI provider endpoint is invalid", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ai.NewProviderError(ai.ErrorConfiguration, "AI provider request is invalid", err)
	}
	adapter.setHeaders(httpRequest)
	response, err := adapter.client.Do(httpRequest)
	if err != nil {
		return nil, ai.NormalizeHTTPError(err, 0)
	}
	if normalized := ai.NormalizeHTTPError(nil, response.StatusCode); normalized != nil {
		_ = response.Body.Close()
		return nil, normalized
	}
	stream := make(chan ai.StreamEvent)
	go readStream(ctx, response.Body, stream)
	return stream, nil
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage `json:"usage"`
}

func readStream(ctx context.Context, body io.ReadCloser, stream chan<- ai.StreamEvent) {
	defer close(stream)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			stream <- ai.StreamEvent{Err: ctx.Err()}
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return
		}
		var payload streamResponse
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			stream <- ai.StreamEvent{Err: ai.NewProviderError(ai.ErrorMalformed, "AI provider returned malformed streaming data", err)}
			return
		}
		event := ai.StreamEvent{}
		if len(payload.Choices) > 0 {
			event.Delta = payload.Choices[0].Delta.Content
			event.ToolCalls = convertToolCalls(payload.Choices[0].Delta.ToolCalls)
			event.FinishReason = normalizeFinishReason(payload.Choices[0].FinishReason)
		}
		if payload.Usage != nil {
			usage := convertUsage(*payload.Usage)
			event.Usage = &usage
		}
		stream <- event
	}
	if err := scanner.Err(); err != nil {
		stream <- ai.StreamEvent{Err: ai.NewProviderError(ai.ErrorMalformed, "AI provider streaming response could not be read", err)}
	}
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage usage `json:"usage"`
}

// Embed creates vectors and validates their count and shape.
func (adapter *Adapter) Embed(ctx context.Context, request ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	var wireResponse embeddingResponse
	if err := adapter.postJSON(ctx, "embeddings", embeddingRequest{Model: request.Model, Input: request.Inputs, Dimensions: request.Dimensions}, &wireResponse); err != nil {
		return ai.EmbeddingResponse{}, err
	}
	vectors := make([][]float32, len(wireResponse.Data))
	for _, item := range wireResponse.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			return ai.EmbeddingResponse{}, ai.NewProviderError(ai.ErrorMalformed, "AI provider returned a malformed embedding response", nil)
		}
		vectors[item.Index] = item.Embedding
	}
	dimensions := 0
	if len(vectors) > 0 {
		dimensions = len(vectors[0])
	}
	response := ai.EmbeddingResponse{Vectors: vectors, Dimensions: dimensions, Usage: convertUsage(wireResponse.Usage)}
	if err := ai.ValidateEmbeddingResponse(response, len(request.Inputs), request.Dimensions); err != nil {
		return ai.EmbeddingResponse{}, ai.NewProviderError(ai.ErrorMalformed, "AI provider returned invalid embedding dimensions", err)
	}
	return response, nil
}

// Probe performs a minimal bounded request for one capability.
func (adapter *Adapter) Probe(ctx context.Context, capability ai.Capability, model string, dimensions int) error {
	switch capability {
	case ai.CapabilityTextGeneration:
		_, err := adapter.Generate(ctx, ai.GenerationRequest{Model: model, Messages: []ai.Message{{Role: ai.RoleUser, Content: "ping"}}, MaxTokens: 1})
		return err
	case ai.CapabilityStreaming:
		stream, err := adapter.Stream(ctx, ai.GenerationRequest{Model: model, Messages: []ai.Message{{Role: ai.RoleUser, Content: "ping"}}, MaxTokens: 1})
		if err != nil {
			return err
		}
		for event := range stream {
			if event.Err != nil {
				return event.Err
			}
		}
		return nil
	case ai.CapabilityStructuredTools:
		response, err := adapter.Generate(ctx, ai.GenerationRequest{Model: model, Messages: []ai.Message{{Role: ai.RoleUser, Content: "Call ping."}}, Tools: []ai.Tool{{Name: "ping", Parameters: json.RawMessage(`{"type":"object"}`)}}, RequireTool: true, MaxTokens: 8})
		if err != nil {
			return err
		}
		if len(response.ToolCalls) == 0 {
			return errors.Wrap(ai.ErrCapabilityUnsupported, "model did not return a structured tool request")
		}
		return nil
	case ai.CapabilityEmbeddings:
		_, err := adapter.Embed(ctx, ai.EmbeddingRequest{Model: model, Inputs: []string{"ping"}, Dimensions: dimensions})
		return err
	default:
		return errors.Wrap(ai.ErrCapabilityUnsupported, "unknown capability")
	}
}

func (adapter *Adapter) postJSON(ctx context.Context, path string, payload, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to encode AI provider request")
	}
	endpoint, err := ai.EndpointWithPath(adapter.config.Endpoint, path)
	if err != nil {
		return ai.NewProviderError(ai.ErrorConfiguration, "AI provider endpoint is invalid", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ai.NewProviderError(ai.ErrorConfiguration, "AI provider request is invalid", err)
	}
	adapter.setHeaders(httpRequest)
	response, err := adapter.client.Do(httpRequest)
	if err != nil {
		return ai.NormalizeHTTPError(err, 0)
	}
	defer response.Body.Close()
	if normalized := ai.NormalizeHTTPError(nil, response.StatusCode); normalized != nil {
		return normalized
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return ai.NewProviderError(ai.ErrorMalformed, "AI provider returned a malformed response", err)
	}
	return nil
}

func (adapter *Adapter) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+adapter.config.APIKey)
}

func buildChatRequest(request ai.GenerationRequest, stream bool) chatRequest {
	messages := make([]message, 0, len(request.Messages))
	for _, item := range request.Messages {
		messages = append(messages, message{Role: string(item.Role), Content: item.Content, ToolCallID: item.ToolCallID})
	}
	tools := make([]tool, 0, len(request.Tools))
	for _, item := range request.Tools {
		parameters := item.Parameters
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object"}`)
		}
		tools = append(tools, tool{Type: "function", Function: toolFunction{Name: item.Name, Description: item.Description, Parameters: parameters}})
	}
	toolChoice := ""
	if request.RequireTool {
		toolChoice = "required"
	}
	return chatRequest{Model: request.Model, Messages: messages, Tools: tools, ToolChoice: toolChoice, MaxTokens: request.MaxTokens, Stream: stream}
}

func convertToolCalls(calls []toolCall) []ai.ToolCall {
	result := make([]ai.ToolCall, 0, len(calls))
	for _, call := range calls {
		result = append(result, ai.ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return result
}

func convertUsage(value usage) ai.Usage {
	return ai.Usage{InputTokens: value.PromptTokens, OutputTokens: value.CompletionTokens, TotalTokens: value.TotalTokens}
}

func normalizeFinishReason(reason string) ai.FinishReason {
	switch reason {
	case "stop":
		return ai.FinishStop
	case "length":
		return ai.FinishLength
	case "tool_calls":
		return ai.FinishToolCalls
	case "":
		return ""
	default:
		return ai.FinishOther
	}
}
