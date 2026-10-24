.PHONY: help build fmt vet lint lint-version-check test race tidy-check verify check ci web-build web-check lodge-package vulncheck clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# golangci-lint's release, read from mise.toml (frostyard/core ADR-0043).
GOLANGCI_LINT_VERSION := $(strip $(shell sed -n 's/^golangci-lint = "\(.*\)"/\1/p' mise.toml))

help: ## List targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 16

build: ## Build porterd into build/
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o build/ ./cmd/...

fmt: ## Format Go code
	gofmt -w .

vet: ## Vet Go code
	go vet ./...

lint-version-check:
	@v=$$(golangci-lint version 2>/dev/null | sed -n 's/.*version \([0-9.]*\).*/\1/p'); \
	[ "$$v" = "$(GOLANGCI_LINT_VERSION)" ] || echo "warning: golangci-lint $$v, mise.toml pins $(GOLANGCI_LINT_VERSION) (mise install)"

lint: lint-version-check ## Run the pinned golangci-lint
	golangci-lint run

test: ## Run the Go tests, the app contract's socket rules among them
	go test ./...

race: ## Run the Go tests under the race detector
	go test -race ./...

tidy-check: ## Fail if go.mod or go.sum is not tidy
	go mod tidy -diff

verify: tidy-check vet lint test ## The gate without formatting: tidy, vet, lint and tests
	test -z "$$(gofmt -l .)"

check: fmt verify ## Format, then verify

ci: verify race web-check ## verify, the race detector, and the web bundle's checks (nsl)

# govulncheck's release, pinned here and not in go.mod (it needs the network).
GOVULNCHECK_VERSION := v1.9.0

vulncheck: ## Report reachable vulnerabilities (govulncheck; needs the network)
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# The web bundle builds in an nsl machine with Node (lodge's
# dev/nsl/provision.sh), never on the host, for lodge's /app/porter/.
NSL_MACHINE ?= lodge
IN_NSL ?= nsl run -m $(NSL_MACHINE) sh -lc
FROZEN = $(if $(wildcard web/pnpm-lock.yaml),--frozen-lockfile,)

web-check: ## Typecheck and test the web bundle (nsl)
	$(IN_NSL) 'cd web && pnpm install $(FROZEN) --silent && pnpm exec tsc --noEmit && pnpm exec vitest run'

web-build: ## Build the web bundle for lodge's /app/porter/ into build/web (nsl)
	$(IN_NSL) 'cd web && pnpm install $(FROZEN) --silent && pnpm exec tsc --noEmit && pnpm exec vite build --outDir ../build/web --emptyOutDir'

# lodge-package stages the app under LODGE_DESTDIR in lodge's install
# layout, with packaging/lodge/stage.sh. It replaces only this app's own
# files, so LODGE_DESTDIR may be a lodge root other apps share; it refuses
# an empty one, or /.
LODGE_DESTDIR ?= build/lodge-root
lodge-package: build ## Stage porterd, its manifest, units and web build for lodge under LODGE_DESTDIR
	packaging/lodge/stage.sh "$(LODGE_DESTDIR)" "$(VERSION)" build

clean: ## Remove build output
	rm -rf build
