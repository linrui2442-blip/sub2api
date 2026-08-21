.PHONY: build generate test test-unit test-personal test-e2e

VERSION ?= $(shell ./scripts/resolve-version.sh)
LDFLAGS ?= -s -w -X main.Version=$(VERSION)

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -trimpath -o bin/server ./cmd/server

generate:
	go generate ./ent
	go generate ./cmd/server

test:
	go test ./...
	golangci-lint run ./...

test-unit:
	go test -tags=unit ./...

test-personal:
	go test ./...
	go test ./internal/repository -run '^TestPersonal' -count=1
	go test ./internal/setup -run '^TestPersonal' -count=1 -timeout=3m
	go test ./cmd/server -run '^TestPersonalApplicationBootSmoke$' -count=1 -timeout=5m

test-e2e:
	./scripts/e2e-test.sh

test-e2e-local:
	go test -tags=e2e -v -timeout=300s ./internal/integration/...
