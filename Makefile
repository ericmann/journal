BINARY := journal
PKG := ./...

.PHONY: build test lint fmt vet tidy clean cover

# CGO_ENABLED=0 yields a fully static binary; all deps (incl. the ncruces
# sqlite-vec driver, which runs SQLite as WASM via wazero) are pure Go.
build:
	CGO_ENABLED=0 go build -o $(BINARY) .

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
