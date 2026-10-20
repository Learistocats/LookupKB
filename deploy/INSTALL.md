# Installing LookupKB from a release bundle

This bundle runs the published, signed images from
`ghcr.io/learistocats/lookupkb-{api,web}` — no source checkout, no build, no `make`.
Prerequisites: **Docker** + **Docker Compose v2**.

Contents (note `.env.example` is a dotfile — use `ls -a`):

| File | Purpose |
|---|---|
| `docker-compose.yml` | The full stack, pinned to this release's images |
| `.env.example` | Every setting, with this release's version + image prefix preset |
| `garage-init.sh` | One-time Garage storage layout |
| `garage/`, `postgres/` | Service configs mounted by the compose file |
| `deploy.md` | Reference: services, health, networks, TLS |

## 1. Configure

```bash
cp .env.example .env
```

Edit `.env` and replace **every** `change-me-…` value and the all-zero
`GARAGE_RPC_SECRET` with fresh secrets (`openssl rand -hex 32`). The defaults are
dev-only. Leave `LOOKUPKB_VERSION` / `LOOKUPKB_IMAGE_PREFIX` as preset.

## 2. Start

```bash
docker compose up -d
docker compose ps          # wait until api and web are healthy
```

Open <http://localhost:3000/> and create an account at `/signup`.
API probes from the host: <http://127.0.0.1:8080/healthz>, <http://127.0.0.1:8080/readyz>.

## 3. One-time Garage layout

```bash
./garage-init.sh
```

## Day-to-day

```bash
docker compose logs -f [service]   # logs
docker compose down                # stop
docker compose down -v             # stop AND delete all data (destructive)
docker compose config -q           # validate the configuration
```

## Upgrading

Download the new release's bundle, carry your `.env` over (keep your secrets, but take
the new `LOOKUPKB_VERSION`, and compare with the new `.env.example` for added
settings), then `docker compose up -d`. Database migrations run automatically
(`migrate` service) before the API starts.

## Verifying the images

```bash
cosign verify ghcr.io/learistocats/lookupkb-api:<version> \
  --certificate-identity-regexp '^https://github.com/Learistocats/LookupKB/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

For anything beyond localhost (TLS, public origin, client IPs) see the
*TLS / public exposure* section of `deploy.md`.
