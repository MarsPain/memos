# Indexing Runner And Reconciliation

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
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
