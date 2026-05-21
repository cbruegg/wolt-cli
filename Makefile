APP_NAME := wolt
MCP_NAME := wolt-mcp
VERSION ?= $(shell git describe --tags --always --dirty)

.PHONY: build mcp all run test race lint cover clean

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(APP_NAME) ./cmd/wolt

mcp:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(MCP_NAME) ./cmd/wolt-mcp

all: build mcp

run:
	go run ./cmd/wolt --help

test:
	go test ./...

race:
	go test -race ./...

lint:
	golangci-lint run

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

clean:
	rm -rf bin coverage.out
