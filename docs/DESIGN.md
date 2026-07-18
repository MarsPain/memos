# Design Contracts

This document defines system-wide design rules. Subsystem decisions belong in `docs/design-docs/` and must link back here when they introduce a new module or seam.

## Deployment Contract

Memos remains useful as a single Go process backed by SQLite, MySQL, or PostgreSQL. Optional integrations may improve scale or quality, but core product capabilities must not require an additional service unless the architecture and deployment contract are deliberately revised.

The AI-native initiative preserves this rule: Chat, Agent proposals, fuzzy search, and semantic search operate with the existing application and database. External vector stores are optional future adapters, not baseline dependencies.

## Module Contract

A module should present a small interface that hides meaningful behavior. Place seams where behavior actually varies:

- Provider differences belong behind model adapters.
- Database differences belong behind store drivers.
- Shared Memo authorization, validation, and mutation behavior belongs behind the Memo application module.
- Chat and Agent orchestration belong in an application module, not in HTTP handlers.
- API v1 and AI callers reuse the same Memo application interface; AI code must not reproduce authorization or call handlers in-process.
- Generated protobuf and OpenAPI outputs never become manually maintained sources of truth.

Callers and tests should use the same interface. Dependencies are injected, results are returned explicitly, and side effects are concentrated behind named operations.

## Data Contract

Primary records and derived records are distinct:

- Memos, users, conversations, messages, and proposals are primary records.
- Search chunks and embeddings are derived records that may be deleted and rebuilt.
- A failure to update derived data must not block a successful Memo write.
- A building embedding generation is not queryable; promotion to active is atomic.

Schema changes must include migrations and fresh-install SQL for all three database drivers.

## API Contract

Public behavior is defined in `proto/api/v1/`. Handlers translate transport requests into application operations; they do not own provider-specific logic or persistence algorithms.

New unauthenticated endpoints require explicit ACL configuration. AI Chat, search, conversations, and proposal operations require authentication.

## AI Interaction Contract

- Read and search operations may execute automatically within the current user's permissions.
- Create and update operations produce a proposal first.
- Apply is not a model tool. Applying a proposal requires an authenticated user confirmation and an atomic Memo-revision compare-and-swap plus
  proposal-state transition.
- The default UI presents a result preview and concise change summary, not a Git-style patch.
- Delete and unattended bulk mutation are outside the first AI-native scope.
- The external MCP module remains independent from the in-product AI application.

See the [AI-native architecture design](design-docs/ai-native-notes.md) for the approved implementation direction.
