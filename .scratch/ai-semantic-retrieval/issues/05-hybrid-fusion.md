# Hybrid Fusion, Rank Reasons, And Semantic Budget

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
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
