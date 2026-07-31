# Index Status, Rebuild Controls, And UI

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 04, 05

## Outcome

Operators and users can see what the semantic index is doing: administrators get generation status, lag counters, and a
rebuild action; users get honest serving-mode disclosure on every search and Chat answer — in product language, never
vector-database terminology or raw provider errors.

## Scope

- Additive `AIService` operations: an index-status read reporting active and building generations, whether semantic retrieval
  is currently callable, eligible/indexed/stale Memo counts, and a normalized last-error summary; and an administrator-only
  rebuild operation requesting a new building generation. Regenerate Go, TypeScript, and OpenAPI outputs; add the rebuild
  route to `server/router/api/v1/acl_config.go` if it introduces a new public surface.
- Settings page: index status display (active/building generation, lag, last error) and a rebuild action with an honest
  in-progress state, alongside the Stage 1 external-embedding disclosure. Status refresh never blocks Memo editing.
- Search and Chat UI: serving-mode disclosure — hybrid, lexical-only with reason, semantic rebuilding (“正在更新语义索引”),
  or semantic budget degraded — plus the Stage 2 degraded/disabled states when no embedding capability is assigned.
- Status and error surfaces carry normalized categories only: no raw query text, Memo text, provider payloads, endpoint
  secrets, or vectors.
- Server data in React Query hooks; follow existing settings-page and search-UI patterns.

## Acceptance

- An administrator sees accurate active/building generation state and lag counters, and can trigger a rebuild whose progress
  becomes visible without blocking Memo use; non-administrators cannot invoke rebuild.
- Every search response and Chat answer discloses its serving mode; a rebuild in progress shows “正在更新语义索引” rather
  than an empty or silently lexical result set.
- With no embedding capability assigned, semantic entry points degrade to the Stage 2 actionable disabled states.
- Stage 1 settings disclosure, connectivity test, and transcription controls are unchanged.

## Verification

```bash
cd proto && buf generate && buf lint
go test -v -race ./server/...
cd web && pnpm lint && pnpm test
```

Frontend tests cover serving-mode disclosure, index status rendering, rebuild action states, and degraded/disabled states.

## Comments
