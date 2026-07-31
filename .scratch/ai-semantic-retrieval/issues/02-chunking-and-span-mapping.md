# Chunking And Source-Span Mapping

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

Long Memos are indexed as multiple chunks whose positions map back to exact source spans, so semantic snippets and citations
point at the right passage — replacing the tracer's one-chunk-per-document scaffold.

## Scope

- A versioned chunker (`chunker version` already feeds the fingerprint from issue 01) that derives chunk boundaries from the
  Stage 2 search-document projection spans instead of splitting raw text.
- Each chunk records its projection span and the source-span mapping, reusing the Stage 2 span-mapping mechanism that lexical
  retrieval already uses to reach source positions.
- Bounded chunk size and per-Memo chunk count, with oversized Memos chunked deterministically rather than truncated silently.
- Re-chunking is deterministic: the same projection version and content produce identical chunk ordinals and spans, which keeps
  the idempotent upsert key (generation, Memo, source revision, chunk ordinal) stable.
- Replace scaffold A from issue 01; the one-shot indexing pass now embeds all chunks of each stale search document.

## Explicitly Out Of Scope (later issues)

- Runner-driven reconciliation of stale chunks (03). What happens to old chunks across generation cutover (04).

## Acceptance

- A long Memo produces multiple chunks, and a semantic hit on any chunk maps back to the correct source span for snippet
  generation and citation.
- Chunk ordinals and spans are identical across repeated indexing of unchanged content.
- Dimension or byte-length mismatches reject the whole batch; a partial batch never commits.
- Stage 2 lexical retrieval and citation tests pass unchanged.

## Verification

```bash
go test -v -race ./server/...
```

Chunking fixtures cover boundary cases: empty documents, documents at exactly the chunk cap, multi-paragraph span mapping, and
determinism across repeated runs.

## Comments

Implemented in this commit: versioned span-derived chunker replaces Scaffold A (`server/ai/search/chunk.go`, ChunkerVersion bumped 1 → 2 since the chunking rules changed, which re-fingerprints generations). Chunk boundaries derive from the Stage 2 projection spans: consecutive spans pack into chunks up to a 1,536-byte target, a span larger than the chunk size splits at rune boundaries, and every chunk maps back to source bytes through the same `mapContentPosition` span mapping lexical retrieval uses (uncovered gap edges fall back to the nearest covered position). Bounded per-Memo chunk count (64): oversized Memos re-chunk at a deterministically escalated size, so coverage is always total — chunk texts concatenate to the full projected content, never silently truncated. The one-shot pass (Scaffold B, unchanged) now embeds every chunk of each stale document in batches of 16, counts a document indexed only when all its chunks commit, prunes the document's superseded-revision chunks only after the fresh set commits (a failed re-embed never leaves the serving generation without the document's chunks), and still rejects a whole batch on dimension/byte-length mismatch before any commit. Determinism verified: repeated indexing of unchanged content produces identical chunk ordinals and spans and embeds nothing again. Tests: chunker boundary fixtures (empty, exactly at chunk target and count cap, oversized memo, multi-paragraph span packing, gap edges, rune-boundary splits, determinism) plus end-to-end fixtures (long memo indexed as multiple chunks, semantic hit on a later chunk maps to the owl source region, stale-chunk replacement on memo edit, re-run span stability). `go test -race ./server/...` and `go test -race ./internal/...` pass; no store or migration changes; no diff under `server/router/mcp/`.
