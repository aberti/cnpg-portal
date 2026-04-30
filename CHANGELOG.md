# Changelog

All notable changes to cnpg-portal are documented here. The project
follows [Conventional Commits](https://www.conventionalcommits.org/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased] — initial public release

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
