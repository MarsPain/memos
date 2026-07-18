# Integrate And Verify AI Foundation

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: ready-for-agent
Blocked by: 02, 05, 06

## Outcome

Capability resolution is wired into application composition, canonical docs reflect the implemented foundation, and the full Stage 1 verification gate
passes without adding Chat or changing external MCP behavior.

## Scope

- Wire provider-neutral capability resolution into application composition.
- Keep Chat, retrieval, conversations, proposals, and Agent endpoints absent.
- Update `ARCHITECTURE.md`, `docs/BACKEND.md`, `docs/SECURITY.md`, `docs/DATA.md`, and the AI design/spec when implementation details change.
- Run the spec's system verification across docs, proto, backend, internal packages, and frontend.
- Inspect the final diff for compatibility, generated artifacts, credential exposure, and scope expansion.

## Acceptance

- Generation and embedding capabilities resolve without provider-specific knowledge in callers.
- Transcription-only configurations and tests remain compatible.
- Canonical docs describe the implemented behavior without copying task state.
- No implementation diff exists under `server/router/mcp/`.
- Every acceptance criterion in the parent spec has supporting automated or documented manual verification.

## Verification

Run every command under the parent spec's **System Verification** section, then inspect `git diff -- server/router/mcp/` and `git diff --check`.

## Comments

No comments yet.
