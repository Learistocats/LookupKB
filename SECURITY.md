# Security policy

## Supported versions

Only the latest release receives security fixes. LookupKB is pre-1.0; upgrade to
the newest `vX.Y.Z` tag (images on `ghcr.io/learistocats/lookupkb-{api,web}`).

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately via GitHub:
[**Security → Report a vulnerability**](https://github.com/Learistocats/LookupKB/security/advisories/new).

Include what you can: affected version/commit, a description of the issue, steps
to reproduce, and its impact. You should get an acknowledgement within a few days;
once a fix is released the advisory is published with credit (unless you prefer
otherwise).

## Scope

In scope: the Go API (`api/`), the SvelteKit web app (`web/`), the published
container images, and the reference Compose stack (`deploy/`).

Out of scope: third-party services the stack runs (Postgres, Valkey, Meilisearch,
Kroki, Garage) — report those upstream — and deployments that diverge from the
documented defaults (e.g. exposing datastores publicly, reusing the
`.env.example` dev secrets).

## Verifying release images

Release images are signed keylessly with [cosign](https://github.com/sigstore/cosign)
and carry SBOM + build provenance attestations:

```bash
cosign verify ghcr.io/learistocats/lookupkb-api:<version> \
  --certificate-identity-regexp '^https://github.com/Learistocats/LookupKB/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```
