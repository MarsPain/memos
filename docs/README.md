# Memos Engineering Documentation

This directory is the canonical record for product, architecture, security, and data knowledge. Root documents are navigation entrypoints; detailed context belongs here, while the configured issue tracker owns work state.

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
| [PLANS.md](PLANS.md) | Historical plan collections and status-ownership boundary |
| [configuration-provisioning.md](configuration-provisioning.md) | Current deployment-configuration design and runtime contract |

## AI-Native Notes Initiative

- [Architecture design](design-docs/ai-native-notes.md)
- [Product specification](product-specs/ai-native-notes.md)
- [Stage 1 — AI Foundation specification](product-specs/ai-foundation.md)
- [Stage 2 — AI Chat and Retrieval specification](product-specs/ai-chat-retrieval.md)

## Documentation Collections

- [`agents/`](agents/) contains operational configuration for repository-aware skills:
  [issue tracker](agents/issue-tracker.md), [triage labels](agents/triage-labels.md), and [domain docs](agents/domain.md).
- [`design-docs/`](design-docs/) contains canonical subsystem designs.
- [`product-specs/`](product-specs/) contains user flows and acceptance requirements.
- [`generated/`](generated/) is reserved for reproducible generated documentation.
- [`references/`](references/) contains supporting material that is not a source of product truth.

## Legacy Collections

Existing [`plans/`](plans/) and [`superpowers/`](superpowers/) documents remain historical implementation records. They do not own current work state. New work is represented by a canonical product spec and linked issues in the [configured tracker](agents/issue-tracker.md).

## Validation

Run:

```bash
python3 scripts/validate_docs.py
python3 -m unittest tests.test_docs_validation
```

The validator discovers active documentation capabilities, then checks entrypoint size, internal links, canonical reachability, context and ADR scopes,
agent-configuration ownership, issue/spec boundaries, and legacy-plan isolation.
