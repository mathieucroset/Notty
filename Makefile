export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

.PHONY: build test lint run

build:
	CGO_ENABLED=0 go build -o notty ./cmd/notty

test:
	go test -race ./...

lint:
	golangci-lint run

run: build
	./notty
