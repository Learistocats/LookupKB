# 00 — Overview, Goals & Scope

## 1. Vision

LookupKB is a **fast, minimal knowledge base**. Authors write Markdown in an
Obsidian-like editor; documents are published into a flat, fully-indexed corpus that
anyone (subject to access rules) can find in milliseconds. There are **no folders and no
file tree** — organization is emergent, via metadata (tags) and search, not hierarchy.

The product bet: *organization by search + metadata beats organization by hierarchy* at
scale, and **UX speed is the feature**. Every interaction should feel instantaneous.

## 2. Core principles

1. **Search-first, not tree-first.** Discovery is driven by full-text + faceted search over tags/author/date, never a folder browser.
2. **Speed is a requirement, not a nice-to-have.** Target sub-100 ms for search and published-document reads (see [07 test book](07-test-book.md) perf budgets).
3. **Reuse, don't reinvent.** Use proven OSS for storage, identity, search, editor, and diagram rendering. LookupKB's own code is glue + the access model + the publish workflow.
4. **Draft-safe.** A document can always be saved privately, even incomplete; publishing is a deliberate, confirmed, metadata-gated act.
5. **Secure by default.** Hardened stack, least-privilege access, no secrets in the browser, everything auditable.
6. **Simple UI.** Few elements on screen, keyboard-friendly, low chrome — the content is the interface.

## 3. Primary use cases

- An engineering / research org keeps runbooks, design notes, ADRs, protocols, and reference material.
- Authors draft privately, iterate, then publish when the metadata (title, description, tags) is complete.
- Readers find documents by typing a few characters and get typo-tolerant, ranked, access-filtered results instantly.
- Admins carve out sensitive material into named groups (e.g. `security`, `hr`, `board`) while everything else stays org-wide readable.

## 4. Actors / roles

| Role | Capabilities |
|---|---|
| **Reader** | Search & read all documents visible to their group memberships. |
| **Author** | Everything a Reader can, plus create/edit their own drafts and publish them into the KB. |
| **Group** | A named access scope (managed in Authentik). A document may be public (default) or restricted to one or more groups. |
| **Admin** | Manage groups & group membership (in Authentik), moderate/unpublish any document, manage tag taxonomy limits, view audit log. |
| **Integrator** | A machine/script client of the read-only [public API](10-public-api.md), authenticated via a Personal Access Token or Authentik service account; sees exactly what its identity's groups allow. |

> Roles map to Authentik groups/roles; LookupKB reads them from the OIDC token. See [06](06-security.md).

## 5. Document lifecycle

```
                 save (any time, no metadata required)
   ┌─────────┐  ────────────────────────────────►  ┌──────────┐
   │  (new)  │                                       │  DRAFT   │  (private to author)
   └─────────┘                                       └────┬─────┘
                                                          │ publish (requires valid metadata + explicit confirm)
                                                          ▼
                                                    ┌──────────┐
                     unpublish (author/admin)  ◄──► │ PUBLISHED │  (visible per access scope, indexed, versioned)
                                                    └────┬─────┘
                                                          │ edit → new draft revision → re-publish (new version snapshot)
                                                          ▼
                                                    (version history retained)
```

- **Draft**: exists in the author's private workspace. No metadata requirements. Not indexed in the public KB index. Not visible to anyone else.
- **Published**: has valid metadata, is indexed, is visible per its access scope, and each publish creates an immutable **version snapshot**.
- **Unpublished**: reverts to draft-like visibility (author only) and is removed from the search index; version history is retained.

## 6. Metadata rules (KB entry requirements)

To **publish**, a document must have:

| Field | Required | Constraint (default, admin-tunable) |
|---|---|---|
| Title | Yes | 3–120 chars, unique-ish (soft), single line |
| Short description | Yes | 10–280 chars |
| Tags | Yes, ≥ 1 | 1–8 tags; each tag 2–32 chars, normalized (lowercase, kebab); from a controlled or free vocabulary (see [09](09-open-questions.md)) |
| Author | Auto | Set from authenticated identity at creation |
| Created date | Auto | Set at creation |
| Published date | Auto | Set at first publish |
| Access scope | Yes | `public` (default) or a set of group IDs |

Field limits are configurable by an Admin but have safe defaults. Drafts have **no**
metadata requirements — validation runs only at the publish gate.

## 7. Markdown / rendering scope (must-haves)

- GitHub-Flavored Markdown (tables, task lists, strikethrough, autolinks, footnotes).
- **Code blocks** with syntax highlighting.
- **Mermaid** diagrams.
- **UML** (PlantUML via Kroki).
- **Chemistry**: reaction equations & formulas via `mhchem`; molecular structures via SMILES (SmilesDrawer).
- **Math**: LaTeX formulas via KaTeX.
- Embedded images / attachments (stored in Garage).

Rendering approach and security posture: [01](01-architecture.md) §Rendering pipeline and [06](06-security.md).

## 8. Explicit non-goals (v1)

- ❌ Folder hierarchy / file tree.
- ❌ Real-time multiplayer editing (single-author drafts only; a Yjs-friendly data model is a *possible* later add — see [09](09-open-questions.md)).
- ❌ Full per-save draft history (only **published** versions are snapshotted).
- ❌ Public internet-scale multi-tenant SaaS (v1 targets single-org self-host; multi-tenant is a later consideration).
- ❌ Rich page-builder / non-Markdown WYSIWYG blocks.
- ❌ Comments / discussion threads (candidate for later).
- ❌ Built-in identity store (delegated to Authentik).

## 9. Success metrics

- p95 search latency < 100 ms; p95 published-read < 100 ms (warm cache) at 100k documents.
- Time-to-first-search on a cold deploy: `docker compose up` → usable in < 5 min.
- Zero secrets exposed to the browser (search proxied, access filters server-injected).
- 100% of publish attempts with invalid metadata are blocked with actionable errors.

See measurable acceptance criteria in [04](04-functional-spec.md) and perf/security budgets in [07](07-test-book.md).
