# Plan 0001: AI Foundation

**Status:** Ready
**Depends on:** [AI-native architecture](../../design-docs/ai-native-notes.md)
**Scope:** Roadmap Stage 1 only

## Outcome

Memos can configure and invoke provider-neutral text-generation and embedding capabilities without changing current transcription behavior or the external MCP module. No Chat, search index, or Agent UI is delivered by this plan.

## In Scope

- Add generation and embedding capability assignments to store and API settings.
- Preserve the provider credential pool and existing transcription assignment.
- Add validation, masked API responses, deployment configuration support, and settings UI.
- Add provider-neutral generation and embedding interfaces under `internal/ai`.
- Implement OpenAI-compatible and Gemini adapters where supported by the configured provider type.
- Add bounded connectivity tests from the admin settings flow.
- Add explicit external-processing disclosure and user-visible capability status.
- Add a shared hardened provider transport with cancellation, endpoint/private-network policy, timeouts, response limits, and normalized errors.
- Update canonical docs in the same implementation changes.

## Out of Scope

- Conversations and messages.
- Retrieval or index schema.
- Agent tools and proposals.
- Changes under `server/router/mcp/`.
- User-level BYOK.

## Work Breakdown

### 1. Contract design

- Design additive store and API protobuf messages for generation and embedding assignments.
- Define model/dimension validation and secret-preservation behavior.
- Define separately reported readiness for text generation, streaming, structured tools, and embeddings; provider type alone is not capability proof.
- Define the connectivity-test request and sanitized response.
- Define the administrator disclosure-confirmation flow and persisted private-network endpoint opt-in without exposing endpoint secrets to ordinary
  users.
- Confirm compatibility behavior for instances with only transcription configured.
- Define migration semantics that preserve existing custom transcription endpoints while visibly recording their prior private-network authorization;
  require explicit opt-in for new, edited, or deployment-supplied private endpoints.

Verification: proto design review, `buf lint`, `buf format`, generated diff inspection.

### 2. Settings persistence

- Round-trip both assignments through store, deployment configuration, API converters, and caches.
- Preserve existing API keys when update requests omit them.
- Reject missing provider references, unsupported provider types, invalid model lengths/dimensions, unsafe endpoint syntax, and private-network use without
  explicit opt-in.
- Extend store and API tests.

Verification: targeted store and API tests; all three database behaviors remain unchanged because settings use the existing setting storage path.

Include upgrade fixtures for an existing custom transcription endpoint and fresh-config fixtures proving private-network access defaults to denied.

### 3. Model interfaces

- Introduce small generation and embedding interfaces with explicit request/response types.
- Support streaming generation without leaking provider event formats.
- Keep structured tool requests as a separately detectable capability so Chat can work while Agent remains unavailable.
- Carry usage, finish reason, and normalized error category where available.
- Inject HTTP clients and limits.

Verification: interface-level fake tests and adapter HTTP fixture tests.

### 4. Provider adapters

- Implement capability adapters for supported OpenAI-compatible and Gemini transports.
- Validate embedding dimensions from provider responses.
- Bound retries, body sizes, connection time, and total request time.
- Revalidate redirects and resolved destinations through one HTTP/HTTPS transport policy shared by all AI capabilities and connectivity tests.
- Ensure logs redact credentials and request content.

Verification: deterministic server fixtures for success, streaming, rate limit, timeout, malformed response, cancellation, oversized response,
redirects, DNS resolution changes, blocked network ranges, and explicit local-endpoint opt-in.

### 5. Admin settings UX

- Add Generation and Embedding sections to the existing AI settings page.
- Make capability dependencies and disabled states clear.
- Add a bounded test action and actionable sanitized errors.
- Explain before save that external embedding incrementally processes every eligible Memo chunk; show all users whether external AI processing is enabled.
- Keep transcription behavior and editor controls unchanged.

Verification: frontend lint, unit tests for extracted state/validation logic, and manual settings flow.

### 6. Integration and documentation

- Wire model capability resolution into the application composition without adding Chat endpoints.
- Update `ARCHITECTURE.md`, `docs/BACKEND.md`, `docs/SECURITY.md`, and `docs/DATA.md` if implementation details change.
- Run documentation validation and the full checks for proto, backend, and frontend surfaces.

## Required Verification

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
cd proto && buf generate && buf lint
go test -v -race ./internal/...
go test -v -race ./server/...
cd web && pnpm lint && pnpm test && pnpm build
```

Also run `git diff --check` and confirm there is no diff under `server/router/mcp/`.

## Exit Criteria

- Generation and embedding assignments are configurable and validated.
- Capability readiness distinguishes text, streaming, structured tools, and embeddings.
- Provider keys remain write-only and preserved on partial updates.
- Model calls and test actions share a provider-neutral, cancellable, bounded, SSRF-aware transport and are testable without live network access.
- External embedding disclosure is required before assignment, and private-network opt-in is explicit and persisted without exposing secrets.
- Existing transcription tests pass unchanged except where additive settings fixtures require updates.
- External MCP files and behavior are unchanged.
- Stage 2 can depend on stable generation/embedding interfaces without knowing provider transports.

## Rollback

The settings schema is additive. Rolling back application code ignores the new fields while preserving existing providers and transcription. No AI source records or index migrations exist in this stage.
