# AI-Native Roadmap

The roadmap defines capability order and go/no-go criteria. Task-level work belongs in [execution plans](PLANS.md).

## Stage 0: Harness and Architecture

Status: complete.

- Establish canonical context documents and validation.
- Approve the AI module, product boundary, data ownership, and MCP non-interference rule.

## Stage 1: Model Foundation

- Add generation and embedding capability configuration alongside transcription.
- Introduce provider-neutral generation and embedding interfaces.
- Add provider capability checks, a shared hardened transport policy, explicit external-processing disclosure, and a bounded settings test action.

Go/no-go: model calls are cancellable and bounded, secrets remain write-only, endpoint/private-network policy is enforced, external embedding disclosure is
visible, configuration round-trips safely, and existing transcription behavior is unchanged.

## Stage 2: Read-Only Chat and Fuzzy Retrieval

- Add private conversations and message persistence.
- Add streaming Chat.
- Add authorized keyword/partial/fuzzy search with citations.
- Add provider-independent derived search documents and corpus-projection versioning.
- Extract the shared Memo read/authorization seam from API v1 handlers.
- Establish benchmarked scan, memory, candidate, context, and wall-clock budgets for all databases.

Go/no-go: Chat cannot mutate Memos, duplicate sends/retries do not duplicate user messages, all citations reauthorize and revision-check their source,
budget exhaustion is reported honestly, and provider failure does not affect Memo capture.

## Stage 3: Semantic and Hybrid Retrieval

- Add portable embedding generation/chunk storage and validated vector encoding over the Stage 2 search-document projection.
- Add bounded reconciliation, atomic generation cutover, vector similarity, and hybrid ranking.
- Add index status and rebuild controls.

Go/no-go: all databases meet the documented benchmark envelope, building generations never serve queries, a model/projection change cuts over atomically,
stale citations do not leave Memos, and keyword fallback stays available.

## Stage 4: Confirmation-Gated Agent

- Add create/update tools that produce proposals.
- Extract the shared Memo mutation seam and add monotonic Memo revisions.
- Add lightweight preview, concise change summary, revision conflict detection, and transactional apply flow.
- Add proposal lifecycle logs and recovery tests.

Go/no-go: Apply is absent from the model tool set, proposal and Memo state commit atomically, duplicate confirmations are idempotent, concurrent edits
cannot be overwritten, and no delete capability is exposed.

## Later Candidates

- User-level BYOK.
- Optional high-scale vector index adapters.
- Attachment text extraction.
- Scheduled personal reviews and automation policies.
- Scoped Agent capabilities beyond create/update.

These candidates require separate designs and do not belong to the approved first architecture.
