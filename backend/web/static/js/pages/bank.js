// Кабинет сотрудника банка: все заявки конвейера в реальном времени и состояние платформы.
// Доступ — по сессии сотрудника банка (вход по телефону и паролю) или по служебному токену BANK_TOKEN.
import { api } from '../core/api.js';
import { $, every, on, storage } from '../core/dom.js';
import { esc, rub, months, rate, dateTime } from '../core/format.js';
import { product, session, isStaff } from '../core/session.js';
import { openAuth, logout } from '../features/auth.js';
import { icon } from '../ui/icons.js';
import { IN_PIPELINE, DECIDED, statusBadge } from '../domain.js';
import { pageHead } from './parts.js';

const CACHE_NAMES = { registry: 'ЕГРЮЛ/ФНС', bureau: 'БКИ', calc: 'калькулятор', sessions: 'сессии' };

function kpis(rows) {
  const count = (statuses) => rows.filter((r) => statuses.includes(r.status)).length;
  const scored = rows.filter((r) => r.score != null);
  const avgScore = scored.length ? Math.round(scored.reduce((s, r) => s + r.score, 0) / scored.length) : '—';
  const times = rows
    .map((r) => {
      const sent = r.history.find((h) => h.status === 'submitted');
      const decided = r.history.find((h) => DECIDED.includes(h.status));
      return sent && decided ? (new Date(decided.at) - new Date(sent.at)) / 1000 : null;
    })
    .filter((x) => x != null);
  const avgTime = times.length ? `${Math.round(times.reduce((a, b) => a + b, 0) / times.length)} с` : '—';
  const items = [
    ['Всего заявок', rows.length],
    ['В конвейере', count(IN_PIPELINE)],
    ['Одобрено / выдано', count(['approved', 'counter_offer', 'full_form', 'signing', 'issued'])],
    ['Отказ', count(['rejected'])],
    ['Средний балл', avgScore],
    ['Время до решения', avgTime],
  ];
  return `<div class="kpis">${items.map(([label, value]) => `<div class="kpi"><span>${label}</span><b>${value}</b></div>`).join('')}</div>`;
}

function platform(p) {
  if (!p) return '';
  const chip = (ok, label, value) => `<span class="${ok ? '' : 'is-warn'}">${label}${value != null ? ` <b>${esc(value)}</b>` : ''}</span>`;
  const caches = (p.caches || [])
    .filter((c) => CACHE_NAMES[c.name] && c.hits + c.misses >= 20)
    .map((c) => chip(true, `Кэш ${CACHE_NAMES[c.name]}`, `${Math.round(c.hit_rate * 100)}%`))
    .join('');
  return `<div class="platform" aria-label="Состояние платформы">
    ${chip(true, 'Хранилище', p.storage === 'postgres' ? 'PostgreSQL' : 'файлы')}
    ${chip(true, 'Шина событий', p.bus === 'kafka' ? 'Kafka' : 'in-memory')}
    ${p.outbox_pending != null ? chip(p.outbox_pending < 50, 'Outbox в очереди', p.outbox_pending) : ''}
    ${chip(true, 'Событий отправлено', p.events_published)}
    ${caches}
  </div>`;
}

const checkDots = (checks) =>
  `<span class="checks-dots">${checks.map((c) => `<i class="${c.result}" title="${esc(`${c.name}: ${c.details}`)}"></i>`).join('')}</span>`;

function table(rows) {
  if (!rows.length) return '<div class="empty">Заявок пока нет — оформите кредит на главной</div>';
  return `<div class="table-wrap" tabindex="0" role="region" aria-label="Заявки конвейера"><table class="table">
    <thead><tr><th>№</th><th>Компания</th><th>Продукт</th><th class="num">Запрошено</th><th>Статус</th><th class="num">Балл</th><th>Решение</th><th>Проверки</th><th>Обновлено</th></tr></thead>
    <tbody>${rows
      .map(
        (r) => `<tr>
      <td class="nowrap"><b>${esc(r.number)}</b></td>
      <td>${r.company ? `${esc(r.company.name)}<div class="xsmall muted">ИНН ${esc(r.inn)}</div>` : '—'}</td>
      <td>${esc(product(r.product_id)?.name)}</td>
      <td class="num nowrap">${rub(r.amount)}<div class="xsmall muted">${months(r.term_months)}</div></td>
      <td>${statusBadge(r.status, r)}</td>
      <td class="num">${r.score ?? '—'}</td>
      <td class="small">${r.decision ? `${r.decision.amount ? `${rub(r.decision.amount)} · ${rate(r.decision.rate)}` : ''}<div class="muted">${esc(r.decision.reasons?.[0] || '')}</div>` : '—'}</td>
      <td>${checkDots(r.checks)}</td>
      <td class="xsmall muted nowrap">${dateTime(r.updated_at)}</td>
    </tr>`,
      )
      .join('')}</tbody></table></div>`;
}

/** Гость: кабинет банка — для сотрудников. Основной вход — учётная запись сотрудника, запасной — служебный токен. */
function gate(root, signal, { client = false } = {}) {
  root.innerHTML = `<div class="container"><section class="card empty bank-gate">
    <span class="bank-gate__icon">${icon('briefcase', 28)}</span>
    <h1>Кабинет банка — для сотрудников</h1>
    <p>${
      client
        ? 'Вы вошли как представитель компании. Очередь заявок, решения и состояние платформы видят только сотрудники банка.'
        : 'Очередь заявок, решения и состояние платформы. Войдите учётной записью сотрудника банка.'
    }</p>
    <div class="row row--wrap bank-gate__actions">
      <button class="btn btn--primary" data-bank-login>${icon('lock', 20)} ${client ? 'Выйти и войти как сотрудник' : 'Войти как сотрудник'}</button>
      <button class="btn btn--ghost" data-bank-token>Служебный токен</button>
    </div>
  </section></div>`;
  on(
    root,
    'click',
    '[data-bank-login]',
    async () => {
      if (client) await logout();
      openAuth({ reason: 'Кабинет банка открывается учётной записью сотрудника банка.' }); // сотрудник после входа попадёт сюда же
    },
    signal,
  );
  on(root, 'click', '[data-bank-token]', () => askToken(root, signal, false), signal);
}

/** Служебный токен (BANK_TOKEN): для интеграций и проверки без учётной записи. В демо-режиме — demo-bank. */
function askToken(root, signal, rejected) {
  root.innerHTML = `<div class="container">
    ${pageHead('Кабинет банка', 'Вход по служебному токену')}
    <form class="card stack bank-login" id="bank-login">
      <div class="field"><label for="bank-token">Токен кабинета банка</label>
        <input class="input" id="bank-token" type="password" autocomplete="off" required></div>
      ${rejected ? '<p class="bank-login__error" role="alert">Токен не подошёл</p>' : ''}
      <button class="btn btn--primary">Войти</button>
    </form>
  </div>`;
  $('#bank-token', root).focus();
  $('#bank-login', root).addEventListener(
    'submit',
    (e) => {
      e.preventDefault();
      storage.set('bank_token', $('#bank-token', root).value.trim());
      bank(root, { signal });
    },
    { signal },
  );
}

export default async function bank(root, { signal }) {
  root.className = 'page';
  const staff = isStaff();
  const token = staff ? '' : storage.get('bank_token', '');
  if (!staff && !token) return gate(root, signal, { client: !!session.me });
  const headers = token ? { 'X-Bank-Token': token } : undefined; // сотруднику хватает cookie сессии
  const who = staff ? `${esc(session.me.full_name)} · ${esc(session.me.position)}` : 'Вход по служебному токену';
  root.innerHTML = `<div class="container">
    ${pageHead('Очередь заявок', `${who} · обновление каждые 2 секунды`, '<span class="status status--ok">Live</span>')}
    <div id="bank-platform"></div>
    <div id="bank-kpis"></div>
    <section class="card" id="bank-table"><div class="skeleton"></div></section>
  </div>`;

  const load = async () => {
    const [rows, p] = await Promise.all([
      api('GET', '/bank/applications?limit=200', undefined, headers),
      api('GET', '/bank/platform', undefined, headers).catch(() => null),
    ]);
    if (signal.aborted) return;
    $('#bank-platform', root).innerHTML = platform(p);
    $('#bank-kpis', root).innerHTML = kpis(rows);
    $('#bank-table', root).innerHTML = table(rows);
  };
  try {
    await load();
  } catch (e) {
    if (staff || e.status !== 401) throw e;
    storage.set('bank_token', '');
    return askToken(root, signal, true);
  }
  every(2000, () => load().catch(() => {}), signal);
}
