# Convert Chat To Streaming

Parent spec: [AI Chat And Retrieval](../../../docs/product-specs/ai-chat-retrieval.md)
Type: task
Status: resolved
Blocked by: 01, 03

## Outcome

Chat answers stream incrementally over a Connect server-streaming RPC, with disconnect cancellation propagated to the provider and
persisted, and reconnecting clients reconciling against authoritative stored message state.

## Scope

- Convert the issue-01 unary SendMessage into a Connect server-streaming operation (additive proto change; regenerate Go,
  TypeScript, and OpenAPI outputs). The existing SSE hub stays dedicated to Memo live-refresh.
- Send flow per spec: atomically persist user message + assistant attempt (`STREAMING`), retrieve, build context, stream tokens,
  persist `COMPLETE`/`FAILED`/`CANCELLED` at the end.
- Propagate client disconnect to provider cancellation and persist the outcome; a reconnecting client reads stored state rather
  than assuming the stream result.
- Frontend: render tokens incrementally; streaming state stays local to the active response and reconciles with persisted state at
  completion; navigating away and back shows the authoritative stored message.
- Enforce centralized rate, timeout, concurrency, and response-size limits; normalize provider errors into safe categories and
  persist the category on failed attempts.

## Acceptance

- Tokens render incrementally and the final UI matches the persisted message exactly.
- Disconnect persists `CANCELLED`; provider failure persists `FAILED` with a normalized category; Memo capture is unaffected.
- No Chat path can create, update, or delete a Memo.
- Retry after failure streams a new attempt without duplicating the user message.

## Verification

```bash
cd proto && buf generate && buf lint
go test -v -race ./server/...
cd web && pnpm lint && pnpm test
```

Chat tests cover streaming, disconnect/reconnect reconciliation, cancellation, and failure categories with deterministic fakes.

## Comments

Implemented: SendChatMessage converted to a Connect server-streaming RPC (start/delta/complete events;
regenerated Go, TypeScript, and OpenAPI outputs; SSE hub untouched). Send flow persists the user
message + STREAMING attempt atomically, streams provider deltas, and persists COMPLETE/FAILED/
CANCELLED with normalized provider error categories on the attempt payload. Client disconnect and
conversation deletion propagate cancellation to the provider and persist CANCELLED; attempt deadline
persists FAILED/timeout; reconnecting clients reconcile via the stored conversation (2s polling while
STREAMING). Centralized limits in server/ai/limits.go: per-user send rate, global concurrency,
per-attempt timeout, and answer-size cap (response_too_large category). Delete-vs-send race from
issue 03 hardened with a deletion tombstone in the active-attempt registry (lifted again if the store
delete fails). Connect streaming handlers now authenticate via a real WrapStreamingHandler
(previously a pass-through). Frontend renders deltas incrementally into the active attempt bubble,
reconciles with the complete event plus invalidation, aborts on navigation, and retries
failed/cancelled attempts with the same request ID.
Notable decision: the google.api.http annotation was dropped from SendChatMessage because the
in-process gRPC-Gateway transport cannot serve streaming methods (it would have served Unimplemented
while OpenAPI advertised the route). Chat send is served over the Connect endpoint (which also
accepts native gRPC and gRPC-Web clients); unary chat RPCs keep their gateway routes.
golangci-lint was not installed locally; `go vet` is clean.
