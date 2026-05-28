.PHONY: build run test test-v lint tidy docker-up docker-down docker-build docker-logs migrate-status migrate-up migrate-down

# Local Go targets
build:
	go build -o ./bin/api ./cmd/api

run:
	go run ./cmd/api

test:
	go test ./...

test-v:
	go test -v -count=1 ./...

tidy:
	go mod tidy

# Docker targets
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-build:
	docker compose build

docker-logs:
	docker compose logs -f api

# Migrations via the goose CLI inside the api container (requires running stack).
# DB_DSN can be overridden if not using docker-compose defaults.
DB_DSN ?= postgres://orgstructure:orgstructure@localhost:5432/orgstructure?sslmode=disable

migrate-status:
	goose -dir migrations postgres "$(DB_DSN)" status

migrate-up:
	goose -dir migrations postgres "$(DB_DSN)" up

migrate-down:
	goose -dir migrations postgres "$(DB_DSN)" down
