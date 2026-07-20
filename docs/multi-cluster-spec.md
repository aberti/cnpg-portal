# Multi-cluster management specification

Status: implemented

## Goal

One `cnpgctl` process can operate several CloudNativePG `Cluster`
resources without hiding the selected target in session state. The
workspace remains the only configuration source and tenant verbs remain
independent of routing.

## Configuration

The `.cnpg-portal-workspace` marker supports a named cluster catalog:

```yaml
default_cluster: primary
clusters:
  primary:
    display_name: Primary workloads
    cluster_yaml: database/primary/cluster.yaml
    secrets_dir: secrets/pg
    namespace: database
    pod: postgres-main-1
    container: postgres
    cluster_name: postgres-main
    service_name: postgres-main
  analytics:
    display_name: Analytics
    cluster_yaml: database/analytics/cluster.yaml
    secrets_dir: secrets/pg
    namespace: database
    pod: postgres-analytics-1
    container: postgres
    cluster_name: postgres-analytics
    service_name: postgres-analytics
    secret_prefix: analytics
```

`default_cluster` is required when the catalog has more than one entry.
A catalog entry ID must be URL-safe. Paths must remain inside the
workspace root.

The legacy flat marker stays supported and resolves as a single target
named `default`.

## Selection contract

- CLI: `--cluster <id>`, then `CNPG_PORTAL_CLUSTER`, then
  `default_cluster`.
- Web: every application route is scoped under `/clusters/{id}/`.
- The root route redirects to the configured default.
- Unknown IDs return `404`; they never fall back to the default.
- The active cluster appears in navigation, commands, logs, and audit
  context.
- Cluster selection is not stored in a cookie.

## Isolation contract

Each selected target supplies its own:

- CNPG manifest path;
- namespace, pod, and container;
- CNPG resource name and stable service name;
- SOPS Secret directory;
- optional credential Secret prefix.

`secret_prefix` is required when equal tenant names in a shared namespace
could otherwise produce the same Kubernetes Secret name. It does not
rename the PostgreSQL role or database.

Tenant mutation code receives one resolved workspace target. It cannot
select or switch clusters internally.

## Web behavior

- The application bar provides an explicit cluster selector.
- Tenant inventory and detail views retain the active cluster in every
  link and form action.
- Backup status and actions apply to the active CNPG resource.
- Shared or unmanaged database owners are visible but password controls
  are hidden unless the database has a matching dedicated login role.
- Existing externally managed database names may contain hyphens. Their
  detail, activity, dump, `psql`, and branch-source paths remain available.
- Databases using a shared external owner role do not expose portal
  credential, sync, rotate, restore, or drop controls.
- Remote URL import is CLI-only.
- Browser dump import accepts PostgreSQL custom-format files only.

## Security requirements

- Served deployments use proxy-supplied identity plus an explicit admin
  allow-list; `--dev-login` is development-only.
- Mutations require a trusted Origin, or browser-controlled
  `Sec-Fetch-Site: same-origin` when a privacy-preserving browser omits
  Origin.
- A foreign Origin always wins over Fetch Metadata and is rejected.
- Public proxy origins are exact allow-list entries; wildcards are not
  supported.
- SOPS plaintext is encrypted in memory and encrypted files are replaced
  atomically.
- Application errors do not expose internal command output to browsers.
- Repository examples, tests, and screenshots use synthetic data only.

## Acceptance criteria

- Both a legacy marker and a two-target catalog load successfully.
- CLI operations select the requested target.
- URLs for two targets remain distinct through list, detail, form, and
  mutation flows.
- A hyphenated database owned by a shared external role opens from
  inventory and reports the actual owner role.
- Equal tenant names produce distinct Secret names when prefixes differ.
- Valid public proxy Origin variants are accepted.
- Foreign, ambiguous, and unmarked mutation requests are rejected.
- Desktop and mobile inventory views remain usable with the cluster
  selector present.
- Unit tests, race tests, vet, lint, and vulnerability scanning pass.

## Out of scope

- Cross-cluster tenant migration.
- Automatic Git commits, pushes, or pull requests.
- A portal control-plane database.
- Billing, customer self-service, or per-tenant Kubernetes isolation.
- Automatic discovery of all CNPG resources visible to a kubeconfig.
