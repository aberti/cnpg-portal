SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

MODULE      := github.com/aberti/cnpg-portal
BIN         := cnpgctl
DIST        := dist
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
               -X $(MODULE)/internal/version.Version=$(VERSION) \
               -X $(MODULE)/internal/version.Commit=$(COMMIT) \
               -X $(MODULE)/internal/version.BuildDate=$(DATE)
IMAGE       ?= ghcr.io/aberti/cnpg-portal
IMAGE_TAG   ?= latest

.PHONY: help
help:
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z_-]+:.*##/{printf "  \033[36m%-14s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: generate
generate: ## regenerate templ + any go generate sources
	templ generate ./internal/web/templates
	go generate ./...

.PHONY: build
build: generate ## compile binary into ./bin
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/$(BIN)

.PHONY: install
install: ## install cnpgctl into $$GOBIN/$$HOME/go/bin
	go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/$(BIN)

.PHONY: dev
dev: ## hot-reload `cnpgctl serve` on :8080 via air
	air -c .air.toml

.PHONY: install-dev
install-dev: build ## rebuild binary; restart the pgm tmux session if running (pgm-dev path)
	tmux -L pgm kill-session -t pgm 2>/dev/null || true
	@echo "binary rebuilt at ./bin/$(BIN); run 'pgm-dev' to restart the live-reload portal"

.PHONY: image
image: ## build container image locally as $(IMAGE):$(IMAGE_TAG)
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE):$(IMAGE_TAG) .

.PHONY: image-up
image-up: ## docker compose up -d (the daily-driver pgm-up path)
	docker compose up -d

.PHONY: image-down
image-down: ## docker compose down
	docker compose down

.PHONY: image-pull
image-pull: ## docker compose pull && up -d (deploy newest :latest)
	docker compose pull
	docker compose up -d

.PHONY: test
test: ## unit tests
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## integration tests against CNPG-on-kind via testcontainers-go
	go test -race -count=1 -tags=integration -timeout=15m ./test/integration/...

.PHONY: lint
lint: ## golangci-lint run
	golangci-lint run

.PHONY: fmt
fmt: ## gofmt + goimports
	gofmt -s -w .
	go run golang.org/x/tools/cmd/goimports@latest -w -local $(MODULE) .

.PHONY: release-snapshot
release-snapshot: ## goreleaser snapshot (no publish)
	goreleaser release --snapshot --clean

.PHONY: clean
clean: ## remove build artifacts
	rm -rf bin $(DIST)
