# Tracer: End-To-End Semantic Retrieval Skeleton

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: resolved
Blocked by: none

## Outcome

A configured instance answers `SearchMemos` with semantically retrieved, fused results — the thinnest path through embedding
configuration → chunk storage → query embedding → in-Go similarity → the existing search RPC. Every simplified layer is real
except the explicitly marked scaffolds below, which later issues replace.

## Scope

- `ai_index_generation` migration: fingerprint, provider reference, non-secret model identity, lifecycle state, progress
  counters, timestamps. Minimal but real schema; the lifecycle state machine itself is scaffolded (see below).
- `ai_index_chunk` migration: generation, Memo identity and revision, chunk ordinal, projection span and source-span mapping,
  vector bytes, dimensions, indexing timestamp. Two separate migrations, one per table, for all three drivers plus each
  driver's `LATEST.sql`.
- Fingerprint computation from provider type, sanitized endpoint identity, model, resolved dimensions, corpus-projection
  version, chunker version, normalization version, and vector-encoding version.
- Vector encoding: finite, L2-normalized float32 in little-endian BLOB/BYTEA; dimension and byte-length mismatches reject the
  batch before commit.
- **Scaffold A — chunking:** one chunk per search document (whole body up to a documented cap). Replaced by issue 02.
- **Scaffold B — indexing:** a manually triggered, bounded one-shot indexing pass over current search documents through the
  Stage 1 `Embed` interface. No runner integration, no reconciliation. Replaced by issue 03.
- **Scaffold C — lifecycle:** a single generation row treated as active once its one-shot pass completes; no BUILDING/ACTIVE
  cutover. Replaced by issue 04.
- Query path: embed the query through the configured embedding capability, cosine-compare against the generation's chunks in
  Go over bounded batches, and merge into the existing `SearchMemos` response. **Scaffold D — fusion:** naive interleave with
  lexical results. Replaced by issue 05.
- No embedding capability configured: Stage 2 behavior unchanged, no semantic path executes.

## Explicitly Out Of Scope (later issues)

- Multi-chunk documents and span-mapped chunking (02). Runner integration, reconciliation, deletion cleanup (03). Generation
  lifecycle, cutover, rebuild isolation (04). Ordinal-rank fusion, rank reasons, semantic scan budget (05). Status/rebuild
  RPCs and all UI (06). Benchmarks (07).

## Acceptance

- On an instance with an embedding assignment, a meaning-based query returns the relevant Memo in fused `SearchMemos` results;
  the path is demoable end to end.
- With no embedding assignment, every Stage 2 search and Chat test passes unchanged.
- Vectors failing finiteness, dimension, or byte-length validation are rejected before commit.
- Existing Memo API, retrieval, and MCP tests pass unchanged; no diff under `server/router/mcp/`.
- The four scaffolds are visibly marked in code (TODO referencing issues 02, 03, 04, 05).

## Verification

```bash
go test -v ./store/...
go test -v -race ./server/...
go test -v -race ./internal/...
```

Embedding calls use the Stage 1 deterministic fakes and local HTTP fixtures; no live provider calls. Migration tests prove
fresh-install and incremental-upgrade equivalence on all three drivers.

## Comments

Implemented in this commit: `ai_index_generation` and `ai_index_chunk` migrations for SQLite/MySQL/PostgreSQL plus `LATEST.sql` updates, store CRUD, and migration-upgrade tests; little-endian L2-normalized float32 vector encoding with finiteness/dimension/byte-length validation in `internal/ai/vector.go`; generation fingerprint from provider type, sanitized endpoint identity, model, dimensions, and projection/chunker/normalization/vector-encoding versions (`server/ai/search/fingerprint.go`). Scaffolds visibly marked: A chunking (TODO issue 02, `server/ai/search/chunk.go`), B one-shot indexing (TODO issue 03, `server/ai/search/indexer.go`), C single-generation activation (TODO issue 04, `server/ai/search/indexer.go`), D naive interleave fusion (TODO issue 05, `server/ai/search/retrieve.go`, `server/ai/search/semantic.go`). Semantic path merges into `SearchMemos` via the Retriever seam; no embedding assignment leaves Stage 2 search and Chat unchanged. No diff under `server/router/mcp/`.
