.PHONY: build test coverage demo check-demo check-docs-policy clean setup install uninstall release

DOCS_POLICY_BASE ?= origin/master

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

demo:
	./docs/demo/render-container.sh

check-demo:
	./docs/demo/render.sh --check

check-docs-policy:
	go run ./tools/docs_policy --base "$(DOCS_POLICY_BASE)" --head HEAD

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
