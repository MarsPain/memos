# Application and AI Security

The root [SECURITY.md](../SECURITY.md) describes vulnerability reporting and deployment posture. This document is the engineering source of truth for runtime trust boundaries and AI data handling.

## Trust Boundaries

```text
User browser ──authenticated request──> Memos
Memos ──selected context + provider key──> AI provider
External Agent ──Memos bearer token──> /mcp
```

The in-product AI path and external MCP path are independent. AI provider keys authenticate Memos to a model provider; Memos access tokens authenticate a user or external Agent to Memos. They are never interchangeable.

## Authorization Invariants

- Every Chat, search, conversation, and proposal operation requires an authenticated user.
- Retrieval may use an index for candidate selection, but source Memo authorization is rechecked before content leaves Memos.
- A model cannot grant itself additional tools or permissions.
- Agent writes require an owned, pending proposal plus explicit confirmation.
- The model tool set can prepare but cannot apply a proposal; Apply is accepted only from the authenticated user-confirmation RPC.
- Applying a proposal repeats authorization and performs the Memo revision check and proposal transition in one database transaction.
- The first in-product Agent has no delete or unattended bulk-write capability.

## Prompt-Injection Handling

Memo content, attachment text, titles, tags, links, and model output are untrusted data. Text inside a Memo must never be interpreted as system policy or tool authorization.

The Agent runtime separates:

- immutable system and capability instructions;
- application-supplied tool schemas;
- user requests;
- retrieved note content clearly marked as quoted data.

Tool calls are schema validated. Unknown tools, unsupported fields, invalid resource names, or calls outside the active capability set fail closed. A statement in a Memo such as “ignore previous instructions” has no authority.

Agent execution has hard limits on tool rounds, total tool calls, tool-output bytes, model-output bytes, and elapsed time. Tool results remain untrusted
data when returned to the model. Confirmation state is never inferred from model text.

## Outbound Data

- Generation sends only the currently authorized chunks selected for a request.
- External embedding is different: after explicit administrator configuration and disclosure, background indexing incrementally sends every eligible
  corpus chunk to the configured provider. It never sends the database as one payload, but over time it processes the complete eligible corpus.
- The settings UI explains this distinction before saving an external embedding assignment, and all users can see whether external AI processing is
  enabled. Per-user opt-in is outside the first release.
- Record which Memos were used as citations without logging full prompts by default.
- Document provider-side retention as an operator responsibility.
- Local or OpenAI-compatible endpoints use the same authorization and minimization rules.

Immediately before a snippet or model context leaves Memos, Retrieval rereads the source through the shared Memo authorization module and verifies the
citation's source revision/hash. It discards or regenerates stale spans. Indexed visibility and stale normalized text are never treated as current
authorization or current content.

## Secrets and Endpoints

Provider credentials remain instance-managed, write-only at the API layer, and unavailable to ordinary users. Configuration responses expose only a masked hint and configured state.

Custom provider endpoints create a server-side request boundary. Transcription, generation, embedding, and connectivity tests use the shared hardened
transport under `internal/ai`. The shared policy:

- allow HTTP and HTTPS only; reject URL userinfo, fragments, malformed hosts, and secret-bearing endpoint parameters;
- validate every redirect and resolved destination, not only the configured string;
- deny loopback, link-local, and private-network destinations by default;
- require an explicit administrator opt-in for private-network destinations used by local models;
- enforce DNS/dial, connection, total-request, redirect-count, retry-count, request-size, and response-size limits;
- do not place provider keys in URLs, logs, errors, fingerprints, or metrics.

To preserve existing transcription installations, a custom endpoint persisted before this policy was introduced is detected through an internal
migration marker, persisted with private-network access enabled, and shown through the normal provider opt-in control. New or edited providers and
deployment-supplied private endpoints must opt in explicitly. Connectivity responses expose only stable categories (configuration, authentication,
rate limit, timeout, unavailable, malformed response, or internal) and fixed safe messages.

## Logging and Audit

Record request class, user, model identifier, latency, token usage, citation references, proposal lifecycle, and normalized error category. Do not log API keys, authorization headers, endpoint secrets, full private Memo content, raw queries or prompts, provider payloads, embeddings, or hidden reasoning. Metrics use bounded categories rather than Memo, conversation, or proposal IDs as labels.

## Abuse and Cost Controls

The application module owns per-user and instance-level concurrency, rate, token, tool-round, scan-byte, candidate, elapsed-time, and response-size
limits. Indexing has separate batch, provider-concurrency, retry, and daily-work budgets so enabling embeddings cannot create unbounded provider cost.
Provider retries are bounded and respect cancellation. A failed or timed-out model call never causes a write, and proposal Apply never invokes a model.
