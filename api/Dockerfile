# syntax=docker/dockerfile:1

# ---- build stage -------------------------------------------------------------
# Pin the toolchain image by digest. Digests are kept fresh by Dependabot.
FROM golang:1.26-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c AS build

WORKDIR /src

# Cache modules first for faster incremental builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY . .

ARG VERSION=dev
# CGO disabled + static build so the binary runs on a scratch/distroless base.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/api ./cmd/api

# ---- runtime stage -----------------------------------------------------------
# distroless/static: no shell, no package manager, minimal attack surface.
# nonroot variant runs as uid 65532. Pinned by digest.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b

# OCI labels for provenance.
LABEL org.opencontainers.image.title="lookupkb-api" \
      org.opencontainers.image.source="https://github.com/Learistocats/LookupKB" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /out/api /api

USER 65532:65532

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/api", "healthcheck"]

ENTRYPOINT ["/api"]
