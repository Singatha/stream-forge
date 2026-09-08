.PHONY: dev fmt lint test test-integration build migrate-up migrate-down docker-up docker-down

dev: docker-up

fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -d . && exit 1)

lint:
	go vet ./...
	golangci-lint run ./...

test:
	go test -race -coverprofile=coverage.out ./...

test-integration:
	go test -tags=integration -count=1 -timeout=10m ./tests/integration/...

build:
	mkdir -p bin
	go build -trimpath -o bin/api ./cmd/api
	go build -trimpath -o bin/migrate ./cmd/migrate

migrate-up:
	go run ./cmd/migrate -path migrations -direction up

migrate-down:
	go run ./cmd/migrate -path migrations -direction down

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

