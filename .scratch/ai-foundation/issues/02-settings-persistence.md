# Persist AI Capability Assignments

Parent spec: [AI Foundation](../../../docs/product-specs/ai-foundation.md)
Type: task
Status: resolved
Blocked by: 01

## Outcome

Generation and embedding assignments round-trip through stored settings, deployment configuration, API conversion, and caches while preserving
credentials and transcription behavior.

## Scope

- Persist both assignments through the existing instance AI setting path.
- Support equivalent file-backed deployment configuration.
- Preserve stored API keys when update requests omit replacements.
- Reject missing providers, unsupported capability/provider combinations, invalid models or dimensions, unsafe endpoints, and unauthorized private
  destinations.
- Keep masked API response behavior consistent across stored and deployment-supplied settings.
- Add upgrade fixtures for legacy custom transcription endpoints and fresh-configuration fixtures proving private access defaults to denied.

## Acceptance

- Stored, deployment-supplied, converted, and cached settings produce the same effective assignments.
- Partial updates do not erase credentials.
- Existing transcription-only configurations continue to round-trip.
- This slice introduces no database schema migration because it uses the existing setting storage path.

## Verification

- Run targeted store and API setting tests while iterating.
- Run `go test -v -race ./server/...` before resolving.
- Run the relevant deployment-configuration tests.

## Comments

Implemented stored/deployment round-trip, credential preservation, assignment/endpoint validation, readiness persistence, and legacy custom-transcription endpoint policy migration without schema changes.
