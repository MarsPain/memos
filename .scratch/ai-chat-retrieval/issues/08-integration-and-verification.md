# Integrate And Verify AI Chat And Retrieval

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 03, 04, 06, 07

## Outcome

Stage 2 is fully wired into application composition, canonical docs reflect the implemented behavior, no scaffolding from issue 01
remains, and the full Stage 2 verification gate passes with Memo capture, existing Memo API, and external MCP behavior unchanged.

## Scope

- Confirm `server/ai` conversation, retrieval, streaming Chat, and unified search use cases are composed behind authenticated
  routes; keep embedding generations, proposals, and Agent endpoints absent (Stages 3 and 4).
- Grep for and remove any leftover scaffold markers from issue 01 (TODOs referencing issues 03/06).
- Update `ARCHITECTURE.md`, `docs/BACKEND.md`, `docs/DATA.md`, `docs/SECURITY.md`, the AI design doc, and `docs/ROADMAP.md` where
  implemented details differ; keep the parent spec's benchmark section in sync with issue 07.
- Run the parent spec's System Verification suite.
- Inspect the final diff for compatibility, generated artifacts, credential exposure, and scope expansion.

## Acceptance

- Every acceptance criterion in the parent spec has supporting automated or documented manual verification.
- Existing Memo API and MCP tests pass unchanged; `git diff -- server/router/mcp/` is empty.
- Canonical docs describe implemented behavior without copying task state.
- Stage 3 can build on the versioned search-document projection without rework.

## Verification

Run every command under the parent spec's **System Verification** section, then inspect `git diff -- server/router/mcp/` and
`git diff --check`.

## Comments

Implemented: composition audit confirms every AI use case sits behind authenticated routes (`server/router/api/v1/v1.go` wires
chat, search documents, and the retriever; `acl_config.go` has no AIService entries in `PublicMethods`/`AuthBootstrapMethods`;
handlers re-check the principal) and no embedding-generation, proposal, or Agent endpoints exist. Scaffolding: no scaffold/TODO
markers from issue 01 remain in code (`server/ai/retrieval.go` scaffold A is gone); one stale test comment referencing the old
scaffold was reworded (`server/ai/chat_test.go`). Docs synced to implemented behavior: `ARCHITECTURE.md` (Stage 2 section, module
rows for `server/memo`, `server/ai`, `server/ai/search`), `docs/BACKEND.md` (read seam delivered, search-document reconciliation
wired as a background runner), `docs/DATA.md` (implemented Stage 2 records section; authorization section now states candidates
are reauthorized without a visibility prefilter), `docs/SECURITY.md` (owner-scoped conversations, persisted-excerpt boundary,
no-generation degradation), `docs/design-docs/ai-native-notes.md` (§4.3 tense, §7.2 no-prefilter wording, SQLite-only envelope
note), and `docs/ROADMAP.md` (Stage 2 delivery-spec link, benchmark bullet annotated SQLite-recorded/MySQL+PostgreSQL-pending).

Found and fixed during integration: account deletion leaked AI rows — `DeleteUser` now removes the account's `ai_conversation`/
`ai_message` rows and the `ai_search_document` rows of its deleted memos in the deletion transaction on all three drivers,
covered by `TestDeleteUserCleansRelatedData`. This aligns the code with the `docs/DATA.md` account-deletion contract.

Verification: `python3 scripts/validate_docs.py` and `python3 -m unittest tests.test_docs_validation` pass; `cd proto &&
buf generate && buf lint` clean with no generated diff; `go test -race ./internal/...` and `go test -race ./server/...` pass;
`go test ./store/...` passes (SQLite; MySQL/PostgreSQL driver suites skip — no Docker runtime locally, CI runs them); `cd web &&
pnpm lint && pnpm test && pnpm build` passes (241 tests). `git diff main...HEAD -- server/router/mcp/` is empty and
`git diff --check` is clean; no credentials or provider keys appear in the branch diff.

Residual: the only open Stage 2 item is issue 07's MySQL/PostgreSQL benchmark evidence, which needs a Docker-capable
environment (fixtures ready in `server/ai/search/benchmark_test.go`). The spec's budget defaults stay provisional for those two
databases until recorded there; everything else in this issue's scope is done. Status stays `ready-for-agent` for that residual
environment-dependent step rather than `resolved`.

Known limitation (out of scope): account deletion removes conversation and message rows but cannot cancel an attempt streaming
on another goroutine the way single-conversation deletion does; a finalize racing the deletion affects zero rows or is rejected
by the message foreign key, and restart reconciliation still settles interrupted `STREAMING` attempts.

Review follow-ups (two-axis review): both axes clean; added a comment explaining the two-message assertion in
`TestDeleteUserCleansRelatedData`; the account-deletion/in-flight-attempt edge above was noted by review and is documented
rather than engineered.
