# Semantic Retrieval Benchmarks

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 05

## Outcome

The provisional semantic scan budget in the parent spec is validated or tightened against measured evidence on SQLite, MySQL,
and PostgreSQL, and the spec's benchmarked semantic support envelope is recorded before the Stage 3 go/no-go gate.

## Scope

- Extend the benchmark fixtures in `server/ai/search/benchmark_test.go` with seeded vector corpora (small, medium, and the
  largest supported envelope) at realistic dimensions, via TestContainers for MySQL/PostgreSQL and file-backed SQLite.
- Measure semantic scan latency, memory, and candidate counts at the provisional 128 MB vector-byte budget, within the existing
  5-second search wall clock.
- Identify where each database breaches the budget and derive the largest chunk corpus each driver serves within it.
- Tighten any provisional default the evidence contradicts, and update the parent spec's budget section with the final values
  and the recorded per-database envelope.
- Add deterministic regression fixtures that fail if semantic budget enforcement regresses, including proof that an
  over-budget corpus degrades with the machine-readable reason rather than ranking an arbitrary vector-row prefix.
- Record the benchmark evidence on this issue, as in the Stage 2 benchmark issue.

## Acceptance

- Every semantic budget default in the spec is backed by recorded benchmark evidence on all three databases.
- The spec states the benchmarked semantic support envelope per database; exceeding it yields explicit degraded results, not
  unbounded work.
- Benchmark fixtures are repeatable and run without live providers.

## Verification

```bash
go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
go test -v ./store/...
go test -v -race ./server/...
```

Benchmark results are appended to this issue and the spec is updated in the same change.

## Comments
