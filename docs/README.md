# Memos Engineering Documentation

This directory is the canonical record for product, architecture, security, data, and execution knowledge. Root documents are navigation entrypoints; detailed decisions belong here.

## Core Context

| Document | Purpose |
| --- | --- |
| [DESIGN.md](DESIGN.md) | System-wide design contracts and module seams |
| [BACKEND.md](BACKEND.md) | Backend topology, application modules, and integration rules |
| [FRONTEND.md](FRONTEND.md) | Frontend state ownership and AI interaction rules |
| [DATA.md](DATA.md) | Data ownership, migrations, derived indexes, and retention |
| [SECURITY.md](SECURITY.md) | Runtime trust boundaries, AI data handling, and Agent safety |
| [PRODUCT_SENSE.md](PRODUCT_SENSE.md) | Product principles, users, and success measures |
| [ROADMAP.md](ROADMAP.md) | Milestones and go/no-go criteria |
| [PLANS.md](PLANS.md) | Versioned execution-plan index and lifecycle |

## AI-Native Notes Initiative

- [Architecture design](design-docs/ai-native-notes.md)
- [Product specification](product-specs/ai-native-notes.md)
- [Active AI foundation plan](exec-plans/active/0001-ai-foundation.md)

## Documentation Collections

- [`design-docs/`](design-docs/) contains canonical subsystem designs.
- [`product-specs/`](product-specs/) contains user flows and acceptance requirements.
- [`exec-plans/`](exec-plans/) tracks active, completed, and debt work.
- [`generated/`](generated/) is reserved for reproducible generated documentation.
- [`references/`](references/) contains supporting material that is not a source of product truth.

## Legacy Collections

Existing [`plans/`](plans/) and [`superpowers/`](superpowers/) documents remain historical implementation records. New cross-cutting work uses `exec-plans/`, while still linking relevant historical designs rather than duplicating them.

## Validation

Run:

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
```

The validator checks required entrypoints, plan buckets, root-map size, internal links, and reachability of managed canonical documents.
