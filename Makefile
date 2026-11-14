.PHONY: all fmt vet lint test test-race test-integration ci-local build clean up down docker-build smoke backup demo

BINARY_NAME=cinema-api
BIN_DIR=bin
TEST_DATABASE_URL ?= postgres://cinema_user:change_me@127.0.0.1:5432/cinema_db?sslmode=disable

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

test-integration:
	./scripts/test-integration.sh

ci-local:
	./scripts/ci-local.sh

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

smoke:
	./scripts/smoke.sh

backup:
	./scripts/backup.sh

demo:
	./scripts/demo.sh

