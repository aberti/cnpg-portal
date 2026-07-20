# syntax=docker/dockerfile:1.7
# Multi-stage build for the cnpgctl binary.
#
# Builder stays Debian-bookworm with the mise-pinned Go version.
# Runtime is debian:12-slim (~75 MB) plus pg_dump + sops + age + tini,
# bundling every external binary cnpg-portal shells out to. The image
# ends ~200 MB compressed; pulls are infrequent (one per CI tag, one per
# host on `pgm-pull`).

ARG GO_VERSION=1.25.12
FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Cache deps separately from sources for faster rebuilds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/aberti/cnpg-portal/internal/version.Version=${VERSION} \
        -X github.com/aberti/cnpg-portal/internal/version.Commit=${COMMIT} \
        -X github.com/aberti/cnpg-portal/internal/version.BuildDate=${DATE}" \
      -o /out/cnpgctl ./cmd/cnpgctl

# Runtime: debian-slim with pg_dump + sops + age + tini.
FROM debian:12-slim AS runtime

ARG SOPS_VERSION=3.12.2
ARG SOPS_SHA256=14e2e1ba3bef31e74b70cf0b674f6443c80f6c5f3df15d05ffc57c34851b4998
ARG AGE_VERSION=1.3.1
ARG AGE_SHA256=bdc69c09cbdd6cf8b1f333d372a1f58247b3a33146406333e30c0f26e8f51377

# pg_dump comes from PGDG's postgresql-client-17 (Debian's own package
# is PG 15, which can't dump from PG 16/17 servers — Neon, modern CNPG,
# etc. The "import-from-URL" path needs pg_dump >= source-server major).
# sops + age are static Go binaries from upstream releases. tini is PID
# 1 so SIGTERM from `docker stop` reaches cnpgctl cleanly.
ARG PG_MAJOR=17
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates gnupg tini wget; \
    install -d /etc/apt/keyrings; \
    wget -qO- https://www.postgresql.org/media/keys/ACCC4CF8.asc \
        | gpg --dearmor -o /etc/apt/keyrings/pgdg.gpg; \
    echo "deb [signed-by=/etc/apt/keyrings/pgdg.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
        > /etc/apt/sources.list.d/pgdg.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        "postgresql-client-${PG_MAJOR}"; \
    wget -qO /usr/local/bin/sops \
        "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.amd64"; \
    echo "${SOPS_SHA256}  /usr/local/bin/sops" | sha256sum -c -; \
    chmod +x /usr/local/bin/sops; \
    wget -qO /tmp/age.tar.gz \
        "https://github.com/FiloSottile/age/releases/download/v${AGE_VERSION}/age-v${AGE_VERSION}-linux-amd64.tar.gz"; \
    echo "${AGE_SHA256}  /tmp/age.tar.gz" | sha256sum -c -; \
    tar xzf /tmp/age.tar.gz -C /usr/local/bin --strip-components=1 age/age age/age-keygen; \
    apt-get purge -y wget gnupg; \
    apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/cnpgctl /usr/local/bin/cnpgctl

# Mount points used by docker-compose; create the dirs so a runtime that
# bind-mounts them owns the parent.
RUN mkdir -p /workspace /run/secrets

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/cnpgctl"]
CMD ["serve", "--addr", ":8080"]
