# LookupKB — Planning Documentation

LookupKB is a lightweight, self-hostable **Knowledge Base** system: a flat (folderless)
collection of indexed Markdown documents with first-class technical rendering
(code, diagrams, math, chemistry), fast typo-tolerant search, private drafts, and
group-scoped access control. Fully open-source, hardened, and Docker-deployable.

## Design decisions (locked)

| Layer | Choice | Rationale doc |
|---|---|---|
| Frontend | SvelteKit + CodeMirror 6 (live-preview) | [02](02-tech-stack.md) |
| Backend | Go + Fiber (v2) | [02](02-tech-stack.md) |
| Observability | OpenTelemetry (OTLP) + coherent `slog` logging | [01](01-architecture.md), [02](02-tech-stack.md) |
| Identity / groups | Authentik (OIDC) | [02](02-tech-stack.md), [06](06-security.md) |
| Search / indexing | Meilisearch | [05](05-search-and-indexing.md) |
| Database | PostgreSQL | [03](03-data-model.md) |
| Object storage | Garage (S3-compatible) | [02](02-tech-stack.md) |
| Diagrams | Kroki (Mermaid, PlantUML/UML, GraphViz, …) | [02](02-tech-stack.md) |
| Math / chemistry eq. | KaTeX + mhchem | [02](02-tech-stack.md) |
| Molecular structures | SmilesDrawer (SMILES) | [02](02-tech-stack.md) |
| Reverse proxy / TLS | Caddy | [01](01-architecture.md) |
| Deployment | Docker Compose | [01](01-architecture.md) |
| Collaboration | Single-author drafts (multiplayer out of scope) | [00](00-overview.md) |
| Versioning | Published-version history + restore | [03](03-data-model.md) |
| Public API | Read-only REST + OpenAPI 3; PATs + Authentik service accounts | [10](10-public-api.md) |

## Documents

1. [00 — Overview, goals & scope](00-overview.md)
2. [01 — System architecture & deployment](01-architecture.md)
3. [02 — Technology stack & libraries](02-tech-stack.md)
4. [03 — Data model & access control](03-data-model.md)
5. [04 — Functional spec & acceptance criteria](04-functional-spec.md)
6. [05 — Search & indexing design](05-search-and-indexing.md)
7. [06 — Security & threat model](06-security.md)
8. [07 — Test book](07-test-book.md)
9. [08 — Roadmap & milestones](08-roadmap.md)
10. [09 — Open questions](09-open-questions.md)
11. [10 — Public API (search / query / retrieve)](10-public-api.md)

## Status

Planning / pre-implementation. These documents are the source of truth for the spec.
Open decisions are tracked in [09-open-questions.md](09-open-questions.md).
