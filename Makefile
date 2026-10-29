.PHONY: dev gateway simseller demo-seed migrate migrate-down generate test lint build docker up down

# Local DB URL used by the migrate CLI (matches docker-compose.yml).
DB_URL ?= postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable
MIGRATIONS := pkg/db/migrations

up:            ## start local Postgres + Redis
	docker compose up -d

down:          ## stop local stack
	docker compose down

dev: up        ## run Core API with hot reload
	air -c .air.toml

gateway:       ## run the Agent Gateway
	go run ./cmd/gateway

demo-seed:     ## LOCAL ONLY: seed demo buyer/sellers + negotiated deals (API + gateway must be running)
	go run ./cmd/demoseed -db "$(DB_URL)"

simseller:     ## LOCAL ONLY: run simulated seller agents (credentials from demo-seed)
	set -a; . ./.simseller.env; set +a; go run ./cmd/simseller

migrate:       ## apply all pending migrations
	migrate -path $(MIGRATIONS) -database "$(DB_URL)" up

migrate-down:  ## roll back the last migration
	migrate -path $(MIGRATIONS) -database "$(DB_URL)" down 1

generate:      ## regenerate sqlc queries + wire graph
	sqlc generate
	wire ./internal/di

test:          ## run all tests
	go test ./...

lint:          ## run golangci-lint
	golangci-lint run

build:         ## compile both binaries into ./bin
	go build -o bin/api ./cmd/api
	go build -o bin/gateway ./cmd/gateway

docker:        ## build the Core Docker image
	docker build -t tradebridge-core:dev .
