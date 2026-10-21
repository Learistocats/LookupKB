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

Groups (`groups`) and memberships (`group_members`) live in Postgres and drive document
ACLs. Super-admins manage them at `/admin/groups`: create, rename and delete groups,
and add members by email or remove them.

Each group also has an **authorized tag policy** (`group_tag_grants`). A tag granted to
at least one group is a *protected* tag; adding the first grant (or removing the last)
asks for confirmation because it changes who can see entries carrying that tag. Access
enforcement for protected tags arrives in a follow-up; today the policy is stored only.

Every admin change is recorded in `audit_log` (`group.*`, `user.super_admin_set`).
