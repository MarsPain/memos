# AI-Native Notes Product Specification

**Status:** Approved
**Architecture:** [AI-Native Memos Architecture](../design-docs/ai-native-notes.md)

## User Outcomes

1. I can ask questions about notes I am allowed to read and see which Memos support the answer.
2. I can find a Memo with incomplete keywords, minor spelling errors, or a description of its meaning.
3. I can ask the Agent to prepare a new Memo.
4. I can ask the Agent to improve an existing Memo without risking an invisible overwrite.
5. I can keep using Memo capture and keyword search when the AI provider or semantic index is unavailable.

## Functional Requirements

### Chat

- Conversations are private to the authenticated user.
- Messages persist until the owner deletes the conversation.
- Responses may stream and display authorized Memo citations.
- Repeated sends and failed-response retries do not duplicate the user message.
- “Save as Memo” creates a proposal.

### Search

- One query supports title, tag, and body matching.
- Partial keywords and minor spelling variations return explainable results.
- Semantic search participates only when a compatible embedding index is available.
- Keyword and semantic candidates are fused into one result list.
- Results include a snippet, matching reason, and Memo navigation target.
- Results disclose when a resource budget made retrieval partial or degraded.
- The first searchable corpus contains `NORMAL`, top-level Memos; comments, archived Memos, attachment bodies, and Chat history are excluded.
- Newly written Memos may remain absent from fuzzy/semantic results until background indexing catches up; direct Memo reads remain current, index lag is
  visible, and stale indexed text is never shown.

### Agent

- Read and search may execute automatically within user permissions.
- Create and update return a pending proposal.
- The Agent cannot delete a Memo in the initial product.
- The Agent cannot directly apply its own proposal.
- Create proposals contain content and visibility; update proposals change content only in the initial product.

### Proposal Review

- Show the complete proposed Memo in normal rendered or editable form.
- Show concise affected-area summaries rather than line patches.
- Offer Apply, Continue editing, and Cancel/Dismiss.
- Detect a changed target before applying an update.
- A conflict never alters the source Memo.

### Administration

- Admins configure instance providers and generation, embedding, and transcription assignments.
- Provider keys remain write-only.
- The settings page reports capability readiness and supports a bounded connectivity test.
- Before enabling external embeddings, the settings page explains that every eligible Memo chunk is sent incrementally to that provider.
- All users can see whether external AI processing is enabled; per-user opt-in is not part of the first release.
- Private-network provider endpoints require an explicit administrator opt-in.
- Embedding configuration changes expose rebuild status without blocking use.

## Non-Functional Requirements

- Baseline deployment uses the existing Memos process and database only.
- SQLite, MySQL, and PostgreSQL provide equivalent product behavior.
- Provider and index failures do not block Memo CRUD.
- Private content is minimized before external-provider transmission.
- Citation content is reauthorized and revision-checked immediately before it is returned or sent to a provider.
- Required automated tests do not call live model providers.
- Existing external MCP behavior is unchanged.

## UX Language

Prefer user concepts such as “建议内容”, “应用”, “继续调整”, “来源”, and “正在更新语义索引”. Avoid “patch”, “hunk”, “commit”, “staging”, vector-database terminology, and raw provider error payloads in the primary UI.

## Acceptance Scenarios

### Semantic fallback

Given generation is configured but embedding is not, when a user asks about their notes, the system uses fuzzy keyword retrieval and returns an answer with citations or explains that no supporting notes were found.

### Unauthorized source

Given the index contains a stale reference to a Memo the user can no longer read, when retrieval selects it as a candidate, the source is removed before snippets or model context are produced.

### Stale citation

Given an indexed chunk references an older Memo revision, when retrieval prepares a snippet or model context, the system rereads the current Memo and
regenerates or discards the citation instead of returning stale indexed text.

### Safe update

Given an update proposal was created from revision A and the user edits the Memo to revision B, when the proposal is applied, the system reports a conflict and preserves revision B.

### Duplicate confirmation

Given two clients apply the same pending proposal concurrently, when one transaction commits, both clients receive the same resulting Memo and only one
Memo mutation exists.

### Rebuild isolation

Given a replacement embedding generation is still building, when a semantic query runs, the system uses only the previous complete active generation
when it remains callable; otherwise it uses lexical retrieval and reports semantic search as rebuilding.

### Retrieval budget

Given a corpus exceeds a configured semantic scan or wall-clock budget, when search runs, it discards the incomplete vector candidate set and returns
lexical results with a degraded reason rather than ranking an arbitrary vector-row prefix or claiming complete semantic coverage.

### Index lag

Given a Memo has committed but its derived search document is stale, when search runs, the system reports index lag and may omit that Memo; it never
returns the older indexed text, and direct navigation returns the current Memo.

### Provider outage

Given the generation or embedding provider is unavailable, when the user creates or edits a normal Memo, the operation succeeds and indexing catches up later.
