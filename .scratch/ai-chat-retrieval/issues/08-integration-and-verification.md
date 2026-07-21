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
