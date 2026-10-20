.PHONY: up down logs test run
up:      ## собрать и запустить в Docker
	docker compose up -d --build
down:
	docker compose down
logs:
	docker compose logs -f api
test:
	cd backend && go test ./...
run:     ## локально без Docker
	cd backend && PIPELINE_STEP_DELAY=2s go run ./cmd/api
