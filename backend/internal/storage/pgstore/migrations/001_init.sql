-- Схема кредитного кабинета. Суммы — в рублях (bigint), как в домене; агрегат заявки — jsonb, ключевые поля продублированы колонками для индексов.

CREATE TABLE IF NOT EXISTS users (
    id            text PRIMARY KEY,
    phone         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    doc           jsonb NOT NULL,                       -- профиль, компания, реквизиты
    balance       bigint NOT NULL CHECK (balance >= 0), -- остаток на расчётном счёте: БД не даст уйти в минус
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE SEQUENCE IF NOT EXISTS application_number_seq;

CREATE TABLE IF NOT EXISTS applications (
    id                text PRIMARY KEY,
    number            text NOT NULL UNIQUE,
    client_id         text NOT NULL REFERENCES users (id),
    status            text NOT NULL,
    product_id        text NOT NULL,
    inn               text NOT NULL DEFAULT '',
    company_name      text NOT NULL DEFAULT '',
    doc               jsonb NOT NULL,          -- агрегат заявки целиком (анкета, проверки, решение, история)
    version           integer NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    status_changed_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS applications_client_created ON applications (client_id, created_at DESC);
CREATE INDEX IF NOT EXISTS applications_status_changed ON applications (status, status_changed_at);
CREATE INDEX IF NOT EXISTS applications_created ON applications (created_at DESC);

-- Сессии: в БД хранится только SHA-256 токена — утечка таблицы не даёт войти.
CREATE TABLE IF NOT EXISTS sessions (
    token_hash text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expires ON sessions (expires_at);

-- Transactional outbox: событие пишется в той же транзакции, что и изменение заявки.
CREATE TABLE IF NOT EXISTS outbox (
    id           bigserial PRIMARY KEY,
    event_id     text NOT NULL UNIQUE,
    topic        text NOT NULL,
    key          text NOT NULL,
    type         text NOT NULL,
    envelope     jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts     integer NOT NULL DEFAULT 0,
    last_error   text
);
CREATE INDEX IF NOT EXISTS outbox_pending ON outbox (id) WHERE published_at IS NULL;

-- Inbox: какие события потребитель уже обработал (идемпотентность при at-least-once доставке).
CREATE TABLE IF NOT EXISTS processed_events (
    consumer     text NOT NULL,
    event_id     text NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, event_id)
);

-- Idempotency-Key: повтор запроса (обрыв сети, двойной клик) возвращает тот же ответ, а не второй платёж.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope        text NOT NULL,   -- клиент
    key          text NOT NULL,
    request_hash text NOT NULL,   -- метод + путь + тело: тот же ключ с другим запросом — ошибка
    status_code  integer,         -- NULL — запрос ещё выполняется
    response     bytea,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);
CREATE INDEX IF NOT EXISTS idempotency_created ON idempotency_keys (created_at);

-- Журнал аудита: только добавление.
CREATE TABLE IF NOT EXISTS audit_log (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL,
    actor      text NOT NULL,
    action     text NOT NULL,
    entity     text NOT NULL,
    entity_id  text NOT NULL,
    outcome    text NOT NULL,
    request_id text,
    details    jsonb
);
CREATE INDEX IF NOT EXISTS audit_entity ON audit_log (entity, entity_id, at);

CREATE OR REPLACE FUNCTION audit_log_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_log_no_change ON audit_log;
CREATE TRIGGER audit_log_no_change BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();
