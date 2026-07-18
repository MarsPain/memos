# Execution Plans

## Lifecycle

- `active/`: approved work currently eligible for implementation.
- `completed/`: accepted work with a completion date and verification record.
- `tech-debt/`: acknowledged gaps with impact and a target stage.

Only one plan should be the default next implementation target. Moving a plan between buckets requires updating this index in the same change.

## Active

| Plan | Status | Outcome |
| --- | --- | --- |
| [0001 — AI foundation](exec-plans/active/0001-ai-foundation.md) | Ready | Capability configuration and provider-neutral generation/embedding seams |

## Completed

| Plan | Completed | Outcome |
| --- | --- | --- |
| [0000 — Harness context foundation](exec-plans/completed/0000-harness-context-foundation.md) | 2026-07-18 | Canonical context maps, AI architecture, plan lifecycle, and docs validation |

## Tech Debt

| Item | Target |
| --- | --- |
| [0001 — Legacy plan indexing](exec-plans/tech-debt/0001-legacy-plan-indexing.md) | Before broad documentation migration |

## Historical Plans

Earlier feature records remain under [`docs/plans/`](plans/) and [`docs/superpowers/plans/`](superpowers/plans/). They are historical references, not active status declarations.
