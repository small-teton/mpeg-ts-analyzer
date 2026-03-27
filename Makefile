.PHONY: build test coverage clean setup install uninstall

build:
	go build -v -ldflags "$(LDFLAGS)" ./...

test:
	go test -v ./...

coverage:
	@mkdir -p out
	go test -coverprofile=out/bitbuffer.out -covermode=set ./bitbuffer/...
	go test -coverprofile=out/tsparser.out -covermode=set ./tsparser/...
	@echo "\n=== bitbuffer ==="
	@go tool cover -func=out/bitbuffer.out | tail -1
	@echo "=== tsparser ==="
	@go tool cover -func=out/tsparser.out | tail -1

clean:
	rm -rf out/ dist/

VERSION := $(shell cat VERSION)
LDFLAGS := -X github.com/small-teton/mpeg-ts-analyzer/cmd.version=$(VERSION)

install:
	go install -ldflags "$(LDFLAGS)" ./...

uninstall:
	rm -f $(shell go env GOPATH)/bin/mpeg-ts-analyzer

setup:
	git config core.hooksPath .githooks
