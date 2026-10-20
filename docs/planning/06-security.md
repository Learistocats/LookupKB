# 06 — Security & Threat Model

Security posture: **hardened, least-privilege, secure-by-default.** LookupKB is meant to be
deployed by security-conscious orgs; the design assumes hostile input (untrusted Markdown
from authors) and hostile network.

## 1. Trust boundaries

| Boundary | Notes |
|---|---|
| Browser ↔ web (BFF) | Public, hostile. TLS terminated by the operator's proxy; security headers, CSP. |
| web ↔ API | Internal network (API port loopback-bound by default). The BFF forwards the API token server-side. |
| API ↔ Postgres/Meili/Kroki/Garage | Internal, network-isolated; least-privilege creds. |
| Author-authored Markdown | **Untrusted content** rendered for many readers → sanitize server-side. |

## 2. Threat model (STRIDE-ish) & mitigations

| Threat | Vector | Mitigation |
|---|---|---|
| **XSS via Markdown** | Author embeds `<script>`, event handlers, `javascript:` URLs, malicious SVG | Server-side render (goldmark) → **bluemonday allowlist sanitize** → strict **CSP**; SVG from Kroki sanitized; KaTeX `trust:false`; SmilesDrawer draws from SMILES only |
| **Access-control bypass** | Guessing slugs, tampering search filters, IDOR on drafts/attachments | Server-side authz on every read/write; **search access filter injected server-side**; attachments brokered through API; drafts owner-scoped |
| **Meili key/host exposure** | Client-side search calls | Client never contacts Meili; API holds keys; optional scoped tenant tokens |
| **Token forgery / replay** | Fake/expired JWT | HS256 with a ≥32-byte server secret; algorithm pinned (no `alg` confusion); `iss`/`exp`/`sub` required and checked; token only ever held server-side in an encrypted HttpOnly cookie |
| **Credential stuffing / brute force** | Password guessing on `/login`, signup spam | Per-IP rate limit on signup/login (Valkey-backed); argon2id (memory-hard) hashing; identical 401 + identical work for unknown email vs wrong password; minimum password length 12 |
| **API token (PAT) leakage/abuse** | Leaked/committed PAT, over-broad scope, no expiry | PATs stored **hashed** (shown once); least-privilege scopes; optional expiry; revocation; `last_used_at` anomaly detection; per-token rate limits; TLS-only; secret-scanning guidance in docs |
| **SSRF via diagram/embeds** | PlantUML/Kroki fetching remote includes; remote image includes | Kroki configured to disable remote fetch/includes; egress-restricted network; only proxy known hosts; attachments stored locally |
| **DoS** | Huge diagrams, giant uploads, expensive queries, render bombs | Size/time limits on Kroki render; upload size/type caps; edge + API rate limiting; pagination caps; publish-time render (not per-read) |
| **SQL injection** | Crafted input | `sqlc`/parameterized queries only; no string-built SQL |
| **Secret leakage** | Secrets in images/logs/browser | Secrets via env/Docker secrets; never in images or client bundles; log redaction |
| **Supply chain** | Compromised deps/images | Pinned digests, Trivy/Semgrep/govulncheck in CI, SBOM (Syft), signed images (cosign), Renovate |
| **Privilege escalation** | Author acting as admin | Admin role stored in Postgres, re-checked server-side per action; audit log |
| **Data exfiltration via search facets** | Enumerate restricted titles via facet counts | Access filter applied before faceting; restricted docs excluded from counts the user can't access |

## 3. Content Security Policy (published pages)

- `default-src 'self'`; no inline scripts (nonce/hash only for the SPA bootstrap).
- `img-src 'self' data:` (attachments via API; inlined SVG allowed).
- `style-src 'self'` (+ KaTeX styles bundled; avoid `unsafe-inline`, use hashes).
- `script-src 'self'` (SvelteKit bundle only); **no** third-party CDNs.
- `frame-ancestors 'none'`; `object-src 'none'`; `base-uri 'self'`.
- `connect-src 'self'`.

## 4. Transport & headers
The web BFF sets `X-Content-Type-Options: nosniff`, `Referrer-Policy:
strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, a minimal `Permissions-Policy`,
and HSTS when cookies are Secure. TLS (1.2+, modern ciphers) is terminated by the
operator's reverse proxy; none is bundled.

## 5. AuthN / AuthZ details
- **AuthN (interactive):** local email/password accounts with open signup. Passwords are
  hashed with **argon2id** (OWASP parameters, per-hash random salt, PHC-encoded so
  parameters can be raised later); the hash is the only persisted form. No MFA in v1.
- **AuthN (programmatic):** **PATs** (LookupKB-issued, hashed, user-scoped, revocable) for
  the public API ([10](10-public-api.md) §2). They resolve to a user + groups and flow
  through the *same* authz path as interactive users.
- **AuthZ:** deny-by-default. Every endpoint declares required capability/scope; middleware enforces it. Group membership is read from `group_members` on every request (never cached in the token). The read API is access-filtered identically to the UI — no separate code path that could drift and leak.
- **Sessions:** the API issues an HS256 access token (default TTL 7 days, no refresh); the
  BFF seals it in an encrypted (A256GCM JWE) HttpOnly, SameSite=Lax cookie. Logout drops
  the cookie; rotating `LOOKUPKB_AUTH_SECRET` invalidates every session. PATs are
  independently revocable and support optional expiry.

## 6. Input validation & limits
- Metadata validated at publish (lengths/counts per settings).
- Uploads: MIME allowlist (images + a curated doc set), magic-byte sniffing, size cap, optional AV scan (ClamAV) hook.
- Markdown size cap per document; diagram source size/time caps at Kroki.

## 7. Data protection
- TLS in transit everywhere public-facing; internal network isolated.
- At rest: rely on host/volume encryption; Garage server-side considerations documented.
- PII minimized: only email + optional display name stored; logs carry the internal user id, never the email; audit log avoids sensitive payloads.
- Backups encrypted; restore runbook documented ([01](01-architecture.md) §5.4).

## 8. Container hardening
- API image: distroless/scratch, **non-root** user, **read-only root FS**, no shell, dropped Linux capabilities, `no-new-privileges`.
- Per-service least-privilege DB users (API cannot DROP; migrations use a separate role).
- Network segmentation in Compose (frontend vs data networks); only `web` is published (the API is loopback-bound by default).
- Resource limits (CPU/mem) per container to bound DoS blast radius.

## 9. Auditing & monitoring
- Immutable-ish `audit_log` for publish/unpublish/ACL/admin actions (actor, target, diff, time).
- Structured security logs; alert on auth failures spikes, authz denials, reindex, admin actions.

## 10. Security testing (see [07](07-test-book.md))
- SAST (Semgrep, gosec), dependency/vuln (govulncheck, Trivy), DAST (OWASP ZAP baseline against staging).
- **Access-control test suite**: automated tests asserting restricted docs never leak via read, search, facets, or attachments.
- Sanitizer test corpus: known XSS payloads must be neutralized in rendered output.
- Periodic manual pentest checklist before releases.

## 11. Responsible disclosure
Ship a `SECURITY.md` with a disclosure policy and contact, and a documented CVE/advisory
process for the OSS project.
