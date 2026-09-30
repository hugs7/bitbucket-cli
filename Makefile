.PHONY: build install test lint tidy run snapshot

# `make install` puts a local build beside the released `bb` so unreleased
# changes can be used without replacing it. Version stays "dev" so the
# update notifier leaves it alone; commit/date identify the build.
PREFIX ?= $(HOME)/.local
INSTALL_NAME ?= bb-local
LDFLAGS := -X main.commit=$(shell git describe --always --dirty) \
	-X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build -ldflags "$(LDFLAGS)" -o bb ./cmd/bb

# Rename into place so a running copy does not fail with "text file busy".
install: build
	install -d $(PREFIX)/bin
	install -m 0755 bb $(PREFIX)/bin/.$(INSTALL_NAME).tmp
	mv -f $(PREFIX)/bin/.$(INSTALL_NAME).tmp $(PREFIX)/bin/$(INSTALL_NAME)
	@echo "Installed $(PREFIX)/bin/$(INSTALL_NAME)"

test:
	go test ./...

tidy:
	go mod tidy

run: build
	./bb --help

snapshot:
	goreleaser release --snapshot --clean
