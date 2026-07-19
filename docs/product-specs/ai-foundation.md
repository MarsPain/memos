# AI Foundation Product Specification

**Status:** Implemented
**Parent product spec:** [AI-Native Notes](ai-native-notes.md)
**Architecture:** [AI-Native Memos Architecture](../design-docs/ai-native-notes.md)
**Roadmap scope:** [Stage 1 — Model Foundation](../ROADMAP.md#stage-1-model-foundation)

## Objective

Memos can configure and invoke provider-neutral text-generation and embedding capabilities without changing current transcription behavior or the
external MCP module. This stage establishes model capability foundations only; it does not deliver Chat, retrieval, or Agent workflows.

## User And Operator Outcomes

- Administrators can assign generation and embedding capabilities independently from transcription.
- Administrators can test a configured capability through a bounded, sanitized connectivity action.
- Administrators explicitly acknowledge external processing and private-network endpoint access before enabling those behaviors.
- All users can see whether external AI processing is enabled without receiving provider credentials.
- Later stages can depend on stable generation and embedding interfaces without knowing provider transports.

## In Scope

- Add generation and embedding capability assignments to store and public API settings.
- Preserve the existing provider credential pool and transcription assignment.
- Add validation, masked API responses, deployment-configuration support, capability readiness, and settings UI.
- Add provider-neutral generation and embedding interfaces under `internal/ai`.
- Implement supported OpenAI-compatible and Gemini generation and embedding adapters.
- Use one hardened provider transport for AI calls and connectivity tests.
- Preserve existing transcription behavior and the external MCP module.

## Out Of Scope

- Conversations, messages, Chat endpoints, and Chat UI.
- Search documents, embedding-index records, retrieval, and citations.
- Agent tools, proposals, and confirmation flows.
- Changes under `server/router/mcp/`.
- Per-user provider credentials or opt-in.

## Capability Contracts

### Configuration

- The instance AI setting retains a provider pool and adds independent `generation` and `embedding` assignments alongside `transcription`.
- Assignments reference a configured provider and identify the model; embedding may additionally constrain dimensions.
- Updating settings without a replacement API key preserves the stored credential.
- API reads never return provider keys and expose only masked configured state.
- Deployment-supplied AI settings follow the existing file-backed configuration lifecycle and validation rules.

### Readiness

- Readiness is reported separately for text generation, streaming, structured tool requests, and embeddings.
- Provider type alone never proves that an arbitrary model supports a capability.
- Chat may eventually depend on text generation without structured tools; Agent availability will require validated structured-tool support.

### Provider Transport

- All model calls and connectivity tests accept cancellation and enforce bounded connection, request, response, redirect, and total-runtime limits.
- Endpoints allow HTTP or HTTPS only and reject userinfo, fragments, malformed hosts, and secret-bearing endpoint parameters.
- Redirects and resolved destinations are revalidated through the same policy.
- Loopback, link-local, and private destinations are denied by default and require an explicit persisted administrator opt-in.
- Errors are normalized into safe categories; logs omit credentials, request content, and provider payloads.

### Administration UX

- The AI settings page exposes Generation and Embedding sections with capability dependencies and disabled states.
- Connectivity tests return actionable sanitized results and do not become unbounded provider calls.
- Before external embeddings are assigned, the UI explains that every eligible Memo chunk will be processed incrementally by the provider.
- All users can see whether external AI processing is enabled.
- Existing transcription controls and editor behavior remain unchanged.

## Accepted Implementation Decisions

- Store and API protobuf changes are additive and preserve compatibility.
- Generation and embedding assignments use the existing instance-setting persistence path; this stage adds no AI source-record or index schema.
- Provider-neutral interfaces expose explicit request and response types, streaming without provider event leakage, usage where available, finish reason,
  normalized errors, and injected HTTP clients and limits.
- OpenAI-compatible and Gemini transports implement those interfaces without leaking provider SDK types to callers.
- Connectivity tests and runtime calls share the hardened transport instead of maintaining separate endpoint policies.

## Testing Decisions

- Required tests use deterministic fakes and local HTTP fixtures rather than live providers.
- Contract tests cover additive protobuf shape, capability readiness, validation, secret preservation, and sanitized responses.
- Persistence tests cover stored settings, deployment configuration, converters, caches, and existing custom-endpoint upgrades.
- Transport and adapter fixtures cover success, streaming, embeddings, rate limits, timeouts, cancellation, malformed and oversized responses, redirects,
  DNS destination changes, blocked network ranges, and explicit private-endpoint opt-in.
- Frontend tests cover settings state, validation, disclosure, disabled capability states, connectivity results, and unchanged transcription controls.

## Migration And Rollback

- The new assignments are additive. Existing installations with only transcription configured continue to behave as before.
- A previously persisted custom transcription endpoint counts as a prior administrator decision and is migrated with private-network access preserved
  and visibly disclosed.
- New, edited, or deployment-supplied private endpoints require explicit opt-in.
- Rolling back application code ignores the additive fields while preserving providers and transcription configuration.
- This stage introduces no conversation, proposal, search-index, or embedding-record migrations requiring data rollback.

## Acceptance Criteria

- Generation and embedding assignments round-trip through storage, deployment configuration, API conversion, and UI without exposing credentials.
- Capability readiness distinguishes text, streaming, structured tools, and embeddings.
- Model calls and connectivity tests share a cancellable, bounded, SSRF-aware transport.
- Invalid provider references, provider/model combinations, dimensions, endpoints, and unauthorized private-network access fail closed.
- External embedding disclosure and private-network opt-in are explicit and persisted without exposing secrets.
- Existing transcription tests pass except for intentional additive fixture updates.
- No Chat, retrieval, Agent, or external MCP behavior is added or changed.
- Stage 2 can consume provider-neutral generation and embedding interfaces.

## System Verification

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
cd proto && buf generate && buf lint
go test -v -race ./internal/...
go test -v -race ./server/...
cd web && pnpm lint && pnpm test && pnpm build
git diff --check
```

Before acceptance, confirm there is no implementation diff under `server/router/mcp/`.
