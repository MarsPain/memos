# Build Search Documents And Corpus Projection

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 02

## Outcome

Provider-independent derived search documents (`ai_search_document`) are built and maintained for the searchable corpus with
corpus-projection and normalization versioning — the storage foundation that lets issue 06 replace scaffold A with real retrieval.

## Scope

- Add the `ai_search_document` migration for SQLite, MySQL, and PostgreSQL plus `LATEST.sql`: Memo identity and revision, source
  content hash, projection/normalization versions, normalized title/tags/body text, and source-span mapping.
- Implement the corpus projection: `NORMAL` top-level Memos only; exclude archived Memos, comments, attachment bodies, and Chat
  history. Project the H1-derived title, extracted tags, and a plain-text projection of Markdown content.
- Enumerate searchable sources through the completed `server/memo` seam from issue 02.
- On Memo writes, emit a lightweight post-commit invalidation signal; projection stays outside the write path.
- Add periodic reconciliation (keyset pagination, bounded batches, per-item backoff) that repairs dropped signals and resumes
  after restart without a durable queue. Memo deletion removes derived search documents.
- Do not change the Chat retrieval path in this issue; scaffold A keeps serving until issue 06.

## Acceptance

- Fresh-install and incremental migrations are equivalent on all three drivers.
- A written/updated/deleted Memo is reflected in its search document after reconciliation; the write path never waits for it.
- Documents carry projection and normalization versions; rows from another version are excludable from retrieval.
- Search documents are rebuildable from source and deletable without touching Memos.

## Verification

```bash
go test -v ./store/...
go test -v -race ./server/...
```

Migration equivalence tests run on all three drivers via TestContainers.
