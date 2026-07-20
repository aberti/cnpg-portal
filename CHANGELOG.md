# Changelog

All notable changes to cnpg-portal are documented here. The project
follows [Conventional Commits](https://www.conventionalcommits.org/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased] — initial public release

### Multi-cluster

- A workspace marker can now declare a catalog of CNPG targets and a
  `default_cluster`.
- CLI commands select a target through `--cluster` or
  `CNPG_PORTAL_CLUSTER`.
- Web routes are cluster-scoped under `/clusters/{id}/...`, with an
  explicit cluster selector in the application bar.
- Optional per-cluster `secret_prefix` prevents credential Secret name
  collisions when clusters share a Kubernetes namespace.
- Existing databases with hyphenated names and shared external owner roles
  can be inspected, dumped, connected to, and used as branch sources without
  exposing shared credentials or unsafe lifecycle controls.

### Security and interface

- Added strict mutation Origin checking with repeatable
  `--allowed-origin` support for reverse-proxied public hosts.
- Added CSP and defensive browser headers; assets are now self-hosted.
- Web dump import accepts custom-format PostgreSQL dumps only. Remote URL
  import is CLI-only.
- SOPS Secrets are built and encrypted in memory before atomic writes, so
  plaintext credentials are never written to the workspace.
- Dependency and runtime toolchain versions were updated; release binaries
  for SOPS and age are checksum-verified during image construction.
- Reworked desktop and mobile views with cluster context, summary cards,
  responsive tenant inventory, clearer ownership status, and explicit
  operation/danger areas.

### Verbs

- `cnpgctl new <app>` — provision a tenant: role, database, K8s Secret,
  SOPS-encrypted Secret in the workspace, `cluster.yaml` `managed.roles`
  entry. Idempotent against existing databases (ALTER OWNER + GRANT).
- `cnpgctl list` / `cnpgctl status <app>` — read-only inventory + per-tenant
  detail (size, connections, last cluster backup, role attributes).
- `cnpgctl psql <app>` — shells out to `kubectl exec` for an interactive
  psql session inside the CNPG primary pod.
- `cnpgctl branch <src> <dst>` — in-cluster logical clone via
  `pg_dump | pg_restore` running inside the primary pod.
- `cnpgctl backup <app>` — trigger an on-demand CNPG `Backup` CR.
- `cnpgctl dump <app> -o file` — stream `pg_dump -Fc` of a tenant.
- `cnpgctl drop <app> --yes-i-mean-it` — full teardown: terminate sessions,
  DROP DATABASE + ROLE, delete K8s Secret, remove SOPS file, patch
  `cluster.yaml`.
- `cnpgctl rotate <app> --yes-i-mean-it` — generate a fresh password,
  ALTER ROLE, update K8s Secret, re-encrypt SOPS file.
- `cnpgctl restore <app> --backup=<name> [--target-time=<rfc3339>]` —
  provision a temporary recovery `Cluster` from a CNPG backup, stream
  `pg_dump | pg_restore` into the live tenant, delete the temp cluster.
- `cnpgctl import-dump <app> --file <dump>` — provision tenant + load an
  uploaded `pg_dump -Fc` or plain SQL file.
- `cnpgctl import-url <app> --from-url <DSN>` — provision tenant + pull
  data from a reachable external Postgres via local `pg_dump` streamed
  to in-cluster `pg_restore`.

### Web UI

- All admin verbs are mirrored as `/tenant/{name}/<verb>` routes,
  gated by Tailscale identity headers (or one of the auth escape hatches:
  bearer token, dev-login, tailnet-CGNAT fallback).
- Connection-string panel renders the live K8s Secret in 6 formats
  (URL, Prisma, libpq, JDBC, env, `psql` one-liner).
- Active-connections viewer with per-pid terminate (admin-gated).

### Deployment

- Self-contained debian-slim container image at
  `ghcr.io/aberti/cnpg-portal:latest`. Bundles `cnpgctl`, `sops`, `age`,
  `pg_dump`, `tini`. Published from CI on every merge to `main`.
- `docker-compose.yml` at repo root with bind-mount layout for the
  GitOps workspace, age key, kubeconfig, and operator env.
- bashrc helpers (`pgm-up` / `pgm-down` / `pgm-pull`) in the README's
  Operator setup; tmux-managed live-reload path documented as `pgm-dev`.

### Architecture decisions

- [ADR-0001](docs/adr/0001-architecture.md) — overall architecture;
  verb-package boundary; auth model.
- [ADR-0003](docs/adr/0003-deployment-on-dev-host-not-cluster.md) —
  why this runs on a host instead of in-cluster.
- [ADR-0004](docs/adr/0004-dockerize-for-portability.md) — why this
  ships as a container.
