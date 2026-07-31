# Chunking And Source-Span Mapping

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
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
