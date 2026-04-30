# ADR-0004 — Dockerize for portability

Status: Accepted
Builds on: ADR-0003 (deploy on a host, not in-cluster)

## Context

ADR-0003 picked a host as the runtime: it already holds the SOPS age
key, the GitOps workspace, a kubeconfig, and the optional GitHub PAT.
That reused-credentials story remains unchanged.

The cost: cnpg-portal is glued to one specific machine by a chain of
host-level deps — Go toolchain, `sops`, `age`, `pg_dump`, kubeconfig
path, age key path, shell helpers, and (typically) an SSH tunnel to
the cluster API. Replacing the host or running cnpg-portal from a
second host means re-doing all of that from memory.

We want the runtime reproducible and movable in two commands on any
tailnet-connected Docker host.

## Decision

Ship `cnpgctl serve` as a Docker image, published to
`ghcr.io/aberti/cnpg-portal:latest` (public package, source repo
public). Run it via `docker-compose.yml` with a local override for
operator-specific paths and auth. Keep a native-binary, live-reload
path for development.

### Image

Multi-stage Dockerfile. Builder stage produces a stripped, trimpathed
`cnpgctl`. Runtime stage:

- `debian:12-slim` base.
- `postgresql-client` from apt → `pg_dump` (used by `import-url`).
- `sops` and `age` binaries downloaded from upstream releases.
- `tini` as PID 1 so SIGTERM from `docker stop` reaches `cnpgctl`.

Image is ~377 MB. Pulls are infrequent — once per CI tag, once per
host on `pgm-pull`.

### Runtime configuration

`docker-compose.yml` is opinionated for the maintainer's host;
operators copy `docker-compose.override.yml.example` →
`docker-compose.override.yml` and edit auth flags + bind-mount paths.
Compose auto-merges the override when invoked from the project
directory (so the bashrc helpers `cd` first).

`network_mode: host` because the kubeconfig in most setups points at
`https://127.0.0.1:6443` — an SSH tunnel on the host. Sharing the
host network namespace is simpler than rewriting kubeconfigs or
configuring `host-gateway`. It also lets `tailscale serve` on the
host front the container's port 8080 unchanged.

`user: "1000:1000"` (or `${UID}:${GID}` in the override) so the
bind-mounted age key (mode 0600 on host) is readable AND so SOPS
files written into the workspace are owned by the host user, not
container `nonroot`.

### Image visibility

Published as a **public** ghcr.io package so any host can `docker
pull` without authenticating. The compiled binary is stripped + trimpathed;
no source files, no `go.sum`, no secrets in the final stage.

## Consequences

- **Two-command bootstrap on a fresh host**: clone the repo, `docker
  compose up -d` (assuming Docker, an age key, and a kubeconfig are
  in place).
- **Upgrades by `pgm-pull`**: `docker compose pull && up -d
  --force-recreate`. No mise, no apt, no go build.
- **`network_mode: host` weakens isolation** — same trust surface as
  running the binary natively on the host. For a single-operator tool
  this is the baseline we're matching.
- **Two paths to maintain** — `pgm-up` for daily container, `pgm-dev`
  for native-binary live reload. One extra block of bashrc helpers.

## What this rejects

- **Distroless static runtime**: would lose `pg_dump`. import-url is
  exercised in production.
- **Self-hosted registry**: would require imagePullSecrets pattern;
  ghcr.io public is friction-free.
- **Private ghcr.io image**: free GitHub plan caps private package
  storage at 500 MB and bandwidth at 1 GB/month. Public removes the
  cap and the auth requirement; the binary leaks no secrets.
- **`docker compose watch` for live reload**: minutes-long image
  rebuilds per file change vs the existing sub-second `air`-driven
  native rebuild. Keep `pgm-dev` for that.

## Operational rollback

If anything in the Docker path breaks, the native-binary path is the
escape hatch:

```bash
pgm-down   # stops the container
pgm-dev    # tmux + native binary
```

Both paths are additive in the repo; switching back is two function
calls.
