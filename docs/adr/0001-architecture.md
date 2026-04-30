# ADR-0001 — Architecture

Status: Accepted

## Context

A small operator wants Neon-flavoured tenant operations (create / list /
branch / rotate / dump / restore / drop) over a single CloudNativePG
cluster, without standing up a separate database control plane or a
parallel state store.

## Decision

cnpg-portal is a single Go binary, `cnpgctl`, that exposes both a CLI
and a web UI (the same binary in two transports — the web UI is
`cnpgctl serve`). Both call the same verb package
(`internal/tenant/`), which holds pure functions taking a `Deps` bundle
(K8s client, psql, workspace) and returning a result + error.

State of record is the operator's **GitOps workspace** — any Git
repository that follows the conventions in
[README.md → Workspace conventions](../../README.md#workspace-conventions):

- A `.cnpg-portal-workspace` marker at the repo root.
- The CNPG `Cluster` CR at the marker's `cluster_yaml` path. Mutating
  verbs patch its `managed.roles` list.
- A `secrets_dir/` of SOPS-encrypted K8s `Secret` files, one per
  tenant.
- A `.sops.yaml` at the repo root pointing at the operator's age key.

There is no parallel control-plane database. Every mutation reads or
writes the same files an SRE would edit by hand; the portal is a
reasoning layer on top.

### Verb package boundary

```
cnpgctl CLI (host) ──┐         ┌── cnpg-portal web UI
                     │         │   (cnpgctl serve)
                     ▼         ▼
            internal/tenant verbs
                     │
                     ▼
       client-go + psql via kubectl exec
                     │
                     ▼
            CNPG cluster + primary pod
```

Verbs do not open PRs or push commits. They mutate the workspace tree
(SOPS file, cluster.yaml, K8s Secret) in place and exit; the operator
runs `git commit && git push`. The trust boundary stays at the
operator's local environment, with no long-lived GitHub credentials
inside the binary.

### Auth model

Two layers, both reified in `internal/web/identity.go`:

1. **Identity**: who is this request? Picked from
   `Tailscale-User-Login` header → bearer token → `--dev-login`. With
   none of those resolved, the request 403s.
2. **Authorization**: is this identity an admin? Read from
   `--admin-list-path` (a newline-separated email file, SIGHUP-reloadable).
   Mutating verbs and data-exfiltration paths (dump, conns,
   connection-string panel) require admin; everything else returns
   metadata only.

A "dev-login" laptop fallback grants admin to the dev-login identity
when no allow-list is set. Logged at WARN level — pair `--dev-login`
with `--admin-list-path` for any deployment where the binary is
network-reachable beyond the operator.

### Connection-string panel

For each tenant, the detail page renders the live K8s Secret in 6
formats (URL, Prisma, libpq, JDBC, env, ready-to-paste `psql`).
Implementation in `internal/connstr/` is a pure renderer over a
`(host, port, database, user, password)` tuple; identity headers
gate access (admin only).

## Consequences

- **Single trust boundary**: the operator's host. Age key, kubeconfig,
  workspace all live there. No long-lived credentials in the cluster.
- **GitOps-native**: every verb leaves the workspace in a state ready
  to commit; no out-of-band state to reconcile.
- **Not for multi-tenant SaaS**: there is no per-customer billing,
  isolation policy, or tenancy model beyond "one Postgres role + one
  database per tenant".

## What this rejects

- A separate control-plane database for portal state. Adding one would
  duplicate cluster.yaml + the SOPS files.
- An in-cluster Deployment of the portal. See ADR-0003 for why that
  failed on the trust-boundary check.
- Auto-pushing to the GitOps repo from the binary. Caller controls
  commits; the binary stays trust-bounded.
