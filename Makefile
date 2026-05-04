.PHONY: build test generate clean fmt vet

BIN := redmine-cli
PKG := ./cmd/redmine-cli

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

generate:
	@echo "codegen disabled -- see api/SOURCE.md"

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
