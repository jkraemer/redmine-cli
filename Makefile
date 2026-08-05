.PHONY: build test generate clean fmt vet dist live-test

BIN := redmine-cli
PKG := ./cmd/redmine-cli
DIST := dist/redmine-cli

build:
	go build -o $(BIN) $(PKG)

dist:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	go build -o $(DIST)/$(BIN) $(PKG)
	cp skills/redmine-cli/SKILL.md $(DIST)/SKILL.md
	@echo "Built $(DIST)/ — drop into ~/.claude/skills/"

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
	rm -rf dist

live-test: # REDMINE_VERSION=<tag> e.g. make live-test REDMINE_VERSION=6.1
	scripts/live/run.sh $(REDMINE_VERSION)
