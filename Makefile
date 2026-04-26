.PHONY: build test lint tidy run snapshot

build:
	go build -o bb ./cmd/bb

test:
	go test ./...

tidy:
	go mod tidy

run: build
	./bb --help

snapshot:
	goreleaser release --snapshot --clean
