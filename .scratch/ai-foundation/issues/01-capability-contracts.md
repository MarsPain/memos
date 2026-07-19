# Define AI Capability Contracts

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: resolved
Blocked by: none

## Outcome

Additive store and public API contracts represent generation and embedding assignments, capability readiness, connectivity tests, disclosure state,
and private-network authorization without breaking existing transcription configuration.

## Scope

- Define additive store and API protobuf messages for generation and embedding assignments.
- Define model and dimension validation plus provider-reference behavior.
- Report readiness separately for text generation, streaming, structured tools, and embeddings.
- Define bounded connectivity-test requests and sanitized responses.
- Represent external-processing disclosure and persisted private-network opt-in without exposing secrets.
- Define compatibility for transcription-only installations and migration semantics for existing custom endpoints.
- Regenerate Go, TypeScript, and OpenAPI outputs from `.proto` sources.

## Acceptance

- Existing fields and wire behavior remain compatible.
- API responses never contain provider keys.
- The contract distinguishes configured assignments from validated capability readiness.
- Existing private endpoints can be migrated visibly; new private endpoints require explicit authorization.
- Generated outputs are updated only through `buf generate`.

## Verification

```bash
cd proto && buf generate && buf lint
cd proto && buf format --diff --exit-code
git diff --check
```

Inspect generated diffs for compatibility and secret exposure.

## Comments

Implemented additive store/API protobuf contracts, independent readiness, bounded connectivity-test messages, disclosure/private-network state, and regenerated Go, TypeScript, OpenAPI outputs.
