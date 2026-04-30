# cnpg-portal

A small self-service DB platform on top of [CloudNativePG](https://cloudnative-pg.io/).
A single Go binary (`cnpgctl`) provides a CLI and a web UI for daily
PostgreSQL tenant operations: create / list / branch / rotate / dump /
restore / drop, plus a connection-string copy panel that renders the
live in-cluster Secret in 6 formats (URL, Prisma, libpq, JDBC, env,
ready-to-paste `psql`).

State of record stays in **your** GitOps repo. Every mutation reads
or writes a SOPS-encrypted Secret + the CNPG `Cluster` resource's
`managed.roles` list — there's no separate control-plane database.

## Screenshots

![Tenant list](docs/screenshots/list.png)
![Tenant detail](docs/screenshots/tenant.png)

## Who this is for

A solo operator or small team running **one** CNPG cluster with a
handful of tenants, already using SOPS + age + GitOps. Single-cluster
and opinionated. If you run dozens of clusters or have a real
multi-tenant SaaS, look elsewhere.

## Prerequisites

- A Kubernetes cluster with the [CloudNativePG operator](https://cloudnative-pg.io/documentation/current/installation_upgrade/) installed.
- One running CNPG `Cluster` (single primary).
- A kubeconfig reachable from the host that runs the portal.
- An age key pair (`age-keygen`).
- A GitOps workspace repo following the [conventions](#workspace-conventions) below.
- Docker on the portal host. Tailscale optional but recommended for ingress.

## Quick start

```bash
git clone https://github.com/aberti/cnpg-portal.git ~/cnpg-portal
cd ~/cnpg-portal
cp docker-compose.override.yml.example docker-compose.override.yml
$EDITOR docker-compose.override.yml      # set auth flags + paths
docker compose up -d
```

Open `http://127.0.0.1:8080/`, or front it with `tailscale serve --bg 8080`
to reach `https://<host>.<tailnet>.ts.net/` from any tailnet device.

## Bashrc helpers

```bash
PGM_DIR="$HOME/cnpg-portal"
pgm-up()    { ( cd "$PGM_DIR" && docker compose up -d --force-recreate ) ; }
pgm-down()  { ( cd "$PGM_DIR" && docker compose down ) ; }
pgm-log()   { ( cd "$PGM_DIR" && docker compose logs -f --tail=200 ) ; }
pgm-pull()  { ( cd "$PGM_DIR" && docker compose pull && docker compose up -d --force-recreate ) ; }
pgm-ps()    { ( cd "$PGM_DIR" && docker compose ps ) ; }
pgm-config(){ ( cd "$PGM_DIR" && docker compose config ) ; }
```

`pgm-config` shows the merged compose used by the running container —
use it to debug auth / path issues.

## Workspace conventions

A Git repo containing:

- `.cnpg-portal-workspace` (root) — marker file pointing at paths:

  ```yaml
  cluster_yaml: database/my-cluster/cluster.yaml
  secrets_dir:  secrets/pg
  namespace:    pg
  pod:          my-cluster-1
  container:    postgres
  ```

- The CNPG `Cluster` CR at `cluster_yaml`. Mutating verbs patch its
  `managed.roles`.
- `secrets_dir/<tenant>-pg-credentials.sops.yaml` — one SOPS-encrypted
  K8s Secret per tenant, single `stringData.password` field. Underscores
  in tenant names normalize to hyphens in the filename.
- `.sops.yaml` (root) — SOPS encryption rules pointing at your age public
  key, `path_regex: secrets/.*\.sops\.yaml$`.

Bootstrap from scratch:

```bash
mkdir -p secrets/pg
age-keygen -o ~/.sops.age.key && chmod 600 ~/.sops.age.key
PUBKEY=$(grep -oE 'age1[a-z0-9]{58}' ~/.sops.age.key)
cat > .sops.yaml <<EOF
creation_rules:
  - path_regex: secrets/.*\.sops\.yaml\$
    age: $PUBKEY
EOF
cat > .cnpg-portal-workspace <<'EOF'
cluster_yaml: database/my-cluster/cluster.yaml
secrets_dir:  secrets/pg
namespace:    pg
pod:          my-cluster-1
container:    postgres
EOF
```

Provide the `Cluster` CR yourself (see CNPG docs for templates).

## Verbs

| Verb | CLI | UI route |
|------|-----|----|
| Create tenant | `cnpgctl new <app>` | `/new` |
| List | `cnpgctl list` | `/` |
| Per-tenant detail | `cnpgctl status <app>` | `/tenant/{name}` |
| `psql` shell | `cnpgctl psql <app>` | — |
| Branch | `cnpgctl branch <src> <dst>` | `/tenant/{name}/branch` |
| On-demand backup | `cnpgctl backup <app>` | header button |
| Stream `pg_dump -Fc` | `cnpgctl dump <app> -o file` | `/tenant/{name}/dump` |
| Drop | `cnpgctl drop <app> --yes-i-mean-it` | `/tenant/{name}/drop` |
| Rotate password | `cnpgctl rotate <app> --yes-i-mean-it` | `/tenant/{name}/rotate` |
| Active sessions | — | `/tenant/{name}/conns` |
| Restore from `Backup` CR | `cnpgctl restore <app> --backup=<name>` | `/tenant/{name}/restore` |
| Import dump file | `cnpgctl import-dump <app> --file <path>` | `/new/import-dump` |
| Import from external DSN | `cnpgctl import-url <app> --from-url <DSN>` | `/new/import-url` |

Mutating verbs and data-exfiltration verbs (`dump`, `conns`, the
connection-string panel) are **admin-gated**. Mutating verbs do **not**
auto-push to your GitOps repo — they modify the workspace tree in
place; you `git commit && git push`.

## Auth

Two layers. Identity (who the request is) → Authorization (is this
identity an admin?). Pick **one** identity source and pair it with
`--admin-list-path`:

- **Tailscale headers** (recommended). `tailscale serve` injects
  `Tailscale-User-Login`. Each tailnet user has their own identity.
- **`--dev-login=<email>`** — single identity for all requests; useful
  for laptop dev or single-user tailnet hosts.
- **`--bearer-token-file` + `--bearer-identity`** — shared-secret
  bearer; bearer-authenticated requests are admin (no allow-list
  needed).
- **`--tailnet-fallback-login`** — IP-based label for
  100.64.0.0/10 traffic without Tailscale headers.

`--admin-list-path` is a newline-separated email file mounted into the
container. SIGHUP hot-reloads it. Identities not in the file get
read-only metadata access (list, status; **no** dump, conns, or
connection-strings).

If you set `--dev-login` without `--admin-list-path`, the binary falls
back to "dev-login is admin" and warns at startup. Don't ship that to
prod.

The shipped `docker-compose.yml` has no auth flags set — vanilla
`docker compose up -d` 403s every request. Configure auth via the
override file. See [`docker-compose.override.yml.example`](docker-compose.override.yml.example).

## Architecture

```
        cnpgctl CLI ──┐         ┌── cnpg-portal web UI
                      │         │   (same binary, `cnpgctl serve`)
                      ▼         ▼
                 internal/tenant verbs
                      │         │
              client-go│         │psql via kubectl exec
                      ▼         ▼
              Kubernetes API   CNPG primary pod
```

The container bind-mounts the workspace, age key, and kubeconfig from
the host. SOPS files round-trip through the bind mount onto the host's
git tree.

## Hacking

```bash
mise install                                 # toolchain
make build && ./bin/cnpgctl serve --addr :8080 \
  --workspace /path/to/workspace \
  --kubeconfig /path/to/kubeconfig \
  --dev-login you@example.com
make dev                                     # air hot-reload
make test lint                               # before pushing
```

CLI and web UI share `internal/tenant/`. Keep verbs pure (deps in,
result + error out) and idempotent.

## Layout

```
cmd/cnpgctl/         # cobra subcommands
internal/tenant/     # verb implementations
internal/cnpg/       # CNPG Cluster + Backup CR helpers
internal/k8s/        # client-go + remote-exec
internal/pg/         # psql via kubectl exec, identifier safety
internal/clusteryaml/# YAML patcher (preserves comments)
internal/sops/       # `sops` shell-out + native age encrypt
internal/connstr/    # 6-format connection-string renderer
internal/web/        # chi server + templ + identity middleware
internal/workspace/  # marker discovery
docs/adr/            # architecture decisions
```

## Author

**A. Beresniewicz** ([@aberti](https://github.com/aberti)).

## License

MIT — see [LICENSE](LICENSE).
