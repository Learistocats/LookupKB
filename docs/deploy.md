# Deploying LookupKB

Single-host **Docker Compose** deployment: one compose file, no
profiles or overlays.

| Service | Role |
|---|---|
| `web` | SvelteKit BFF — the only browser-facing service (`LOOKUPKB_WEB_PORT`, default 3000) |
| `api` | Go API (loopback-bound `LOOKUPKB_API_PORT`, default 8080) |
| `migrate` | One-shot schema migration before `api` starts |
| `postgres` | Authoritative store (users, documents, groups, ACLs, audit) |
| `valkey` | Login/signup rate-limit counters |
| `meilisearch` | Search index (rebuildable from Postgres) |
| `kroki` (+ `kroki-mermaid`) | Server-side diagram rendering |
| `garage` | S3-compatible attachment storage |

## Bring-up

```bash
cp .env.example .env      # then edit the secrets (or: make env)
make up                   # build + start the stack (detached)
```

Once up, open <http://localhost:3000/>, create an account at `/signup`, and you're in.
API probes (from the host): <http://127.0.0.1:8080/healthz>, <http://127.0.0.1:8080/readyz>.

Stop with `make down`; `make clean` also drops the named volumes (destructive).

### One-time Garage layout

Garage boots healthy but has no storage layout until you assign one. Run once after
`make up`:

```bash
make garage-init
```

## TLS / public exposure

No reverse proxy is bundled. For anything beyond localhost, put your own proxy (Caddy,
nginx, Traefik, a cloud LB…) in front of `web` and:

- set `ORIGIN=https://kb.example.com` and `SESSION_COOKIE_INSECURE=false`;
- set `WEB_ADDRESS_HEADER=x-forwarded-for` so login rate limiting sees real client IPs;
- to expose the public read API, route `/api/v1/*` to the API port (set
  `LOOKUPKB_API_BIND=0.0.0.0` if the proxy is not on the same host).

## Startup ordering & health

| Service | Probe |
|---|---|
| postgres | `pg_isready` |
| valkey | `valkey-cli ping` |
| meilisearch | `GET /health` (`curl`) |
| kroki | `GET /health` (`curl`) |
| garage | `garage status` |
| api | self-probe `/api readycheck` → `GET /readyz` |
| web | Node `fetch` of `/healthz` |

`migrate` waits for Postgres; `api` waits for `migrate` plus healthy datastores; `web`
waits for a healthy `api`. `/readyz` gates on Postgres/Valkey/Meilisearch/Kroki/Garage.

## Network boundary

Two bridge networks: `edge` (web ↔ api) and `data` (`internal: true`; datastores only).
**Postgres, Valkey, Meilisearch, Kroki and Garage never publish a host port**.

## Logs

Every service logs to stdout; read them with `make logs` or `docker compose logs <svc>`.
The API emits JSON lines with `request_id` and `actor_id` (internal user id).

## Configuration reference

`.env.example` documents every variable; the defaults there are **dev-only** — generate
strong unique secrets (`openssl rand -hex 32`) for anything deployed. `.env` is
git-ignored and must never be committed. Auth-specific settings: [auth.md](auth.md).

## Images

All third-party images are **pinned by digest**, updated via Renovate and scanned by Trivy
in CI. The API (distroless/static) and web (Node) images are built from multi-stage
Dockerfiles. Validate the compose file with `make config`.
