#!/usr/bin/env bash
# Проверка запущенного стека (docker compose up -d --build) одной командой.
# На машине нужен только Docker: самопроверка (/selfcheck) уже лежит в образе сервиса.
#
#   ./scripts/check.sh            # здоровье, PostgreSQL, Kafka и весь путь клиента и банка через API
#   ./scripts/check.sh --chaos    # + отказ Kafka: заявки принимаются, а после восстановления доходят до решения
set -euo pipefail
cd "$(dirname "$0")/.."

CHAOS=0
[ "${1:-}" = "--chaos" ] && CHAOS=1

dc() { docker compose "$@"; }
step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

step "Контейнеры"
dc ps --format 'table {{.Service}}\t{{.Status}}'

# ждём, пока API станет здоровым (первый старт: Kafka и PostgreSQL поднимаются ~20–40 с)
for i in $(seq 1 60); do
  if dc exec -T api /api -healthcheck >/dev/null 2>&1; then break; fi
  [ "$i" = 60 ] && { echo "API не поднялся за 2 минуты — смотрите: docker compose logs api"; exit 1; }
  sleep 2
done

step "Сквозная самопроверка (клиент → Kafka → воркер → решение → выдача → оплата → банк)"
dc exec -T api /selfcheck -expect-bus kafka

step "Обработчики событий (worker)"
dc logs worker 2>/dev/null | grep -E '"msg":"(kafka consumer started|application step|notification sent)' | tail -n 6 || true

if [ "$CHAOS" = 1 ]; then
  step "Отказ Kafka: останавливаем брокер"
  dc stop kafka
  OUT=$(dc exec -T api /selfcheck -phase submit)
  echo "$OUT"
  NUMBER=$(printf '%s\n' "$OUT" | sed -n 's/^NUMBER=//p')
  [ -n "$NUMBER" ] || { echo "заявка не подана при остановленной Kafka"; exit 1; }

  step "Kafka снова в строю: событие из outbox должно дойти до воркера"
  dc start kafka
  dc exec -T api /selfcheck -phase await -number "$NUMBER" -wait 180s
fi

step "Готово: всё работает"
