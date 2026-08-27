BINARY  := basa
PKG     := ./cmd/basa
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/Basa-Futura/basa-cli/internal/cli.Version=$(VERSION) \
	-X github.com/Basa-Futura/basa-cli/internal/cli.Commit=$(COMMIT) \
	-X github.com/Basa-Futura/basa-cli/internal/cli.Date=$(DATE)

.DEFAULT_GOAL := check

.PHONY: build
build: ## Build for the current platform
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

.PHONY: build-all
build-all: ## Cross-compile for macOS and Linux, arm64 and amd64
	@mkdir -p dist
	@for pair in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do \
		os=$${pair%/*}; arch=$${pair#*/}; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)-$$os-$$arch $(PKG) || exit 1; \
	done
	@cd dist && shasum -a 256 $(BINARY)-* > checksums.txt
	@echo "dist/ built, checksums.txt written"

.PHONY: install
install: build ## Build and copy into ~/.local/bin
	@mkdir -p $(HOME)/.local/bin
	cp $(BINARY) $(HOME)/.local/bin/$(BINARY)
	@echo "installed to $(HOME)/.local/bin/$(BINARY)"

.PHONY: test
test: ## Run the tests
	go test ./... -count=1

.PHONY: test-race
test-race: ## Run the tests with the race detector
	go test ./... -count=1 -race

.PHONY: fmt
fmt: ## Format
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if anything is unformatted
	@test -z "$$(gofmt -l .)" || { echo "unformatted:"; gofmt -l .; exit 1; }

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: check
check: fmt-check vet test ## The inner-loop gate: run this before committing

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf dist $(BINARY)

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
