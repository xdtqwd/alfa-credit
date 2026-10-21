#!/usr/bin/env bash
# Все проверки одной командой: vet → юнит/интеграционные тесты с race-детектором и покрытием → e2e в браузере.
set -euo pipefail
cd "$(dirname "$0")/../backend"

echo "== go vet"
go vet ./...

echo "== go test -race (с покрытием)"
go test -race -count=1 -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | tail -1

echo "== гонки при оплате и подписании (20 повторов)"
go test -race -count=20 -run 'Concurrent' ./internal/httpapi/ ./internal/storage/ ./internal/pipeline/

if command -v node >/dev/null && node -e "require.resolve('playwright')" 2>/dev/null; then
  echo "== e2e в браузере"
  DATA_DIR=$(mktemp -d)
  DATA_FILE=$DATA_DIR/a.json USERS_FILE=$DATA_DIR/u.json PIPELINE_STEP_DELAY=1s HTTP_ADDR=:8099 go run ./cmd/api >/dev/null 2>&1 &
  PID=$!; trap 'kill $PID 2>/dev/null' EXIT
  for i in $(seq 1 50); do curl -sf localhost:8099/healthz >/dev/null && break; sleep 0.2; done
  BASE_URL=http://localhost:8099 node ../tests/e2e/e2e.mjs
else
  echo "== e2e пропущены: нужен Node 18+ и playwright (npm i -D playwright && npx playwright install chromium)"
fi
