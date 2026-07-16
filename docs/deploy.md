# Deploying LookupKB

Single-host **Docker Compose** deployment (see
[`planning/01-architecture.md`](planning/01-architecture.md) §5). Everything runs as
containers on two networks; **only Caddy is published to the host**.

## Prerequisites

- Docker Engine + Docker Compose v2 (`docker compose version`).
- On a clean host, the full core stack reaches a usable state in **< 5 minutes** (AC-11.3).

## Bring-up

```bash
cp .env.example .env      # then edit the secrets (or: make env)
make up                   # build + start the core stack (detached)
```

`make up` is `docker compose -f deploy/docker-compose.yml --env-file .env up -d --build`.

Once up:

- App (SPA): <http://localhost/>
- API liveness: <http://localhost/healthz>
- API readiness: <http://localhost/readyz>

Stop with `make down`; `make clean` also drops the named volumes (destructive).

### One-time Garage layout

Garage boots healthy but has no storage layout until you assign one. Run once after
`make up`:

```bash
make garage-init
```

This assigns and applies a single-node layout. Creating the attachments bucket and an S3
app key (`garage bucket create` / `garage key create`) is documented by the uploads
ticket; paste the minted keys into `.env` as `GARAGE_S3_ACCESS_KEY`/`GARAGE_S3_SECRET_KEY`
when that lands. (An init container was avoided because the Garage image is distroless — no
shell to script against.)

## Startup ordering & health

Every service has a healthcheck, and `depends_on: condition: service_healthy` gates the
graph so startup is deterministic:

| Service | Probe |
|---|---|
| postgres | `pg_isready` |
| valkey | `valkey-cli ping` |
| meilisearch | `GET /health` (busybox `wget`) |
| kroki | `GET /health` (`curl`) |
| garage | `garage status` (RPC, healthy pre-layout) |
| api | self-probe `/api healthcheck` → `GET /healthz` |
| web | `wget --spider` on the file server |
| authentik-server | `ak healthcheck` |
| otel-collector | `health_check` extension on `:13133` (no container probe — distroless) |

`api` waits for `postgres`, `valkey`, `meilisearch`, `garage` (healthy) and `kroki`; `caddy`
waits for `api` and `web` (healthy). This is what lets `/readyz` reach live datastores
(AC-11.1).

## Network boundary

Two bridge networks: `edge` (browser-facing: Caddy ↔ web/api/authentik) and `data`
(`internal: true`; datastores only). **Postgres, Meilisearch, Kroki, Garage, Valkey, and
the collector never publish a host port** — this is a security boundary
([`planning/06-security.md`](planning/06-security.md) §1). Verify:

```bash
docker compose -f deploy/docker-compose.yml ps   # only caddy shows 80/443 mappings
```

## Optional profiles & overlays

### Identity (Authentik)

```bash
make auth-up      # docker compose ... --profile auth up -d
```

Authentik shares the `postgres` service but uses its **own database and role**, provisioned
on first boot by [`deploy/postgres/init/10-authentik.sh`](../deploy/postgres/init/10-authentik.sh).
Set `AUTHENTIK_BOOTSTRAP_PASSWORD` in `.env` to get a usable `akadmin` account without manual
setup. OIDC application/provider configuration is a later ticket.

### Observability (Prometheus + Tempo + Loki + Grafana)

Opt-in bundled telemetry backend:

```bash
make obs-up       # core stack + docker-compose.observability.yml
```

This swaps the collector config for one that fans OTLP out to Tempo (traces), Prometheus
(metrics), and Loki (logs), starts those backends, and adds a `/grafana` route to Caddy.
**No backend publishes a host port** — Grafana is reached through Caddy:

- Grafana: <http://localhost/grafana> (user `admin`, password `GRAFANA_ADMIN_PASSWORD`)

Datasources are auto-provisioned. Preloaded AC-budget dashboards land with the API
instrumentation ticket (the API does not emit OTLP yet). Stop with `make obs-down`.

Production deployments skip this overlay and point the collector at their own backend.

## Redis / Valkey role

The `valkey` service is **dual-purpose**: it is a hard dependency of Authentik (sessions,
task queue) and an optional LookupKB read cache. It carries a small AOF/save policy and a
named volume — treat it as **semi-persistent**, not disposable, once Authentik is in use.

## Configuration reference

All config is 12-factor via `.env` ([`planning/01-architecture.md`](planning/01-architecture.md)
§5.3). `.env.example` documents every variable; the defaults there are **dev-only** —
generate strong unique secrets (`openssl rand -hex 32`) for anything deployed. `.env` is
git-ignored and must never be committed.

## Images

All third-party images are **pinned by digest** (`image@sha256:…`), updated via Renovate and
scanned by Trivy in CI. The API and web images are built locally from multi-stage Dockerfiles
(distroless/static API, Caddy-served static web). Validate the compose files render cleanly:

```bash
make config       # validates both the core stack and the observability overlay
```
