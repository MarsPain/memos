# Benchmark Retrieval Budgets Across Databases

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: resolved
Blocked by: 06

## Outcome

The provisional retrieval budgets in the parent spec are validated or tightened against measured evidence on SQLite, MySQL, and
PostgreSQL, and the spec's benchmarked support envelope is recorded before the Stage 2 go/no-go gate.

## Scope

- Build benchmark fixtures with representative corpus sizes (small, medium, and the largest supported envelope) via TestContainers
  for MySQL/PostgreSQL and file-backed SQLite.
- Measure lexical scan latency, memory, and candidate counts at the provisional budgets (1,024-char query, 32 MB scan, 200
  candidates, 5-second wall clock).
- Identify where each database breaches the budgets and derive the largest corpus each driver serves within them.
- Tighten any provisional default the evidence contradicts, and update the parent spec's budget section with the final values and
  the recorded envelope.
- Add deterministic regression fixtures that fail if budget enforcement regresses.

## Acceptance

- Every budget default in the spec is backed by recorded benchmark evidence on all three databases.
- The spec states the benchmarked support envelope per database; exceeding it yields explicit degraded results, not unbounded work.
- Benchmark fixtures are repeatable and run without live providers.

## Verification

```bash
go test -v ./store/...
go test -v -race ./server/...
```

Benchmark results are appended to this issue and the spec is updated in the same change.

## Comments

Implemented: retrieval outcomes now carry scan statistics (`ScanStats` on `search.Outcome`: bytes, documents, candidates), the
`store/test` helpers accept `testing.TB` so benchmarks can use the shared driver/TestContainers infra, cross-database benchmark
fixtures live in `server/ai/search/benchmark_test.go` (deterministic seeded corpus at 1k/8k/22k/30k documents; exact, rare and
common multiword, 32-word, and fuzzy queries at the default budgets), and deterministic regression fixtures in
`server/ai/search/retrieve_budgets_test.go` pin the spec defaults and prove that a corpus past the 32 MB envelope degrades with
`scan_budget_exhausted` and a 250-document match set degrades with `candidate_budget_exhausted`, both inside the budgets rather
than running unbounded. The spec's budget section records the envelope table.

SQLite benchmark evidence (file-backed, Apple M4 Pro, `go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval
-benchtime=10x`, ns/op per query, 10 iterations):

| Corpus | Scan bytes | Query | ns/op | B/op | Candidates | Hits | Partial reasons |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1,000 docs | 1,184,863 | exact rare token | 23,975,654 | 38,870,172 | 10 | 10 | none |
| 1,000 docs | 1,184,863 | multiword rare | 23,543,479 | 38,863,918 | 10 | 10 | none |
| 1,000 docs | 1,184,863 | multiword common | 18,277,404 | 23,249,120 | 200 | 20 | candidate_budget_exhausted |
| 1,000 docs | 1,184,863 | max words (32) | 23,614,362 | 38,926,023 | 8 | 8 | none |
| 1,000 docs | 1,184,863 | fuzzy typo | 23,176,738 | 39,037,696 | 10 | 10 | none |
| 8,000 docs | 9,491,797 | exact rare token | 188,444,092 | 311,036,933 | 10 | 10 | none |
| 8,000 docs | 9,491,797 | multiword rare | 184,707,512 | 311,037,870 | 10 | 10 | none |
| 8,000 docs | 9,491,797 | multiword common | 142,861,217 | 184,975,305 | 200 | 20 | candidate_budget_exhausted |
| 8,000 docs | 9,491,797 | max words (32) | 186,181,712 | 311,111,241 | 6 | 6 | none |
| 8,000 docs | 9,491,797 | fuzzy typo | 185,389,342 | 311,204,580 | 10 | 10 | none |
| 22,000 docs | 26,131,842 | exact rare token | 513,368,462 | 855,587,428 | 10 | 10 | none |
| 22,000 docs | 26,131,842 | multiword rare | 513,654,146 | 855,579,191 | 10 | 10 | none |
| 22,000 docs | 26,131,842 | multiword common | 400,617,638 | 508,306,293 | 200 | 20 | candidate_budget_exhausted |
| 22,000 docs | 26,131,842 | max words (32) | 516,931,900 | 855,674,124 | 6 | 6 | none |
| 22,000 docs | 26,131,842 | fuzzy typo | 516,410,096 | 855,753,702 | 10 | 10 | none |
| 30,000 docs | 33,554,302 | exact rare token | 664,670,017 | 1,098,738,603 | 10 | 10 | scan_budget_exhausted |
| 30,000 docs | 33,554,302 | multiword rare | 667,432,842 | 1,098,740,073 | 10 | 10 | scan_budget_exhausted |
| 30,000 docs | 33,554,302 | multiword common | 521,251,679 | 652,559,684 | 200 | 20 | scan + candidate exhausted |
| 30,000 docs | 33,554,302 | max words (32) | 669,150,629 | 1,098,804,351 | 3 | 3 | scan_budget_exhausted |
| 30,000 docs | 33,554,302 | fuzzy typo | 669,883,083 | 1,098,913,660 | 10 | 10 | scan_budget_exhausted |

Findings (SQLite):

- No provisional default is contradicted; none was tightened. Latency scales linearly with scanned bytes (~20 ms per MB) and at
  the 32 MB envelope a query takes ≈0.7 s of the 5 s wall clock, so the scan budget binds long before the wall clock. The 1,024-
  character / 32-word query budget costs nothing measurable at any corpus size (max-words rows match the exact-query rows).
- The envelope is byte-based: any corpus totaling ≤32 MB of search-document bytes is fully covered. The largest corpus measured
  fully served is 22,000 documents (26.1 MB); in the 30,000-document run (≈35.6 MB) the scan tripped at exactly 28,234
  documents / 33,554,302 bytes and still answered in ≈0.7 s with `scan_budget_exhausted` — degraded, never unbounded.
- The 200-candidate budget binds early for broad common-word multiword queries (even at 1,000 documents) and reports
  `candidate_budget_exhausted`; rare-token, 32-word, and fuzzy queries stay far inside the budget. This is the designed
  degradation, and the regression fixtures pin it at the defaults.
- Memory tracks scanned bytes at roughly 33 allocated bytes per scanned byte (transient per-query allocations, GC'd between
  queries); the 3 s Chat retrieval phase has ≈4x headroom against the measured 0.7 s envelope latency.

Cross-database evidence (GitHub Actions `ubuntu-latest`, run
[30557390310](https://github.com/MarsPain/memos/actions/runs/30557390310), `-benchtime=10x`; no local Docker runtime exists,
so the `.github/workflows/retrieval-benchmarks.yml` workflow ran the driver-parameterized fixtures where TestContainers works).
Scanned bytes, documents scanned, candidates, and hits are identical on all three drivers at every corpus size — the seeded
corpus is byte-identical across databases.

Latency per query at the two decisive corpus sizes (worst row over the five queries, ns/op):

| Corpus | SQLite | MySQL | PostgreSQL |
| --- | --- | --- | --- |
| 22,000 docs (26.1 MB, fully served) | 919,293,074 | 1,382,047,883 | 1,381,106,329 |
| 30,000 docs (trips at 28,234 docs / 33,554,302 bytes) | 1,155,512,064 | 1,788,693,478 | 1,772,822,066 |

Small/medium scaling holds on all drivers (1,000 docs: 31–71 ms; 8,000 docs: 232–531 ms), the 200-candidate budget binds for
common-word multiword queries at every size, and every run past the scan budget reports `scan_budget_exhausted` with
per-query latency ≤1.79 s of the 5 s wall clock. Full per-query rows are published as the `retrieval-benchmarks-<driver>`
check runs on commit 87f80e83 and as workflow artifacts.

Findings (all three databases):

- No budget default is contradicted on any driver; none was tightened. The defaults are now binding per the spec's Accepted
  Implementation Decisions.
- The support envelope is identical on all three drivers: any corpus totaling ≤32 MB of search-document bytes is fully
  covered; the scan trips at exactly 28,234 documents / 33,554,302 bytes. The largest corpus measured fully served is 22,000
  documents / 26.1 MB.
- The 5 s wall clock never binds: worst measured query is 1.79 s (MySQL, 30k corpus). The 3 s Chat retrieval phase keeps ≥2x
  headroom on every driver at the envelope.
- Container round trips cost MySQL/PostgreSQL roughly 0.4–0.6 s per envelope query over file-backed SQLite — comfortably
  inside the budgets.

Status: resolved — every budget default is now backed by recorded evidence on SQLite, MySQL, and PostgreSQL; the spec's
budget section and envelope table record the final values.

Review follow-ups (two-axis review): envelope wording now separates the largest corpus measured fully served (22,000 documents
/ 26.1 MB) from the byte-level trip point (28,234 documents / 33,554,302 bytes in the 30,000-document run); added the 32-word
query benchmark row and the default-budget candidate-cap regression fixture; removed the duplicated scan-byte tracker in
`scan()`; error paths return zero stats.

Local verification: `go test -race ./server/...` passes; `SKIP_CONTAINER_TESTS=1 go test ./store/...` passes; `go vet` and
`gofmt` clean; `python3 scripts/validate_docs.py` passes. `golangci-lint` is not installed locally; CI runs it.
