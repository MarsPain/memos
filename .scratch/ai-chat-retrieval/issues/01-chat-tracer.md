# Tracer: End-To-End Chat Skeleton

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: none

## Outcome

A user can open a Chat page, send a message, and receive an answer that cites real, authorized Memos — the thinnest path through
UI → `AIService` → `server/memo` read seam → retrieval → `internal/ai` text generation. Every simplified layer is real except the
two explicitly marked scaffolds below, which later issues replace.

## Scope

- Minimal `server/memo` module: authorized read of a single Memo and enumeration of the current user's `NORMAL` top-level Memos.
  This seam is real and grows in issue 02; do not extract the full handler read logic yet.
- **Scaffold A — retrieval:** naive substring match over enumerated Memos, top 3, snippets generated from current source. Always
  fresh, so no staleness handling exists yet. Replaced by issue 06.
- **Scaffold B — conversation state:** process-local in-memory message list, lost on restart. No request IDs, attempts, or
  statuses. Replaced by issue 03.
- Additive `AIService` proto with a unary (non-streaming) SendMessage operation plus conversation-read operations sufficient for
  the page; regenerate Go, TypeScript, and OpenAPI outputs.
- Chat flow: persist nothing; retrieve top 3, build instructions + question + quoted context (trivial cap, e.g. 3 Memos),
  generate through the Stage 1 provider-neutral interface, return answer + citations.
- Minimal web Chat page: one thread, send box, answer rendering, citation chips linking to source Memos. React Query hooks, `@/`
  imports, Tailwind v4.
- No generation model configured: the page shows an actionable unavailable state.

## Explicitly Out Of Scope (later issues)

- Persistence, request-ID idempotency, attempts, conversation list (03). Streaming and cancellation (04). Search documents,
  fuzzy matching, budgets, revision-checked citations, unified search UI (05, 06). Benchmarks (07).

## Acceptance

- The full path works on a configured instance and is demoable; history is lost on restart (documented, not a bug at this stage).
- Answers never cite a Memo the current user cannot read (the minimal seam enforces this).
- Existing Memo API and MCP tests pass unchanged; no diff under `server/router/mcp/`; no schema migration.
- The two scaffolds are visibly marked in code (TODO referencing issues 03 and 06).

## Verification

```bash
cd proto && buf generate && buf lint
go test -v -race ./server/...
cd web && pnpm lint && pnpm test
```

Seam and RPC tests use deterministic model fakes; no live provider calls.
