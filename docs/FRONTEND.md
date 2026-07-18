# Frontend Context

## State Ownership

Server state belongs in React Query hooks under `web/src/hooks/`. UI-only state belongs in component state or a focused context. Generated protobuf TypeScript under `web/src/types/proto/` is never edited manually.

## AI Surfaces

The AI-native product adds three related surfaces:

- Chat: a user-private conversational workspace with citations.
- Search: one entrypoint for keyword, fuzzy, and semantic results.
- Proposal preview: confirmation for AI-created or AI-updated Memos.

These surfaces share server-backed conversation and retrieval state but must not place Chat messages into the Memo timeline.

## Lightweight Proposal Interaction

Proposal review is deliberately not a Git diff:

- render the proposed Memo as the user would read it;
- summarize affected areas with short labels such as “正文已调整” or “新增 2 个标签”;
- provide Apply, Continue editing, and Cancel actions;
- hide hashes, patches, hunks, and line-level `+/-` notation;
- report a friendly conflict if the source Memo changed after the proposal was created.

## Streaming and Failure States

Chat may stream assistant text. Persist the user message and assistant attempt atomically before model execution. A client request ID makes duplicate
sends idempotent; retry creates another assistant attempt without duplicating the user message. Citations become interactive only after their Memo
references have passed server authorization and revision checks.

When semantic indexing is disabled or rebuilding, keep Chat and fuzzy keyword search available and show a quiet status indicator rather than a blocking error. Search also distinguishes complete, partial, and degraded responses when a resource budget is exhausted.

Before an administrator enables external embeddings, explain that background indexing incrementally sends every eligible Memo chunk to that provider.
All users can see whether external AI processing is enabled. Private-network endpoints require a separate explicit administrator opt-in.

## Accessibility and Responsiveness

AI controls must be keyboard reachable, preserve focus while streaming, announce completion and errors, and work in the existing responsive layout. Reuse current UI primitives before adding new ones.
