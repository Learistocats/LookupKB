# syntax=docker/dockerfile:1

# ---- build stage -------------------------------------------------------------
# Pin the toolchain image by digest. Digests are kept fresh by Dependabot.
# Runs on the build host's platform and cross-compiles (GOARCH below), so
# multi-arch release builds don't compile Go under QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm@sha256:632dff7351799a5a9954a48dc06670ade72ee44e8c351cdc0e5a57af2fea21af AS build

WORKDIR /src

# Cache modules first for faster incremental builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY . .

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
# CGO disabled + static build so the binary runs on a scratch/distroless base.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/api ./cmd/api

# ---- runtime stage -----------------------------------------------------------
# distroless/static: no shell, no package manager, minimal attack surface.
# nonroot variant runs as uid 65532. Pinned by digest.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

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
