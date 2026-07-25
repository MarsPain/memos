# Build Search Documents And Corpus Projection

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: resolved
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

## Comments

Implemented: `ai_search_document` migrations for SQLite, MySQL, and PostgreSQL plus `LATEST.sql`
(memo identity and `memo_updated_ts` revision, SHA-256 content hash, projection/normalization
versions, normalized title/tags/body, and a JSON source-span mapping projected content byte ranges
back to source content ranges; one document per memo via `UNIQUE (memo_id)`, no FK to `memo` —
derived data stays decoupled). Corpus projection in `server/ai/search`: `NORMAL` top-level memos
only, enumerated keyset-paginated through the new `server/memo.Service.ListSearchableMemos` seam
method (`FindMemo.IDGreaterThan`/`OrderByIDAsc`); H1-derived title and tags from
`internal/markdown.ExtractAll`, plain-text body from the new `internal/markdown.ExtractText`
(chunked, code blocks included, source segments recorded; `TagNode` gained a source `Segment`).
All corpus source reads go through the seam: keyset enumeration via `ListSearchableMemos` and
single-memo membership via the new `GetSearchableMemo`.
Normalization v1: NFC + case folding + whitespace collapse. Memo writes (create/update/delete in
the API v1 handlers) emit a non-blocking post-commit `Invalidate` signal; the reconciler
(started in `server.go` alongside the s3presign runner) drains signals, sweeps the corpus every
5 minutes in bounded batches of 100 with per-item exponential backoff, removes documents for
deleted/archived memos, and rebuilds rows from other versions. Retrieval path untouched: scaffold
A in `server/ai/retrieval.go` still serves until issue 06.

Notes for issue 06: retrieval can filter candidates on `projection_version`/`normalization_version`
and must reauthorize per the spec; spans map normalized content byte ranges to source byte ranges
at chunk granularity (within-chunk mapping is approximate when normalization changes byte lengths,
and autolink URLs carry no span). `Service.PendingInvalidations()` exposes signal backlog depth.
Local verification: `go test -race ./internal/... ./server/...` and
`SKIP_CONTAINER_TESTS=1 go test ./store/...` pass; mysql/postgres driver and TestContainers
migration-equivalence tests need Docker (unavailable locally) — CI runs
`go test -v ./store/...` for them. `golangci-lint` is not installed locally; `go vet` and
`gofmt` are clean.
