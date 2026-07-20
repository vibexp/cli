# vibexp CLI — developer tasks.
BINARY      := vibexp
CMD         := ./cmd/vibexp
BIN_DIR     := bin

# Build metadata injected via ldflags into internal/version.
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE          ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
INSTALL_SOURCE ?= source

VERSION_PKG := github.com/vibexp/cli/internal/version
LDFLAGS     := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE) \
	-X $(VERSION_PKG).InstallSource=$(INSTALL_SOURCE)

export CGO_ENABLED := 0

.PHONY: all build install test lint fmt tidy clean e2e e2e-stack-up e2e-stack-down

E2E_COMPOSE := docker compose -f e2e/docker-compose.yml

all: lint test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

# E2E suite (docs/e2e.md). Consumes VIBEXP_CLI_TEST_URL/VIBEXP_CLI_TEST_API_KEY
# from the environment (the dev environment provides them automatically) and
# skips cleanly when they are absent. -count=1: e2e runs are never cacheable.
e2e:
	go test -tags e2e -count=1 -v ./e2e/

# Ephemeral local stack mirroring the CI job — boot, then mint a key with
# e2e/bootstrap.sh. VIBEXP_E2E_IMAGE overrides the platform image tag.
e2e-stack-up:
	$(E2E_COMPOSE) up -d --wait

e2e-stack-down:
	$(E2E_COMPOSE) down -v
