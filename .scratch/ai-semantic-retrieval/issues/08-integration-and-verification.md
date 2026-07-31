# Integration And Verification

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 06, 07

## Outcome

Stage 3 is verified end to end against the parent spec's acceptance criteria, and the spec closes out as Implemented.

## Scope

- Run the spec's full System Verification suite and fix any fallout:

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
cd proto && buf generate && buf lint
go test -v -race ./internal/...
go test -v -race ./server/...
go test -v ./store/...
go test ./server/ai/search/ -run '^$' -bench BenchmarkRetrieval -benchtime=10x -v
cd web && pnpm lint && pnpm test && pnpm build
git diff --check
```

- Walk every acceptance criterion in the parent spec and attach the evidence (test, fixture, or demo) that satisfies it.
- Confirm there is no implementation diff under `server/router/mcp/`.
- Confirm no scaffold TODOs from issues 01–05 remain in the codebase.
- Flip the parent spec status to Implemented.

## Acceptance

- Every check in the System Verification suite passes.
- Every parent-spec acceptance criterion has recorded evidence, including: building generations never serve queries; atomic
  cutover on model/projection change; benchmarked envelope on all three databases; budget exhaustion discards the incomplete
  vector candidate set; citations reauthorized and revision-checked on every path; Memo capture never blocked by indexing;
  keyword fallback always available.
- No diff under `server/router/mcp/`; no leftover scaffold markers.
- The parent spec reads Implemented.

## Comments
