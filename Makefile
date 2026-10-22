.PHONY: up down logs test test-integration run run-local
up:      ## собрать и запустить в Docker: API + worker + PostgreSQL + Kafka
	docker compose up -d --build
down:
	docker compose down
logs:
	docker compose logs -f api worker
test:    ## юнит-тесты (без внешних зависимостей)
	cd backend && go test ./...
test-integration: ## + интеграционные тесты на PostgreSQL и Kafka из docker compose (Go ставить не нужно)
	docker compose up -d postgres kafka
	docker run --rm --network $(notdir $(CURDIR))_default -v "$(CURDIR)/backend":/src -w /src \
		-e TEST_DATABASE_URL="postgres://credit:$${POSTGRES_PASSWORD:-credit-local}@postgres:5432/credit?sslmode=disable" \
		-e TEST_KAFKA_BROKERS=kafka:9092 golang:1.24-alpine go test -count=1 ./...
run:     ## локально без Docker и без БД/Kafka (файлы + in-memory шина)
	cd backend && APP_MODE=demo PIPELINE_STEP_DELAY=2s go run ./cmd/api
