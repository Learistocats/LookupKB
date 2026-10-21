# Authentication

LookupKB has its own email/password accounts — no external identity provider.

## Flow

```
Browser ──▶ SvelteKit BFF (web/) ──▶ Go API (api/)
            /login, /signup forms     POST /api/v1/auth/{signup,login}
            encrypted session cookie   argon2id hashes in Postgres `users`
            Bearer token (server-side) HS256 token, verified on every /api request
```

1. `/signup` (open to anyone) and `/login` post to SvelteKit form actions
   (`web/src/routes/{signup,login}`), which call the API.
2. The API (`api/internal/server/accounts.go`) validates input, hashes/verifies the
   password with **argon2id** (`api/internal/auth/password.go`) and returns an access
   token signed with `LOOKUPKB_AUTH_SECRET` (`api/internal/auth/token.go`).
3. The BFF seals the token into an encrypted HttpOnly cookie (`lkb_session.*`,
   `web/src/lib/server/session.ts`); the browser never sees it. `hooks.server.ts`
   redirects to `/login` when the cookie is missing or the token has expired.
4. Every protected API request is verified by `api/internal/auth/middleware.go`, which
   also loads the caller's groups from `group_members` and their `is_super_admin` flag.

Login failures never reveal whether an email exists (same 401, same argon2 work). Signup
and login are rate-limited to 10 attempts per minute per client IP (counters in Valkey).

## Configuration

| Variable | Service | Purpose |
|---|---|---|
| `LOOKUPKB_AUTH_SECRET` | api | HS256 signing secret, **≥ 32 bytes** (`openssl rand -hex 32`). Empty disables auth entirely (no protected routes). Rotating it signs everyone out. |
| `LOOKUPKB_AUTH_TOKEN_TTL` | api | Token lifetime (default `168h`). No refresh — users sign in again. |
| `SESSION_COOKIE_SECRET` | web | Encrypts the session cookie (≥ 16 chars). Empty disables the login gate (dev only). |
| `SESSION_COOKIE_INSECURE` | web | `true` drops the cookie `Secure` flag for plain-http local use. Set `false` behind HTTPS. |
| `WEB_ADDRESS_HEADER` | web | Behind a reverse proxy, the header carrying the client IP (e.g. `x-forwarded-for`) so rate limiting sees real clients. |

## Super-admins

A **super-admin** manages groups, memberships, group tag policies and the super-admin
role itself from the **Admin** page (`/admin`, API under `/api/admin/*`). The flag is
`users.is_super_admin`; the API re-reads it from the database on **every request**
(it is never put in the token), so revoking it takes effect immediately.

The first super-admin cannot be appointed through the UI. Grant it with the API binary
(the image is distroless, so there is no shell or `psql` — the binary does it):

```bash
docker compose -f deploy/docker-compose.yml --env-file .env exec api /api admin grant you@example.com
# or, equivalently
make admin-grant EMAIL=you@example.com
```

`/api admin revoke <email>` (`make admin-revoke EMAIL=…`) removes it. Both write an
`audit_log` row with a null actor. Running locally without containers:
`cd api && go run ./cmd/api admin grant you@example.com` (needs `LOOKUPKB_DATABASE_URL`).

From the API/UI, removing the **last** super-admin is refused (409); the CLI is the
break-glass path and is not limited by that guard.

## Groups

Groups (`groups`) and memberships (`group_members`) live in Postgres and drive access to
protected tags. Super-admins manage them at `/admin/groups`: create, rename and delete groups,
and add members by email or remove them.

Each group also has an **authorized tag policy** (`group_tag_grants`). A tag granted to
at least one group is a *protected* tag; adding the first grant (or removing the last)
asks for confirmation because it changes who can see entries carrying that tag.

A published entry is readable by a caller who is a super-admin, its author, or a member
of groups granting **every** protected tag on it. Anyone else gets the same 404 as for a
missing entry, no search hit, no autocomplete suggestion for the tag, and a 422 when
publishing with it. The rule lives in `api/internal/access`; the policy is read from the
database on each request, so grants and memberships apply at once with no reindex.
Drafts stay author-only (super-admins included).

Every admin change is recorded in `audit_log` (`group.*`, `user.super_admin_set`).

## Personal access tokens & the public API

A **personal access token** (PAT) lets a script, CI job or agent read the knowledge base
as you, with exactly your access (your groups decide which protected entries it sees).
Create one under **Settings → API tokens**: give it a name and an optional expiry
(1–365 days). The secret (`lkb_pat_…`) is shown **once**; only its hash is stored.
Revoke it there at any time; a revoked or expired token gets `401` on its next request.

A PAT is **read-only**: it reaches `GET /api/v1/*` (except the account routes) and the
MCP endpoint `/mcp`, nothing else (`403` for drafts, publishing, token management,
admin). Each token is limited to 120 requests per minute (`429` + `Retry-After`).
Programmatic clients go through the web origin (the API port is loopback-only); the web
server forwards only the `Authorization: Bearer lkb_pat_…` header, never cookies.

| Endpoint | What it returns |
|---|---|
| `GET /api/v1/search?q=&tags=&author_id=&published_from=&published_to=&facets=tags&limit=&offset=` | Ranked hits (`slug`, `title`, `snippet`, `tags`, `published_at`, …) and `next_offset` when more exist. Same semantics as the in-app search. |
| `GET /api/v1/documents?limit=&cursor=` | Published entries you can read, newest first, metadata only (limit 20 by default, max 100). Pass `next_cursor` back as `cursor` for the next page. |
| `GET /api/v1/documents/{slug}?format=json\|markdown\|html\|text` | One entry. `json` (default) is metadata + `html` + `markdown`; the others return the raw body. `Accept: text/markdown`, `text/html` or `text/plain` works too. |
| `GET /api/v1/tags?prefix=` | Tags you can see, most used first. |

Only **published** content is served: `markdown` is the source as of the last publish
(later draft edits stay private until you re-publish), `html` is the sanitized HTML
rendered at publish, and `text` is plain text extracted from it. Drafts never appear. An
entry you cannot read answers the same `404` as one that does not exist.

```bash
export LKB=https://kb.example.com TOKEN=lkb_pat_...
curl -H "Authorization: Bearer $TOKEN" "$LKB/api/v1/search?q=kubernetes+runbook&tags=ops&limit=10"
curl -H "Authorization: Bearer $TOKEN" "$LKB/api/v1/documents/kubernetes-runbook-a1b2c3d4?format=markdown"
```

### MCP server for agents

`/mcp` is a read-only [Model Context Protocol](https://modelcontextprotocol.io) server,
so an agent (Claude Code, Claude Desktop, any MCP client) can search and read the
knowledge base with your PAT. It uses the Streamable HTTP transport, **stateless with
JSON responses** (POST only; there is no SSE stream, `GET` answers `405`).

| Tool | Arguments | Returns |
|---|---|---|
| `search` | `query` (required), `tags` (all must match), `limit` (≤ 50, default 10) | Hits: slug, title, plain-text snippet, tags, published date |
| `get_document` | `slug` (required), `format`: `markdown` (default), `text`, `html` | Metadata block + the body; unknown and inaccessible slugs both answer `document not found` |
| `list_documents` | `limit` (≤ 100, default 20), `cursor` | A metadata page + `next_cursor` |
| `list_tags` | `prefix` | Tags you can see, most used first |

The tools use the same services and access rule as the REST endpoints above, so an
agent sees exactly what the token's user may read, and nothing it can do changes the
knowledge base.

Claude Code:

```bash
claude mcp add --transport http lookupkb https://kb.example.com/mcp \
  --header "Authorization: Bearer lkb_pat_..."
```

Generic client configuration (e.g. a `.mcp.json` / `mcpServers` block):

```json
{
  "mcpServers": {
    "lookupkb": {
      "type": "http",
      "url": "https://kb.example.com/mcp",
      "headers": { "Authorization": "Bearer lkb_pat_..." }
    }
  }
}
```

Requests carrying a browser `Origin` header are refused (`403`) unless that origin is
listed in `LOOKUPKB_MCP_ALLOWED_ORIGINS` (comma-separated, e.g.
`http://localhost:6274` for the browser-based MCP Inspector). Native clients send no
`Origin` and need no configuration. The PAT's 120 requests/minute budget is shared
between `/mcp` and `/api/v1`.
