# Архитектура: цифровой кредитный конвейер для МСБ

## 1. Границы MVP

| Входит | Не входит (и почему) |
|---|---|
| Калькулятор → анкета **без промежуточной «заявки»** | Реальные интеграции ЕГРЮЛ/БКИ — по условиям кейса только моки |
| Автозаполнение компании по ИНН (ЕГРЮЛ/ЕГРИП-мок) | Загрузка и распознавание документов — следующий этап |
| Автосохранение анкеты, прогресс заполнения | Реальная ЭП и выдача денег — симуляция ПЭП |
| Конвейер: проверка данных → скоринг → решение | Ручной андеррайтинг (очередь сотрудника) — заложен в статусах, UI read-only |
| 4 типа решения: одобрение / контрпредложение / уточнение / отказ | Мобильное приложение — запрещено условиями |
| Подписание и выдача внутри продукта (без SMS/почты) | |
| Прогресс заявки в реальном времени, кабинет банка | |

**Цель по UX — минимум времени клиента:** 6 обязательных пунктов в анкете, остальное берём из реестров. Параметры кредита переносятся из калькулятора, контактное лицо предзаполняется руководителем из ЕГРЮЛ, выручку можно подставить из данных ФНС одной кнопкой. Решение приходит примерно за 7–15 секунд.

## 2. Компоненты MVP (модульный монолит на Go)

```mermaid
flowchart LR
  subgraph Client["Браузер клиента / сотрудника"]
    UI["SPA: кабинет, хот-бар,<br/>анкета + калькулятор,<br/>прогресс заявки, кабинет банка"]
  end

  subgraph API["Контейнер api (Go 1.24, distroless, nonroot)"]
    HTTP["httpapi<br/>REST /api/v1, middleware:<br/>request-id, логи, recover,<br/>security headers, лимит тела"]
    SVC["service<br/>сценарии клиента и банка"]
    DOM["domain<br/>заявка, статусная машина,<br/>продукты, аннуитет"]
    PIPE["pipeline.Worker<br/>фоновый конвейер"]
    SCR["scoring<br/>балльная модель + кредитная логика"]
    INT["integrations<br/>Registry (ЕГРЮЛ/ЕГРИП, ФНС)<br/>CreditBureau (БКИ)"]
    AUTH["auth + AuthService<br/>сессии, PBKDF2, лимит попыток,<br/>профиль, реквизиты, баланс"]
    REPO["storage.Repository"]
  end

  VOL[("volume /data<br/>applications.json")]

  UI -- "HTTPS JSON" --> HTTP
  HTTP --> SVC --> DOM
  HTTP --> AUTH --> REPO
  SVC --> REPO
  SVC --> INT
  PIPE --> REPO
  PIPE --> INT
  PIPE --> SCR --> DOM
  REPO --> VOL
```

Почему модульный монолит: за время хакатона это один артефакт и один `docker compose up`. При этом границы пакетов совпадают с будущими сервисами (`service`, `pipeline`, `scoring`, `integrations`), зависимости идут только внутрь, к `domain`. Хранилище и интеграции закрыты интерфейсами, поэтому их можно заменить без правки бизнес-логики.

## 3. Целевая архитектура (enterprise)

```mermaid
flowchart LR
  WEB["Web SPA"] --> GW["API Gateway<br/>OIDC (Альфа ID), rate limit, WAF"]
  GW --> APP["application-service<br/>анкеты, статусы"]
  GW --> CALC["product-catalog / calc"]
  APP -- "ApplicationSubmitted" --> BUS[["Kafka"]]
  BUS --> CHK["data-check-service"]
  CHK --> REG["Адаптеры: ЕГРЮЛ, ФНС, БКИ<br/>circuit breaker, ретраи, кэш"]
  CHK -- "DataChecked" --> BUS
  BUS --> SCO["scoring/decision-service<br/>версионируемые правила"]
  SCO -- "DecisionMade" --> BUS
  BUS --> APP
  BUS --> UW["Очередь андеррайтера<br/>(нестандартные кейсы)"]
  APP --> PG[("PostgreSQL<br/>+ outbox")]
  APP --> AUD[("Аудит-журнал")]
  APP -- SSE/WebSocket --> WEB
```

| Требование | MVP | Цель |
|---|---|---|
| Безопасность | вход обязателен для оформления, подписания и оплаты; сессия в HttpOnly-cookie (SameSite=Strict, TTL 12 ч); проверка Origin против CSRF; пароли PBKDF2-SHA256 с солью; блокировка после 5 неудачных попыток по телефону и IP; nonroot distroless, read-only FS, `cap_drop: ALL`, CSP; чужие заявки отдают 404; лимит тела 64 КБ | OIDC/JWT, mTLS между сервисами, шифрование ПДн (152-ФЗ), Vault |
| Надёжность | атомарная запись (tmp + rename), оптимистичная проверка статуса в worker, graceful shutdown, healthcheck | PostgreSQL + transactional outbox, идемпотентные консьюмеры |
| Отказоустойчивость | ретрай шага конвейера на следующем тике | несколько реплик, circuit breaker на адаптерах, DLQ |
| Масштабирование | stateless HTTP, worker отделён логически | HPA по lag'у Kafka, партиционирование по ID заявки |
| Сопровождение | структурные JSON-логи с request-id, тесты домена и e2e | OpenTelemetry, метрики конверсии по этапам, feature flags на правила скоринга |

## 4. Диаграмма состояний заявки

```mermaid
stateDiagram-v2
  [*] --> draft: калькулятор → «Перейти к анкете»
  draft --> submitted: клиент отправил (валидация полноты)
  draft --> cancelled
  submitted --> data_check: конвейер
  data_check --> scoring: ЕГРЮЛ ок, выручка сходится с ФНС
  data_check --> need_info: выручка > 1,5 × ФНС
  data_check --> rejected: компания ликвидирована / не найдена
  scoring --> approved: балл ≥ 650 и DTI ≤ 50%
  scoring --> counter_offer: 500 ≤ балл, подобрана сумма/срок
  scoring --> rejected: балл < 500
  need_info --> submitted: клиент исправил анкету
  approved --> signing: клиент принял
  counter_offer --> signing: клиент принял новые условия
  approved --> cancelled
  counter_offer --> cancelled
  signing --> issued: подписал ПЭП
  issued --> closed: оплачены все платежи
  closed --> [*]
  rejected --> [*]
  cancelled --> [*]
```

Каждый переход проверяется в `domain.CanTransition` и пишется в `history` (кто, когда, комментарий). Это аудит-журнал заявки.

## 5. Модель данных

```mermaid
erDiagram
  APPLICATION ||--o| COMPANY : "снимок из ЕГРЮЛ"
  APPLICATION ||--o| BUREAU_REPORT : "ответ БКИ"
  APPLICATION ||--o{ CHECK : "проверки"
  APPLICATION ||--o| DECISION : "решение"
  APPLICATION ||--|{ EVENT : "история статусов"
  APPLICATION }o--|| PRODUCT : "продукт"
  APPLICATION {
    string id PK
    string number "КР-000001"
    string client_id
    string product_id FK
    int amount
    int term_months
    string purpose
    string inn
    json financials "выручка, обязательства, штат"
    json contact
    bool consent
    string status
    int score
    timestamp created_at
    timestamp status_changed_at
  }
  PRODUCT { string id PK
    float rate
    int min_amount
    int max_amount
    int min_term
    int max_term }
  DECISION { string type
    int amount
    int term_months
    float rate
    int monthly_payment
    string[] reasons }
  EVENT { string status
    timestamp at
    string actor
    string comment }
```

## 6. Скоринг и кредитная логика

Скоринг — это один из элементов конвейера, а не всё решение. Балл считается от базы 600:

| Фактор | Влияние |
|---|---|
| Бизнесу < 1 года / ≥ 3 лет | −80 / +50 |
| Рейтинг БКИ | (рейтинг − 650) / 2, в пределах [−60; +80] |
| Просрочки в БКИ | −150 |
| > 3 действующих кредитов | −40 |
| DTI = (обязательства + новый платёж) / месячная выручка: ≤30% / ≤50% / ≤70% / >70% | +80 / +30 / −40 / −120 |
| Сумма > годовой выручки | −60 |

Решение: балл < 500 → отказ с перечнем факторов. Балл ≥ 650 и DTI ≤ 50% → одобрение (−1 п.п. к ставке при балле ≥ 750). Иначе → **контрпредложение**: максимальная сумма, при которой платёж укладывается в 50% выручки, сначала на запрошенный срок, потом на максимальный срок продукта, ставка +1,5 п.п. Если и так выходит меньше минимума продукта, это отказ.

## 7. Сквозной сценарий

```mermaid
sequenceDiagram
  actor C as Клиент
  participant UI as SPA
  participant API as api
  participant W as pipeline
  participant R as ЕГРЮЛ (мок)
  participant B as БКИ (мок)
  C->>UI: калькулятор: сумма, срок
  UI->>API: POST /applications (draft)
  C->>UI: ИНН
  UI->>API: PATCH {inn}
  API->>R: Lookup(inn)
  R-->>API: компания, руководитель, выручка ФНС
  API-->>UI: анкета с автозаполнением
  C->>UI: телефон, согласие → «Отправить»
  UI->>API: POST /submit
  loop каждые ~3 сек
    W->>R: проверка статуса компании
    W->>B: кредитная история
    W->>W: scoring.Evaluate → decision
  end
  UI->>API: GET /applications/{id} (поллинг 1,2 сек)
  API-->>UI: approved / counter_offer / need_info / rejected
  C->>UI: «Принять» → «Подписать ПЭП»
  UI->>API: POST /accept, POST /sign
  API-->>UI: issued + график платежей
```
