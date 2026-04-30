# CLAUDE.md — session onboarding

`cnpg-portal` — Go binary (`cnpgctl`) providing CLI + web UI for daily ops
on a CloudNativePG cluster. See [README.md](README.md) for what the app
does and the workspace conventions it expects.

## Layout

- `cmd/` — entrypoints (cobra subcommands, one file per verb).
- `internal/` — implementation (verb code, k8s/pg/sops adapters, web).
- `pkg/` — exported helpers; keep small and avoid unless needed.
- Standard Go project: `go.mod`, `Makefile`, `.golangci.yml`,
  `.air.toml` (live reload via `make dev`).
- Releases via `.goreleaser.yaml`; container build via `Dockerfile`.
- Architecture decisions in `docs/adr/` — read those before proposing
  large changes.

## Working style

- Conventional Commits. `pre-commit run --all-files` before large PRs.
- `go vet`, `golangci-lint run`, `go test ./...` before pushing.
- Commit often, small.
- For UI work, use `make dev` (air hot-reload) on the operator's host;
  for runtime work, `pgm-up` against the locally-built image.

## House rules

- **Drop ceremony, go simple.** No premature abstractions, no
  speculative interfaces.
- **Honest cost/benefit framing.** Concrete reasoning, no hedging
  padding.
- **Ask before risky action**: anything touching the live cluster, DNS,
  or pushing to `main`.

## On session start

`git status && git log --oneline -5`, then ask what to work on.
