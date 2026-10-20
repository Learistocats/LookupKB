# Contributing to LookupKB

Thanks for your interest! Bug reports, ideas and pull requests are welcome.

## Getting set up

Prerequisites: Go 1.26, Node 22 + pnpm, Docker (for the stack and integration tests).

```bash
make up            # build + start the full stack (web on :3000)
make test          # Go tests + web unit tests
make lint          # go vet + golangci-lint + eslint + svelte-check
cd api && go test -tags integration ./internal/db/...   # needs Docker
```

For fast iteration without containers, see *Local development* in the
[README](README.md). `make help` lists every target.

If you edit SQL in `api/internal/db/queries/`, run `make sqlc` and commit the
generated `dbgen/` output — CI fails when it is stale.

## Branches

- **`develop`** is the integration branch. Open pull requests against `develop`.
- **`main`** holds released code only. Maintainers merge `develop` → `main` to cut
  a release.

PRs into `develop` are **squash-merged**, so the PR title becomes the commit
message.

## Commit / PR title convention

Titles follow [Conventional Commits](https://www.conventionalcommits.org/) — CI
checks the PR title, and the release changelog is generated from it:

```
feat: add tag filter to search
fix(web): keep the cursor position after autosave
chore(deps): bump vite to 6.4.3
```

Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`,
`revert`. Mark breaking changes with `!` (`feat!: …`).

## Pull request checklist

- `make lint test` passes locally.
- New behaviour has tests (Go handlers are tested against a fake store; see
  `api/internal/server/*_test.go`).
- No secrets, credentials or personal data in code, fixtures or logs.
- Keep the product rules intact: Markdown is rendered and sanitized **once at
  publish** (never on read), drafts are never indexed, and access checks happen
  server-side without leaking whether a document exists.

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please):
merging to `main` updates a release PR; merging that PR tags `vX.Y.Z`, publishes
multi-arch images to GHCR (signed, with SBOM + provenance) and attaches a deploy
bundle to the GitHub Release.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
