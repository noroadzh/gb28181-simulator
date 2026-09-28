# gb28181-simulator top-level Makefile.
#
# The web bundle is shipped into the Go binary via //go:embed under
# internal/webui/embed, so before running `go run` or `go test` we build the
# SPA once. The npm script writes the bundle into the embed directory by
# design; we never need a manual copy step.

VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LD_FLAGS = -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.builtAt=$(BUILT_AT)

BIN_DIR = bin

.PHONY: web build run test lint fmt clean all release-matrix sip-test sip-smoke sipprobe-build service-build smoke

all: web build

fmt:
	gofmt -s -w .
	goimports -w . 2>/dev/null || true

web:
	cd web && npm install --no-audit --no-fund && npm run build

build: web
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LD_FLAGS)" -o $(BIN_DIR)/gb28181-simulator ./cmd/gb28181-simulator

# Build the sipprobe diagnostic binary (Change 2 §7).
sipprobe-build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LD_FLAGS)" -o $(BIN_DIR)/sipprobe ./cmd/sipprobe

# Build every service binary for the host platform (Change 3 §12.1).
service-build: build sipprobe-build

run: web
	CGO_ENABLED=0 go run ./cmd/gb28181-simulator

test:
	CGO_ENABLED=0 go test -race -cover ./...

# Fast subset: SIP / SDP / Digest only. Skips the webui/storage/api test
# packages which include slow end-to-end loops. Used as a quick feedback loop.
sip-test:
	CGO_ENABLED=0 go test -race -count=1 -timeout=60s \
		./internal/adapter/sip/... ./internal/adapter/sdp/... ./internal/adapter/auth/... ./internal/sipprobe/...

# End-to-end smoke: two sipprobe processes talk to each other.
sip-smoke: sipprobe-build
	./scripts/smoke-sip.sh

lint:
	go vet ./...

clean:
	rm -rf $(BIN_DIR) web/node_modules internal/interface/webui/embed/dist/assets

# Change: fix-problems-and-smoke-deploy-docs, task 2.2.
# One-command smoke baseline: tests + vet + build + cross-compile + web build + e2e.
# Writes docs/smoke-results.json; exits non-zero on first failed step.
smoke:
	bash scripts/smoke.sh

# Cross-platform matrix used by CI; convenience for local reproduction.
# Builds both cmd/gb28181-simulator and cmd/sipprobe per platform (Change 2 §7.4).
release-matrix:
	mkdir -p $(BIN_DIR)
	@for plat in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 ; do \
	  os=$${plat%/*} ; arch=$${plat#*/} ; \
	  ext= ; [ $$os = windows ] && ext=.exe ; \
	  for cmd in gb28181-simulator sipprobe; do \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LD_FLAGS)" \
	      -o $(BIN_DIR)/$$cmd-$$os-$$arch$$ext ./cmd/$$cmd ; \
	    sha256sum $(BIN_DIR)/$$cmd-$$os-$$arch$$ext > $(BIN_DIR)/$$cmd-$$os-$$arch$$ext.sha256 || \
	      shasum -a 256 $(BIN_DIR)/$$cmd-$$os-$$arch$$ext > $(BIN_DIR)/$$cmd-$$os-$$arch$$ext.sha256 ; \
	  done ; \
	done