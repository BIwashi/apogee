# apogee — developer Makefile
#
# Common targets:
#   make dev               # run collector + web dev server together
#   make build             # build the apogee binary into ./bin/apogee
#   make build-collector   # build only the Go binary
#   make build-web         # build the Next.js UI into web/.next
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
LOCAL_DB      ?= $(PWD)/.local/apogee.duckdb

.PHONY: all
all: build

.PHONY: dev
dev:
	@echo ">> starting collector and web (Ctrl-C to stop)"
	@$(MAKE) build-collector >/dev/null
	@( $(BINARY) & echo $$! > .apogee.pid ) ; \
	  ( cd $(WEB_DIR) && npm run dev ) ; \
	  kill `cat .apogee.pid` 2>/dev/null ; rm -f .apogee.pid

.PHONY: build
build: build-collector build-web

.PHONY: build-collector
build-collector:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./cmd/apogee

.PHONY: run-collector
run-collector: build-collector
	@mkdir -p $(dir $(LOCAL_DB))
	$(BINARY) serve -addr=:4100 -db=$(LOCAL_DB)

.PHONY: build-web
build-web: web-install
	cd $(WEB_DIR) && npm run build

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
	rm -rf $(WEB_DIR)/.next $(WEB_DIR)/out
