# AI Chat And Retrieval Product Specification

**Status:** Approved
**Parent product spec:** [AI-Native Notes](ai-native-notes.md)
**Architecture:** [AI-Native Memos Architecture](../design-docs/ai-native-notes.md)
**Roadmap scope:** [Stage 2 — Read-Only Chat and Fuzzy Retrieval](../ROADMAP.md#stage-2-read-only-chat-and-fuzzy-retrieval)

## Objective

Memos gains a private, persistent Chat workspace grounded in authorized Memos, and a unified keyword/partial/fuzzy search with
traceable citations. This stage is read-only with respect to Memos: no Chat or retrieval path can create, update, or delete a Memo.
Semantic (embedding) retrieval, proposals, and the Agent remain out of scope.

## User And Operator Outcomes

- Users can hold private conversations that persist until they delete them; Chat history is never indexed as Memos and never appears
  in the Memo timeline.
- Users can search their authorized Memos by exact token, substring, title, tags, and typo-tolerant similarity, and receive snippets
  with citations that identify the source Memo.
- Chat answers about the note collection cite the supporting Memos, and every citation is reauthorized and revision-checked
  immediately before its text leaves Memos.
- Search and Chat work with no embedding configuration and degrade honestly when text generation is unavailable.
- Memo capture is never blocked by provider failure, retrieval failure, or index lag.

## In Scope

- A shared Memo read/authorization module (`server/memo` read path) consumed by both the existing Memo RPC handlers and AI code.
- Provider-independent derived search documents with corpus-projection and normalization versioning (`ai_search_document`).
- Bounded lexical/partial/fuzzy retrieval with rank reasons, citations, and honest partial/degraded reporting.
- Private conversation and message persistence (`ai_conversation`, `ai_message`) with client request IDs and assistant attempts.
- Streaming Chat over a Connect server-streaming RPC on `AIService`, with bounded context construction from retrieved citations.
- Web UI for Chat and unified search, including streaming, degraded states, and citation display.
- Cross-database benchmarks that validate or tighten the retrieval budgets below.

## Out Of Scope

- Embedding storage, index generations, semantic or hybrid retrieval (Stage 3).
- Proposals, Memo mutations from AI flows, and the Agent tool set (Stage 4).
- Indexing Chat history as Memos; exposing hidden provider reasoning.
- Per-user provider credentials or opt-in.
- Changes under `server/router/mcp/`.
- Ephemeral/disappearing conversation history (deferred by the architecture).

## Shared Memo Read Seam

- Memo read authorization, visibility and archived-state rules, and searchable-source enumeration move behind a deep `server/memo`
  module used by both the existing MemoService RPC handlers and `server/ai`.
- The extraction is behavior-preserving: existing Memo API responses, permissions, and tests continue to pass unchanged.
- Store remains raw persistence; it is not an authorization interface, and AI code never reads raw Store records to bypass Memo policy.
- The mutation path stays in the API v1 handlers until Stage 4.

## Retrieval

### Search Documents

- `ai_search_document` stores Memo identity and revision, source content hash, corpus-projection and normalization versions,
  normalized title/tags/body text, and the source-span mapping needed to turn projected matches back into source positions.
- The searchable corpus is `NORMAL`, top-level Memos; archived Memos, comments, attachment bodies, and Chat history are excluded.
- Search documents are derived data: rebuildable from source, deletable without touching Memos, and version-isolated so a projection
  change requires a rebuild rather than a silent mix.
- Memo writes emit a lightweight invalidation signal after the source transaction commits; projection and normalization stay outside
  the write path. A periodic reconciliation repairs dropped signals so a fresh Memo eventually appears in fuzzy results.

### Query Behavior

- Lexical retrieval covers exact tokens, substrings, normalized title/tag/body text, and typo-tolerant similarity.
- Structured filters (tags, time, visibility, creator) narrow candidates; the retrieval interface translates product search intent
  and never asks a model to manufacture raw SQL.
- Every candidate is reauthorized against current Memo read permission before a snippet is returned or provider context is built;
  the indexed visibility snapshot is only a prefilter.
- A citation carries the Memo resource name, source revision and hash, and a source span. The source is reread immediately before
  the snippet leaves Memos; on revision/hash mismatch the snippet is regenerated from current source or the citation is discarded.

### Budgets

Each query enforces hard limits; exhaustion returns a machine-readable partial/degraded reason rather than unbounded work.
Defaults (validated by the Stage 2 cross-database benchmarks on all three databases — evidence in the benchmark issue):

- Normalized query text: 1,024 characters.
- Lexical scan: 32 MB of search-document bytes per query, read in bounded batches.
- Candidates per query: 200 before fusion; final results: 20.
- Retrieval wall clock: 5 seconds per search request; 3 seconds for the retrieval phase of a Chat request.
- Chat provider context: 8,000 tokens, favoring diverse source Memos, with retrieved text marked as untrusted quotation.

Benchmarked support envelope per database, measured by the fixtures in `server/ai/search/benchmark_test.go` at the defaults
above. A corpus past the envelope still answers within the wall clock but reports `scan_budget_exhausted` — explicit degraded
results, never unbounded work. On the measured hardware the 32 MB scan budget binds long before the 5-second wall clock.

| Database | Largest corpus served within budgets | Query latency at the envelope | Evidence |
| --- | --- | --- | --- |
| SQLite | 32 MB of search-document bytes (≈28,200 documents at ≈1.19 KB each); largest corpus measured fully served: 22,000 documents / 26.1 MB | ≈0.9 s of the 5 s wall clock (≈0.7 s on Apple M4 Pro) | Recorded in the benchmark issue |
| MySQL | Same byte envelope; the scan trips at the identical point: 28,234 documents / 33,554,302 bytes | ≈1.4 s of the 5 s wall clock | Recorded in the benchmark issue |
| PostgreSQL | Same byte envelope; the scan trips at the identical point: 28,234 documents / 33,554,302 bytes | ≈1.4 s of the 5 s wall clock | Recorded in the benchmark issue |

The 200-candidate budget binds early for broad common-word multiword queries — by design; the response reports
`candidate_budget_exhausted` rather than presenting partial coverage as complete.

## Conversation And Chat

- Conversations are private to their owner and always persist until the owner deletes them.
- `ai_message` stores conversation, role, visible content, status (`STREAMING`, `COMPLETE`, `FAILED`, `CANCELLED`), model metadata,
  usage where available, authorized Memo citations, a client request ID, and an attempt relationship. Hidden provider reasoning is
  neither requested nor stored.
- The client supplies a request ID unique within the conversation. Repeating it returns the existing user message and its
  active/completed attempt; retrying a failed answer creates a new assistant attempt linked to the same user message instead of
  duplicating it.
- Sending a message atomically persists the user message and the assistant attempt. The server then retrieves bounded citations,
  builds instructions + question + quoted context, and streams the answer over a Connect server-streaming RPC.
- Disconnect cancellation propagates to the provider and is persisted; reconnecting clients read the authoritative stored state
  rather than assuming the stream outcome.
- Deleting a conversation while an attempt is active cancels that attempt.
- Chat cannot mutate Memos; no write capability exists in this stage's server surface.

### Degradation

- No generation model configured: search works fully; Chat explains that generation is unavailable.
- Provider failure or timeout: the attempt is persisted as `FAILED` with a normalized error category; Memo capture and search are
  unaffected.
- Budget exhausted: the response carries machine-readable partial/degraded reasons and never presents incomplete coverage as complete.
- Index lag: a fresh Memo may be absent from fuzzy results until reconciliation catches up; stale indexed text is never returned and
  direct Memo reads remain current.

## Data Model And Migrations

- `ai_search_document` and `ai_conversation`/`ai_message` ship as separate incremental migrations, each landing with the issue that
  introduces the feature, for all three drivers (SQLite, MySQL, PostgreSQL) plus each driver's `LATEST.sql`.
- Fresh-install SQL and incremental migrations remain equivalent.
- Rollback of application code leaves the additive tables harmlessly in place; dropping them is an operator action, not a code path.

## Frontend

- The web app adds a Chat view (conversation list, streaming answer, persisted-state reconciliation, citation chips) and a unified
  search view (results with snippets, rank reasons, citations, and partial/degraded banners).
- Server data stays in React Query hooks; streaming state is local to the active response and reconciles with persisted message
  state at completion.
- All AI entry points are hidden or disabled with an actionable explanation when text generation is not configured.

## Accepted Implementation Decisions

- Chat streaming uses a Connect server-streaming RPC on `AIService`; the existing SSE hub remains dedicated to Memo live-refresh
  events and is not reused for request-scoped token streaming.
- Delivery follows a tracer-bullet shape: the first issue lands a thin end-to-end Chat path (real Memo read seam and text
  generation, with explicitly marked scaffolding for retrieval and conversation persistence), and later issues replace each
  scaffold with the real implementation — persistence, streaming, then real retrieval — before full seam extraction completes.
- Search documents and lexical retrieval ship without any generation or embedding configuration, so fuzzy search works on a default
  deployment.
- Retrieval budget defaults were provisional in this spec and became binding when the Stage 2 benchmark issue recorded evidence
  for all three databases; no default was contradicted, so none was tightened.
- Each new table migrates with its own feature issue rather than in a combined schema issue.

## Testing Decisions

- Retrieval fixtures cover exact, partial, fuzzy, and filtered queries; permission changes between indexing and retrieval; projection
  version isolation; stale-citation rejection; and deterministic budget exhaustion.
- Conversation tests cover complete, failed, cancelled, and retry flows; duplicate request IDs; conversation deletion during an
  active attempt; and persisted-state reconciliation after disconnect.
- Seam extraction is guarded by the existing Memo API test suite passing unchanged.
- Migration tests prove fresh-install and incremental-upgrade equivalence on all three drivers.
- Benchmark fixtures run on SQLite, MySQL, and PostgreSQL via TestContainers and record the observed support envelope.
- Frontend tests cover streaming interruption, citation rendering, degraded states, and disabled Chat states.

## Acceptance Criteria

- Chat and fuzzy search work with no embedding configuration; Chat explains itself when no generation model is configured.
- Duplicate sends/retries never duplicate user messages.
- Search and Chat never return or transmit an unauthorized Memo; every citation is reauthorized and revision-checked immediately
  before its text leaves Memos.
- Budget exhaustion produces machine-readable partial/degraded responses, and the documented envelope is benchmarked on all three
  databases.
- Provider failure never affects Memo capture; Chat history stays out of the Memo timeline and index.
- Existing Memo API and MCP tests pass unchanged; there is no implementation diff under `server/router/mcp/`.
- Stage 3 can build embedding generations on top of the versioned search-document projection without rework.

## System Verification

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
cd proto && buf generate && buf lint
go test -v -race ./internal/...
go test -v -race ./server/...
go test -v ./store/...
cd web && pnpm lint && pnpm test && pnpm build
git diff --check
```

Before acceptance, confirm there is no implementation diff under `server/router/mcp/`.
