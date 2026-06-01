BINARY := journal
PKG := ./...

# Version is derived from git tags (e.g. v1.0.0), overridable: make build VERSION=v1.2.3
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/ericmann/journal/cmd.version=$(VERSION)

# Platforms for `make release` (all pure-Go, CGO_ENABLED=0 cross-compiles).
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

# Install location. Override e.g. `make install PREFIX=$HOME/.local`.
PREFIX ?= /usr/local
BINDIR := $(PREFIX)/bin

.PHONY: build install uninstall test lint fmt vet tidy clean cover release

# CGO_ENABLED=0 yields a fully static binary; all deps (incl. the ncruces
# sqlite-vec driver, which runs SQLite as WASM via wazero) are pure Go.
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# install builds a fresh, version-stamped binary and copies it onto PATH in one
# step — so you can never run a stale binary by forgetting to reinstall.
install: build
	@mkdir -p "$(BINDIR)"
	@install -m 0755 $(BINARY) "$(BINDIR)/$(BINARY)"
	@echo "installed $(BINARY) $(VERSION) -> $(BINDIR)/$(BINARY)"

uninstall:
	@rm -f "$(BINDIR)/$(BINARY)" && echo "removed $(BINDIR)/$(BINARY)"

# release cross-compiles a versioned static binary per platform into dist/.
release:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=dist/$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done
	@echo "artifacts:" && ls -1 dist/

test:
	go test $(PKG)

cover:
	go test -coverprofile=coverage.out $(PKG) && go tool cover -func=coverage.out

vet:
	go vet $(PKG)

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

# lint runs gofmt check + vet. If golangci-lint is installed it is used too.
lint: vet
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran gofmt+vet only"

clean:
	rm -f $(BINARY) coverage.out
