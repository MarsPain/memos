# Replace The Conversation Scaffold With Real Persistence

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

Conversations and messages survive restart: private, owner-scoped persistence with client request IDs, assistant attempts, and
message statuses replaces scaffold B from issue 01. Duplicate sends/retries never duplicate user messages.

## Scope

- Add `ai_conversation` (owner, title, timestamps) and `ai_message` (conversation, role, visible content, status, model metadata,
  usage, citations, client request ID, attempt relationship) migrations for SQLite, MySQL, and PostgreSQL plus `LATEST.sql`.
- Implement the owner-scoped store layer; conversations persist until the owner deletes them.
- Enforce request-ID uniqueness within a conversation: repeats return the existing user message and its active/completed attempt.
- Model attempts: retrying a failed answer creates a new attempt linked to the same user message, never a duplicate.
- Message statuses: `STREAMING`, `COMPLETE`, `FAILED`, `CANCELLED`. Hidden provider reasoning is neither requested nor stored.
- Deleting a conversation while an attempt is active cancels that attempt.
- Remove scaffold B (in-memory state) and route the issue-01 Chat flow through the store; add conversation list and deletion to
  the existing Chat page (still non-streaming).

## Acceptance

- Fresh-install and incremental migrations are equivalent on all three drivers.
- History survives restart; conversation reads are owner-scoped with no cross-user path.
- Duplicate request IDs never duplicate user messages; retries link new attempts to the same message.
- Chat history is stored apart from Memos and is never indexed as Memos.

## Verification

```bash
go test -v ./store/...
go test -v -race ./server/...
```

Persistence tests cover complete/failed/cancelled/retry flows, duplicate request IDs, and deletion during an active attempt.

## Comments

Implemented in 2b91caea: ai_conversation/ai_message migrations for SQLite, MySQL, and PostgreSQL plus LATEST.sql;
owner-scoped store layer with atomic user-message-plus-attempt creation and cascading deletion; chat flow routed
through the store with request-ID idempotency (repeats return the existing pair, retries add attempts to the same
user message), statuses STREAMING/COMPLETE/FAILED/CANCELLED, delete-cancels-active-attempt, and restart
reconciliation of interrupted attempts to FAILED; CreateChatConversation/ListChatConversations/
DeleteChatConversation RPCs; Chat page conversation list with create/delete. Scaffold B removed.

Notes for issue 04: a delete landing between conversation lookup and attempt registration cannot cancel that send
(documented in server/ai/chat.go); on drivers with foreign keys disabled it may leave orphaned ai_message rows,
which all read paths ignore. Harden with the streaming/cancellation lifecycle work.
