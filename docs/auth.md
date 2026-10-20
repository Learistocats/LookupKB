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
   also loads the caller's groups from `group_members`.

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

## Groups

Groups (`groups`) and memberships (`group_members`) live in Postgres and drive document
ACLs. Until the admin UI lands, manage them in SQL, e.g.:

```sql
INSERT INTO groups (name) VALUES ('security');
INSERT INTO group_members (group_id, user_id)
SELECT g.id, u.id FROM groups g, users u
WHERE g.name = 'security' AND u.email = 'ada@example.com';
```
