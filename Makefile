.PHONY: build test coverage clean setup

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

setup:
	git config core.hooksPath .githooks
