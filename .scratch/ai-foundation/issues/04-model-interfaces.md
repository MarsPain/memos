# Define Provider-Neutral Model Interfaces

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

Callers can use generation, streaming, structured-tool readiness, and embeddings through small provider-neutral interfaces under `internal/ai`.

## Scope

- Introduce explicit generation and embedding request/response types.
- Support streaming without exposing provider event formats.
- Represent structured tool requests as a separately detectable capability.
- Carry usage, finish reason, dimensions, and normalized error categories where available.
- Accept injected HTTP clients, cancellation, and limits.
- Provide deterministic fakes used by application and adapter tests.

## Acceptance

- Interface consumers do not import provider SDK or wire types.
- Text generation can be ready while structured tools remain unavailable.
- Embedding responses validate dimensions before callers consume vectors.
- Tests and production callers use the same interface.

## Verification

Run interface-level fake tests and `go test -v -race ./internal/...`.

## Comments

Implemented provider-neutral generation, streaming, structured-tool, embedding, usage, finish-reason and validation contracts plus deterministic fakes and gateway resolution.
