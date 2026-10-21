# Как вносить изменения

## Ветки

`main` — всегда рабочая версия: из неё собирается то, что видит жюри. Напрямую в `main` не пушим: каждая задача делается в своей ветке и попадает в `main` через pull request после зелёного CI.

| Префикс | Для чего | Пример |
|---|---|---|
| `feature/` | новая функциональность | `feature/kafka-outbox`, `feature/profile-menu` |
| `fix/` | исправление ошибки | `fix/mobile-menu-offscreen` |
| `docs/` | только документация | `docs/readme-github` |
| `test/` | только тесты | `test/e2e-overdraft` |
| `chore/` | сборка, CI, зависимости | `chore/ci-selfcheck` |

Имя — коротко и по-английски, через дефис. Одна ветка — одна задача.

## Порядок работы

```bash
git switch main && git pull                 # свежий main
git switch -c feature/short-name            # новая ветка от main
# ... изменения ...
./scripts/test-all.sh                       # локально: vet, тесты, e2e (если стоит Node)
git add -A && git commit -m "feat: что сделано"
git push -u origin feature/short-name       # GitHub предложит открыть pull request
```

Дальше на GitHub: **Compare & pull request** → CI сам прогонит все проверки → когда всё зелёное, **Squash and merge** → **Delete branch**. После слияния:

```bash
git switch main && git pull && git branch -d feature/short-name
```

## Сообщения коммитов

Формат [Conventional Commits](https://www.conventionalcommits.org/ru/): `тип: что сделано` — `feat`, `fix`, `docs`, `test`, `refactor`, `chore`. Например: `fix: меню профиля на телефоне уходило за экран`.

## Что проверяет CI (`.github/workflows/ci.yml`)

| Задача | Что внутри |
|---|---|
| **Go** | `go mod verify`, `gofmt`, `go vet`, все тесты с race-детектором на настоящих PostgreSQL 16 и Kafka, гонки при оплате и идемпотентности ×20 |
| **Фронтенд** | ESLint, Prettier (`.prettierrc.json`), проверка необъявленных CSS-переменных и классов со словом `default`, юнит-тесты чистых модулей (`tests/unit`): `cd tests/lint && npm ci && npm run lint`, автоформат — `npm run format:fix` |
| **Название PR** | название PR и все коммиты ветки — в формате Conventional Commits |
| **Полный стек** | `docker compose up --build` (api, worker, PostgreSQL, Kafka), 26 браузерных e2e, сквозная самопроверка `/selfcheck`, отказ Kafka и восстановление без потери заявок |

При падении в артефактах сборки лежат логи всех контейнеров и скриншоты упавших e2e.

## Защита main (один раз, в настройках репозитория)

GitHub → **Settings → Branches → Add branch ruleset** (или *Branch protection rule*) для `main`:

- **Require a pull request before merging**;
- **Require status checks to pass** → отметить `Go — тесты на PostgreSQL и Kafka`, `Фронтенд — линтеры, CSS-переменные, юнит-тесты`, `Название PR и коммиты — Conventional Commits` и `Полный стек — e2e, самопроверка, отказ Kafka` (появятся в списке после первого запуска CI);
- **Block force pushes**.
