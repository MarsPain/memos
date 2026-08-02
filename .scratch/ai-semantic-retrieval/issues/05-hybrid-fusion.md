# Hybrid Fusion, Rank Reasons, And Semantic Budget

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

Search and Chat return one honestly-ranked list: lexical and semantic candidates fused by rank, every result disclosing which
path produced it, and an over-budget semantic scan degrading to lexical with a machine-readable reason instead of ranking an
arbitrary prefix of vector rows — replacing the tracer's naive fusion scaffold.

## Scope

- Ordinal-rank fusion of lexical and semantic candidate lists with per-Memo deduplication; never assume lexical and cosine
  scores share a scale.
- Rank reasons on every result disclosing the contributing path(s), extending the Stage 2 reason vocabulary.
- Semantic scan budget enforcement: the provisional 128 MB of chunk vector bytes per query, read in bounded batches; on
  exhaustion, discard the incomplete semantic candidate set and return lexical results with a machine-readable
  `SEMANTIC_BUDGET_EXCEEDED`-style degraded reason.
- The query embedding call is bounded and counted within the existing 5-second search / 3-second Chat retrieval wall clocks;
  embedding failure or timeout during a query falls back to lexical results with a disclosed reason.
- Chat's retrieval phase consumes the same fused path, so citations and provider context inherit the same guarantees.
- Candidate reauthorization through `server/memo` and citation reread/revision-check remain exactly as Stage 2; semantic
  candidates get no shortcut.
- Replace scaffold D from issue 01.

## Explicitly Out Of Scope (later issues)

- Benchmark validation of the provisional semantic budget (07). UI disclosure of serving mode (06).

## Acceptance

- A query matching only semantically and one matching only lexically both surface in one fused list, each with its rank reason;
  a Memo found by both paths appears once.
- A corpus exceeding the semantic scan budget returns lexical results plus the degraded reason inside the wall clock — never
  unbounded work, never a silently partial semantic ranking presented as complete.
- Embedding provider failure during a query degrades to lexical with a disclosed reason.
- No snippet or provider context includes an unauthorized Memo or stale indexed text, from either path.
- Stage 2 lexical-only behavior is unchanged when no callable active generation exists.

## Verification

```bash
go test -v -race ./server/...
```

Fixtures cover semantic-only, lexical-only, and overlapping matches; deterministic budget exhaustion; embedding failure and
timeout mid-query; permission change between indexing and retrieval on the semantic path.

## Comments

Implemented in this change, replacing scaffold D (`server/ai/search/fuse.go`, `semantic.go`, `retrieve.go`):

- Ordinal-rank fusion by reciprocal rank (RRF, k=60) of the lexical and semantic candidate lists with per-Memo deduplication
  (`fuseByRank`); a Memo found by both paths appears once, keeps its lexical representative and snippet anchor, and outranks
  comparable single-path candidates. `mergeInterleaved` and the scaffold TODOs are gone.
- Path rank reasons: `lexical` and `semantic` disclose the contributing path(s) on every result of a genuinely hybrid-served
  list, extending the Stage 2 field/tier vocabulary. Decision (documented in `fuse.go`): when the semantic path serves
  nothing — degraded, disabled, rebuilding, or no positive-similarity match — the list stays byte-identical to Stage 2, per
  the acceptance line "Stage 2 lexical-only behavior is unchanged when no callable active generation exists"; degraded modes
  disclose at the outcome level through `partial_reasons`.
- Semantic scan budget: `Budgets.MaxSemanticScanBytes` (provisional 128 MB of chunk-vector bytes) enforced inside the
  bounded-batch scan; exhaustion discards the incomplete semantic candidate set and returns lexical results with the
  machine-readable `semantic_budget_exceeded` reason — never a ranked arbitrary prefix.
- The single query embedding call is bounded by `Budgets.EmbeddingTimeout` (2s, counted within the wall clock); provider
  failure degrades with `embedding_failed`, its own deadline with `embedding_timeout`, the query wall clock with
  `time_budget_exhausted`. Caller cancellation still fails the query. Decision: semantic-path store failures fail the query
  (consistent with the lexical scan's documented posture) rather than hiding behind lexical coverage.
- A positive-cosine floor (`score <= 0` excluded) makes path disclosure honest: orthogonal vectors share no direction with
  the query, so they are not evidence the semantic path produced the memo. Without it every corpus memo would be a
  "semantic candidate" on every query.
- Chat's retrieval phase now consumes the same fused path (`server/ai/chat.go` wires a `SemanticSearcher` into its
  retriever), so citations and provider context inherit hybrid ranking, budgets, and degradation disclosure; chat behavior
  is unchanged when no embedding capability is assigned.
- Reauthorization, revision-check, and snippet guarantees unchanged: semantic candidates flow through the same `hydrate`
  reread; matches carry the document the scan validated them against (no double read).
- Tests: `server/ai/search/fuse_test.go` (RRF ordering vs. interleave, path disclosure, budget exhaustion, embedding
  failure/timeout, wall-clock edge, semantic-path permission change), chat fused-path and degradation tests in
  `server/ai/chat_test.go`; `go test -race -count=1 ./server/...` green. `golangci-lint` is not installed locally; CI must
  run `golangci-lint run`.
