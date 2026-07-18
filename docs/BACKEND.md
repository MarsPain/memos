# Backend Context

## Current Topology

`server/server.go` composes the Echo server, API v1 services, frontend and file routes, RSS, the external MCP endpoint, and background runners. `store.Store` is the persistence facade over SQLite, MySQL, and PostgreSQL drivers.

Public contracts originate in `proto/api/v1/`. Store-only protobuf messages originate in `proto/store/`. Change protobuf sources first and regenerate outputs with `cd proto && buf generate`.

## AI Module Placement

The approved design introduces three distinct layers:

- `server/memo`: shared Memo application behavior used by existing Memo RPC handlers and AI orchestration.
- `server/ai`: authenticated application orchestration for conversations, retrieval, Agent proposals, and indexing.
- `internal/ai`: provider-neutral model interfaces and concrete OpenAI/Gemini-compatible adapters for generation, embeddings, and transcription.

API handlers depend on application-module interfaces. Each operation accepts an explicit authenticated principal and `context.Context`; modules do not
recover identity from global state. `server/ai` does not call API handlers in-process or use raw Store reads as an authorization substitute.

## Memo Capability Seam

Existing Memo authorization, content validation, Markdown payload construction, and post-write side effects currently live in API v1 handlers. They
move incrementally behind the deep `server/memo` module: read/authorization behavior in Stage 2 and mutation behavior in Stage 4. Both Memo RPC handlers
and `server/ai` use the same interface and behavioral tests.

The in-product Agent receives only these capabilities:

- read an authorized Memo;
- search authorized Memos;
- prepare a create proposal;
- prepare an update proposal.

The model-facing seam does not include Apply, deletion, or direct unconstrained writes. Apply is a separate user-confirmation operation on `server/ai`
that delegates to a transactional Memo command. The shared module preserves content limits, authorization, Markdown payload extraction,
notifications, webhooks, and SSE while Store remains raw persistence.

Confirmed proposal application performs the Memo revision compare-and-swap and proposal state transition in one database transaction. Database driver
adapters implement equivalent transaction semantics for SQLite, MySQL, and PostgreSQL. Side effects run only after commit and cannot cause the mutation
to be repeated.

## Background Work

Indexing is recoverable reconciliation, not a durable workflow engine. Memo changes signal index invalidation. A background runner uses keyset
pagination, bounded batches, per-item backoff, and idempotent upserts to compare source revisions/hashes with index metadata. Restarting the server is
sufficient to resume work.

Provider-independent search documents keep lexical/fuzzy retrieval available without embeddings. Embedding changes build a non-queryable generation;
projection changes first rebuild search documents and then any configured embedding generation. A complete verification pass promotes an embedding
generation atomically, while the prior active generation remains queryable when its model is callable. The baseline runs one Memos process. Multiple
concurrently writing processes require a future database-lease design rather than relying on process-local locks.

## External MCP

`server/router/mcp/` remains the external, OpenAPI-derived tool surface. The in-product AI module must not call MCP over HTTP, register MCP tools, change its allowlist, or reuse provider credentials as MCP authentication.

## Verification Routing

- AI model adapters: focused tests under `internal/ai/...`.
- AI application behavior: focused tests under `server/ai/...`.
- Shared Memo behavior: focused interface tests under `server/memo/...`, reused by Memo and AI callers.
- API behavior: tests under `server/router/api/v1/...`.
- Store and migrations: tests under `store/...` for all drivers.
- MCP regression: existing `server/router/mcp/...` tests must remain unchanged and pass.
