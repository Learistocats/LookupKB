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

# Prune to production dependencies for the runtime image.
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm prune --prod

# ---- runtime stage -----------------------------------------------------------
# The SvelteKit BFF runs as a small Node server (adapter-node): it owns the OIDC
# flow and the session cookie. Caddy reverse-proxies to it on :8080.
FROM node:22-bookworm-slim@sha256:6c74791e557ce11fc957704f6d4fe134a7bc8d6f5ca4403205b2966bd488f6b3

WORKDIR /app
ENV NODE_ENV=production \
    PORT=8080 \
    HOST=0.0.0.0

# Copy only what the server needs: the built output and production deps.
COPY --from=build /app/build ./build
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json ./package.json

# Run unprivileged (the node image ships a `node` user).
USER node

EXPOSE 8080

CMD ["node", "build"]
