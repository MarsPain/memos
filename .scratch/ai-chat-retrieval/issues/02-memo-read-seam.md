# Complete The Shared Memo Read Seam

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

All Memo read authorization, visibility and archived-state rules, and searchable-source enumeration live behind the `server/memo`
module started in issue 01, consumed by both the existing MemoService RPC handlers and AI code, with zero behavior change.

## Scope

- Move the remaining read authorization, visibility/archived-state filtering, and searchable-source enumeration out of API v1
  handlers into `server/memo`, growing the module interface from issue 01 rather than creating a parallel one.
- Rewire the existing MemoService RPC handlers to call the module.
- Keep Store as raw persistence; the module is the only authorization interface for reads.
- Do not extract the mutation path (Stage 4 scope) and add no AI-visible behavior.

## Acceptance

- Handlers contain no authorization logic; AI retrieval and Memo RPCs share one enforcement path.
- Existing Memo API and permission tests pass unchanged.
- No new endpoints or schema migrations; no diff under `server/router/mcp/`.

## Verification

```bash
go test -v -race ./server/...
```

Run the existing Memo service test suite before and after and confirm identical results.

## Comments

Implemented in 5540febe: Memo read authorization, visibility and archived-state list rules, related-memo visibility filter, and searchable-source enumeration moved into `server/memo` behind CheckReadAccess, CheckRelatedReadAccess, ApplyReadScope, ReadableMemoFilter, and ListReadableMemos; MemoService read handlers, attachment access checks, and user-stats memo scoping rewired to the seam; store remains raw persistence. No new endpoints, no schema migration, no diff under `server/router/mcp/`.
