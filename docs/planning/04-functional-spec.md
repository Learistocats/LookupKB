# 04 — Functional Spec & Acceptance Criteria

Format: each feature has **user stories** and **acceptance criteria** in Given/When/Then
form. IDs (`AC-x.y`) are referenced by the [test book](07-test-book.md).

Priority: **P0** = MVP, **P1** = fast-follow, **P2** = later.

---

## F1 — Authentication & session (P0)

**Story:** As a user, I create an account with my email and a password and sign in so I can access the KB.

- **AC-1.1** Given an unauthenticated user, when they open any app route, then they are redirected to `/login` (and back to the original route after signing in).
- **AC-1.2** Given an access token, when the API receives a request, then it verifies the HS256 signature, issuer, subject and expiry before processing; any failure is a bare 401.
- **AC-1.3** Given an expired access token, when a request is made, then the session is dropped and the user is redirected to `/login` (no silent refresh).
- **AC-1.4** Given a signed-in user, when a request is processed, then the user's current group memberships (from `group_members`) are used for all access decisions in that request.
- **AC-1.5** Passwords are stored only as argon2id hashes; they are never logged, returned, or stored in plaintext. Login failures for an unknown email and a wrong password are indistinguishable (same status, body and work).
- **AC-1.6** Given anyone reaching `/signup`, when they submit a valid email and a password of ≥ 12 characters, then an account is created and they are signed in; a duplicate email (any casing) is rejected.
- **AC-1.7** Signup and login are rate-limited per client IP.

## F2 — Draft creation & private workspace (P0)

**Story:** As an author, I can create and save a document privately, even without metadata.

- **AC-2.1** Given an author, when they create a new document, then a `draft` is created owned by them with no metadata required. The row is created on the first non-blank save (any of title, description or body): opening a new entry and leaving it untouched persists nothing.
- **AC-2.2** Given a draft with empty/partial metadata, when the author saves, then the save succeeds and no validation error is shown.
- **AC-2.3** Given a draft, when any other non-admin user queries or searches, then the draft is never returned or visible.
- **AC-2.4** Given a draft, when saved, then it is **not** added to the search index.
- **AC-2.5** Given an author, when they open their workspace, then they see a list of only their own drafts (title-or-"Untitled", updated time).
- **AC-2.6** Auto-save: given active editing, when the author pauses (debounced) or every N seconds, then the draft persists without explicit action, and unsaved state is visibly indicated.

## F3 — Markdown editor (Obsidian-like) (P0)

**Story:** As an author, I write Markdown with live preview and technical extensions.

- **AC-3.1** Given the editor, when the author types Markdown, then formatting (headings, bold, lists, links, etc.) renders inline (live preview) via CodeMirror 6.
- **AC-3.2** Given a fenced code block with a language, when previewed, then it is syntax-highlighted.
- **AC-3.3** Given a `mermaid`/`plantuml`/`uml` block, when the author previews, then a diagram preview is shown (client preview via Kroki call; final render is server-side at publish).
- **AC-3.4** Given `$...$`/`$$...$$` math, when previewed, then KaTeX renders it; `\ce{...}` renders chemical equations via mhchem.
- **AC-3.5** Given a `smiles` block, when previewed, then SmilesDrawer renders the molecular structure.
- **AC-3.6** Given the editor, when the author uses common shortcuts (bold, italic, link, code), then they work as expected and the UI stays minimal/uncluttered.

## F4 — Attachments (P1)

**Story:** As an author, I embed images/files in a document.

- **AC-4.1** Given an author, when they paste/drag an image into the editor, then it uploads to Garage and an embed reference is inserted.
- **AC-4.2** Given an upload, when the type is not in the allowlist or exceeds the size cap, then it is rejected with a clear message.
- **AC-4.3** Given an attachment, when a reader without access to the owning document requests it, then it is denied (served via API, not a public bucket).

## F5 — Metadata & publish gate (P0)

**Story:** As an author, I publish a document into the KB via a deliberate, confirmed action requiring valid metadata.

- **AC-5.1** Given a draft, when the author clicks Publish, then a confirmation step shows the metadata to be published and requires explicit confirmation.
- **AC-5.2** Given missing/invalid metadata (title, description, or ≥1 tag, or a limit violated), when the author confirms publish, then publish is blocked with per-field errors and the document stays a draft.
- **AC-5.3** Given valid metadata, when the author confirms, then the document becomes `published`, `published_at` is set, and it is indexed.
- **AC-5.4** Title/description/tag counts and lengths are enforced per configurable limits (defaults in [00](00-overview.md) §6).
- **AC-5.5** Given a published doc, when the author edits and re-publishes, then the published content is overwritten (no version history) and the index is updated.
- **AC-5.6** Given a published doc, when the author or an admin unpublishes it, then it is removed from the index, becomes author-only visible, and history is retained.

## F6 — Tags (P0)

**Story:** As an author, I tag documents so they're discoverable by facet.

- **AC-6.1** Given the tag field, when the author types, then existing tags are suggested (autocomplete) with usage counts.
- **AC-6.2** Given a new tag within limits, when publishing, then it is normalized (lowercase/kebab) and created.
- **AC-6.3** Given fewer than 1 or more than the max tags, when publishing, then it is blocked with an error.
- **AC-6.4** Tags are available as search facets and filters.

## F7 — Search & discovery (P0, the focus)

**Story:** As a reader, I find documents instantly by typing, filtered to what I can access.

- **AC-7.1** Given any authenticated user, when they type in search, then results update as-they-type with p95 latency < 100 ms at 100k docs.
- **AC-7.2** Given a typo (e.g. "kuberntes"), when searching, then relevant results ("kubernetes") still appear (typo tolerance).
- **AC-7.3** Given results, when displayed, then each shows title, description, tags, author, and a matched snippet; matched terms are highlighted.
- **AC-7.4** Given a restricted document the user cannot access, when searching, then it never appears in results.
- **AC-7.5** Given facets (tags, author, date range), when the user filters, then results narrow accordingly.
- **AC-7.6** The Meilisearch host/keys are never exposed to the browser; all queries go through the API with server-injected access filters.
- **AC-7.7** Given an empty query, when the user opens search, then sensible defaults appear (e.g. recently published accessible docs).

## F8 — Read a document (P0)

**Story:** As a reader, I open a document and see fully rendered technical content fast.

- **AC-8.1** Given a published doc the user can access, when opened, then pre-rendered sanitized HTML loads with p95 < 100 ms (warm).
- **AC-8.2** Given diagrams, when the page loads, then they appear as inlined sanitized SVG (no client-side diagram engine).
- **AC-8.3** Given math/chemistry, when the page loads, then KaTeX/SmilesDrawer render them.
- **AC-8.4** Given a restricted doc the user cannot access, when they request it by slug/URL, then they get 403/404 (no content leak).
- **AC-8.5** Given a document, when viewed, then metadata (author, published date, tags) is shown minimally.

## F9 — Version history (removed)

Dropped in the lightweight revamp: there are no published-version snapshots; a re-publish
overwrites the document. AC-9.x are retired (a post-v1 candidate, see [08](08-roadmap.md)).

## F10 — Access groups & administration (P0 core / P1 UI)

**Story:** As an admin, I restrict documents to named groups and manage the taxonomy.

- **AC-10.1** Given an admin, when they create a group in LookupKB and assign members, then the group is immediately usable as a document ACL.
- **AC-10.2** Given an author of a doc (or admin), when they set access to restricted + select groups, then only members of those groups (plus author/admin) can read or find it.
- **AC-10.3** Given an admin, when they change a document's ACL, then the search index and read access reflect it immediately and the change is audited.
- **AC-10.4** Given an admin, when they adjust metadata limits (title/desc/tag counts), then new publishes enforce the updated limits.
- **AC-10.5** Given any publish/unpublish/ACL change, when it occurs, then an audit-log entry is recorded.

## F11 — Health, ops & DR (P0)

- **AC-11.1** `/healthz` returns 200 when the process is up; `/readyz` returns 200 only when Postgres, Meilisearch, Kroki, and Garage are reachable.
- **AC-11.2** Given a wiped Meilisearch, when `admin/reindex` runs, then the full accessible corpus is rebuilt from Postgres and search works again.
- **AC-11.3** `docker compose up` on a clean host brings the full stack to a usable state in < 5 minutes with documented `.env`.

## F12 — Public read API (P0/P1)

**Story:** As an integrator, I search, query, and retrieve documents programmatically with the same access rules as the UI. Full spec: [10](10-public-api.md).

- **AC-12.1** Given a valid PAT (`lkb_pat_…`), when a request is made, then it authenticates as the issuing user and inherits that user's group access.
- **AC-12.2** *(retired — no external service accounts; integrations use PATs.)*
- **AC-12.3** Given an invalid/expired/revoked token or a missing required scope, when a request is made, then it returns `401`/`403` and no data.
- **AC-12.4** `GET /api/v1/search` returns typo-tolerant ranked hits **filtered to the caller's accessible corpus** — a restricted doc the caller can't access never appears.
- **AC-12.5** `GET /api/v1/documents` returns metadata-filtered listings (tags/author/date), cursor-paginated (`limit`≤100, `next_cursor`).
- **AC-12.6** `GET /api/v1/documents/{slug}` returns the current published version; `?format=`/`Accept` selects Markdown, sanitized HTML, plain text, or JSON envelope.
- **AC-12.7** Given a restricted document the caller can't access, when retrieved by slug, then it returns `403`/`404` with no content or existence leak; drafts are never retrievable via the public API.
- **AC-12.8** *(retired with version history.)*
- **AC-12.9** `GET /api/v1/tags` returns tags with usage counts for discovery.
- **AC-12.10** Per-token rate limiting is enforced; exceeding it returns `429` with `X-RateLimit-*`/`Retry-After` headers.
- **AC-12.11** `GET /api/v1/openapi.json` returns a valid OpenAPI 3 spec matching the live API; interactive docs are served at `/api/docs`.
- **AC-12.12** Document reads return `ETag`; a matching `If-None-Match` yields `304`.
- **AC-12.13** A user can create, name, scope, view (prefix only), and revoke PATs in the UI; the secret is shown once and stored only as a hash.

---

## Cross-cutting non-functional acceptance

- **NFR-1 Performance:** p95 search < 100 ms, p95 read < 100 ms at 100k docs (warm). See [07](07-test-book.md) budgets.
- **NFR-2 Security:** passes gosec/govulncheck/Trivy/ZAP baseline with no high/critical findings; CSP enforced; content sanitized. See [06](06-security.md).
- **NFR-3 Simplicity:** primary flows (search, read, draft, publish) reachable within ≤ 2 clicks; minimal on-screen chrome.
- **NFR-4 Accessibility:** keyboard-navigable, WCAG 2.1 AA color contrast, focus states.
- **NFR-5 Portability:** single `docker compose` deploy; no managed-cloud dependency.
