// Package gemini implements Gemini generation and embedding APIs.
package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/usememos/memos/internal/ai"
)

// Adapter implements ai.Model using the Gemini REST API.
type Adapter struct {
	config ai.ProviderConfig
	client *http.Client
}

// New creates a Gemini model adapter.
func New(config ai.ProviderConfig, client *http.Client) *Adapter {
	return &Adapter{config: config, client: client}
}

type part struct {
	Text         string        `json:"text,omitempty"`
	FunctionCall *functionCall `json:"functionCall,omitempty"`
}

type functionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type generationRequest struct {
	Contents          []content `json:"contents"`
	SystemInstruction *content  `json:"systemInstruction,omitempty"`
	Tools             []struct {
		FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
	ToolConfig *struct {
		FunctionCallingConfig struct {
			Mode string `json:"mode"`
		} `json:"functionCallingConfig"`
	} `json:"toolConfig,omitempty"`
	GenerationConfig *struct {
		MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig,omitempty"`
}

type generationResponse struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Generate performs a non-streaming Gemini request.
func (adapter *Adapter) Generate(ctx context.Context, request ai.GenerationRequest) (ai.GenerationResponse, error) {
	var response generationResponse
	if err := adapter.postJSON(ctx, modelAction(request.Model, "generateContent"), buildGenerationRequest(request), &response); err != nil {
		return ai.GenerationResponse{}, err
	}
	return convertGenerationResponse(response)
}

// Stream performs a Gemini SSE request without exposing provider events.
func (adapter *Adapter) Stream(ctx context.Context, request ai.GenerationRequest) (<-chan ai.StreamEvent, error) {
	body, err := json.Marshal(buildGenerationRequest(request))
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode AI provider request")
	}
	endpoint, err := adapter.endpoint(modelAction(request.Model, "streamGenerateContent"))
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, ai.NewProviderError(ai.ErrorConfiguration, "AI provider endpoint is invalid", err)
	}
	query := parsed.Query()
	query.Set("alt", "sse")
	parsed.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(body))
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

func readStream(ctx context.Context, body io.ReadCloser, stream chan<- ai.StreamEvent) {
	defer close(stream)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			sendStreamEvent(ctx, stream, ai.StreamEvent{Err: ctx.Err()})
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var payload generationResponse
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload); err != nil {
			sendStreamEvent(ctx, stream, ai.StreamEvent{Err: ai.NewProviderError(ai.ErrorMalformed, "AI provider returned malformed streaming data", err)})
			return
		}
		converted, err := convertGenerationResponse(payload)
		if err != nil {
			sendStreamEvent(ctx, stream, ai.StreamEvent{Err: err})
			return
		}
		usage := converted.Usage
		if !sendStreamEvent(ctx, stream, ai.StreamEvent{Delta: converted.Content, ToolCalls: converted.ToolCalls, FinishReason: converted.FinishReason, Usage: &usage}) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		sendStreamEvent(ctx, stream, ai.StreamEvent{Err: ai.NewProviderError(ai.ErrorMalformed, "AI provider streaming response could not be read", err)})
	}
}

func sendStreamEvent(ctx context.Context, stream chan<- ai.StreamEvent, event ai.StreamEvent) bool {
	select {
	case stream <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

type batchEmbeddingRequest struct {
	Requests []struct {
		Model                string  `json:"model"`
		Content              content `json:"content"`
		OutputDimensionality int     `json:"outputDimensionality,omitempty"`
	} `json:"requests"`
}

type batchEmbeddingResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// Embed creates Gemini vectors and validates their shape.
func (adapter *Adapter) Embed(ctx context.Context, request ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	payload := batchEmbeddingRequest{Requests: make([]struct {
		Model                string  `json:"model"`
		Content              content `json:"content"`
		OutputDimensionality int     `json:"outputDimensionality,omitempty"`
	}, 0, len(request.Inputs))}
	for _, input := range request.Inputs {
		payload.Requests = append(payload.Requests, struct {
			Model                string  `json:"model"`
			Content              content `json:"content"`
			OutputDimensionality int     `json:"outputDimensionality,omitempty"`
		}{Model: "models/" + strings.TrimPrefix(request.Model, "models/"), Content: content{Parts: []part{{Text: input}}}, OutputDimensionality: request.Dimensions})
	}
	var wireResponse batchEmbeddingResponse
	if err := adapter.postJSON(ctx, modelAction(request.Model, "batchEmbedContents"), payload, &wireResponse); err != nil {
		return ai.EmbeddingResponse{}, err
	}
	vectors := make([][]float32, 0, len(wireResponse.Embeddings))
	for _, embedding := range wireResponse.Embeddings {
		vectors = append(vectors, embedding.Values)
	}
	dimensions := 0
	if len(vectors) > 0 {
		dimensions = len(vectors[0])
	}
	response := ai.EmbeddingResponse{Vectors: vectors, Dimensions: dimensions}
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
	endpoint, err := adapter.endpoint(path)
	if err != nil {
		return err
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

func (adapter *Adapter) endpoint(path string) (string, error) {
	endpoint, err := ai.EndpointWithPath(adapter.config.Endpoint, path)
	if err != nil {
		return "", ai.NewProviderError(ai.ErrorConfiguration, "AI provider endpoint is invalid", err)
	}
	return endpoint, nil
}

func (adapter *Adapter) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", adapter.config.APIKey)
}

func modelAction(model, action string) string {
	return "models/" + strings.TrimPrefix(model, "models/") + ":" + action
}

func buildGenerationRequest(request ai.GenerationRequest) generationRequest {
	result := generationRequest{}
	for _, item := range request.Messages {
		if item.Role == ai.RoleSystem {
			result.SystemInstruction = &content{Parts: []part{{Text: item.Content}}}
			continue
		}
		role := "user"
		if item.Role == ai.RoleAssistant {
			role = "model"
		}
		result.Contents = append(result.Contents, content{Role: role, Parts: []part{{Text: item.Content}}})
	}
	if request.MaxTokens > 0 {
		result.GenerationConfig = &struct {
			MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
		}{MaxOutputTokens: request.MaxTokens}
	}
	if len(request.Tools) > 0 {
		declarations := make([]functionDeclaration, 0, len(request.Tools))
		for _, item := range request.Tools {
			parameters := item.Parameters
			if len(parameters) == 0 {
				parameters = json.RawMessage(`{"type":"object"}`)
			}
			declarations = append(declarations, functionDeclaration{Name: item.Name, Description: item.Description, Parameters: parameters})
		}
		result.Tools = append(result.Tools, struct {
			FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
		}{FunctionDeclarations: declarations})
		if request.RequireTool {
			result.ToolConfig = &struct {
				FunctionCallingConfig struct {
					Mode string `json:"mode"`
				} `json:"functionCallingConfig"`
			}{}
			result.ToolConfig.FunctionCallingConfig.Mode = "ANY"
		}
	}
	return result
}

func convertGenerationResponse(response generationResponse) (ai.GenerationResponse, error) {
	if len(response.Candidates) == 0 {
		return ai.GenerationResponse{}, ai.NewProviderError(ai.ErrorMalformed, "AI provider returned a malformed generation response", nil)
	}
	result := ai.GenerationResponse{FinishReason: normalizeFinishReason(response.Candidates[0].FinishReason)}
	for _, item := range response.Candidates[0].Content.Parts {
		result.Content += item.Text
		if item.FunctionCall != nil {
			result.ToolCalls = append(result.ToolCalls, ai.ToolCall{Name: item.FunctionCall.Name, Arguments: item.FunctionCall.Args})
		}
	}
	result.Usage = ai.Usage{
		InputTokens:  response.UsageMetadata.PromptTokenCount,
		OutputTokens: response.UsageMetadata.CandidatesTokenCount,
		TotalTokens:  response.UsageMetadata.TotalTokenCount,
	}
	return result, nil
}

func normalizeFinishReason(reason string) ai.FinishReason {
	switch reason {
	case "STOP":
		return ai.FinishStop
	case "MAX_TOKENS":
		return ai.FinishLength
	case "":
		return ""
	default:
		return ai.FinishOther
	}
}
