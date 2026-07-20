# Authentication setup (Authentik OIDC)

LookupKB delegates all authentication to **Authentik** (OIDC Authorization Code +
PKCE). No password ever reaches LookupKB (AC-1.5). This document explains the
moving parts and how to configure them.

## Architecture

```
Browser ──▶ Caddy ──▶ SvelteKit BFF (Node)  ──▶ Authentik   (login: Auth Code + PKCE)
                          │  holds the session in an encrypted HttpOnly cookie
                          │  attaches Authorization: Bearer <access_token>
                          ▼
                       Go API  ── verifies the JWT against Authentik's JWKS
                                  (signature + issuer + audience + expiry),
                                  extracts sub/email/groups, upserts users row.
```

- **SvelteKit** (`web/`, `adapter-node`) is the confidential OIDC client. It runs
  the interactive login/refresh/logout and never exposes tokens to browser JS
  (`web/src/hooks.server.ts`, `web/src/routes/auth/*`).
- **The Go API** (`api/internal/auth/`) is a stateless verifier. It trusts only
  the bearer token forwarded per request. It performs **no** interactive flow.

## The issuer / host model (important)

Authentik derives an OIDC **provider issuer** from its host:

```
issuer = <authentik-base-url>/application/o/<app-slug>/
```

For the default app slug `lookupkb` and a dev stack, that is
`http://authentik.localhost/application/o/lookupkb/`. Discovery lives at
`.../.well-known/openid-configuration`, JWKS at `.../jwks/`, and the token
**`aud`** equals the provider **client id**.

Two independent trust checks must line up:

- **`iss`** — the API and the SvelteKit client both validate the token issuer
  against this URL.
- **`aud`** — the API validates the audience is the LookupKB client id (this is
  what stops a token minted for another Authentik app being replayed here).

Because everything keys off the Authentik host, LookupKB gives Authentik its own
hostname (`AUTH_SITE_ADDRESS`, default `http://authentik.localhost` in dev) rather
than a path under the app. In `deploy/docker-compose.yml` the `caddy` service
carries a network alias for that host, so the API and web containers reach the
**same** URL the browser uses — the issuer stays consistent end to end. In
production, point `AUTH_SITE_ADDRESS` at a real hostname (e.g.
`https://auth.example.com`) with normal DNS.

The Go API can optionally fetch discovery/JWKS from an internal URL while still
validating the external issuer (split-horizon), via
`LOOKUPKB_OIDC_ISSUER_INTERNAL` — see the env reference. With the caddy alias in
place this is not needed for the default stack.

## 1. Configure Authentik

Bring up Authentik: `make auth-up` (starts the core stack + `--profile auth`).

### Option A — apply the blueprint (recommended)

`deploy/authentik/blueprints/lookupkb.yaml` is mounted into the Authentik worker
and applied automatically on boot. It creates the provider, the application, and
a **`groups`** scope mapping. Set these before starting the `auth` profile
(they flow into the worker):

```
LOOKUPKB_OIDC_CLIENT_ID=lookupkb
LOOKUPKB_OIDC_CLIENT_SECRET=<openssl rand -hex 32>
LOOKUPKB_OIDC_REDIRECT_URI=http://localhost/auth/callback
```

Check **Admin → Customization → Blueprints**: `LookupKB OIDC` should be
*Successful*. Re-apply after edits by restarting the worker.

### Option B — manual admin UI

1. **Customization → Property Mappings → Create → Scope Mapping**
   - Name: `LookupKB groups scope`, Scope name: `groups`
   - Expression: `return [group.name for group in request.user.ak_groups.all()]`
2. **Applications → Providers → Create → OAuth2/OpenID Provider**
   - Name: `LookupKB`; Client type: **Confidential**
   - Client ID / Secret: record these (they become the env values)
   - Authorization flow: *default-provider-authorization-explicit-consent*
   - Redirect URIs: **Strict** → `http://localhost/auth/callback`
   - Signing Key: *authentik Self-signed Certificate* (RS256)
   - Scopes: `openid`, `email`, `profile`, **`groups`** (the mapping above)
   - Advanced: `Include claims in id_token` on; access token validity ~10 min
3. **Applications → Create**: Name `LookupKB`, slug `lookupkb`, provider = above.

Ensure your test user is a member of at least one Authentik **group** so the
`groups` claim is exercised.

## 2. Configure LookupKB

Copy `.env.example` to `.env` and set the OIDC block (see the file for the full
list). At minimum:

| Variable | Consumer | Example |
|---|---|---|
| `AUTH_SITE_ADDRESS` | Caddy | `http://authentik.localhost` |
| `LOOKUPKB_OIDC_ISSUER` | Go API | `http://authentik.localhost/application/o/lookupkb/` |
| `LOOKUPKB_OIDC_AUDIENCE` | Go API | `lookupkb` (the client id) |
| `LOOKUPKB_OIDC_ISSUER_INTERNAL` | Go API (optional) | `http://authentik-server:9000/application/o/lookupkb/` |
| `OIDC_ISSUER` | web | `http://authentik.localhost/application/o/lookupkb/` |
| `OIDC_CLIENT_ID` / `LOOKUPKB_OIDC_CLIENT_ID` | web / worker | `lookupkb` |
| `OIDC_CLIENT_SECRET` / `LOOKUPKB_OIDC_CLIENT_SECRET` | web / worker | `<hex>` |
| `OIDC_REDIRECT_URI` / `LOOKUPKB_OIDC_REDIRECT_URI` | web / worker | `http://localhost/auth/callback` |
| `OIDC_ALLOW_INSECURE` | web | `true` (dev http only) |
| `SESSION_COOKIE_SECRET` | web | `<openssl rand -hex 32>` |

Leaving `LOOKUPKB_OIDC_ISSUER` / `OIDC_ISSUER` **empty disables auth** so the core
stack boots without Authentik (mirrors the empty-`DATABASE_URL` behaviour).

Restart: `make auth-up` (rebuilds api + web with the new config).

## 3. Verify end-to-end

1. Open `http://localhost/`. You are redirected to Authentik (AC-1.1), log in,
   consent, and land back on the app showing your email, subject and **groups**.
2. The home page renders “API verified token (/api/v1/me)” — the Go API verified
   the bearer against JWKS and returned your identity (AC-1.2, AC-1.4).
3. **Tamper test** (AC-1.2): call the API directly with a bad token:
   ```
   curl -i http://localhost/api/v1/me -H 'Authorization: Bearer not.a.jwt'
   ```
   → `401 Unauthorized`. Same for an expired or wrong-audience token.
4. **No secrets test** (AC-1.5): inspect the `users` row and confirm there is no
   password/secret column:
   ```
   docker compose exec postgres psql -U lookupkb -c '\d users'
   docker compose exec postgres psql -U lookupkb -c 'SELECT oidc_sub,email,display_name,last_seen_at FROM users;'
   ```
5. **Logout** (`/auth/logout`) clears the session cookie and revokes the SSO
   session at Authentik; revisiting the app requires logging in again.

## Troubleshooting

- **`oidc: issuer did not match`** — `LOOKUPKB_OIDC_ISSUER` / `OIDC_ISSUER` must
  equal the token `iss` exactly (trailing slash included). Confirm the app slug
  is `lookupkb` and `AUTH_SITE_ADDRESS` matches what the browser uses.
- **`redirect_uri` mismatch at Authentik** — the provider redirect URI must match
  `OIDC_REDIRECT_URI` exactly (scheme, host, path). Default `http://localhost/auth/callback`.
- **Cookie not stored / immediate re-login loop over HTTP** — set
  `OIDC_ALLOW_INSECURE=true` in dev so cookies are not marked `Secure` on plain
  HTTP. Never set it in production.
- **`groups` empty** — the test user is in no Authentik group, or the `groups`
  scope mapping is not attached to the provider.
- **API can't reach JWKS** — with the default caddy alias the external issuer is
  reachable from the api container; otherwise set `LOOKUPKB_OIDC_ISSUER_INTERNAL`
  to `http://authentik-server:9000/application/o/lookupkb/`.

## Security notes

- Cookies are `HttpOnly; SameSite=Lax` and `Secure` (unless `OIDC_ALLOW_INSECURE`).
- PKCE verifier / state / nonce live only in a short-lived encrypted cookie,
  never in `localStorage`.
- Tokens, authorization codes and PKCE verifiers are never logged.
- The API validates **both** issuer and audience — a JWKS-only check would allow
  cross-app token replay.
