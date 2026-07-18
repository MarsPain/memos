# Memos Architecture

This file is the top-level architecture map. Canonical design rules and subsystem detail live under [`docs/`](docs/README.md).

## System Shape

Memos is a single deployable Go application with a React SPA and one configured relational database.

```text
React SPA
   │ Connect RPC / REST / SSE
   ▼
Echo HTTP server
   ├── API v1 services
   ├── file and frontend routes
   ├── MCP endpoint for external agents
   └── background runners
           │
           ▼
       Store facade
           │
   SQLite / MySQL / PostgreSQL
```

Protocol Buffer sources define the public service contracts. Generated Go, TypeScript, and OpenAPI outputs are build artifacts, not hand-edited source.

## Major Modules

| Module | Current location | Responsibility |
| --- | --- | --- |
| Server composition | `server/server.go` | HTTP topology, route registration, runner lifecycle |
| API services | `server/router/api/v1/` | Authentication-aware application operations |
| External MCP | `server/router/mcp/` | Curated external tool surface derived from OpenAPI |
| Model adapters | `internal/ai/` | Provider-specific AI transports and transcription |
| Store | `store/` | Persistence facade, cache, migrations, database adapters |
| Web application | `web/src/` | React UI, client state, Connect clients |
| Contracts | `proto/` | Public and internal protobuf source |

## Planned AI-Native Module

The approved AI-native direction adds a shared `server/memo` application module for Memo authorization and commands plus an in-process `server/ai`
module for Chat, hybrid retrieval, conversations, indexing, and confirmation-gated Agent proposals. Provider transports remain under `internal/ai`.
The existing external MCP module is explicitly unchanged.

Read the canonical [AI-native architecture design](docs/design-docs/ai-native-notes.md), [product specification](docs/product-specs/ai-native-notes.md), and [security rules](docs/SECURITY.md) before implementing AI work.

## Canonical References

- [Design and dependency rules](docs/DESIGN.md)
- [Backend module map](docs/BACKEND.md)
- [Frontend state and UI rules](docs/FRONTEND.md)
- [Data ownership and migration rules](docs/DATA.md)
- [Security and trust boundaries](docs/SECURITY.md)
- [Historical plan index](docs/PLANS.md)
- [Roadmap](docs/ROADMAP.md)
