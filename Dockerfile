# syntax=docker/dockerfile:1.7
# Multi-stage build for the cnpgctl binary.
#
# Builder stays Debian-bookworm with the mise-pinned Go version.
# Runtime is debian:12-slim (~75 MB) plus pg_dump + sops + age + tini,
# bundling every external binary cnpg-portal shells out to. The image
# ends ~200 MB compressed; pulls are infrequent (one per CI tag, one per
# host on `pgm-pull`).

ARG GO_VERSION=1.25.9
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

ARG SOPS_VERSION=3.9.2
ARG AGE_VERSION=1.2.0

# pg_dump comes from postgresql-client (apt). sops + age are static Go
# binaries downloaded from upstream releases. tini is PID 1 so SIGTERM
# from `docker stop` reaches cnpgctl cleanly.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates postgresql-client tini wget; \
    wget -qO /usr/local/bin/sops \
        "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.amd64"; \
    chmod +x /usr/local/bin/sops; \
    wget -qO- \
        "https://github.com/FiloSottile/age/releases/download/v${AGE_VERSION}/age-v${AGE_VERSION}-linux-amd64.tar.gz" \
        | tar xz -C /usr/local/bin --strip-components=1 age/age age/age-keygen; \
    apt-get purge -y wget; \
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
