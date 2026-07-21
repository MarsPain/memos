# Benchmark Retrieval Budgets Across Databases

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: ready-for-agent
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
