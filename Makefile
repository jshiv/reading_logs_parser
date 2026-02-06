# ──────────────────────────────────────────────────────────────
#  reading-logs-parser  —  GoReleaser build targets
# ──────────────────────────────────────────────────────────────

BINARY := reading-logs-parser

# ── Local dev build (no GoReleaser needed) ───────────────────
.PHONY: build
build: ## Quick build for current platform
	go build -trimpath -o $(BINARY) .

.PHONY: run
run: build ## Build and run
	./$(BINARY)

# ── GoReleaser ───────────────────────────────────────────────
.PHONY: snapshot
snapshot: ## Build all targets locally (no publish)
	goreleaser build --snapshot --clean

.PHONY: release-dry-run
release-dry-run: ## Full release dry-run (no publish)
	goreleaser release --snapshot --clean

.PHONY: release
release: ## Create a release (requires GITHUB_TOKEN and a git tag)
	goreleaser release --clean

.PHONY: check
check: ## Validate .goreleaser.yaml
	goreleaser check

# ── Go tooling ───────────────────────────────────────────────
.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Run gofmt
	gofmt -s -w .

.PHONY: test
test: ## Run tests
	go test -v ./...

# ── Cleanup ──────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist/

# ── Help ─────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
