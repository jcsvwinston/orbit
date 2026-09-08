# Orbit — pluggable observability/admin product for Nucleus (ADR-019).
#
# The repository is a go.work workspace: the root module (./) plus three
# modules under proto/, agent/, and server/ that make up the cluster
# observability subsystem, and a React/Vite dashboard under ui/.
#
# Targets:
#   * proto-*    — the admin observability proto (see proto/).
#   * build/vet/test — workspace-wide Go convenience targets.
#   * fuzz/fuzz-seeds — the native Go fuzz targets over the parsing surfaces.

ROOT := $(shell pwd)

# Allow callers to override executables (e.g. `make BUF=/opt/buf proto`).
GO  ?= go
BUF ?= buf

# How long `make fuzz` mutates each target for. `make fuzz FUZZTIME=5m` before
# touching one of the parsing surfaces those targets cover.
FUZZTIME ?= 20s

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN { FS = ":.*?## " } \
	  /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' \
	  $(MAKEFILE_LIST)

# ----------------------------------------------------------------------------
# proto — generate Go and TypeScript stubs from proto/**/*.proto.
#
# Generated stubs are committed (proto/gen/go and ui/src/gen) so that a fresh
# checkout builds without buf installed. Run `make proto` only after editing a
# .proto file; CI can verify it produces no diff.
# ----------------------------------------------------------------------------
.PHONY: proto proto-lint proto-breaking proto-format proto-clean
proto: ## Regenerate Go + TypeScript stubs from the .proto files.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd proto && $(BUF) generate

proto-lint: ## Run buf lint against the proto module.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd proto && $(BUF) lint

proto-breaking: ## Fail if the proto introduces breaking changes vs origin/main.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd proto && $(BUF) breaking --against '$(ROOT)/.git#branch=main,subdir=proto'

proto-format: ## Format all .proto files in place.
	@command -v $(BUF) >/dev/null 2>&1 || { \
	  echo "buf not found on PATH. Install: https://buf.build/docs/installation/"; exit 1; }
	cd proto && $(BUF) format -w

proto-clean: ## Remove generated stubs (Go + TS).
	rm -rf proto/gen/go ui/src/gen/*
	@touch proto/gen/.gitkeep ui/src/gen/.gitkeep

# ----------------------------------------------------------------------------
# Go — workspace-wide build/vet/test across all four modules.
# ----------------------------------------------------------------------------
.PHONY: sync build vet test
sync: ## Sync the go.work workspace.
	$(GO) work sync

build: ## go build ./... in every module.
	cd $(ROOT) && $(GO) build ./...
	cd $(ROOT)/proto && $(GO) build ./...
	cd $(ROOT)/agent && $(GO) build ./...
	cd $(ROOT)/server && $(GO) build ./...
	cd $(ROOT)/quarkbridge && $(GO) build ./...
	cd $(ROOT)/quarkdatasource && $(GO) build ./...

vet: ## go vet ./... in every module.
	cd $(ROOT) && $(GO) vet ./...
	cd $(ROOT)/proto && $(GO) vet ./...
	cd $(ROOT)/agent && $(GO) vet ./...
	cd $(ROOT)/server && $(GO) vet ./...
	cd $(ROOT)/quarkbridge && $(GO) vet ./...
	cd $(ROOT)/quarkdatasource && $(GO) vet ./...

test: ## go test ./... in every module.
	cd $(ROOT) && $(GO) test ./...
	cd $(ROOT)/proto && $(GO) test ./...
	cd $(ROOT)/agent && $(GO) test ./...
	cd $(ROOT)/server && $(GO) test ./...
	cd $(ROOT)/quarkbridge && $(GO) test ./...
	cd $(ROOT)/quarkdatasource && $(GO) test ./...

# ----------------------------------------------------------------------------
# Fuzzing — native Go fuzz targets over the surfaces that take untrusted input
# (the Data Studio query string, record ids and tenant values, the CSV import,
# the LIKE escaping, the fleet node-id filter). scripts/ci/fuzz.sh holds the
# list; a target missing from it fails the seed lane.
# ----------------------------------------------------------------------------
.PHONY: fuzz fuzz-seeds
fuzz: ## Fuzz every target for FUZZTIME (default 20s).
	bash $(ROOT)/scripts/ci/fuzz.sh $(FUZZTIME)

fuzz-seeds: ## Run every fuzz target over its seed corpus only (what CI does).
	bash $(ROOT)/scripts/ci/fuzz.sh
