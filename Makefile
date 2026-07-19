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

.PHONY: all build install test lint fmt tidy clean

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
