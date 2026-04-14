# apogee — developer Makefile
#
# Common targets:
#   make dev               # run collector + web dev server together
#   make build             # build the apogee binary (includes the web bundle)
#   make build-collector   # build only the Go binary
#   make build-web         # build the Next.js UI into web/out and copy to internal/webassets/dist
#   make web-build         # alias for build-web
#   make run-collector     # build then run the collector against ./.local/apogee.duckdb
#   make test              # run Go tests with race detector
#   make test-integration  # run only collector integration tests
#   make tidy              # go mod tidy
#   make fmt               # go fmt
#   make clean             # remove build artefacts

GO            ?= go
BIN_DIR       ?= bin
BINARY        ?= $(BIN_DIR)/apogee
PKG           ?= ./...
WEB_DIR       ?= web
WEB_OUT       ?= $(WEB_DIR)/out
WEB_EMBED_DST ?= internal/webassets/dist
LOCAL_DB      ?= $(PWD)/.local/apogee.duckdb

# Version / build metadata injected via -ldflags. Users can override any of
# these on the command line: `make build VERSION=0.2.0`.
VERSION    ?= 0.0.0-dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X github.com/BIwashi/apogee/internal/version.Version=$(VERSION) \
              -X github.com/BIwashi/apogee/internal/version.Commit=$(COMMIT) \
              -X github.com/BIwashi/apogee/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: all
all: build

.PHONY: dev
dev:
	@echo ">> starting collector and web (Ctrl-C to stop)"
	@$(MAKE) build-collector >/dev/null
	@( $(BINARY) serve & echo $$! > .apogee.pid ) ; \
	  ( cd $(WEB_DIR) && npm run dev ) ; \
	  kill `cat .apogee.pid` 2>/dev/null ; rm -f .apogee.pid

.PHONY: build
build: build-web build-collector

.PHONY: build-collector
build-collector:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/apogee

.PHONY: run-collector
run-collector: build-collector
	@mkdir -p $(dir $(LOCAL_DB))
	$(BINARY) serve --addr=:4100 --db=$(LOCAL_DB)

.PHONY: build-web web-build
build-web web-build: web-install
	cd $(WEB_DIR) && npm run build
	@mkdir -p $(WEB_EMBED_DST)
	@# rsync keeps the embed tree in lock-step with web/out. --delete is
	@# critical: leftover files from a previous bundle would otherwise
	@# inflate the embedded FS.
	rsync -a --delete $(WEB_OUT)/ $(WEB_EMBED_DST)/

.PHONY: web-dev
web-dev: web-install
	cd $(WEB_DIR) && npm run dev

.PHONY: web-install
web-install:
	cd $(WEB_DIR) && npm install

.PHONY: test
test:
	$(GO) test $(PKG) -race -count=1

.PHONY: test-integration
test-integration:
	$(GO) test ./internal/collector/... -race -count=1 -run Integration

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: fmt
fmt:
	$(GO) fmt $(PKG)

.PHONY: vet
vet:
	$(GO) vet $(PKG)

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) dist
	rm -rf $(WEB_DIR)/.next $(WEB_OUT)
	@# Leave internal/webassets/dist/index.html (placeholder) in place so
	@# `go build ./...` still works after a clean.
