# Deployment and security

## Host deployment

The supported deployment is a container on an operator-controlled host.
Mount a GitOps workspace, age private key, and kubeconfig:

```bash
export CNPG_PORTAL_WORKSPACE="$HOME/path/to/gitops-workspace"
export CNPG_PORTAL_KUBECONFIG="$HOME/.kube/config"
docker compose up -d
```

Use `docker-compose.override.yml` for identity, authorization, and local
port settings. Never commit that file.

When a reverse proxy exposes a hostname different from the internal
listener, configure its exact public origin:

```yaml
- --allowed-origin=https://portal.example-tailnet.ts.net
```

Do not use wildcard origins. Let the trusted proxy provide identity and
use `--admin-list-path` for mutation authorization.

## Verification

After rebuilding or changing configuration:

```bash
docker compose up -d --force-recreate
docker compose ps
curl -I https://portal.example-tailnet.ts.net/clusters/primary/
```

Before publishing:

```bash
mise exec -- go test ./...
mise exec -- go test -race ./internal/web
mise exec -- go vet ./...
mise exec -- golangci-lint run
govulncheck ./...
git diff --check
```

## Implemented hardening

- self-hosted browser assets and restrictive security headers;
- exact Origin validation with a Fetch Metadata fallback for same-origin
  privacy-preserving browsers;
- no web surface for remote database URLs;
- custom-format-only web dump import;
- in-memory SOPS encryption followed by atomic encrypted writes;
- sanitized request IDs and generic 5xx browser messages;
- checksum verification for downloaded SOPS and age binaries;
- current Go and dependency security updates;
- no source-control token required by the application container.

## Operator responsibilities

- Use a dedicated least-privilege ServiceAccount rather than a general
  administrator kubeconfig.
- Keep the age key and kubeconfig outside the repository with restrictive
  filesystem permissions.
- Rotate credentials immediately if they appear in logs, shell history,
  screenshots, CI artifacts, or repository history.
- Review the effective Compose configuration before startup.
- Retain structured audit events without passwords, DSNs, dump contents,
  or decrypted Secret data.
- Run periodic restore drills; a successful backup alone does not prove
  recoverability.

## Recommended next work

1. Publish a reviewed reference RBAC policy with the minimum resources and
   verbs required by every supported operation.
2. Add a disposable CNPG integration target covering create, rotate,
   branch, dump, restore, and drop.
3. Add retained audit-log integration and document retention expectations.
4. Add scheduled backup recovery drills and record recovery time.
5. Publish immutable image tags, an SBOM, signatures, and deployment
   examples pinned to an image digest.

## Intentional boundaries

- Mutations update the workspace but never commit or push it.
- Remote URL import is a trusted CLI-only operation.
- The portal stays host-side unless the identity and age-key trust
  boundaries are redesigned.
