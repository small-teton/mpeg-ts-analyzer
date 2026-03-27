.PHONY: build test coverage clean setup install uninstall release

build:
	go build -v ./...

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

install:
	go install ./...

uninstall:
	rm -f $(shell go env GOPATH)/bin/mpeg-ts-analyzer

release:
	@VERSION=$$(cat VERSION | tr -d '[:space:]') && \
	echo "Releasing v$$VERSION..." && \
	git tag "v$$VERSION" && \
	git push origin "v$$VERSION"

setup:
	git config core.hooksPath .githooks
