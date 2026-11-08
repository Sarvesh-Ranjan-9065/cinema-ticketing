.PHONY: all fmt vet lint test test-race build clean up down docker-build

BINARY_NAME=cinema-api
BIN_DIR=bin

all: build

fmt:
	gofmt -w -s .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -v ./...

test-race:
	go test -v -race ./...

build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/api

clean:
	rm -rf $(BIN_DIR) coverage.txt coverage.html

docker-build:
	docker build -t cinema-api:local .

up:
	docker compose -f deploy/compose.yaml up -d

down:
	docker compose -f deploy/compose.yaml down
