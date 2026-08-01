# Generation Lifecycle And Atomic Cutover

Parent spec: [AI Semantic And Hybrid Retrieval](../../../docs/product-specs/ai-semantic-retrieval.md)
Type: task
Status: resolved
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

Implemented in this commit: Scaffold C replaced with the full generation lifecycle and atomic cutover. `ai_index_generation` gained `retired_ts` (new migration `07__ai_index_generation_retired_ts.sql` for all three drivers plus `LATEST.sql`; additive column, 0 = not retired) and the store gained `PromoteAIIndexGeneration` — a single conditional transaction that promotes a generation only while it is still BUILDING and still matches the desired fingerprint, retiring every other ACTIVE generation with the retirement timestamp in the same transaction, so queries never observe zero or two ACTIVE generations (`store/ai_index_generation.go`, per-driver implementations). Lifecycle states are now store-level constants (`AIIndexGenerationBuilding/Active/Retired`). The indexer sweep (`server/ai/search/indexer.go`) drives the lifecycle: expired retired generations are removed with their chunks after a 10-minute grace period (with or without an embedding assignment); with embeddings disabled no building work runs and builders stay inert; a newer desired fingerprint supersedes and removes older builders (chunks included); a retired generation whose fingerprint is desired again resumes building with its chunks as a head start and must re-pass verification. Promotion follows the complete verification pass (every eligible memo observed has current chunks, which also clears LastError) and re-checks the desired fingerprint against the current setting first: a fingerprint that changed mid-build skips promotion and the stale builder is removed; disable mid-build leaves the builder inert. An ACTIVE generation is never demoted by a partial sweep. Query path (`server/ai/search/semantic.go`): callability no longer requires fingerprint equality — the ACTIVE generation serves while the provider pool still has a provider whose type and sanitized endpoint identity match it, embedding the query with the generation's recorded model and dimensions (rebuild isolation: the previous complete generation keeps serving while the replacement builds); otherwise lexical serves with a machine-readable reason surfaced in `Outcome.PartialReasons` — `semantic_rebuilding` (assignment configured, no callable generation) or `semantic_disabled` (assignment removed after generations existed); a never-configured instance stays byte-identical Stage 2. `SemanticSearcher.Search` now returns `(matches, reason, error)`; `fuseSemantic` propagates the reason. Chat is untouched (its retriever never attached the semantic path). Fixtures cover BUILDING/ACTIVE promotion and retirement, rebuild isolation with per-generation embed-model proof, conditional-promotion failure on a mid-build fingerprint change, superseded-builder removal, disable mid-build and mid-sweep, grace-period removal, config-revert resume without re-embedding, rebuilding/disabled reasons, and endpoint-change callability loss; store fixtures cover the conditional transaction. `go test -race ./server/...`, `go test ./store/...`, `go test -race ./internal/...` pass; no diff under `server/router/mcp/`.
