# Tech Debt 0001: Legacy Plan Indexing

**Status:** Open
**Impact:** Historical plan status is difficult to discover from the canonical plan index.
**Target:** Before any broad documentation migration.

## Context

The repository already contains feature records under `docs/plans/` and `docs/superpowers/{plans,specs}/`. Their formats predate the canonical `docs/exec-plans/` lifecycle and do not consistently declare active versus completed status.

## Constraint

Do not bulk-move or rewrite these files during AI implementation. Existing links and historical context may depend on their locations.

## Resolution Direction

- Inventory historical records and determine completion state from source and commits.
- Add a generated or curated legacy index.
- Keep redirect stubs if any document is eventually moved.
- Expand validation only after the migration avoids false orphan or status errors.
