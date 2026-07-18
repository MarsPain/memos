# Implement Generation And Embedding Adapters

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: ready-for-agent
Blocked by: 03, 04

## Outcome

Supported OpenAI-compatible and Gemini providers implement the provider-neutral generation and embedding interfaces through the hardened transport.

## Scope

- Implement non-streaming and streaming generation where supported.
- Implement embeddings and validate response dimensions and vector shape.
- Detect and report text, streaming, structured-tool, and embedding readiness independently.
- Map provider failures into the normalized error contract.
- Respect injected limits, cancellation, bounded retries, and response-size enforcement.
- Keep credentials and request content out of logs and errors.

## Acceptance

- Adapters satisfy the same provider-neutral interface and fixtures.
- Unsupported model capabilities fail explicitly rather than being inferred from provider type.
- Streaming and embedding wire formats do not leak to callers.
- No required test calls a live provider.

## Verification

Run deterministic adapter fixtures for success, streaming, embeddings, authentication failure, rate limiting, timeout, cancellation, malformed data,
dimension mismatch, and oversized responses. Run `go test -v -race ./internal/...`.

## Comments

No comments yet.
