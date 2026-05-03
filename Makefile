# qdesk Makefile — common developer + release tasks.
#
# `make help` lists everything. Most useful day-to-day: `make build test smoke`.

GO              ?= go
DOCKER          ?= docker
DIST_DIR        ?= dist
INSTALL_PREFIX  ?= /usr/local
SANDBOX_IMAGE   ?= qdesk/ubuntu-chrome:dev
APT_MIRROR      ?= mirrors.aliyun.com
GOPROXY_BUILD   ?= https://goproxy.cn,direct

# Version + commit injection.
VERSION         := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT          := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS         := -s -w \
                   -X github.com/jeffwang/qdesk/pkg/version.Version=$(VERSION) \
                   -X github.com/jeffwang/qdesk/pkg/version.Commit=$(COMMIT)

# Cross-compile matrix for the host-side binaries (qdesk, qdesk-control, qdesk-mcp).
# qdesk-agentd ships only as linux/amd64 because it lives inside the sandbox image.
HOST_BINARIES   := qdesk qdesk-control qdesk-mcp
HOST_PLATFORMS  := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nqdesk targets:\n"} /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo
	@echo "Common combos:"
	@echo "  make build test smoke      — verify everything still works"
	@echo "  make image                 — rebuild sandbox docker image"
	@echo "  make demo                  — full pipeline against examples/recompdaily"
	@echo "  make release VERSION=v0.1.0 — see scripts/release.sh"
	@echo

# ----- Build -----

.PHONY: build
build: ## Build all 4 binaries into dist/ (host platform).
	@mkdir -p $(DIST_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/qdesk-agentd  ./cmd/qdesk-agentd
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/qdesk-control ./cmd/qdesk-control
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/qdesk         ./cmd/qdesk
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(DIST_DIR)/qdesk-mcp     ./cmd/qdesk-mcp
	@ls -la $(DIST_DIR)

.PHONY: build-cross
build-cross: ## Cross-compile host binaries for $(HOST_PLATFORMS).
	@mkdir -p $(DIST_DIR)
	@set -e; \
	for p in $(HOST_PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  for b in $(HOST_BINARIES); do \
	    out=$(DIST_DIR)/$$b-$$os-$$arch; \
	    echo "  →  $$out"; \
	    GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
	      $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $$out ./cmd/$$b; \
	  done; \
	done
	@ls -la $(DIST_DIR)

.PHONY: install
install: build ## Install binaries to $(INSTALL_PREFIX)/bin (may need sudo).
	install -m 0755 $(DIST_DIR)/qdesk-agentd  $(INSTALL_PREFIX)/bin/
	install -m 0755 $(DIST_DIR)/qdesk-control $(INSTALL_PREFIX)/bin/
	install -m 0755 $(DIST_DIR)/qdesk         $(INSTALL_PREFIX)/bin/
	install -m 0755 $(DIST_DIR)/qdesk-mcp     $(INSTALL_PREFIX)/bin/
	@echo "Installed to $(INSTALL_PREFIX)/bin"

# ----- Test -----

.PHONY: test
test: ## Run unit tests.
	$(GO) test ./...

.PHONY: vet
vet: ## go vet all packages.
	$(GO) vet ./...

.PHONY: fmt
fmt: ## go fmt all files.
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## go mod tidy.
	$(GO) mod tidy

# ----- Sandbox image + smoke test -----

.PHONY: image
image: ## Build the qdesk/ubuntu-chrome sandbox docker image.
	$(DOCKER) build \
	  --build-arg APT_MIRROR=$(APT_MIRROR) \
	  --build-arg GOPROXY=$(GOPROXY_BUILD) \
	  -t $(SANDBOX_IMAGE) \
	  -f images/ubuntu-chrome/Dockerfile .

.PHONY: smoke
smoke: ## Run the end-to-end sandbox smoke test (needs $(SANDBOX_IMAGE)).
	IMAGE=$(SANDBOX_IMAGE) ./scripts/smoke-sandbox.sh

# ----- Demo / dogfood -----

.PHONY: demo
demo: build image ## Full pipeline against examples/recompdaily-landing (requires $$GEMINI_API_KEY).
	@./scripts/demo.sh

# ----- MCP install convenience -----

.PHONY: mcp-install
mcp-install: install ## Register qdesk-mcp with Claude Code (claude mcp add).
	@command -v claude >/dev/null 2>&1 || { echo "Claude Code CLI not on PATH; install it first."; exit 1; }
	claude mcp add --transport stdio qdesk -- $(INSTALL_PREFIX)/bin/qdesk-mcp \
	  --control http://127.0.0.1:8090 \
	  --api-key "$$QDESK_DEV_KEY" \
	  --gemini-key "$$GEMINI_API_KEY"
	@echo "qdesk MCP server registered."

# ----- Release -----

.PHONY: release
release: ## Cut a versioned release. Usage: make release VERSION=v0.1.0
	@if [ -z "$(VERSION_ARG)" ] && [ -z "$$VERSION" ]; then \
	  echo "Usage: make release VERSION=v0.1.0"; exit 2; fi
	@VERSION=$${VERSION:-$(VERSION_ARG)} ./scripts/release.sh

# ----- Cleanup -----

.PHONY: clean
clean: ## Remove dist/ and trace artifacts.
	rm -rf $(DIST_DIR) qdesk-runs/ qdesk-control.db*

.PHONY: ci
ci: vet test build image smoke ## What CI runs.
