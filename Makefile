GO ?= go
BINARY ?= bin/gist
PKG ?= ./...
COVER_PROFILE ?= coverage.out

.PHONY: all build test test-race cover vet fmt clean run init help

all: build

## build: compile the static binary
build:
	CGO_ENABLED=0 $(GO) build -o $(BINARY) ./cmd/gist

## test: run unit tests
test:
	$(GO) test $(PKG)

## test-race: run unit tests with race detector
test-race:
	$(GO) test -race $(PKG)

## cover: produce coverage profile and HTML report
cover:
	$(GO) test -coverprofile=$(COVER_PROFILE) $(PKG)
	$(GO) tool cover -func=$(COVER_PROFILE)
	$(GO) tool cover -html=$(COVER_PROFILE) -o coverage.html

## vet: run go vet
vet:
	$(GO) vet $(PKG)

## fmt: format sources
fmt:
	gofmt -w .
	goimports -w . || true

## run: build and run with stdin attached
run: build
	./$(BINARY)

## init: write default config to disk
init: build
	./$(BINARY) init

## clean: remove build and coverage artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## help: print this help
help:
	@grep -E '^## ' Makefile | sed 's/^## //'