# Generation Lifecycle And Atomic Cutover

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: ready-for-agent
Blocked by: 03

## Outcome

Changing the embedding model or configuration never serves a half-built index: a new generation builds invisibly, promotes
atomically once fully verified, and queries fall back honestly when no complete generation is callable — replacing the tracer's
single-generation scaffold.

## Scope

- Full lifecycle state machine: `BUILDING`, `ACTIVE`, `RETIRED`; at most one `ACTIVE`; `BUILDING` never serves queries.
- A complete verification pass confirming every eligible Memo observed in the pass has current chunks, then atomic promotion
  in a single conditional store transaction; the former `ACTIVE` becomes `RETIRED` and is removed after a bounded grace period.
- Promotion is conditional on the generation still matching the desired embedding assignment; a newer desired fingerprint
  supersedes and removes any older building generation; disabling embeddings cancels building work and disables semantic
  retrieval.
- Callability: a generation is usable only while the current provider pool still has a provider whose type and sanitized
  endpoint identity match it.
- Rebuild isolation in the query path: semantic queries use only the previous complete active generation while it remains
  callable; otherwise lexical retrieval serves the query with a machine-readable semantic-rebuilding reason.
- A corpus-projection or normalization change rebuilds search documents first and creates a new generation when embeddings
  are configured.
- Replace scaffold C from issue 01.

## Explicitly Out Of Scope (later issues)

- Operator-visible status and rebuild controls (06) — this issue lands the mechanics and internal progress counters.

## Acceptance

- A building generation never participates in semantic retrieval; cutover is atomic and queries never mix partial generations.
- Promotion does not happen when the desired fingerprint changed mid-build; the stale building generation is removed.
- Disabling embeddings mid-build cancels work and semantic queries report the disabled/rebuilding state while lexical
  continues.
- An interrupted build resumes and still promotes correctly; retired generations are removed after the grace period.
- Memo capture and lexical search remain available through every cutover path.

## Verification

```bash
go test -v -race ./server/...
go test -v ./store/...
```

Fixtures cover BUILDING/ACTIVE promotion, conditional-promotion failure on fingerprint change, superseded builders, interrupted
rebuilds, grace-period removal, disable-mid-build, and rebuild-isolation query behavior.

## Comments
