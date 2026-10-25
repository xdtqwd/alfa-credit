<p align="center">
  <img src="docs/readme/banner.svg" width="100%" alt="Умный кредитный кабинет предпринимателя — кредит для малого бизнеса онлайн">
</p>

<p align="center">
  <a href="https://github.com/xdtqwd/alfa-credit/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/xdtqwd/alfa-credit/ci.yml?branch=main&style=for-the-badge&label=CI" alt="CI"></a>
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24">
  <img src="https://img.shields.io/badge/PostgreSQL-16-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL 16">
  <img src="https://img.shields.io/badge/Kafka-outbox-231F20?style=for-the-badge&logo=apachekafka&logoColor=white" alt="Kafka">
  <img src="https://img.shields.io/badge/Docker-compose-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker Compose">
</p>

<p align="center">
  <b>Клиент</b> подаёт заявку за пару минут · <b>банковский конвейер</b> проверяет данные и решает · <b>обе стороны</b> видят процесс вживую
</p>

<p align="center">
  <a href="#-быстрый-старт"><b>Быстрый старт</b></a> ·
  <a href="#-возможности"><b>Возможности</b></a> ·
  <a href="#-путь-заявки"><b>Путь заявки</b></a> ·
  <a href="#-платформа"><b>Платформа</b></a> ·
  <a href="#-скриншоты"><b>Скриншоты</b></a> ·
  <a href="#-проверка-и-тесты"><b>Тесты</b></a> ·
  <a href="docs/ARCHITECTURE.md"><b>Архитектура</b></a> ·
  <a href="docs/openapi.yaml"><b>API</b></a>
</p>

<br>

<p align="center">
  <img src="docs/demo.gif" width="860" alt="Демо: заявка клиента проходит конвейер банка и получает решение">
  <br>
  <sub>GIF ускорен вдвое · ▶ полное видео с подписями, ~85 с: <a href="demo/alfa-credit-demo.mp4">demo/alfa-credit-demo.mp4</a></sub>
  <br>
  <sub>калькулятор → вход → быстрая анкета → кабинет банка → предварительное решение → полная анкета с залогом → окончательное решение → подписание → деньги → платёж → отказ другому клиенту с объяснением</sub>
</p>

<br>

## 🚀 Быстрый старт

> [!TIP]
> Нужен только **Docker** — Desktop или Engine 24+ с Compose v2. Интернет нужен лишь для первой сборки, дальше всё работает офлайн.

```bash
git clone https://github.com/xdtqwd/alfa-credit.git && cd alfa-credit
docker compose up -d --build   # API, worker, PostgreSQL и Kafka; первый запуск — 1–2 минуты
```

Откройте **http://localhost:8080** и выберите демо-аккаунт в окне входа. Пароль у всех — `demo1234`.

| Телефон | Кто | Что покажет |
|---|---|---|
| <code>+7&nbsp;900&nbsp;000‑00‑01</code> | ООО «Северный ветер» | ✅ одобрение → подписание → деньги → оплата |
| <code>+7&nbsp;900&nbsp;000‑00‑02</code> | ООО «Алтайские сыроварни» | 🔄 контрпредложение (от 15 млн) или уточнение (выручка 50 млн) |
| <code>+7&nbsp;900&nbsp;000‑00‑03</code> | ООО «Сибирский кран» | ❌ отказ: просрочки в БКИ |
| <code>+7&nbsp;900&nbsp;000‑00‑04</code> | ООО «Закрытый горизонт» | ❌ отказ: компания ликвидирована |
| <code>+7&nbsp;900&nbsp;000‑00‑05</code> | Сотрудник банка | 🏦 очередь заявок вживую и профиль с правами |

Можно и зарегистрироваться с любым 10- или 12-значным ИНН — компания подтянется из реестра.

<details>
<summary><b>⚙️ Без Docker, Makefile и переменные окружения</b></summary>
<br>

С Go 1.24+ без БД и Kafka: `make run` — тот же код на JSON-файлах и шине событий в памяти. Docker: `make up`, `make down`, `make logs`. Kafka UI: `docker compose --profile tools up -d` → http://localhost:8081.

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `APP_MODE` | `production` (в compose и `make run` — `demo`) | `production`: нужен свой `BANK_TOKEN` (от 16 символов), cookie только по HTTPS, иначе сервис не стартует. `demo`: публичный токен и cookie без `Secure` для `http://localhost` |
| `DATABASE_URL` | в compose задан | PostgreSQL; без него — файловый режим |
| `KAFKA_BROKERS` | в compose `kafka:9092` | брокеры через запятую; без них — шина в памяти |
| `APP_ROLE` | `all` | `api` (HTTP и outbox relay), `worker` (обработчики событий и sweeper) или `all` |
| `HTTP_PORT` / `METRICS_PORT` | `8080` / `9090` | порты на хосте; `/metrics` открыт только на 127.0.0.1 |
| `BANK_TOKEN` | в демо `demo-bank` | служебный токен кабинета банка (`X-Bank-Token`) для интеграций; сотрудники входят своей учётной записью |
| `COOKIE_SECURE` | `true` в production, `false` в демо | флаг `Secure` у cookie сессии |
| `PIPELINE_STEP_DELAY` | `3s` | имитация времени каждого этапа конвейера |
| `RATE_LIMIT_RPS` / `RATE_LIMIT_BURST` | `20` / `60` | лимит запросов к API на IP |
| `REQUEST_TIMEOUT` | `10s` | предел времени запроса |
| `CACHE_REGISTRY_TTL` / `CACHE_BUREAU_TTL` | `1h` / `10m` | кэш ответов ЕГРЮЛ/ФНС и БКИ |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | логи (`text` — для отладки) |
| `POSTGRES_PASSWORD` | `credit-local` | пароль БД в compose |

Здоровье: `GET /healthz` (процесс жив), `GET /readyz` (PostgreSQL и Kafka доступны, с причиной отказа). Метрики: `GET :9090/metrics`.
</details>

## ✨ Возможности

<table>
  <tr>
    <td width="50%" valign="top">
      <h3>🧮 Калькулятор прямо в анкете</h3>
      Платёж пересчитывается при каждом движении ползунка, параметры с главной переносятся в анкету. Считает только сервер — один источник расчёта.
    </td>
    <td width="50%" valign="top">
      <h3>⚡ Минимум полей</h3>
      Кнопка сразу открывает анкету. Компания — из ЕГРЮЛ по ИНН, контакт — из профиля, выручка — из ФНС одной кнопкой. Решение за 7–15 секунд.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <h3>📍 Прогресс вживую</h3>
      Анкета → проверка данных → оценка риска → решение → подписание → деньги. У каждого шага крупно написано, нужно ли что-то от клиента.
    </td>
    <td valign="top">
      <h3>💬 Решение с объяснением</h3>
      Одобрение, контрпредложение, уточнение или отказ — всегда с причинами на цифрах из анкеты, советом и разбором балла.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <h3>🏦 Кабинет сотрудника банка</h3>
      Очередь заявок конвейера вживую: балл, проверки, решение и состояние платформы. Клиенту этот кабинет закрыт.
    </td>
    <td valign="top">
      <h3>💾 Честное автосохранение</h3>
      «Сохраняем… / Сохранено / Не сохранено · Повторить». Без сети поля не теряются и досохраняются сами.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <h3>🎨 Брендбук Альфа-Банка</h3>
      Главная свёрстана по макету Figma: Styrene A Web, платиновый фон, 3D-иллюстрации. Кабинет — в палитре брендбука, адаптив до телефона.
    </td>
    <td valign="top">
      <h3>🔒 Безопасность</h3>
      Без входа нельзя оформить, подписать и оплатить. HttpOnly-cookie <code>SameSite=Strict</code>, проверка Origin, PBKDF2, защита от перебора, лимиты.
    </td>
  </tr>
</table>

## 🔁 Путь заявки

```mermaid
flowchart LR
  subgraph Клиент
    A[Калькулятор] --> B[Быстрая анкета<br/>2 минуты, предзаполнена]
    B --> C[Отправка]
    P[Предварительное решение] --> Q[Полная анкета<br/>о бизнесе и обеспечении]
    R[Окончательное решение<br/>простыми словами] --> S[Подписание ПЭП]
    S --> M[Деньги на счёте<br/>график и оплата]
  end
  subgraph Банк
    C --> D[Проверка данных<br/>ЕГРЮЛ · ФНС · БКИ]
    D --> E[Оценка риска<br/>балльная модель]
    E --> F{Решение}
    Q --> V[Проверка полной анкеты<br/>залог · поручитель в БКИ]
  end
  D -->|выручка больше 1,5 × ФНС: уточнить| B
  F -->|одобрено или другие условия| P
  F -->|отказ| R
  V -->|обеспечения хватает| R
  V -->|обеспечения не хватает: уточнить| Q
```

**Анкета в два уровня.** Быстрая анкета — сумма, срок, выручка, текущие платежи, контакт и согласие на запрос в БКИ. По ней конвейер за секунды выносит **предварительное** решение. Подробности спрашиваем только у того, кому уже готовы дать деньги: после принятия условий открывается **полная анкета** — сотрудники, помещение, цель кредита, залог или поручитель. По ней банк выносит **окончательное** решение:

- **больше 5 млн ₽** — нужен залог хотя бы на половину суммы (с дисконтом: недвижимость 70 %, транспорт 60 %, оборудование 50 %, депозит 95 %) или поручитель без просрочек в БКИ;
- **залог на всю сумму** — ставка на 1 п.п. ниже предварительной;
- **обеспечения не хватает** — «нужно уточнение» с причиной; клиент правит только полную анкету.

Овердрафт — в один уровень: решение по быстрой анкете сразу окончательное.

<details>
<summary><b>🎯 Какие данные дают какой исход</b></summary>
<br>

| Исход | Сценарий | Аккаунт | Данные |
|---|---|---|---|
| ✅ Одобрение (ставка −1 п.п.) | здоровый бизнес, небольшая нагрузка | `…01` «Северный ветер» | 3 млн ₽ на 24 мес., выручка 84 млн ₽ |
| ✅ Одобрение | крупный кредит на оборудование | `…01` «Северный ветер» | 15 млн ₽ на 60 мес. |
| ✅ Одобрение | тот же клиент, что ниже, но разумная сумма | `…02` «Алтайские сыроварни» | 3 млн ₽, выручка 18 млн ₽ |
| 🔄 Другие условия | нагрузка больше 50 % выручки — банк уменьшает сумму | `…02` | 20 млн ₽, текущие платежи 200 тыс. ₽/мес. |
| ✍️ Уточнение | выручка в анкете больше данных ФНС в 1,5+ раза | `…02` | выручка 50 млн ₽ |
| ❌ Отказ | просрочки до 45 дней, 4 кредита — балл ниже 500 | `…03` «Сибирский кран» | 3 млн ₽ |
| ❌ Отказ | компания ликвидирована по ЕГРЮЛ | `…04` «Закрытый горизонт» | любая сумма |
| ✍️ Уточнение (полная анкета) | 8 млн ₽, поручитель с просрочками в БКИ | `…01` | ИНН поручителя `540100000009` |
| ✅ Окончательно, ставка −1 п.п. | 8 млн ₽, залог-недвижимость на всю сумму | `…01` | залог 15 млн ₽ |

Каждая строка — эталонный случай в тесте `TestScenariosProduceExpectedOutcome`: он прогоняет данные через настоящий конвейер и скоринг, поэтому таблица не расходится с поведением.
</details>

## 🧱 Платформа

Кредитный процесс меняет деньги и юридически значимые статусы, поэтому платформа устроена как в банке:

```mermaid
flowchart LR
  UI[Браузер] --> API["api × N<br/>REST, кэш, лимиты,<br/>Idempotency-Key"]
  API -- "изменение + событие<br/>одной транзакцией" --> PG[("PostgreSQL<br/>заявки · счета · сессии<br/>outbox · аудит")]
  API -- "outbox relay" --> K[["Kafka<br/>credit.applications.v1"]]
  K --> W["worker × M<br/>конвейер решения"]
  K --> N["worker<br/>уведомления"]
  W -- "шаг + inbox<br/>одной транзакцией" --> PG
```

| | |
|---|---|
| 🐘 **PostgreSQL** | Выдача и платёж атомарны (`SELECT … FOR UPDATE`, `CHECK (balance >= 0)`). Журнал аудита неизменяем: триггер запрещает UPDATE и DELETE. Сессии и ключи идемпотентности в БД — API масштабируется горизонтально. |
| 📨 **Kafka + outbox** | Событие пишется в той же транзакции, что и смена статуса, relay доставляет его в Kafka. Событие не теряется, даже если брокер недоступен, а API продолжает принимать заявки. At-least-once с inbox против дублей, DLQ для «ядовитых» сообщений. |
| ⚡ **Кэш** | ЕГРЮЛ/ФНС (1 ч), БКИ (10 мин), калькулятор, сессии; singleflight против шторма запросов; `ETag`/304 для опроса статуса. |
| 🛡 **Надёжность API** | `Idempotency-Key` (повтор не задвоит платёж), лимит `429 + Retry-After`, таймауты, JSON-ошибки с `request_id`, пагинация, gzip. |
| 📊 **Наблюдаемость** | JSON-логи с `request_id` и маскированием персональных данных, метрики Prometheus, `/readyz` с причиной отказа. |

Отказы, гарантии доставки, диаграммы статусов и ER-модель — в **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**.

## 📸 Скриншоты

<table>
  <tr>
    <td align="center"><b>Главная</b><br><img src="docs/screens/landing.png" alt="Главная"></td>
    <td align="center"><b>Быстрая анкета с калькулятором</b><br><img src="docs/screens/apply.png" alt="Анкета"></td>
  </tr>
  <tr>
    <td align="center"><b>Прогресс заявки</b><br><img src="docs/screens/progress.png" alt="Прогресс"></td>
    <td align="center"><b>Кабинет сотрудника банка</b><br><img src="docs/screens/bank.png" alt="Кабинет банка"></td>
  </tr>
  <tr>
    <td align="center"><b>Окончательное решение</b><br><img src="docs/screens/approved.png" alt="Одобрено"></td>
    <td align="center"><b>Отказ с объяснением</b><br><img src="docs/screens/reject.png" alt="Отказ"></td>
  </tr>
</table>

<details>
<summary><b>🖼 Ещё скриншоты</b></summary>
<br>

<table>
  <tr>
    <td align="center"><b>Предварительное решение</b><br><img src="docs/screens/preliminary.png" alt="Предварительное решение"></td>
    <td align="center"><b>Полная анкета</b><br><img src="docs/screens/full-form.png" alt="Полная анкета"></td>
  </tr>
  <tr>
    <td align="center"><b>Контрпредложение</b><br><img src="docs/screens/counter.png" alt="Контрпредложение"></td>
    <td align="center"><b>Как банк оценил заявку</b><br><img src="docs/screens/score.png" alt="Разбор балла"></td>
  </tr>
  <tr>
    <td align="center"><b>Кабинет: кредиты</b><br><img src="docs/screens/cab-credits.png" alt="Кредиты"></td>
    <td align="center"><b>Кабинет банка: платформа</b><br><img src="docs/screens/bank-platform.png" alt="Платформа"></td>
  </tr>
  <tr>
    <td align="center"><b>Профиль и реквизиты</b><br><img src="docs/screens/profile.png" alt="Профиль"></td>
    <td align="center"><b>Меню профиля</b><br><img src="docs/screens/profile-menu.png" alt="Меню профиля"></td>
  </tr>
  <tr>
    <td align="center"><b>Профиль сотрудника банка</b><br><img src="docs/screens/staff-profile.png" alt="Профиль сотрудника"></td>
    <td align="center"><b>Вход: демо-аккаунты</b><br><img src="docs/screens/login.png" alt="Вход"></td>
  </tr>
</table>

<p align="center">
  <img src="docs/screens/mobile-landing.png" width="240" alt="Главная на телефоне">
  &nbsp;&nbsp;
  <img src="docs/screens/mobile-cabinet.png" width="240" alt="Кабинет на телефоне">
</p>
</details>

## 🧪 Проверка и тесты

После `docker compose up -d --build` одна команда проходит весь кредитный процесс через API — вход, заявку, решение конвейера через Kafka, подписание, выдачу, оплату без двойного списания и журнал аудита:

```bash
bash scripts/check.sh           # около 10 секунд
bash scripts/check.sh --chaos   # плюс остановка Kafka: заявки принимаются и после восстановления доходят до решения
```

Самопроверка `/selfcheck` уже лежит в образе — Go, Node и curl на машине не нужны.

```bash
bash scripts/test-all.sh                  # vet, тесты с race-детектором и покрытием, e2e в браузере
cd backend && go test ./...               # только Go, без внешних зависимостей
make test-integration                     # интеграционные тесты на PostgreSQL и Kafka из docker compose
cd tests/lint && npm ci && npm run lint   # фронтенд: ESLint, Prettier, CSS-переменные, юнит-тесты
```

Каждый pull request проходит то же самое в [CI](.github/workflows/ci.yml): тесты на настоящих PostgreSQL и Kafka, весь стек в docker compose, браузерные e2e, самопроверку и отказ Kafka. Правила веток и коммитов — в [CONTRIBUTING.md](CONTRIBUTING.md).

<details>
<summary><b>🔬 Что покрыто тестами</b></summary>
<br>

| Уровень | Что проверяется | Где |
|---|---|---|
| Домен | переходы статусов, валидация анкеты, свойства графика платежей на сотнях комбинаций | `internal/domain` |
| Скоринг и конвейер | одобрение, контрпредложение, уточнение, отказ; сбой внешних сервисов повторяется, а не отклоняет заявку | `internal/scoring`, `internal/pipeline` |
| Сервис | ограничения продукта, чужие заявки невидимы, выдача, оплата, вход при сбое хранилища | `internal/service` |
| PostgreSQL | миграции, outbox в той же транзакции, параллельные оплаты без двойного списания, неизменяемый аудит, сессии и Idempotency-Key | `internal/storage/pgstore` |
| Kafka | порядок внутри ключа, fan-out на две consumer group, «ядовитое» сообщение уходит в DLQ | `internal/events/kafkabus` |
| API | Idempotency-Key, ETag/304, пагинация, лимиты, CSRF, роли, заголовки безопасности, `/readyz` | `internal/httpapi` |
| E2E | 29 сценариев в браузере: полный путь клиента через обе анкеты до оплаты, все исходы решения, кабинет банка, телефон, клавиатура, автосохранение без сети. Прогоняются на файлах, PostgreSQL и PostgreSQL + Kafka | `tests/e2e` |

E2E без Node — в образе Playwright (сервер запущен на 8080):

```bash
docker run --rm --network host -v "$PWD":/w -w /w mcr.microsoft.com/playwright:v1.56.0-noble \
  sh -c "cd tests/e2e && npm ci >/dev/null && BASE_URL=http://localhost:8080 node e2e.mjs"
```

С `demo.mjs` вместо `e2e.mjs` тот же запуск записывает демо-видео, с `screens.mjs` — скриншоты. На macOS и Windows замените `localhost` на `host.docker.internal` и уберите `--network host`.
</details>

## 🗂 Структура

Модульный монолит на **Go 1.24** (stdlib, `lib/pq`, `franz-go`) с двумя ролями из одного образа — **api** и **worker**. Фронтенд — SPA на чистом JS без сборки, встроен в бинарь; контейнер — distroless без root.

```
backend/
├── cmd/api/                точка входа, конфиг, graceful shutdown
├── cmd/selfcheck/          сквозная самопроверка запущенного стека
├── internal/domain/        заявка, статусы, продукты, расчёт платежа и графика
├── internal/service/       сценарии клиента и банка: транзакции, аудит
├── internal/pipeline/      конвейер решения: обработчик событий Kafka и sweeper
├── internal/scoring/       балльная модель и кредитная логика
├── internal/notify/        уведомления клиента
├── internal/events/        события и шина; kafkabus/ — Kafka (продюсер, consumer groups, DLQ)
├── internal/storage/       хранение в файлах; pgstore/ — PostgreSQL (миграции, outbox, inbox, аудит)
├── internal/cache/         TTL-LRU кэш с singleflight
├── internal/integrations/  моки ЕГРЮЛ/ФНС и БКИ с кэширующими адаптерами
├── internal/httpapi/       REST /api/v1 и middleware
└── web/static/             фронтенд: css/, js/core, js/pages, js/features, js/ui, fonts/
tests/                      e2e в браузере, линтеры и юнит-тесты фронтенда
docs/                       архитектура, OpenAPI, примеры запросов
```

Контракт API — [docs/openapi.yaml](docs/openapi.yaml), примеры запросов — [docs/requests.http](docs/requests.http).

<details>
<summary><b>📋 Соответствие условиям кейса</b></summary>
<br>

| Требование | Как выполнено |
|---|---|
| Связанный процесс клиент ↔ банк | Клиент подаёт заявку и получает результат; фоновый конвейер проверяет данные через ЕГРЮЛ/ФНС/БКИ, считает балл и принимает решение. Банк видит то же в своём кабинете. |
| Enterprise-ориентированность | Безопасность: обязательный вход, HttpOnly-cookie, CSRF, PBKDF2, CSP, distroless nonroot, маскирование персональных данных, неизменяемый аудит. Надёжность: ACID-транзакции для денег, transactional outbox, идемпотентность. Отказоустойчивость: API работает без Kafka, повторы и DLQ, `/readyz`, graceful shutdown. Масштабирование: stateless API, воркеры по партициям (`--scale worker=N`). Сопровождение: логи, метрики, миграции, OpenAPI. |
| Целостность решения | Один процесс от калькулятора до платежей; каждый шаг виден клиенту и банку, каждое изменение — событие в Kafka и запись в аудите. |
| Контракты внешних сервисов | ЕГРЮЛ/ФНС и БКИ — адаптеры за интерфейсами `Registry` и `CreditBureau`; замена мока на реальный клиент не трогает бизнес-логику. |
| Без платных API | Внешних платных вызовов нет: ЕГРЮЛ, ФНС, БКИ — локальные моки, подписание и выдача симулируются. Работает офлайн. |
| Обычный ПК | Около 1 ГБ RAM на всё, из них Kafka — 512 МБ. ML и GPU не нужны: решение принимает детерминированная балльная модель. |
| Демонстрация | Готовые аккаунты клиента и банка и [видео](demo/alfa-credit-demo.mp4). |
| Репозиторий | Docker, `go.mod`, README, [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES), [OpenAPI](docs/openapi.yaml), [архитектура](docs/ARCHITECTURE.md), тесты. |

**Ресурсы:** Windows 10+, macOS 12+ или Linux; Docker Engine 24+ с Compose v2; 2 ядра, 2 ГБ свободной памяти, около 2 ГБ на диске под образы. Итоговый образ сервиса — около 12 МБ.
</details>

## 📌 Допущения MVP

> [!NOTE]
> - Вход по телефону и паролю без SMS. В бою — OIDC (Альфа ID) на API Gateway.
> - Один брокер Kafka и один экземпляр PostgreSQL, чтобы хватило обычного ПК. В бою — 3 брокера с `min.insync.replicas=2`, PostgreSQL с репликацией, общий кэш и лимиты в Redis.
> - ЕГРЮЛ, ФНС и БКИ — локальные моки с сокращёнными контрактами; подписание и выдача денег симулируются.
> - Ставки, лимиты и компании демонстрационные, все данные синтетические.

<br>

<p align="center">
  <sub>Сделано для <b>Альфа Хак 2026</b> · кейс «Умный кредитный кабинет предпринимателя»</sub>
</p>
