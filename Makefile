.PHONY: build test test-int lint migrate-up migrate-down sqlc up down
build:
	go build -o bin/server ./cmd/server
test:
	go test -race -cover ./...
test-int:
	go test -race -tags=integration ./...
lint:
	golangci-lint run
sqlc:
	sqlc generate
migrate-up:
	goose -dir db/migrations postgres "$$DATABASE_ADMIN_URL" up
migrate-down:
	goose -dir db/migrations postgres "$$DATABASE_ADMIN_URL" down
up:
	docker compose -f deploy/compose/docker-compose.yml up -d
down:
	docker compose -f deploy/compose/docker-compose.yml down -v
