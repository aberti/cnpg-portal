# ADR-0003 — Deploy on a host, not in-cluster

Status: Accepted

## Context

A previous design ran cnpg-portal as a Kubernetes Deployment on the
same cluster it managed. Three blockers killed that direction:

1. **The pod can't do its job without the SOPS age private key.**
   Connection-string rendering and any encrypt-then-commit flow need
   it. Mounting the cluster-wide age key into a single user-facing pod
   would mean pod compromise = age compromise.
2. **Auth has no clean answer in-cluster.** Traefik (and most generic
   ingresses) don't inject Tailscale identity headers. Bearer tokens,
   CGNAT fallback, and the Tailscale K8s operator all work but each
   adds infrastructure for one app.
3. **Single-operator redundancy buys nothing.** Higher cluster uptime
   than the operator's host is moot when every mutation is user-driven
   anyway.

## Decision

cnpg-portal runs as a process on a tailnet-connected host. `tailscale
serve` on that host terminates inbound connections, injects identity
headers, and exposes the portal at `https://<host>.<tailnet>.ts.net/`.

The host owns the trust-relevant material: SOPS age key, GitOps
workspace, kubeconfig with cluster access, optional GitHub PAT. The
binary reads them from disk paths the operator already manages.

## Rationale

- Reusing the operator's existing credential surface (no new secrets
  to provision) means no new attack surface either.
- `tailscale serve` injects `Tailscale-User-Login` directly into the
  proxied request — the middleware in `internal/web/identity.go`
  consumes that header without further plumbing.
- Tailnet ACL is the network boundary. Off-tailnet clients can't even
  resolve `*.ts.net` names.
- Lifecycle is whatever the operator wants: bashrc helpers calling
  `docker compose up -d`, a systemd unit, a tmux session — all easy
  to swap.

## Consequences

- Host availability ≈ portal availability. Acceptable for a single
  operator.
- Multi-user growth would re-open this decision. Two viable paths at
  that point: (a) Tailscale K8s operator + in-cluster pod with an
  OIDC bridge, (b) keep host-side and lean on `--admin-list-path` for
  per-user authorization. Choose then.

## Auth modes shipped

To support the few cases where Tailscale headers aren't available,
the binary also accepts:

- `--bearer-token-file` + `--bearer-identity` — shared-secret bearer
  via `Authorization: Bearer <token>`.
- `--tailnet-fallback-login` — IP-based label for 100.64.0.0/10
  traffic when no header is injected.
- `--dev-login` — single label for laptop/dev work.

Pair any of these with `--admin-list-path` so the dev-login fallback
doesn't auto-promote requests to admin.
