# AI Semantic And Hybrid Retrieval Product Specification

**Status:** Approved
**Parent product spec:** [AI-Native Notes](ai-native-notes.md)
**Architecture:** [AI-Native Memos Architecture](../design-docs/ai-native-notes.md)
**Roadmap scope:** [Stage 3 — Semantic and Hybrid Retrieval](../ROADMAP.md#stage-3-semantic-and-hybrid-retrieval)

## Objective

Memos gains semantic retrieval over the Stage 2 search-document projection: portable embedding generations and chunks stored in the
configured database, bounded in-process vector similarity, and hybrid ranking that fuses semantic and lexical candidates into one
result list. Index lifecycle becomes operator-visible: generations build in the background, cut over atomically, and report status
and rebuild controls without ever blocking Memo capture. Proposals and the Agent remain out of scope.

## User And Operator Outcomes

- Users can find a Memo by describing its meaning, not only by keywords, when an embedding capability is configured.
- Users receive one fused result list with snippets, matching reasons, and citations; every citation keeps the Stage 2
  reauthorization and revision-check guarantee.
- Users keep full keyword/fuzzy search and Chat when embeddings are unconfigured, rebuilding, or the provider is down, and the UI
  says which mode served the results.
- Users can see when semantic results are partial or degraded and why, never presented as complete coverage.
- Administrators can see index status — active and building generations, lag, and last error — and request a rebuild without
  blocking Memo use.
- Administrators who change the embedding model or configuration get an atomic cutover: queries never mix partial generations.

## In Scope

- Versioned embedding generations (`ai_index_generation`) and chunk storage (`ai_index_chunk`) over the Stage 2 search-document
  projection, for all three databases.
- Background indexing: chunking, bounded embedding batches, reconciliation, and idempotent upserts in the existing runner.
- Atomic generation cutover with conditional promotion, retirement, and a bounded grace period.
- In-Go vector similarity over bounded batches, fused with lexical candidates into hybrid results on the existing
  `SearchMemos` RPC and Chat retrieval path.
- Index status and rebuild controls on `AIService`, plus settings-page and search-UI surfacing of index state.
- Cross-database benchmarks that validate or tighten the semantic retrieval budgets below.

## Out Of Scope

- Proposals, Memo mutations from AI flows, and the Agent tool set (Stage 4).
- High-scale vector index adapters beyond the in-Go scan (later candidate; the retrieval seam must not preclude one).
- Corpus changes: the searchable corpus remains `NORMAL`, top-level Memos with the Stage 2 projection; a projection or
  normalization change is a rebuild trigger handled by the Stage 2 mechanism, not a new corpus definition.
- Attachment text extraction and indexing Chat history as Memos.
- Per-user provider credentials or opt-in.
- Changes under `server/router/mcp/`.

## Embedding Index

### Generations

- `ai_index_generation` stores the full embedding fingerprint, the configured provider reference and non-secret model identity,
  lifecycle state (`BUILDING`, `ACTIVE`, `RETIRED`), progress counters, and timestamps.
- The fingerprint derives from provider type, sanitized endpoint identity, model, resolved dimensions, corpus-projection version,
  chunker version, normalization version, and vector-encoding version. Endpoint identity never includes credentials or secret
  query values.
- At most one generation is `ACTIVE`. A `BUILDING` generation never serves queries. A generation is callable only while the
  current provider pool still has a provider whose type and sanitized endpoint identity match it.

### Chunks

- `ai_index_chunk` stores generation, Memo identity and revision, chunk ordinal, a search-document projection span and
  source-span mapping, vector bytes, dimensions, and indexing timestamp. It does not duplicate the complete search document.
- Chunking derives from the Stage 2 projection spans, so a chunk can be mapped back to source positions through the same
  span mapping lexical retrieval already uses.
- Vectors are finite, L2-normalized IEEE 754 float32 values in the documented little-endian BLOB/BYTEA encoding. Dimension and
  byte-length mismatches reject the batch before commit.
- Generations and chunks are derived data: rebuildable from source and current configuration, deletable without touching Memos.
  Memo deletion removes derived chunks from every generation; account deletion cleanup covers them in the deletion transaction.

### Indexing And Reconciliation

- Memo writes keep the Stage 2 contract: a lightweight invalidation signal after the source transaction commits, and projection,
  chunking, and embedding stay outside the write path. Normal Memo writes never wait for embedding.
- The runner refreshes provider-independent search documents first, then, when an embedding generation exists, batches stale
  chunks for embedding through the Stage 1 provider-neutral `Embed` interface and hardened transport.
- Reconciliation compares source revisions and hashes with index metadata using stable keyset pagination, bounded batches,
  cancellation, per-item backoff, and fair progress so one poison Memo cannot starve the corpus. It resumes after restart without
  a durable queue.
- Upserts are idempotent on generation, Memo, source revision, and chunk ordinal; results commit only if the source revision
  still matches. Only one local worker indexes a given Memo revision at a time; cross-process exclusion is not claimed for the
  single-process baseline.

### Cutover

- An embedding configuration change creates a `BUILDING` generation that is not queryable. A corpus-projection or normalization
  change first rebuilds search documents and also creates a new generation when embeddings are configured.
- After a complete verification pass confirms every eligible Memo observed in the pass has current chunks, the runner atomically
  promotes the generation to `ACTIVE`; the former active generation becomes `RETIRED` and is removed after a bounded grace
  period. Promotion is conditional on the generation still matching the desired embedding assignment.
- A newer desired fingerprint supersedes and removes any older building generation. Disabling embeddings cancels building work
  and disables semantic retrieval.
- While a replacement generation builds, semantic queries use only the previous complete active generation when it remains
  callable; otherwise semantic search reports itself as rebuilding and lexical retrieval serves the query.

## Retrieval

### Query Behavior

- Semantic retrieval embeds the query through the configured embedding capability and compares it only with chunks in the
  `ACTIVE` generation. It participates only when a compatible active generation is callable; otherwise lexical results serve the
  response with a machine-readable reason.
- The baseline similarity is a complete scan of the active generation in Go over database rows read in bounded batches. If that
  scan cannot finish inside its budget, retrieval discards the incomplete semantic candidate set and returns lexical results
  with a `SEMANTIC_BUDGET_EXCEEDED`-style degraded reason — it never ranks an arbitrary prefix of vector rows as though it
  represented the corpus.
- Hybrid ranking fuses lexical and semantic ordinal ranks rather than assuming lexical and cosine scores share a scale, then
  deduplicates by Memo. Rank reasons disclose which path(s) produced each result.
- Structured filters, candidate reauthorization through `server/memo`, and citation reread/revision-check behave exactly as in
  Stage 2; semantic candidates are subject to the same guarantees before any snippet leaves Memos or enters provider context.

### Budgets

The Stage 2 budgets continue to apply unchanged (query text 1,024 characters; 200 candidates before fusion; 20 final results;
5-second search and 3-second Chat retrieval wall clocks; 8,000-token provider context). This stage adds a semantic scan budget.
Defaults below are provisional and become binding when the Stage 3 cross-database benchmark issue records evidence on all three
databases:

- Semantic scan: 128 MB of chunk vector bytes per query (≈20,000 chunks at 1,536 dimensions), read in bounded batches.
- Query embedding: one bounded provider call per search or Chat retrieval phase, counted within the existing wall clock.

The benchmark fixtures extend `server/ai/search/benchmark_test.go` to record the semantic support envelope per database at the
defaults above, alongside the Stage 2 lexical envelope. A corpus past the envelope still answers within the wall clock with an
explicit degraded reason, never unbounded work.

## Index Status And Administration

- `AIService` gains an index-status operation reporting the active and building generations, whether semantic retrieval is
  currently callable, eligible/indexed/stale Memo counts, and a normalized last-error summary; and an administrator rebuild
  operation that requests a new building generation.
- The settings page exposes rebuild status and progress without blocking Memo use, on top of the Stage 1 disclosure that
  external embedding sends every eligible Memo chunk incrementally to the provider.
- Status and error surfaces use normalized categories only: no raw query text, Memo text, provider payloads, endpoint secrets,
  or vectors.

## Data Model And Migrations

- `ai_index_generation` and `ai_index_chunk` ship as separate incremental migrations, each landing with the issue that
  introduces the feature, for all three drivers (SQLite, MySQL, PostgreSQL) plus each driver's `LATEST.sql`.
- Fresh-install SQL and incremental migrations remain equivalent.
- Rollback of application code leaves the additive tables harmlessly in place; semantic retrieval simply finds no callable
  generation and lexical search continues. Dropping the tables is an operator action, not a code path.

## Frontend

- Search results and Chat citations disclose the serving mode: hybrid, lexical-only (with reason), semantic rebuilding, or
  semantic budget degraded — using product language such as “正在更新语义索引”, never vector-database terminology or raw
  provider errors.
- The AI settings page shows index status (active/building generation, lag, last error) and offers administrators a rebuild
  action with an honest in-progress state.
- Semantic search entry points degrade to the Stage 2 disabled/actionable states when no embedding capability is assigned.
- Server data stays in React Query hooks; index status polls or refreshes without blocking editing.

## Accepted Implementation Decisions

- Embeddings are stored in the configured database and compared in the Go process; no external vector service is added to the
  baseline deployment.
- Chunks reference search-document projection spans instead of duplicating normalized text, keeping the Stage 2 span mapping the
  single way back to source positions.
- Generation promotion is a single conditional store transaction; the runner never serves a generation it has not finished
  verifying.
- Similarity runs as a bounded complete scan behind the existing retrieval seam, so a future high-scale adapter can replace it
  without changing Chat or search callers.
- Each new table migrates with its own feature issue rather than in a combined schema issue.
- Delivery follows a tracer-bullet shape: an early issue lands a thin end-to-end semantic path (schema, indexing of a bounded
  slice, fused results behind the existing search RPC) with explicitly marked scaffolding that later issues replace with full
  reconciliation, cutover, and status surfaces.

## Testing Decisions

- Required tests use the deterministic fakes and local HTTP fixtures from Stage 1; no test calls a live provider.
- Indexing fixtures cover chunking and span mapping, idempotent upserts, source-revision races, poison-Memo backoff,
  reconciliation resume after restart, and Memo/account deletion cleanup across generations.
- Cutover fixtures cover BUILDING/ACTIVE promotion, conditional-promotion failure when the desired fingerprint changes,
  superseded building generations, interrupted rebuilds, retirement and grace-period removal, and disabling embeddings mid-build.
- Retrieval fixtures cover semantic-only, hybrid fusion and deduplication, fingerprint and dimension isolation, lexical fallback
  during embedding failure and rebuild, permission changes between indexing and retrieval, stale-citation rejection, and
  deterministic semantic budget exhaustion.
- Migration tests prove fresh-install and incremental-upgrade equivalence on all three drivers.
- Benchmark fixtures run on SQLite, MySQL, and PostgreSQL via TestContainers and record the observed semantic support envelope.
- Frontend tests cover serving-mode disclosure, index status rendering, rebuild action states, and degraded/disabled states.

## Acceptance Criteria

- Semantic search requires an explicit embedding capability assignment; with none configured, Stage 2 behavior is unchanged.
- A building generation never serves queries; a model or projection change cuts over atomically, and queries never mix partial
  generations.
- The documented semantic support envelope is benchmarked and recorded for SQLite, MySQL, and PostgreSQL.
- Semantic budget exhaustion discards the incomplete vector candidate set and returns lexical results with a machine-readable
  degraded reason.
- Every citation is reauthorized and revision-checked immediately before its text leaves Memos; stale indexed text is never
  returned by any path.
- Keyword/fuzzy search and Chat remain available during provider outage, embedding failure, and rebuild; Memo capture is never
  blocked by indexing.
- Index status distinguishes active and building generations and reports whether semantic retrieval is callable; rebuild status
  is visible without blocking use.
- Existing Memo API, Chat, retrieval, and MCP tests pass unchanged; there is no implementation diff under `server/router/mcp/`.
- Stage 4 can retrieve through the same seam without reworking generations or chunks.

## System Verification

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
cd proto && buf generate && buf lint
go test -v -race ./internal/...
go test -v -race ./server/...
go test -v ./store/...
go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
cd web && pnpm lint && pnpm test && pnpm build
git diff --check
```

Before acceptance, confirm there is no implementation diff under `server/router/mcp/`.
