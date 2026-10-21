APP=fast-crud

.PHONY: run build test test-race lint fmt vet tidy compose-up compose-down security

run:
	go run ./cmd/api

build:
	mkdir -p bin
	go build -trimpath -o bin/$(APP) ./cmd/api

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w ./cmd

vet:
	go vet ./...

tidy:
	go mod tidy

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

security:
	govulncheck ./...
	gosec ./...
