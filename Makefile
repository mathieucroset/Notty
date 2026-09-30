export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

PREFIX ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test lint run

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o notty ./cmd/notty

install: build
	install -Dm755 notty $(PREFIX)/bin/notty

test:
	go test -race ./...

lint:
	golangci-lint run

run: build
	./notty
