# Memos

Memos is a self-hosted note-taking app. This glossary pins the domain vocabulary shared by the server, the web client, and the AI
module; use these exact terms in code, specs, and issues.

## Language

### Core

**Memo**:
A single note owned by a user, with Markdown content, visibility, and state. The unit of authorization, search, and citation.
_Avoid_: note, post, entry

**Visibility**:
A Memo's audience rule (private, workspace, or public), checked at read time — never trusted from a cached or indexed snapshot.
_Avoid_: permission, access level

### AI Conversations

**Conversation**:
A private, persistent Chat session owned by one user. It lives until its owner deletes it and is never indexed as a Memo.
_Avoid_: chat session, thread

**Message**:
One persisted turn in a Conversation, with a role, visible content, and a status (`STREAMING`, `COMPLETE`, `FAILED`, `CANCELLED`).
_Avoid_: chat entry, prompt/response pair

**Attempt**:
One assistant effort to answer a specific user Message. Retrying a failed answer creates a new Attempt linked to the same Message,
never a duplicate Message.
_Avoid_: retry, regeneration

**Citation**:
A traceable reference from a search result or Chat answer to a Memo: resource name, source revision and hash, and a source span.
Reauthorized and revision-checked immediately before its text leaves Memos.
_Avoid_: reference, footnote, link

### Retrieval And Indexing

**Corpus**:
The searchable set: `NORMAL`, top-level Memos. Excludes archived Memos, comments, attachment bodies, and Chat history.
_Avoid_: document set, index

**Search Document**:
Provider-independent derived data: a normalized title/tags/body projection of a Memo, with projection/normalization versions and a
source-span mapping. Rebuildable from source; powers lexical and fuzzy retrieval.
_Avoid_: index entry, embedding document

**Text Generation**:
A configured model capability that produces or streams text. Chat depends on it.
_Avoid_: generation (unqualified — ambiguous with Index Generation), completion, LLM call

**Index Generation**:
A versioned build of embedding vectors over the Corpus, identified by a configuration fingerprint, with `BUILDING`, `ACTIVE`, or
`RETIRED` state. At most one is `ACTIVE`; a building one never serves queries.
_Avoid_: generation (unqualified), embedding version, snapshot

### Agent (Stage 4)

**Proposal**:
A pending, user-confirmable create/update of a Memo, owned by a user and carrying a base revision. The only path by which AI output
can become a write; applying always requires explicit user confirmation.
_Avoid_: suggestion, draft, patch
