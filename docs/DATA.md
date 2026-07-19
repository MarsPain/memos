# Data Context

## Ownership

| Record | Owner | Source or derived | Retention |
| --- | --- | --- | --- |
| Memo | Memo creator | Source | Existing Memo lifecycle |
| AI conversation | Authenticated user | Source | Until user deletion or account deletion |
| AI message | Conversation owner | Source | Follows conversation |
| AI proposal | Authenticated user | Source workflow record | Expire pending proposals; retain minimal applied metadata per policy |
| AI search document | Search subsystem | Derived | Rebuildable; remove with source Memo |
| AI index generation | Indexing subsystem | Derived control state | Retire and remove after bounded rebuild grace period |
| AI index chunk | Indexing subsystem | Derived | Rebuildable; remove with source Memo |

Chat history is not a Memo and is not included in Memo search. A user may explicitly create a Memo through a confirmed proposal.

## Implemented AI Foundation State

Stage 1 adds no relational schema. The existing instance-setting record stores the provider pool, transcription assignment, generation assignment,
embedding assignment and optional dimensions, external-processing acknowledgement, per-capability readiness, and the private-network policy marker.
Provider API keys remain in that write-only setting payload and are removed from API reads. Deployment-supplied AI settings shadow stored settings
through the existing file-backed configuration lifecycle and are not copied into database tables.

## Planned AI Records

### Conversation

Stores owner, title, and timestamps. Conversations are private to their owner and persist until deletion in the first release. Ephemeral or
history-disabled conversations require a later retention design.

### Message

Stores conversation, role, visible content, lifecycle status, model metadata, usage, a client request ID, attempt relationships, and authorized Memo
references. A request ID is unique within its conversation. Retrying a failed response adds an assistant attempt linked to the existing user message;
it does not duplicate the user message.

Messages do not store hidden model reasoning. Citation records contain a Memo resource name, source revision and hash, and source span without becoming
a second canonical copy of a Memo. Citation text is rehydrated from the current authorized source and discarded or regenerated when its revision is
stale.

### Proposal

Stores create/update kind, owner, optional conversation, optional target Memo, proposed fields, target base revision for updates, status, expiry, and
the applied Memo reference. First-release create proposals contain content and visibility. Update proposals change content only.

Proposal states are `PENDING`, `APPLIED`, `DISMISSED`, and `EXPIRED`. Applying performs the Memo revision compare-and-swap and the proposal transition
in one database transaction. Repeating a successful Apply returns the existing applied result. Conflicts and rollbacks leave both source Memo and
proposal unapplied.

### Memo revision

Before Stage 4, Memo storage gains a monotonic revision. Every user-visible mutation, including content, visibility, state, pinning, location,
attachments, and relations, increments it. Update proposals capture the base revision and can apply only with a conditional update against that exact
revision. Existing update timestamps are user-settable and are not a concurrency token.

### Search document

Stores source Memo identity and revision, source content hash, corpus-projection and normalization versions, normalized title/tags/body text, and the
mapping from projected fields back to source spans. Search documents are provider-independent and power exact/partial/fuzzy retrieval when embeddings
are disabled or rebuilding.

### Index generation

Stores the full embedding fingerprint, configured provider reference and non-secret model identity, lifecycle state (`BUILDING`, `ACTIVE`, or
`RETIRED`), progress counters, and timestamps. At most one generation is active. Building generations never serve queries. Promotion and retirement
are atomic and conditional on the desired fingerprint; superseded or disabled building generations are abandoned and removed.

### Index chunk

Stores generation, source Memo identity and revision, chunk ordinal, search-document projection span and source-span mapping, embedding bytes,
dimensions, and index timestamp. The generation fingerprint already owns provider, model, corpus projection, chunking, normalization, and encoding
identity, so chunks do not redefine those fields or duplicate the complete search document.

Embedding vectors contain finite L2-normalized IEEE 754 float32 values in documented little-endian order that works with BLOB/BYTEA columns in all
supported databases. Dimension and byte-length validation occurs before commit. Rows from different generations or dimensions never participate in the
same similarity calculation.

## Index Lifecycle

- Source Memo writes succeed independently of index writes.
- Create and update mark a Memo's search document and embedding chunks stale; delete removes or tombstones all derived rows.
- Search-document projection is local and remains available without an embedding provider.
- A batch runner uses keyset pagination, bounded batches, cancellation, per-item backoff, and idempotent upserts to reconcile stale and missing search
  documents and chunks.
- Changing embedding configuration creates a non-queryable building generation. Projection/normalization changes rebuild search documents and any
  configured embedding generation.
- A complete verification pass promotes the building generation atomically; the previous generation remains active when callable until that point.
- If the previous model is unavailable, semantic retrieval pauses during the build instead of querying partial new data.
- Keyword retrieval remains available when embeddings are absent or stale.
- Operators can delete all derived AI index data and rebuild it from Memos.

## Authorization

Index metadata may prefilter candidates by creator and visibility, but it is never an authorization source. Retrieval rechecks candidates through current Memo access rules immediately before returning content or sending context to a provider.

The first searchable corpus contains `NORMAL`, top-level Memos only. It projects the H1 title, extracted tags, and Markdown plain text. Archived Memos,
comments, attachment bodies, and Chat history are excluded. Any corpus, normalization, chunking, or encoding change increments the corresponding
fingerprint version and rebuilds derived data.

## Migration Rules

Every AI schema change must update SQLite, MySQL, and PostgreSQL migrations and each driver's `LATEST.sql`. Fresh installations and incremental upgrades must produce equivalent schemas. Account deletion must remove conversations, messages, proposals, search documents, and embedding chunks owned by or exclusively derived from the account. Memo and account deletion cleanup covers every active, building, and retired generation.
