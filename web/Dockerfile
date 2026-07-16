# syntax=docker/dockerfile:1

# ---- build stage -------------------------------------------------------------
# Node build image pinned by digest (updated via Renovate).
FROM node:22-bookworm-slim@sha256:6c74791e557ce11fc957704f6d4fe134a7bc8d6f5ca4403205b2966bd488f6b3 AS build

# Enable pnpm via corepack.
RUN corepack enable

WORKDIR /app

# Install dependencies against the committed lockfile for reproducibility.
# pnpm-workspace.yaml carries the build-script approvals (esbuild) and .npmrc
# the engine settings, so both must be present before install.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY . .
RUN pnpm run build

# ---- runtime stage -----------------------------------------------------------
# Static assets served by Caddy's file_server. Pinned by digest.
FROM caddy:2.8-alpine@sha256:af32e97399febea808609119bb21544d0265c58a02836576e32a2d082c262c17

COPY --from=build /app/build /srv
COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 8080
