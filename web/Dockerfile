# syntax=docker/dockerfile:1

# ---- build stage -------------------------------------------------------------
# Node build image pinned by digest (updated via Renovate).
FROM node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 AS build

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

# Prune to production dependencies for the runtime image.
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm prune --prod

# ---- runtime stage -----------------------------------------------------------
# The SvelteKit BFF runs as a small Node server (adapter-node): it owns the login
# flow and the session cookie, and is the stack's only browser-facing service.
FROM node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392

WORKDIR /app
ENV NODE_ENV=production \
    PORT=8080 \
    HOST=0.0.0.0

# The runtime runs `node build` directly and never invokes npm/corepack. Remove
# the base image's bundled npm so its transitive dependencies aren't shipped as
# runtime attack surface (they otherwise trip the Trivy CVE gate for packages
# the server never loads). Runs as root before dropping to the node user.
RUN rm -rf /usr/local/lib/node_modules/npm \
    /usr/local/bin/npm /usr/local/bin/npx /opt/yarn* /usr/local/bin/yarn* /usr/local/bin/corepack

# Apply Debian security updates the upstream node image hasn't been rebuilt with
# yet (e.g. pcre2/perl), so fixed OS CVEs don't ship or trip the Trivy gate.
RUN apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

# Copy only what the server needs: the built output and production deps.
COPY --from=build /app/build ./build
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json ./package.json

# Run unprivileged (the node image ships a `node` user).
USER node

EXPOSE 8080

CMD ["node", "build"]
