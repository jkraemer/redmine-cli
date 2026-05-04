.PHONY: build test generate clean fmt vet

BIN := redmine-cli
PKG := ./cmd/redmine-cli

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

generate:
	oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
