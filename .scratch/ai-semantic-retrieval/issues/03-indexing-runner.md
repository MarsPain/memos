# Indexing Runner And Reconciliation

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: resolved
Blocked by: 02

## Outcome

Embedding indexing keeps up with Memo writes on its own: fresh Memos appear in semantic results without any manual trigger,
dropped signals are repaired, and a poison Memo cannot starve the corpus — replacing the tracer's manual indexing scaffold.

## Scope

- Runner integration: after the Stage 2 search-document refresh, the runner batches stale chunks for embedding when a
  generation exists. Projection, chunking, and embedding stay outside the Memo write path; normal writes never wait for
  embedding.
- Periodic reconciliation comparing source revisions and hashes with chunk metadata, using stable keyset pagination, bounded
  batches, cancellation, per-item backoff, and fair progress so one poison Memo cannot starve the corpus. Resumes after
  restart without a durable queue.
- Idempotent upserts on (generation, Memo, source revision, chunk ordinal); results commit only if the source revision still
  matches. Only one local worker indexes a given Memo revision at a time.
- Memo deletion removes derived chunks from every generation; account deletion covers chunks in the deletion transaction.
- Embedding provider failure: the affected batch backs off; Memo capture, search, and Chat are unaffected.
- Replace scaffold B from issue 01.

## Explicitly Out Of Scope (later issues)

- BUILDING/ACTIVE lifecycle and cutover (04); progress reporting is consumed by issue 06's status surface.

## Acceptance

- A newly written or edited Memo appears in semantic results after the runner catches up, with no manual trigger; stale
  indexed text is never returned in the interim.
- A simulated dropped invalidation signal is repaired by reconciliation; a restart mid-pass resumes without duplicate work.
- A Memo whose embedding repeatedly fails backs off without blocking other Memos.
- Memo deletion and account deletion leave no orphaned chunks in any generation.
- Embedding provider outage does not affect Memo CRUD, lexical search, or Chat.

## Verification

```bash
go test -v -race ./server/...
go test -v ./store/...
```

Fixtures use deterministic embedding fakes, simulated dropped signals, forced restart between reconciliation batches, and a
poison Memo that always fails.

## Comments

Implemented in this commit: Scaffold B replaced with runner-integrated reconciliation (`server/ai/search/indexer.go`). The search-document service gained refresh hooks (`server/ai/search/search.go` RefreshHooks): `AfterTargetedSync` follows signal-driven syncs and document removals, `AfterSweep` follows every completed full sweep, and `APIV1Service.SearchService()` wires both to the indexer (`server/router/api/v1/v1.go`), so a memo written through the API reaches semantic results after the runner catches up with no manual trigger. The indexer's full sweep (RunOnce) lost the 512-document scaffold cap: stable keyset pagination over all current-version documents, cancellation between batches, per-memo exponential backoff shared with the Stage 2 reconciler via the new `backoffTracker` (a poison memo backs off alone; an immediate re-sweep does not retry it), and progress derived from stored chunks, so a restart mid-pass resumes without a durable queue and never re-embeds committed work — verified by a forced-cancel fixture. Embedding batches are per document (≤16 chunks), which isolates provider failures to the memo that caused them; commits re-read the document and land only if the source revision and content hash still match, and superseded chunks prune only after the fresh set commits. Reconciliation compares revisions **and hashes**: `ai_index_chunk` gained `content_hash` (new migration `06__ai_index_chunk_content_hash.sql` for all three drivers plus `LATEST.sql`; 0.30 was already released, so the column is additive), so a document rewritten without a revision bump is still detected as stale. Upserts stay idempotent on (generation, memo, source revision, chunk ordinal); a mutex serializes passes so only one local worker indexes at a time. Targeted sync (SyncMemos) drops a memo's chunks from every generation when its document is gone (deleted or archived), and the full sweep forwards sweepDocuments removals through the same path, repairing dropped delete signals. Account deletion now covers `ai_index_chunk` in the delete-user transaction on all three drivers alongside `ai_search_document`. The semantic query path drops matches built from a superseded revision or hash (`semantic.go` staleness guard), so stale indexed text never serves in the interim between document refresh and re-embedding. Scaffold C activation semantics are unchanged (generation activates only when a sweep covers every eligible document; a partial index never serves) and remain TODO(issue 04). Tests: package fixtures for dropped-signal repair, hash-only drift, restart resume without duplicate work, poison-memo backoff, cross-generation deletion cleanup, interim stale-match suppression, and provider outage leaving lexical search unaffected; v1 fixtures proving no-manual-trigger indexing after an API write and deletion cleanup across generations; store fixtures covering the new column and delete-user chunk cleanup. `go test -race ./server/...`, `go test ./store/...`, `go test -race ./internal/...` pass (MySQL/PostgreSQL driver tests skip without Docker locally); no diff under `server/router/mcp/`; golangci-lint not installed locally, gofmt/go vet clean.
