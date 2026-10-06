BINARY := gsc
MODULE := github.com/piyush-gambhir/gsc-cli/cli-go
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Built-in OAuth client for one-command login. Never commit these values:
# export them, or put them in the untracked cli-go/.env.local as
#   GSC_OAUTH_CLIENT_ID=...
#   GSC_OAUTH_CLIENT_SECRET=...
-include .env.local
GSC_OAUTH_CLIENT_ID ?=
GSC_OAUTH_CLIENT_SECRET ?=

LDFLAGS := -s -w -X $(MODULE)/internal/build.Version=$(VERSION) -X $(MODULE)/internal/build.Commit=$(COMMIT) -X $(MODULE)/internal/build.Date=$(DATE) \
	-X $(MODULE)/internal/auth.BuiltinClientID=$(GSC_OAUTH_CLIENT_ID) -X $(MODULE)/internal/auth.BuiltinClientSecret=$(GSC_OAUTH_CLIENT_SECRET)
# Like go install: $GOBIN when set, else $(go env GOPATH)/bin.
INSTALL_DIR ?= $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: build install test vet lint fmt tidy docs clean
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .
install:
	mkdir -p "$(INSTALL_DIR)"
	go build -ldflags "$(LDFLAGS)" -o "$(INSTALL_DIR)/$(BINARY)$(shell go env GOEXE)" .
test:
	go test -race -count=1 ./...
vet:
	go vet ./...
# Same Staticcheck version and checks as CI.
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -checks=all,-ST1000,-ST1003,-ST1005 ./...
fmt:
	gofmt -w .
tidy:
	go mod tidy
# The skill bundles copies of the command and auth references so gsc/ works on its own.
docs:
	go run ./tools/gendocs > ../docs/commands.md
	mkdir -p ../gsc/references
	cp ../docs/commands.md ../gsc/references/commands.md
	cp ../docs/auth.md ../gsc/references/auth.md
clean:
	rm -rf bin
	go clean
