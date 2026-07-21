# Replace The Retrieval Scaffold With Real Search

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 05

## Outcome

Real lexical/partial/fuzzy retrieval over search documents replaces scaffold A everywhere it is used (Chat grounding and a new
unified search operation), with hard budgets, revision-checked citations, and a unified search UI.

## Scope

- Implement exact token, substring, normalized title/tag/body, and typo-tolerant matching over search documents, reading in
  bounded batches; support structured filters (tags, time, visibility, creator) translated from product intent.
- Enforce the spec's provisional budgets: 1,024-character normalized query, 32 MB scan, 200 candidates, 20 final results,
  5-second search wall clock, 3-second Chat retrieval phase, 8,000-token provider context favoring diverse source Memos.
- Staleness now exists, so harden citations: reread and reauthorize the source immediately before a snippet leaves Memos or enters
  provider context; regenerate the snippet from current source or discard the citation on revision/hash mismatch.
- Remove scaffold A; Chat grounding and the new unified search operation share the real retrieval path.
- Add the unified search operation to `AIService` (additive proto change; regenerate outputs).
- Return machine-readable partial/degraded reasons on budget exhaustion; never present incomplete coverage as complete.
- Web: add the unified search view — query input with structured filters, results with snippets, rank reasons, citations, and
  partial/degraded banners.

## Acceptance

- Exact, partial, fuzzy, and filtered queries return ranked results with snippets, reasons, and citations.
- A Memo whose permission changed between indexing and retrieval is never returned; stale indexed text never leaves Memos.
- Budget exhaustion is deterministic in tests and honestly reported.
- Search works with no generation or embedding configuration; Chat answers cite supporting Memos from the real path.
- Chat behavior from issues 01–04 keeps working against real retrieval.

## Verification

```bash
cd proto && buf generate && buf lint
go test -v -race ./server/...
cd web && pnpm lint && pnpm test
```

Retrieval fixtures cover ranking, permission changes, projection-version isolation, stale-citation rejection, and budget
exhaustion.
