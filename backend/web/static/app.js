'use strict';
/* Кредитный кабинет предпринимателя — клиентская часть (vanilla JS, без сборки). */

// ---------- utils ----------
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const nf = new Intl.NumberFormat('ru-RU');
const fmt = (n) => nf.format(Math.round(n || 0));
const rub = (n) => fmt(n) + ' ₽';
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const digits = (s) => String(s).replace(/\D/g, '');
// Деньги считает только сервер (domain.Quote в Go). Клиент запрашивает расчёт и показывает его —
// так правила округления и ставки не могут разойтись между фронтом и бэком.
const quoteCache = new Map();
function fetchQuote(productId, amount, term) {
  const k = `${productId}|${amount}|${term}`;
  if (!quoteCache.has(k)) {
    if (quoteCache.size > 300) quoteCache.clear();
    quoteCache.set(k, api('POST', '/calc', { product_id: productId, amount, term_months: term }).catch((e) => { quoteCache.delete(k); throw e; }));
  }
  return quoteCache.get(k);
}
/** Живой расчёт для ползунков: последний запрос побеждает, промежуточные ответы отбрасываются. */
function makeQuoter(onQuote, onError) {
  let seq = 0;
  // Экран, который запросил расчёт. Если пользователь ушёл со страницы раньше ответа,
  // ответ игнорируется — иначе колбэк писал бы в уже удалённые элементы.
  const owner = $('#view');
  const alive = () => document.body.contains(owner);
  const run = debounce(async (pid, amount, term, my) => {
    if (!alive()) return;
    try { const q = await fetchQuote(pid, amount, term); if (my === seq && alive()) onQuote(q); }
    catch (e) { if (my === seq && alive()) onError?.(e); }
  }, 60);
  return (pid, amount, term) => run(pid, amount, term, ++seq);
}
const months = (n) => { const m = n % 10, h = n % 100; const w = m === 1 && h !== 11 ? 'месяц' : m >= 2 && m <= 4 && (h < 10 || h >= 20) ? 'месяца' : 'месяцев'; return `${n} ${w}`; };
const timeOf = (iso) => new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
const dateOf = (iso) => new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
const store = {
  get(k, d) { try { const v = localStorage.getItem(k); return v ? JSON.parse(v) : d; } catch { return d; } },
  set(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* приватный режим */ } },
};

// Сессия — в HttpOnly cookie (JS её не видит). Текущий пользователь — в ME.
let ME = null;

// Действия с деньгами и статусом заявки идут с Idempotency-Key: если связь оборвалась после
// отправки, запрос безопасно повторяется с тем же ключом — сервер не выполнит его второй раз
// (не будет двойного платежа или второй заявки), а вернёт сохранённый ответ.
const IDEMPOTENT = /^\/applications(\/[^/]+\/(submit|accept|sign|pay|cancel))?$/;
const newKey = () => (crypto.randomUUID ? crypto.randomUUID() : [...crypto.getRandomValues(new Uint8Array(16))].map((b) => b.toString(16).padStart(2, '0')).join(''));

async function api(method, path, body, headers = {}) {
  if (navigator.onLine === false) throw Object.assign(new Error('Нет подключения к интернету'), { offline: true });
  const idem = method === 'POST' && IDEMPOTENT.test(path);
  if (idem && !headers['Idempotency-Key']) headers = { ...headers, 'Idempotency-Key': newKey() };
  const send = () => fetch('/api/v1' + path, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...headers },
    body: body ? JSON.stringify(body) : undefined,
  });
  const res = await send()
    .catch(() => (idem ? new Promise((r) => setTimeout(r, 700)).then(send) : Promise.reject()))
    .catch(() => { throw Object.assign(new Error('Нет связи с сервером'), { offline: true }); });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const e = new Error(data?.error?.message || res.statusText);
    e.status = res.status; e.field = data?.error?.field; e.code = data?.error?.code;
    if (res.status === 401 && !path.startsWith('/auth/') && ME) { ME = null; paintAuthSlot(); paintIsland(); }
    throw e;
  }
  return data;
}

const phoneFmt = (raw) => {
  let d = digits(raw);
  if (d.startsWith('8')) d = '7' + d.slice(1);
  if (d && !d.startsWith('7')) d = '7' + d;
  d = d.slice(0, 11);
  const p = [d.slice(1, 4), d.slice(4, 7), d.slice(7, 9), d.slice(9, 11)];
  return d ? '+7' + (p[0] ? ` (${p[0]}` : '') + (p[0].length === 3 ? ')' : '') + (p[1] ? ' ' + p[1] : '') + (p[2] ? '-' + p[2] : '') + (p[3] ? '-' + p[3] : '') : '';
};
const initials = (name) => (name || '').split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join('').toUpperCase();
const shortCompany = (n) => (n || '').replace(/^ООО\s*/, '').replace(/[«»"]/g, '');
const maskAcc = (a) => '•• ' + String(a).slice(-4);

function toast(msg, isErr = false) {
  const t = $('#toast');
  t.textContent = msg; t.className = 'toast show' + (isErr ? ' err' : '');
  clearTimeout(toast.t); toast.t = setTimeout(() => (t.className = 'toast'), 3200);
}

// ---------- icons ----------
const I = {
  plus: '<path d="M12 5v14M5 12h14"/>',
  calc: '<rect x="5" y="3" width="14" height="18" rx="3"/><path d="M8.5 7.5h7M8.5 12h.01M12 12h.01M15.5 12h.01M8.5 15.5h.01M12 15.5h.01M15.5 15.5h.01"/>',
  list: '<path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01"/>',
  bolt: '<path d="M13 3L5 13.5h6L10 21l8-10.5h-6L13 3z"/>',
  truck: '<path d="M3 6h11v10H3zM14 10h4l3 3v3h-7"/><circle cx="7" cy="17.5" r="1.8"/><circle cx="17" cy="17.5" r="1.8"/>',
  cycle: '<path d="M20 12a8 8 0 01-14.3 4.9M4 12a8 8 0 0114.3-4.9"/><path d="M18.5 3.5v4h-4M5.5 20.5v-4h4"/>',
  bank: '<path d="M3 10l9-6 9 6M5 10v8M9.5 10v8M14.5 10v8M19 10v8M3 20h18"/>',
  doc: '<path d="M7 3h7l5 5v13H7z"/><path d="M14 3v5h5M10 13h6M10 17h6"/>',
  chat: '<path d="M4 5h16v11H9l-5 4z"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M12 2.5v3M12 18.5v3M21.5 12h-3M5.5 12h-3M18.7 5.3l-2.1 2.1M7.4 16.6l-2.1 2.1M18.7 18.7l-2.1-2.1M7.4 7.4L5.3 5.3"/>',
  back: '<path d="M15 5l-7 7 7 7"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2.5"/><path d="M8 11V8a4 4 0 018 0v3"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2.5"/><path d="M5 15V6a2 2 0 012-2h9"/>',
  out: '<path d="M15 4h3a2 2 0 012 2v12a2 2 0 01-2 2h-3M10 16l-4-4 4-4M6 12h10"/>',
  card: '<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M3 10h18M7 15h4"/>',
  check: '<path d="M5 12.5l4.5 4.5L19 7"/>',
};
// Иконки дизайн-системы Альфы (icons.js); I — запасной контурный набор для того, чего в ней нет.
const icon = (n, size = 24) => (typeof ALFA_ICONS !== 'undefined' && ALFA_ICONS[n])
  ? `<svg class="ic" viewBox="0 0 24 24" width="${size}" height="${size}" fill="currentColor" aria-hidden="true" focusable="false">${ALFA_ICONS[n]}</svg>`
  : `<svg class="ic" viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${I[n]}</svg>`;

// ---------- domain dictionaries ----------
const STATUS = {
  draft: ['Черновик', 'grey'], submitted: ['Отправлена', 'blue'], data_check: ['Проверка данных', 'blue'],
  scoring: ['Оценка риска', 'blue'], approved: ['Одобрено', 'green'], counter_offer: ['Есть предложение', 'amber'],
  need_info: ['Нужны уточнения', 'amber'], rejected: ['Отказ', 'red'], signing: ['Подписание', 'blue'],
  issued: ['Кредит выдан', 'green'], cancelled: ['Отменена', 'grey'], closed: ['Погашен', 'grey'],
};
const pill = (s) => `<span class="pill pill--${STATUS[s][1]}">${STATUS[s][0]}</span>`;
const STAGES = ['Анкета', 'Проверка данных', 'Оценка риска', 'Решение', 'Подписание', 'Деньги на счёте'];
const STAGE_OF = { draft: 0, need_info: 0, submitted: 1, data_check: 1, scoring: 2, approved: 3, counter_offer: 3, rejected: 3, signing: 4, issued: 5, closed: 5, cancelled: 0 };
const IN_PIPELINE = ['submitted', 'data_check', 'scoring'];
const FINAL = ['issued', 'cancelled', 'rejected', 'closed'];
const NEEDS_ACTION = { approved: ['Кредит одобрен', 'Подписать', 'green'], counter_offer: ['Есть предложение', 'Смотреть', 'amber'], signing: ['Документы готовы', 'Подписать', 'blue'], need_info: ['Нужно уточнение', 'Исправить', 'amber'] };
const DOING = {
  submitted: 'Заявка принята, ставим в очередь на проверку…',
  data_check: 'Проверяем компанию в ЕГРЮЛ/ЕГРИП и кредитную историю в БКИ…',
  scoring: 'Считаем долговую нагрузку и принимаем решение…',
};
const DEMO_ACCOUNTS = [
  ['79000000001', 'ООО «Северный ветер»', 'одобрение'],
  ['79000000002', 'ООО «Алтайские сыроварни»', 'контрпредложение от 15 млн'],
  ['79000000003', 'ООО «Сибирский кран»', 'отказ — просрочки'],
  ['79000000004', 'ООО «Закрытый горизонт»', 'отказ — ликвидирована'],
];
const DEMO_PASSWORD = 'demo1234';
const PROTECTED = ['apply', 'app', 'apps', 'profile', 'credits', 'messages'];

let PRODUCTS = [];
const product = (id) => PRODUCTS.find((p) => p.id === id);
let timers = [];
const later = (fn, ms) => timers.push(setInterval(fn, ms));
const clearTimers = () => { timers.forEach(clearInterval); timers = []; };

// ---------- router ----------
function go(hash) { if (location.hash === hash) render(); else location.hash = hash; }

async function render() {
  clearTimers();
  closeModal();
  if (typeof closeDD === 'function') { closeDD(); hideFly(); }
  const h = location.hash || '#/';
  const [, route, id] = h.split('/');
  $$('[data-nav]').forEach((a) => a.classList.toggle('active', a.dataset.nav === (route || 'home') || (route === 'app' && a.dataset.nav === 'apps')));
  island.open = false;
  if (route !== 'apply') setIslandCtx(null);
  document.body.dataset.page = (route === 'products' || (!route && !ME)) ? 'landing' : 'app';
  const old = $('#view');
  const view = old.cloneNode(false); // сбрасываем обработчики предыдущего экрана
  old.replaceWith(view);
  view.innerHTML = '<div class="skeleton"></div>';
  window.scrollTo(0, 0);
  try {
    if (!PRODUCTS.length) PRODUCTS = await api('GET', '/products');
    if (PROTECTED.includes(route) && !ME) gateView(view);
    else if (route === 'profile') await viewProfile(view);
    else if (route === 'apply') await viewApply(view, id);
    else if (route === 'app') await viewStatus(view, id);
    else if (route === 'apps') await viewDashboard(view);
    else if (route === 'credits') await viewCredits(view);
    else if (route === 'messages') await viewMessages(view);
    else if (route === 'bank') await viewBank(view);
    else if (route === 'products') await viewHome(view);
    else if (ME && !route) await viewDashboard(view);   // после входа «главная» — это кабинет
    else await viewHome(view);
  } catch (e) {
    view.innerHTML = `<div class="card empty"><h2>Не удалось загрузить</h2><p>${esc(e.message)}</p><a class="btn btn--red" href="#/">На главную</a></div>`;
  }
  renderHotbar();
}

function gateView(view) {
  view.innerHTML = `
    <div class="card gate">
      <span class="gate__ic">${icon('lock', 30)}</span>
      <h2>Войдите в кабинет компании</h2>
      <p class="muted">Оформление, подписание договора и оплата доступны только после входа — так данные и деньги вашей компании под защитой.</p>
      <button class="btn btn--red" data-act="login">Войти или зарегистрироваться</button>
    </div>`;
}

const startApply = (productId, amount, term) => requireAuth(() => doStartApply(productId, amount, term), 'Чтобы оформить кредит, войдите — данные компании и реквизиты подставим из профиля.');

async function doStartApply(productId, amount, term) {
  try {
    const a = await api('POST', '/applications', { product_id: productId, amount: amount || 0, term_months: term || 0 });
    go('#/apply/' + a.id);
  } catch (e) { toast(e.message, true); }
}

// ---------- calculator component ----------
function calcBlock(p, amount, term, { compact = false } = {}) {
  const pct = (v, a, b) => (b === a ? 100 : ((v - a) / (b - a)) * 100);
  return `
    <div class="calc__field">
      <div class="calc__label"><span>Сумма кредита</span><span>от ${fmt(p.min_amount)} до ${fmt(p.max_amount)} ₽</span></div>
      <input class="amount-input" inputmode="numeric" data-calc="amount-text" value="${fmt(amount)} ₽" aria-label="Сумма кредита">
      <input type="range" data-calc="amount" min="${p.min_amount}" max="${p.max_amount}" step="10000" value="${amount}" style="--p:${pct(amount, p.min_amount, p.max_amount)}%" aria-label="Сумма кредита, ползунок">
      <div class="range-scale"><span>${fmt(p.min_amount)}</span><span>${fmt(p.max_amount)}</span></div>
    </div>
    ${p.min_term === p.max_term ? `
    <div class="calc__field calc__field--fixed">
      <div class="calc__label"><span>Срок</span><span>фиксированный</span></div>
      <input class="amount-input" data-calc="term-text" value="${months(p.min_term)}" readonly tabindex="-1">
      <input type="hidden" data-calc="term" value="${p.min_term}">
      <div class="calc__note">Срок у этого продукта не меняется: лимит открывается на ${months(p.min_term)}, потом его можно продлить.</div>
    </div>` : `
    <div class="calc__field">
      <div class="calc__label"><span>Срок</span><span>от ${p.min_term} до ${p.max_term} мес.</span></div>
      <input class="amount-input" data-calc="term-text" value="${months(term)}" readonly tabindex="-1" aria-hidden="true">
      <input type="range" data-calc="term" min="${p.min_term}" max="${p.max_term}" step="1" value="${term}" style="--p:${pct(term, p.min_term, p.max_term)}%" aria-label="Срок кредита">
      <div class="range-scale"><span>${months(p.min_term)}</span><span>${months(p.max_term)}</span></div>
    </div>`}
    ${compact ? '' : ''}`;
}

/** Подключает обработчики калькулятора; onChange({amount, term}) вызывается на каждое изменение. */
function bindCalc(root, p, onChange) {
  const ar = $('[data-calc=amount]', root), at = $('[data-calc=amount-text]', root);
  const tr = $('[data-calc=term]', root), tt = $('[data-calc=term-text]', root);
  const paint = (r) => r.style.setProperty('--p', ((r.value - r.min) / (r.max - r.min || 1)) * 100 + '%');
  const emit = () => onChange({ amount: +ar.value, term: +tr.value });
  ar.addEventListener('input', () => { at.value = fmt(ar.value) + ' ₽'; paint(ar); emit(); });
  tr.addEventListener('input', () => { tt.value = months(+tr.value); paint(tr); emit(); });
  // при фокусе выделяем значение целиком — новая сумма сразу заменяет старую
  let justFocused = false;
  at.addEventListener('focus', () => { at.value = fmt(digits(at.value)); at.select(); justFocused = true; });
  at.addEventListener('mouseup', (e) => { if (justFocused) { e.preventDefault(); justFocused = false; } });
  at.addEventListener('input', () => { const v = +digits(at.value); at.value = v ? fmt(v) : ''; });
  const commit = () => {
    let v = +digits(at.value) || p.min_amount;
    v = Math.min(p.max_amount, Math.max(p.min_amount, v));
    ar.value = v; at.value = fmt(v) + ' ₽'; paint(ar); emit();
  };
  at.addEventListener('blur', commit);
  at.addEventListener('keydown', (e) => { if (e.key === 'Enter') at.blur(); });
}

// ---------- ГЛАВНАЯ (продуктовая страница в стиле сайта Альфа-Банка) ----------
const BENEFITS = [
  ['clock', 'Анкета за 3 минуты', 'Компанию, руководителя и реквизиты подставим из ЕГРЮЛ и профиля — вводить почти ничего не нужно.'],
  ['bolt', 'Решение за 15 секунд', 'Проверка данных и оценка риска проходят автоматически, статус обновляется на ваших глазах.'],
  ['doc', 'Без документов и визита', 'Не нужны выписки и справки: выручку подтягиваем из данных налоговой одной кнопкой.'],
  ['card', 'Деньги сразу на счёт', 'Подписываете договор простой электронной подписью прямо в кабинете — без SMS и звонков.'],
];
const STEPS = [
  ['Рассчитайте платёж', 'Выберите кредит, сумму и срок в калькуляторе'],
  ['Заполните анкету', 'Почти всё уже заполнено из профиля — останется выручка и согласие'],
  ['Получите решение', 'Через 10–20 секунд: одобрение, другие условия или понятное объяснение'],
  ['Подпишите онлайн', 'Договор — в кабинете, деньги — на расчётном счёте'],
];
const FAQ = [
  ['Кому доступен кредит?', 'ИП и ООО, зарегистрированным в России. Компания должна действовать по данным ЕГРЮЛ/ЕГРИП, а платежи по всем кредитам не должны превышать половину месячной выручки.'],
  ['Какие документы нужны?', 'Никаких. Данные компании берём из ЕГРЮЛ, выручку можно подставить из данных ФНС, подписание — простой электронной подписью в кабинете.'],
  ['Сколько ждать решения?', 'Обычно 10–20 секунд после отправки анкеты. Страницу можно закрыть — статус сохранится, а уведомление появится в кабинете.'],
  ['Что если банк предложит другие условия?', 'Покажем таблицу «Вы просили / Банк предлагает» с суммой, сроком, ставкой, платежом и переплатой и объясним причину на цифрах из анкеты. Принять или отказаться — решаете вы.'],
  ['Можно ли погасить досрочно?', 'Да. Платежи вносятся с расчётного счёта прямо в кабинете, график пересчитывается автоматически.'],
];
Object.assign(I, { clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>' });

async function viewHome(view) {
  const apps = ME ? island.apps : [];
  const active = apps.find((a) => !FINAL.includes(a.status));
  const st = store.get('calc', { product: 'business-dev' });
  let p = product(st.product) || PRODUCTS[0];
  let amount = st.amount && st.product === p.id ? st.amount : p.default_amount;
  let term = st.term && st.product === p.id ? st.term : p.default_term;
  const maxAmount = Math.max(...PRODUCTS.map((x) => x.max_amount));
  const minRate = Math.min(...PRODUCTS.map((x) => x.rate));
  const maxTerm = Math.max(...PRODUCTS.map((x) => x.max_term));

  view.innerHTML = `
    ${active ? activeBanner(active) : ''}
    <section class="a-hero">
      <div class="a-hero__txt">
        <div class="a-crumbs">Малому бизнесу и ИП <span>›</span> Кредиты</div>
        <h1>Кредит для бизнеса онлайн</h1>
        <p class="a-lead">Анкета за 3 минуты без визита в банк и документов. Решение за 15 секунд, деньги — сразу на расчётный счёт.</p>
        <div class="a-facts">
          <div><b>до ${fmt(maxAmount / 1e6)} млн ₽</b><span>сумма кредита</span></div>
          <div><b>от ${rateStr(minRate)}</b><span>ставка годовых</span></div>
          <div><b>до ${Math.round(maxTerm / 12)} лет</b><span>срок</span></div>
          <div><b>15 сек</b><span>решение</span></div>
        </div>
        <div class="a-hero__btns">
          <button class="btn btn--red btn--lg" data-act="start" data-product="business-dev">Оформить онлайн</button>
          <a class="btn btn--white btn--lg" href="#calc" data-scroll="calc">Рассчитать платёж</a>
        </div>
      </div>
      <div class="a-hero__art" aria-hidden="true">
        <div class="a-art-coin">₽</div>
        <div class="a-art-card a-art-card--1"><span class="a-art-ok">${icon('check', 18)}</span><div><small>Решение по заявке</small><b>Одобрено 3 000 000 ₽</b></div></div>
        <div class="a-art-card a-art-card--2"><small>Ежемесячный платёж</small><b>${rub(p.default_payment)}</b><span class="a-art-bar"><i></i></span></div>
        <div class="a-art-card a-art-card--3"><small>Деньги на счёте</small><b>+ 3 000 000 ₽</b></div>
      </div>
    </section>

    <section class="a-sec">
      <h2>Выберите кредит</h2>
      <div class="a-products">${PRODUCTS.map((x) => `
        <article class="a-prod">
          <div class="a-prod__top">${x.badge ? `<span class="badge">${esc(x.badge)}</span>` : ''}<h3>${esc(x.name)}</h3><p>${esc(x.description)}</p></div>
          <dl class="a-prod__facts">
            <div><dt>Ставка</dt><dd>от ${rateStr(x.rate)}</dd></div>
            <div><dt>Сумма</dt><dd>до ${fmt(x.max_amount)} ₽</dd></div>
            <div><dt>Срок</dt><dd>до ${months(x.max_term)}</dd></div>
          </dl>
          <div class="a-prod__btns"><button class="btn btn--red btn--sm" data-act="start" data-product="${x.id}">Оформить</button><button class="btn btn--light btn--sm" data-calc-pick="${x.id}">Рассчитать</button></div>
        </article>`).join('')}</div>
    </section>

    <section class="a-sec" id="calc">
      <h2>Рассчитайте платёж</h2>
      <div class="a-calc">
        <div class="a-calc__form">
          <div class="chips">${PRODUCTS.map((x) => `<button class="chip ${x.id === p.id ? 'on' : ''}" data-calc-product="${x.id}">${esc(x.name)}</button>`).join('')}</div>
          <div id="calc-body"></div>
        </div>
        <div class="a-calc__res">
          <span class="muted">Ежемесячный платёж</span>
          <div class="a-calc__pay num" id="calc-pay">…</div>
          <div class="kv-list">
            <div><span>Ставка</span><b id="calc-rate"></b></div>
            <div><span>Переплата</span><b id="calc-over"></b></div>
            <div><span>Всего вернуть</span><b id="calc-total"></b></div>
          </div>
          <button class="btn btn--red btn--block btn--lg" data-act="calc-apply">Оформить онлайн</button>
          <small class="muted">Расчёт предварительный. Параметры перенесутся в анкету — заново вводить не нужно.</small>
        </div>
      </div>
    </section>

    <section class="a-sec">
      <h2>Почему это удобно</h2>
      <div class="a-benefits">${BENEFITS.map(([ic, t, d]) => `<div class="a-benefit"><span class="a-benefit__ic">${icon(ic, 26)}</span><h3>${t}</h3><p>${d}</p></div>`).join('')}</div>
    </section>

    <section class="a-sec" id="how">
      <h2>Как получить кредит</h2>
      <ol class="a-steps">${STEPS.map(([t, d], i) => `<li><span class="a-step__n">${i + 1}</span><h3>${t}</h3><p>${d}</p></li>`).join('')}</ol>
    </section>

    <section class="a-sec a-two">
      <div class="a-box"><h3>Требования</h3><ul class="a-checks">
        <li>ИП или ООО, зарегистрированные в России</li><li>Компания действует по данным ЕГРЮЛ/ЕГРИП</li>
        <li>Нет текущих просрочек по кредитам</li><li>Платежи по всем кредитам — не больше 50% месячной выручки</li></ul></div>
      <div class="a-box"><h3>Документы</h3><p class="a-box__big">Не нужны</p>
        <p class="muted">Данные компании берём из ЕГРЮЛ, выручку — из данных налоговой. Договор подписывается простой электронной подписью в кабинете.</p></div>
    </section>

    <section class="a-sec" id="faq">
      <h2>Частые вопросы</h2>
      <div class="a-faq">${FAQ.map(([q, a], i) => acc({ key: 'faq-' + i, header: q, body: `<p>${a}</p>` })).join('')}</div>
    </section>`;

  const body = $('#calc-body');
  const quoter = makeQuoter((q) => {
    $('#calc-pay').textContent = rub(q.monthly_payment);
    $('#calc-rate').textContent = 'от ' + rateStr(q.rate);
    $('#calc-over').textContent = rub(q.overpayment);
    $('#calc-total').textContent = rub(q.total);
    $('#calc-pay').classList.remove('stale');
  }, () => { $('#calc-pay').textContent = '—'; });
  const upd = () => { const el = $('#calc-pay'); if (!el) return; el.classList.add('stale'); quoter(p.id, amount, term); store.set('calc', { product: p.id, amount, term }); };
  const paintCalc = () => { body.innerHTML = calcBlock(p, amount, term); bindCalc(body, p, (v) => { amount = v.amount; term = v.term; upd(); }); upd(); };
  const pick = (id) => {
    p = product(id); amount = p.default_amount; term = p.default_term;
    $$('[data-calc-product]').forEach((c) => c.classList.toggle('on', c.dataset.calcProduct === id));
    paintCalc();
  };
  paintCalc();

  view.addEventListener('click', (e) => {
    const chip = e.target.closest('[data-calc-product]');
    if (chip) pick(chip.dataset.calcProduct);
    const cp = e.target.closest('[data-calc-pick]');
    if (cp) { pick(cp.dataset.calcPick); $('#calc').scrollIntoView({ behavior: 'smooth', block: 'start' }); }
    const sc = e.target.closest('[data-scroll]');
    if (sc) { e.preventDefault(); $('#' + sc.dataset.scroll)?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }
    if (e.target.closest('[data-act=calc-apply]')) startApply(p.id, amount, term);
  });
  let want = null;
  try { want = sessionStorage.getItem('scrollTo'); sessionStorage.removeItem('scrollTo'); } catch { /* недоступно */ }
  if (want) setTimeout(() => $('#' + want)?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 60);
}

function activeBanner(a) {
  const idx = STAGE_OF[a.status];
  const link = a.status === 'draft' || a.status === 'need_info' ? '#/apply/' + a.id : '#/app/' + a.id;
  const cta = a.status === 'draft' ? 'Продолжить анкету' : a.status === 'need_info' ? 'Уточнить данные' : ['approved', 'counter_offer', 'signing'].includes(a.status) ? 'Посмотреть решение' : 'Следить за заявкой';
  return `
    <a class="card active-app" href="${link}">
      <div>
        <div class="row" style="flex-wrap:wrap"><h3>Заявка ${esc(a.number)} · ${esc(product(a.product_id)?.name)}</h3>${pill(a.status)}</div>
        <div class="muted small" style="margin-top:4px">${rub(a.amount)} на ${months(a.term_months)} · этап ${idx + 1} из 6: ${STAGES[idx]}</div>
      </div>
      <span class="btn btn--red btn--sm">${cta} →</span>
      <div class="mini-steps">${STAGES.map((_, i) => `<i class="${i < idx ? 'done' : i === idx ? 'active' : ''}"></i>`).join('')}</div>
    </a>`;
}

// ---------- APPLY (анкета) ----------
async function viewApply(view, id) {
  let a = await api('GET', '/applications/' + id);
  if (!['draft', 'need_info'].includes(a.status)) return go('#/app/' + id);
  let p = product(a.product_id);
  const saveQueue = {};
  let saving = 0;
  let saveErr = null;      // текст последней ошибки сохранения (null — всё сохранено)
  let retryT = 0;
  const alive = () => document.body.contains(view);

  const checklist = () => {
    return [
      ['Сумма и срок', true],
      ['Компания из профиля', !!a.company],
      ['Выручка за год', a.financials.annual_revenue > 0],
      ['ФИО контактного лица', (a.contact.full_name || '').trim().length >= 5],
      ['Телефон', digits(a.contact.phone).length >= 10],
      ['Согласие на запрос в БКИ', a.consent],
    ];
  };

  view.innerHTML = `
    <div class="form-head">
      <a class="back" href="#/" aria-label="Назад">${icon('back', 20)}</a>
      <div style="flex:1">
        <h1 style="font-size:26px">Анкета · ${esc(p.name)}</h1>
        <div class="muted small">Заявка ${esc(a.number)} · всё сохраняется автоматически, можно вернуться позже</div>
      </div>
    </div>
    ${a.status === 'need_info' ? `<div class="banner banner--amber"><span style="font-size:22px">⚠️</span><div><b>Банку нужно уточнение</b>${(a.decision?.reasons || []).map(esc).join('<br>')}</div></div>` : ''}
    <div class="form-layout">
      <div class="grid">
        <section class="card fsec" data-sec="0">
          <div class="fsec__head"><span class="fsec__n">1</span><h3>Сумма и срок</h3></div>
          <div class="fsec__body">
            <div class="chips">${PRODUCTS.map((x) => `<button class="chip ${x.id === p.id ? 'on' : ''}" data-prod="${x.id}">${esc(x.name)}</button>`).join('')}</div>
            <div id="apply-calc"></div>
            <div class="calc__field"><div class="calc__label"><span>Цель</span></div><div class="chips" id="purposes"></div></div>
          </div>
        </section>

        <section class="card fsec" data-sec="1">
          <div class="fsec__head"><span class="fsec__n">2</span><div><h3>Компания</h3><div class="muted small">Взяли из профиля и сверили с ЕГРЮЛ/ЕГРИП — вводить ничего не нужно</div></div></div>
          <div class="fsec__body">
            <div id="company"></div>
            <div class="req-line"><span>${icon('card', 18)} Деньги поступят на р/с ${esc(ME.requisites.account)}, ${esc(ME.requisites.bank_name)}</span><a href="#/profile">Реквизиты</a></div>
          </div>
        </section>

        <section class="card fsec" data-sec="2">
          <div class="fsec__head"><span class="fsec__n">3</span><div><h3>Финансы</h3><div class="muted small">Три цифры — без выписок и справок</div></div></div>
          <div class="fsec__body"><div class="fields">
            <div class="field" data-field="financials.annual_revenue"><label for="rev">Выручка за последний год, ₽</label><input id="rev" inputmode="numeric" data-money="annual_revenue" value="${a.financials.annual_revenue ? fmt(a.financials.annual_revenue) : ''}" placeholder="например, 24 000 000"><span class="hint" id="rev-hint"></span></div>
            <div class="field"><label for="obl">Платежи по другим кредитам в месяц, ₽</label><input id="obl" inputmode="numeric" data-money="monthly_obligations" value="${a.financials.monthly_obligations ? fmt(a.financials.monthly_obligations) : ''}" placeholder="0, если нет"></div>
            <div class="field"><label for="emp">Сотрудников</label><input id="emp" inputmode="numeric" data-money="employees" value="${a.financials.employees || ''}" placeholder="например, 12"></div>
          </div></div>
        </section>

        <section class="card fsec" data-sec="3">
          <div class="fsec__head"><span class="fsec__n">4</span><h3>Контактное лицо и согласие</h3></div>
          <div class="fsec__body"><div class="fields">
            <div class="field field--wide" data-field="contact.full_name"><label for="fio">ФИО</label><input id="fio" data-contact="full_name" value="${esc(a.contact.full_name)}" autocomplete="name"></div>
            <div class="field"><label for="pos">Должность</label><input id="pos" data-contact="position" value="${esc(a.contact.position)}" list="positions"></div>
            <datalist id="positions"><option>Генеральный директор</option><option>Собственник бизнеса</option><option>Финансовый директор</option><option>Главный бухгалтер</option><option>Индивидуальный предприниматель</option></datalist>
            <div class="field" data-field="contact.phone"><label for="tel">Телефон</label><input id="tel" data-contact="phone" inputmode="tel" value="${esc(phoneFmt(a.contact.phone))}" placeholder="+7 900 000-00-00" autocomplete="tel"></div>
            <div class="field field--wide"><label for="mail">Эл. почта <span class="muted">(необязательно)</span></label><input id="mail" data-contact="email" type="email" value="${esc(a.contact.email)}" autocomplete="email"></div>
            <label class="check-box field--wide" data-field="consent"><input type="checkbox" id="consent" ${a.consent ? 'checked' : ''}>
              <span class="small">Согласен на обработку персональных данных и запрос кредитной истории компании в бюро кредитных историй</span></label>
          </div></div>
        </section>
      </div>

      <aside class="card summary">
        <div class="muted small">Ежемесячный платёж</div>
        <div class="big num" id="s-pay"></div>
        <div class="kv-list">
          <div><span>Сумма</span><b id="s-amount"></b></div>
          <div><span>Срок</span><b id="s-term"></b></div>
          <div><span>Ставка</span><b>от ${String(p.rate).replace('.', ',')}%</b></div>
          <div><span>Переплата</span><b id="s-over"></b></div>
        </div>
        <div class="progress"><i id="s-progress"></i></div>
        <div class="fill-meta"><span id="s-pct"></span><span class="saved" id="s-saved" aria-live="polite">Сохранено</span></div>
        <ul class="todo" id="s-todo"></ul>
        <button class="btn btn--red btn--block" id="submit">Отправить на решение</button>
        <p class="muted small" style="text-align:center;margin:10px 0 0">Решение — примерно через 15 секунд, прямо здесь</p>
      </aside>
    </div>`;

  // --- calculator in form ---
  const paintPurposes = () => {
    $('#purposes').innerHTML = p.purposes.map((x) => `<button class="chip ${x === a.purpose ? 'on' : ''}" data-purpose="${esc(x)}">${esc(x)}</button>`).join('');
  };
  // живой расчёт платежа с сервера (единый источник)
  const quoter = makeQuoter((q) => { a.quote = q; paintSummary(); }, () => { a.quote = null; paintSummary(); });
  const paintCalc = () => {
    $('#apply-calc').innerHTML = calcBlock(p, a.amount, a.term_months);
    bindCalc($('#apply-calc'), p, (v) => { a.amount = v.amount; a.term_months = v.term; $('#s-pay').classList.add('stale'); quoter(p.id, a.amount, a.term_months); queue({ amount: v.amount, term_months: v.term }); refresh(); });
    paintPurposes();
  };
  paintCalc();

  // --- autosave ---
  // Индикатор отражает реальное состояние: «Сохраняем…», «Сохранено» или «Не сохранено» с повтором.
  const pending = () => Object.keys(saveQueue).length > 0;
  const indicator = () => {
    const el = $('#s-saved');
    if (!el) return;
    const state = saving > 0 ? 'saving' : saveErr ? 'err' : pending() ? 'saving' : 'ok';
    el.className = 'saved ' + state;
    el.innerHTML = state === 'saving' ? 'Сохраняем…'
      : state === 'err' ? `Не сохранено: ${esc(saveErr)} · <button type="button" data-act="retry-save">Повторить</button>`
      : 'Сохранено';
    if (island.ctx) { island.ctx.saving = state === 'saving'; island.ctx.error = state === 'err'; paintIsland(); }
  };
  async function doFlush() {
    clearTimeout(retryT);
    if (!pending() || saving) return;
    const patch = { ...saveQueue }; Object.keys(saveQueue).forEach((k) => delete saveQueue[k]);
    saving++; indicator();
    try {
      const fresh = await api('PATCH', '/applications/' + id, patch);
      saveErr = null;
      if ('inn' in patch) { a.company = fresh.company; paintCompany(); }
      // расчёт с сервера принимаем, только если параметры не успели измениться
      if (fresh.quote && fresh.amount === a.amount && fresh.term_months === a.term_months && fresh.product_id === a.product_id) { a.quote = fresh.quote; }
    } catch (e) {
      // возвращаем несохранённые поля в очередь (не затирая более свежие правки) и пробуем снова
      for (const [k, v] of Object.entries(patch)) if (!(k in saveQueue)) saveQueue[k] = v;
      saveErr = e.status === 401 ? 'сессия истекла, войдите снова' : e.message;
      if (e.status !== 401 && e.status !== 422 && alive()) retryT = setTimeout(() => alive() && doFlush(), 4000);
    } finally {
      saving--;
      if (alive()) { indicator(); refresh(); if (pending() && !saveErr) doFlush(); }
    }
  }
  const flush = debounce(doFlush, 500);
  const queue = (patch) => { Object.assign(saveQueue, patch); indicator(); flush(); };

  // --- company ---
  const paintCompany = () => {
    const box = $('#company');
    const c = a.company;
    if (!c) { box.innerHTML = ''; $('#rev-hint').innerHTML = ''; return; }
    box.innerHTML = `
      <dl class="company ${c.status !== 'active' ? 'company--bad' : ''}">
        <div class="company__name"><span>${esc(c.name)} <span class="muted small">ИНН ${esc(c.inn)}</span></span>${c.status === 'active' ? '<span class="pill pill--green">Действует</span>' : '<span class="pill pill--red">Ликвидирована</span>'}</div>
        <div><dt>ОГРН</dt><dd>${esc(c.ogrn)}</dd></div>
        <div><dt>Дата регистрации</dt><dd>${new Date(c.reg_date).toLocaleDateString('ru-RU')}</dd></div>
        <div><dt>ОКВЭД</dt><dd>${esc(c.okved)} — ${esc(c.okved_name)}</dd></div>
        <div><dt>Руководитель</dt><dd>${esc(c.director)}</dd></div>
        <div style="grid-column:1/-1"><dt>Адрес</dt><dd>${esc(c.address)}</dd></div>
        <div style="grid-column:1/-1" class="muted small">Источник: ${esc(c.source)}</div>
      </dl>`;
    if (c.revenue_last_year) $('#rev-hint').innerHTML = `По данным ФНС за прошлый год: ${rub(c.revenue_last_year)} · <button type="button" data-act="use-fns">подставить</button>`;
    // предзаполняем контакт руководителем — меньше ввода
    if (!a.contact.full_name && c.director) {
      a.contact.full_name = c.director; $('#fio').value = c.director;
      a.contact.position = c.type === 'ИП' ? 'Индивидуальный предприниматель' : 'Генеральный директор'; $('#pos').value = a.contact.position;
      queue({ contact: { ...a.contact } });
    }
  };
  paintCompany();


  // --- summary / progress ---
  const paintSummary = () => {
    if (!$('#s-pay')) return; // экран анкеты уже закрыт
    const q = a.quote;
    $('#s-pay').textContent = q ? rub(q.monthly_payment) : '—';
    $('#s-pay').classList.toggle('stale', !q || q.amount !== a.amount || q.term_months !== a.term_months);
    $('#s-amount').textContent = rub(a.amount);
    $('#s-term').textContent = months(a.term_months);
    $('#s-over').textContent = q ? rub(q.overpayment) : '—';
  };
  const refresh = () => {
    paintSummary();
    const list = checklist();
    const done = list.filter((x) => x[1]).length;
    const pct = Math.round((done / list.length) * 100);
    const left = list.length - done;
    $('#s-progress').style.width = pct + '%';
    $('#s-pct').textContent = `Заполнено ${pct}%` + (left ? ` · осталось ~${left <= 2 ? 'меньше минуты' : Math.ceil(left * 20 / 60) + ' мин'}` : ' · можно отправлять');
    $('#s-todo').innerHTML = list.map(([t, ok]) => `<li class="${ok ? 'ok' : ''}">${t}</li>`).join('');
    $('#submit').disabled = left > 0;
    const secDone = [true, list[1][1], list[2][1], list[3][1] && list[4][1] && list[5][1]];
    $$('.fsec').forEach((s, i) => s.classList.toggle('done', secDone[i]));
    setIslandCtx({ type: 'apply', pct, saving: saving > 0 || pending(), error: !!saveErr, number: a.number, id });
  };
  refresh();

  // --- events ---
  view.addEventListener('click', (e) => {
    const t = e.target;
    const prod = t.closest('[data-prod]');
    if (prod) {
      p = product(prod.dataset.prod);
      a.product_id = p.id;
      a.amount = Math.min(p.max_amount, Math.max(p.min_amount, a.amount));
      a.term_months = Math.min(p.max_term, Math.max(p.min_term, a.term_months));
      a.purpose = p.purposes[0];
      $$('[data-prod]').forEach((c) => c.classList.toggle('on', c === prod));
      $('h1').textContent = 'Анкета · ' + p.name;
      $('.kv-list div:nth-child(3) b').textContent = 'от ' + String(p.rate).replace('.', ',') + '%';
      queue({ product_id: p.id, amount: a.amount, term_months: a.term_months, purpose: a.purpose });
      paintCalc(); quoter(p.id, a.amount, a.term_months); refresh();
    }
    if (t.closest('[data-act=retry-save]')) { saveErr = null; indicator(); doFlush(); return; }
    const pur = t.closest('[data-purpose]');
    if (pur) { a.purpose = pur.dataset.purpose; queue({ purpose: a.purpose }); paintPurposes(); }
    if (t.closest('[data-act=use-fns]') && a.company) {
      a.financials.annual_revenue = a.company.revenue_last_year;
      $('#rev').value = fmt(a.financials.annual_revenue);
      queue({ financials: { ...a.financials } }); refresh();
    }
  });
  $$('[data-money]').forEach((inp) => inp.addEventListener('input', () => {
    const v = +digits(inp.value);
    inp.value = v ? fmt(v) : '';
    a.financials[inp.dataset.money] = v;
    queue({ financials: { ...a.financials } }); refresh();
  }));
  $$('[data-contact]').forEach((inp) => inp.addEventListener('input', () => {
    a.contact[inp.dataset.contact] = inp.value;
    queue({ contact: { ...a.contact } }); refresh();
  }));
  $('#consent').addEventListener('change', (e) => { a.consent = e.target.checked; queue({ consent: a.consent }); refresh(); });

  $('#submit').addEventListener('click', async () => {
    const btn = $('#submit');
    btn.disabled = true; btn.textContent = 'Отправляем…';
    try {
      // сначала дожидаемся сохранения — нельзя отправлять анкету, если правки не дошли до сервера
      while (saving) await new Promise((r) => setTimeout(r, 100));
      await doFlush();
      if (saveErr) throw new Error('Анкета не сохранена (' + saveErr + '). Проверьте связь и нажмите «Повторить».');
      await api('PATCH', '/applications/' + id, {
        product_id: a.product_id, amount: a.amount, term_months: a.term_months, purpose: a.purpose,
        financials: a.financials, contact: a.contact, consent: a.consent,
      });
      await api('POST', `/applications/${id}/submit`);
      go('#/app/' + id);
    } catch (e) {
      toast(e.message, true);
      $$('.field.err').forEach((f) => f.classList.remove('err'));
      const f = e.field && $(`[data-field="${e.field}"]`);
      if (f) { f.classList.add('err'); f.scrollIntoView({ behavior: 'smooth', block: 'center' }); }
      btn.disabled = false; btn.textContent = 'Отправить на решение';
    }
  });
}

// ---------- STATUS ----------
async function viewStatus(view, id) {
  const draw = (a) => {
    const p = product(a.product_id);
    const idx = STAGE_OF[a.status];
    const working = IN_PIPELINE.includes(a.status);
    const sent = a.history.find((h) => h.status === 'submitted');
    const decided = a.history.find((h) => ['approved', 'counter_offer', 'rejected', 'need_info'].includes(h.status));
    const secs = sent && decided ? Math.round((new Date(decided.at) - new Date(sent.at)) / 1000) : null;
    view.innerHTML = `
      <div class="form-head">
        <a class="back" href="#/apps" aria-label="К заявкам">${icon('back', 20)}</a>
        <div style="flex:1">
          <div class="row" style="flex-wrap:wrap"><h1 style="font-size:26px">Заявка ${esc(a.number)}</h1>${pill(a.status)}</div>
          <div class="muted small">${esc(p.name)} · ${rub(a.amount)} на ${months(a.term_months)}${a.company ? ' · ' + esc(a.company.name) : ''}</div>
        </div>
      </div>
        ${todoBanner(a)}
      <section class="card">
        <div class="row between"><h3>Прогресс заявки</h3><span class="muted small">${secs !== null ? `Решение принято за ${secs} сек` : ''}</span></div>
        <div class="tracker">${STAGES.map((s, i) => {
          let cls = i < idx ? 'done' : i === idx ? 'active' : '';
          if (a.status === 'issued' && i === idx) cls = 'done';
          if (a.status === 'rejected' && i === idx) cls = 'fail';
          if (working && i === idx) cls += ' spin';
          const mark = cls.startsWith('done') ? '✓' : cls === 'fail' ? '✕' : i + 1;
          return `<div class="tstep ${cls}"><span class="tstep__dot">${mark}</span><span class="tstep__t">${s}</span></div>`;
        }).join('')}</div>
        ${working ? `<div class="now-doing"><span class="spinner"></span>${DOING[a.status]}</div>` : ''}
      </section>
      <div class="status-layout">
        <div class="grid">${decisionCard(a)}
          ${a.checks.length ? `<section class="card"><h3>Проверки банка</h3><ul class="checks" style="margin-top:14px">${a.checks.map((c) => `<li class="${c.result}"><i>${c.result === 'ok' ? '✓' : c.result === 'warn' ? '!' : '✕'}</i><div><b>${esc(c.name)}</b><small>${esc(c.details)}</small></div></li>`).join('')}</ul></section>` : ''}
        </div>
        <aside class="card"><h3>История</h3><ul class="timeline" style="margin-top:14px">
          ${[...a.history].reverse().map((h) => `<li><time>${timeOf(h.at)}</time><b>${STATUS[h.status][0]}</b><div class="muted small">${esc(h.comment)}</div></li>`).join('')}
        </ul></aside>
      </div>`;
    };

  let a = await api('GET', '/applications/' + id);
  if (a.status === 'draft') return go('#/apply/' + id);
  draw(a);
  let last = a.status + a.updated_at;
  later(async () => {
    if (!IN_PIPELINE.includes(a.status)) return;
    try {
      a = await api('GET', '/applications/' + id);
      if (a.status + a.updated_at !== last) { last = a.status + a.updated_at; draw(a); renderHotbar(); }
    } catch { /* сеть */ }
  }, 1200);

  view.addEventListener('click', async (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    try {
      if (act === 'accept') { a = await api('POST', `/applications/${id}/accept`); draw(a); signModal(a, (x) => { a = x; draw(x); }); }
      if (act === 'sign-open') signModal(a, (x) => { a = x; draw(x); });
      if (act === 'cancel' && confirmCancel()) { a = await api('POST', `/applications/${id}/cancel`); draw(a); toast('Заявка отменена'); }
      if (act === 'fix') go('#/apply/' + id);
      if (act === 'pay') await payLoan(a, (x) => { a = x; draw(x); });
      if (act === 'retry') { store.set('calc', { product: a.product_id, amount: Math.round(a.amount / 2 / 10000) * 10000, term: a.term_months }); go('#/products'); }
    } catch (err) { toast(err.message, true); }
  });
}

const confirmCancel = () => true; // без браузерных диалогов: отмена обратима созданием новой заявки за 1 клик

// Что требуется от предпринимателя на каждом статусе — показывается крупно на странице заявки,
// в списке заявок, в хот-баре и в меню.
function todoFor(a) {
  const d = a.decision;
  const T = (need, title, text, cta) => ({ need, title, text, cta });
  switch (a.status) {
    case 'draft': return T(true, 'Заполните и отправьте анкету', 'Всё сохраняется автоматически — можно продолжить в любой момент.', { label: 'Продолжить анкету', href: '#/apply/' + a.id });
    case 'submitted': case 'data_check': case 'scoring':
      return T(false, 'От вас ничего не требуется', 'Банк сам проверяет данные и принимает решение — обычно 10–20 секунд. Страницу можно закрыть: статус сохранится.');
    case 'approved': return T(true, 'Примите условия и подпишите договор', `Банк одобрил ${rub(d.amount)} — проверьте платёж и переплату ниже.`, { label: 'Принять и подписать', act: 'accept' });
    case 'counter_offer': return T(true, 'Сравните условия и решите', 'Банк готов выдать кредит, но на других условиях. Сравнение «было/стало» — ниже.', { label: 'Принять новые условия', act: 'accept' });
    case 'need_info': return T(true, 'Исправьте данные в анкете и отправьте снова', d?.reasons?.[0] || 'Банку нужно уточнение.', { label: 'Исправить анкету', act: 'fix' });
    case 'rejected': return T(false, 'Ничего делать не нужно', 'Заявка закрыта. Ниже — почему и что можно сделать, чтобы получить кредит.', { label: 'Рассчитать меньшую сумму', act: 'retry', ghost: true });
    case 'signing': return T(true, 'Подпишите документы', 'Кредитный договор, график платежей и заявление на зачисление готовы. Подпись — прямо здесь, без SMS.', { label: 'Подписать', act: 'sign-open' });
    case 'issued': return T(true, `Вносите платёж ${rub(a.next_payment)} раз в месяц`, `Оплачено ${(a.payments || []).length} из ${a.term_months}. Платёж списывается с вашего расчётного счёта.`, { label: 'Оплатить сейчас', act: 'pay' });
    case 'closed': return T(false, 'Ничего делать не нужно', 'Кредит полностью погашен.');
    case 'cancelled': return T(false, 'Ничего делать не нужно', 'Заявка отменена. Новую можно оформить за пару минут.', { label: 'Новая заявка', href: '#/' , ghost: true });
  }
  return T(false, '', '');
}

function todoBanner(a) {
  const t = todoFor(a);
  if (!t.title) return '';
  const btn = t.cta ? (t.cta.href ? `<a class="btn ${t.cta.ghost ? 'btn--light' : 'btn--red'}" href="${t.cta.href}">${t.cta.label}</a>` : `<button class="btn ${t.cta.ghost ? 'btn--light' : 'btn--red'}" data-act="${t.cta.act}">${t.cta.label}</button>`) : '';
  return `<section class="todo-banner ${t.need ? 'need' : 'ok'}">
    <span class="todo-banner__ic">${icon(t.need ? 'arrow' : 'check', 22)}</span>
    <div class="todo-banner__txt"><small>${t.need ? 'Что нужно от вас' : 'От вас ничего не нужно'}</small><b>${esc(t.title)}</b><span>${esc(t.text)}</span></div>
    ${btn}</section>`;
}

const rateStr = (r) => String(r).replace('.', ',') + '%';

/** Таблица сравнения: что просил клиент и что предлагает банк. Все цифры — с сервера. */
function compareTable(req, off) {
  const rows = [
    ['Сумма', 'amount', rub, 'more'],
    ['Срок', 'term_months', months, 'neutral'],
    ['Ставка', 'rate', rateStr, 'less'],
    ['Платёж в месяц', 'monthly_payment', rub, 'less'],
    ['Переплата', 'overpayment', rub, 'less'],
    ['Всего вернуть', 'total', rub, 'less'],
  ];
  const delta = (k, f, better) => {
    const dv = off[k] - req[k];
    if (!dv) return '<span class="cmp-same">без изменений</span>';
    const sign = dv > 0 ? '+' : '−';
    const txt = k === 'rate' ? `${sign}${String(Math.abs(dv).toFixed(1)).replace('.', ',')} п.п.` : k === 'term_months' ? `${sign}${Math.abs(dv)} мес.` : `${sign}${rub(Math.abs(dv))}`;
    const good = better === 'neutral' ? null : (better === 'less' ? dv < 0 : dv > 0);
    return `<span class="cmp-d ${good === null ? '' : good ? 'good' : 'bad'}">${txt}</span>`;
  };
  return `<table class="cmp"><thead><tr><th></th><th>Вы просили</th><th>Банк предлагает</th><th>Разница</th></tr></thead><tbody>
    ${rows.map(([t, k, f, b]) => `<tr class="${off[k] !== req[k] ? 'changed' : ''}"><td>${t}</td><td>${f(req[k])}</td><td><b>${f(off[k])}</b></td><td>${delta(k, f, b)}</td></tr>`).join('')}
  </tbody></table>`;
}

function offerGrid(d) {
  return `<div class="offer offer--5">
    <div><span>Сумма</span><b>${rub(d.amount)}</b></div>
    <div><span>Срок</span><b>${months(d.term_months)}</b></div>
    <div><span>Ставка</span><b>${rateStr(d.rate)}</b></div>
    <div><span>Платёж в месяц</span><b>${rub(d.monthly_payment)}</b></div>
    <div><span>Переплата</span><b>${rub(d.overpayment)}</b></div></div>`;
}

const whyList = (d) => (d?.reasons?.length ? `<h3 class="why-h">Почему так</h3><ul class="reasons">${d.reasons.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>` : '');
const adviceList = (d) => (d?.advice?.length ? `<h3 class="why-h">Что можно сделать</h3><ul class="reasons reasons--advice">${d.advice.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>` : '');

function decisionCard(a) {
  const d = a.decision;
  const box = (cls, title, body) => `<section class="card decision decision--${cls}">${title}${body}</section>`;
  switch (a.status) {
    case 'submitted': case 'data_check': case 'scoring':
      return box('blue', '<h2>Рассматриваем заявку</h2>', `<p class="muted">Вы запросили ${rub(a.amount)} на ${months(a.term_months)}${a.quote ? ` — ориентировочный платёж ${rub(a.quote.monthly_payment)} в месяц` : ''}. Статус обновится сам.</p>`);
    case 'approved': {
      const same = d.requested && d.requested.rate === d.rate;
      return box('green', '<h2>Кредит одобрен</h2>', `${same ? offerGrid(d) : compareTable(d.requested, d)}${whyList(d)}${scoreBlock(a)}
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--red" data-act="accept">Принять и подписать</button><button class="btn btn--light" data-act="cancel">Отказаться</button></div>`);
    }
    case 'counter_offer':
      return box('amber', '<h2>Можем выдать на других условиях</h2>', `<p class="muted" style="margin:6px 0 14px">Сравните, что вы просили и что предлагает банк:</p>${d.requested ? compareTable(d.requested, d) : offerGrid(d)}${whyList(d)}${scoreBlock(a)}
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--red" data-act="accept">Принять новые условия</button><button class="btn btn--light" data-act="cancel">Отказаться</button></div>`);
    case 'need_info':
      return box('amber', '<h2>Нужно уточнить данные</h2>', `${whyList(d)}${adviceList(d)}<button class="btn btn--red" style="margin-top:20px" data-act="fix">Исправить анкету</button>`);
    case 'rejected':
      return box('red', '<h2>Сейчас не можем выдать кредит</h2>', `${whyList(d)}${adviceList(d)}${scoreBlock(a)}
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--dark" data-act="retry">Рассчитать меньшую сумму</button></div>`);
    case 'signing':
      return box('blue', '<h2>Документы готовы к подписанию</h2>', `${offerGrid(d)}<button class="btn btn--red" data-act="sign-open">Подписать документы</button>`);
    case 'issued': case 'closed': {
      const paid = (a.payments || []).length, n = a.term_months;
      const rows = (a.schedule || []).slice(Math.max(0, paid - 1), paid + 5).map((r) => `<tr class="${r.n <= paid ? 'paid' : r.n === paid + 1 ? 'next' : ''}"><td>${r.n}-й месяц${r.n <= paid ? ' ✓' : r.n === paid + 1 ? ' · следующий' : ''}</td><td>${rub(r.payment)}</td><td>${rub(r.principal)}</td><td>${rub(r.interest)}</td><td>${rub(r.rest)}</td></tr>`);
      const head = a.status === 'closed' ? '<h2>Кредит полностью погашен</h2>' : '<h2>Деньги на расчётном счёте</h2>';
      return box('green', head, `<p class="muted">Договор подписан, ${rub(a.amount)} зачислены на р/с ${ME ? esc(maskAcc(ME.requisites.account)) : ''} (симуляция).</p>${offerGrid(d)}
        <div class="pay-row"><div><b>Оплачено ${paid} из ${n} платежей</b><div class="progress progress--green" style="margin-top:8px"><i style="width:${(paid / n) * 100}%"></i></div></div>
          ${a.status === 'issued' ? `<button class="btn btn--red" data-act="pay">Оплатить ${rub(a.next_payment)}</button>` : ''}</div>
        <h3>График платежей</h3><table class="schedule"><thead><tr><th>Платёж</th><th>Сумма</th><th>Долг</th><th>Проценты</th><th>Остаток</th></tr></thead><tbody>${rows.join('')}</tbody></table>`);
    }
    case 'cancelled':
      return box('', '<h2>Заявка отменена</h2>', '<p class="muted">Можно оформить новую заявку за пару минут — данные компании подтянутся снова.</p><a class="btn btn--red" href="#/">На главную</a>');
  }
  return '';
}

function signModal(a, draw) {
  const d = a.decision;
  modal(`
    <h2>Подписание документов</h2>
    <p class="muted">Подпишите простой электронной подписью — без визита в офис и без SMS.</p>
    <div class="grid" style="gap:8px;margin:16px 0">
      ${['Кредитный договор', 'График платежей', 'Заявление на зачисление средств'].map((n) => `<div class="doc"><span class="doc__ic">PDF</span><div><b>${n}</b><div class="muted small">${esc(a.number)} · ${rub(d.amount)}, ${months(d.term_months)}, ${String(d.rate).replace('.', ',')}%</div></div></div>`).join('')}
    </div>
    <div class="req-line" style="margin-bottom:12px"><span>${icon('card', 18)} Зачисление: ${esc(ME.company.name)}, р/с ${esc(ME.requisites.account)}, БИК ${esc(ME.requisites.bik)}</span></div>
    <label class="check-box"><input type="checkbox" id="sign-ok"><span class="small">Ознакомился с условиями и подписываю документы простой электронной подписью</span></label>
    <div class="row" style="margin-top:18px"><button class="btn btn--red btn--block" id="sign-btn" disabled>Подписать</button></div>`);
  $('#sign-ok').addEventListener('change', (e) => ($('#sign-btn').disabled = !e.target.checked));
  $('#sign-btn').addEventListener('click', async () => {
    try {
      const fresh = await api('POST', `/applications/${a.id}/sign`, { confirm: true });
      closeModal(); draw(fresh); await refreshMe(); renderHotbar(); toast('Готово! Деньги на счёте');
    } catch (e) { toast(e.message, true); }
  });
}

// ---------- MY APPLICATIONS ----------
async function viewApps(view) {
  const apps = await api('GET', '/applications');
  view.innerHTML = `
    <div class="section-title" style="margin-top:0"><h1>Мои заявки</h1><button class="btn btn--red btn--sm" data-act="start" data-product="business-dev">+ Новая заявка</button></div>
    ${apps.length ? `<div class="app-list">${apps.map((a) => `
      <a class="card app-row" href="${['draft', 'need_info'].includes(a.status) ? '#/apply/' : '#/app/'}${a.id}">
        <b>${esc(a.number)}</b>
        <div><b>${esc(product(a.product_id)?.name)}</b><div class="muted small">${a.company ? esc(a.company.name) : 'компания не указана'}</div></div>
        <div class="num">${rub(a.amount)}<div class="muted small">${months(a.term_months)}</div></div>
        <div class="small ${todoFor(a).need ? 'todo-need' : 'muted'}">${todoFor(a).need ? '● ' + esc(todoFor(a).title) : esc(todoFor(a).title)}</div>
        ${pill(a.status)}
      </a>`).join('')}</div>`
    : `<div class="card empty"><h2>Заявок пока нет</h2><p>Рассчитайте кредит и сразу заполните анкету — это займёт пару минут.</p><button class="btn btn--red" data-act="start" data-product="business-dev">Оформить кредит</button></div>`}`;
}

// ---------- BANK ----------
async function viewBank(view) {
  const token = store.get('bank_token', 'demo-bank');
  const draw = (rows) => {
    const by = (s) => rows.filter((r) => s.includes(r.status)).length;
    const scored = rows.filter((r) => r.score != null);
    const avg = scored.length ? Math.round(scored.reduce((s, r) => s + r.score, 0) / scored.length) : '—';
    const times = rows.map((r) => { const s = r.history.find((h) => h.status === 'submitted'); const d = r.history.find((h) => ['approved', 'counter_offer', 'rejected', 'need_info'].includes(h.status)); return s && d ? (new Date(d.at) - new Date(s.at)) / 1000 : null; }).filter((x) => x != null);
    const avgT = times.length ? Math.round(times.reduce((a, b) => a + b, 0) / times.length) + ' с' : '—';
    $('#bank-body').innerHTML = `
      <div class="kpis">
        <div class="card kpi"><span>Всего заявок</span><b>${rows.length}</b></div>
        <div class="card kpi"><span>В конвейере</span><b>${by(IN_PIPELINE)}</b></div>
        <div class="card kpi"><span>Одобрено / выдано</span><b>${by(['approved', 'counter_offer', 'signing', 'issued'])}</b></div>
        <div class="card kpi"><span>Отказ</span><b>${by(['rejected'])}</b></div>
        <div class="card kpi"><span>Средний балл</span><b>${avg}</b></div>
        <div class="card kpi"><span>Время до решения</span><b>${avgT}</b></div>
      </div>
      <div class="card table-wrap" style="padding:0">${rows.length ? `<table class="bank"><thead><tr><th>№</th><th>Компания</th><th>Продукт</th><th>Запрошено</th><th>Статус</th><th>Балл</th><th>Решение</th><th>Проверки</th><th>Обновлено</th></tr></thead><tbody>
        ${rows.map((r) => `<tr>
          <td><b>${esc(r.number)}</b></td>
          <td>${r.company ? esc(r.company.name) + `<div class="muted small">ИНН ${esc(r.inn)}</div>` : '<span class="muted">—</span>'}</td>
          <td>${esc(product(r.product_id)?.name)}</td>
          <td class="num">${rub(r.amount)}<div class="muted small">${months(r.term_months)}</div></td>
          <td>${pill(r.status)}</td>
          <td class="score">${r.score ?? '—'}</td>
          <td class="small">${r.decision ? (r.decision.amount ? `${rub(r.decision.amount)} · ${r.decision.rate}%` : '') + `<div class="muted">${esc((r.decision.reasons || [])[0] || '')}</div>` : '—'}</td>
          <td>${r.checks.map((c) => `<span title="${esc(c.name + ': ' + c.details)}">${c.result === 'ok' ? '🟢' : c.result === 'warn' ? '🟠' : '🔴'}</span>`).join(' ')}</td>
          <td class="muted small">${dateOf(r.updated_at)}</td></tr>`).join('')}
      </tbody></table>` : '<div class="empty">Заявок пока нет — оформите кредит на главной</div>'}</div>`;
  };
  view.innerHTML = `
    <div class="section-title" style="margin-top:0"><div><h1>Кабинет сотрудника банка</h1><div class="muted small">Все заявки конвейера в реальном времени · обновление каждые 2 сек</div></div>
      <span class="pill pill--green">Live</span></div>
    <div id="bank-plat" class="plat" aria-label="Состояние платформы"></div>
    <div id="bank-body"><div class="skeleton"></div></div>`;
  const CACHE_NAMES = { registry: 'ЕГРЮЛ/ФНС', bureau: 'БКИ', calc: 'калькулятор', sessions: 'сессии' };
  const plat = (p) => {
    const el = $('#bank-plat');
    if (!el) return;
    const chip = (ok, label, value) => `<span class="plat__chip ${ok ? 'ok' : 'warn'}"><i></i>${label}${value != null ? `<b>${value}</b>` : ''}</span>`;
    const caches = (p.caches || []).filter((c) => CACHE_NAMES[c.name] && c.hits + c.misses >= 20)
      .map((c) => chip(true, 'Кэш ' + CACHE_NAMES[c.name], Math.round(c.hit_rate * 100) + '%')).join('');
    el.innerHTML = chip(true, 'Хранилище', p.storage === 'postgres' ? 'PostgreSQL' : 'файлы')
      + chip(true, 'Шина событий', p.bus === 'kafka' ? 'Kafka' : 'in-memory')
      + (p.outbox_pending != null ? chip(p.outbox_pending < 50, 'Outbox в очереди', p.outbox_pending) : '')
      + chip(true, 'Событий отправлено', p.events_published) + caches;
  };
  const load = async () => {
    const [rows, p] = await Promise.all([
      api('GET', '/bank/applications?limit=200', null, { 'X-Bank-Token': token }),
      api('GET', '/bank/platform', null, { 'X-Bank-Token': token }).catch(() => null),
    ]);
    draw(rows);
    if (p) plat(p);
  };
  await load();
  later(() => load().catch(() => {}), 2000);
}

// ---------- AUTH ----------
let pendingAfterLogin = null;

function requireAuth(fn, reason) {
  if (ME) return fn();
  pendingAfterLogin = fn;
  authModal(reason);
}

async function refreshMe() {
  try { ME = await api('GET', '/me'); } catch { /* 401 обработан в api */ }
  paintAuthSlot(); paintIsland();
}

function authModal(reason, tab = 'login') {
  modal(`
    <div class="auth">
      <div class="auth__brand"><span class="brand__mark">₽</span><div><b>Альфа Бизнес</b><div class="muted small">Вход в кредитный кабинет</div></div></div>
      ${reason ? `<div class="auth__reason">${icon('lock', 18)}<span>${esc(reason)}</span></div>` : ''}
      <div class="seg" role="tablist">
        <button class="${tab === 'login' ? 'on' : ''}" data-auth-tab="login">Вход</button>
        <button class="${tab === 'register' ? 'on' : ''}" data-auth-tab="register">Регистрация</button>
      </div>
      <div id="auth-body"></div>
    </div>`);
  const body = $('#auth-body');
  const paint = (t) => {
    $$('[data-auth-tab]').forEach((b) => b.classList.toggle('on', b.dataset.authTab === t));
    body.innerHTML = t === 'login' ? `
      <form id="auth-form" class="grid" style="gap:12px" novalidate>
        <div class="field" data-field="phone"><label for="a-phone">Телефон</label><input id="a-phone" inputmode="tel" autocomplete="username" placeholder="+7 (900) 000-00-00" required></div>
        <div class="field" data-field="password"><label for="a-pass">Пароль</label><input id="a-pass" type="password" autocomplete="current-password" required></div>
        <div class="auth__err" id="a-err"></div>
        <button class="btn btn--red btn--block" type="submit">Войти</button>
      </form>
      <div class="demo-acc">
        <div class="muted small">Демо-аккаунты для проверки (пароль <code>${DEMO_PASSWORD}</code>)</div>
        ${DEMO_ACCOUNTS.map(([ph, name, hint]) => `<button type="button" class="demo-acc__item" data-demo="${ph}"><b>${esc(name)}</b><span>${phoneFmt(ph)} · ${esc(hint)}</span></button>`).join('')}
      </div>` : `
      <form id="auth-form" class="grid" style="gap:12px" novalidate>
        <div class="field" data-field="inn"><label for="r-inn">ИНН компании или ИП</label><input id="r-inn" inputmode="numeric" maxlength="12" placeholder="10 или 12 цифр" required><span class="hint" id="r-company">Компанию, реквизиты и руководителя подтянем из ЕГРЮЛ</span></div>
        <div class="field" data-field="full_name"><label for="r-name">ФИО представителя</label><input id="r-name" autocomplete="name" required></div>
        <div class="field" data-field="phone"><label for="r-phone">Телефон — это ваш логин</label><input id="r-phone" inputmode="tel" autocomplete="username" placeholder="+7 (900) 000-00-00" required></div>
        <div class="field" data-field="password"><label for="r-pass">Пароль</label><input id="r-pass" type="password" autocomplete="new-password" minlength="8" required><span class="hint">Не короче 8 символов</span></div>
        <div class="auth__err" id="a-err"></div>
        <button class="btn btn--red btn--block" type="submit">Создать кабинет</button>
      </form>`;
    $$('#a-phone, #r-phone').forEach((i) => i.addEventListener('input', () => { i.value = phoneFmt(i.value); }));
    const inn = $('#r-inn');
    if (inn) {
      inn.addEventListener('input', debounce(async () => {
        inn.value = digits(inn.value);
        const h = $('#r-company');
        if (![10, 12].includes(inn.value.length)) { h.textContent = 'Компанию, реквизиты и руководителя подтянем из ЕГРЮЛ'; return; }
        try {
          const c = await api('GET', '/companies/' + inn.value);
          h.innerHTML = `<b style="color:var(--green)">✓ ${esc(c.name)}</b> · ${esc(c.address)}`;
          const nm = $('#r-name'); if (!nm.value) nm.value = c.director;
        } catch (e) { h.innerHTML = `<span style="color:var(--red)">${esc(e.message)}</span>`; }
      }, 300));
    }
    $('#auth-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = e.target.querySelector('button[type=submit]');
      const err = $('#a-err'); err.textContent = '';
      $$('.field.err').forEach((f) => f.classList.remove('err'));
      btn.disabled = true;
      try {
        const user = t === 'login'
          ? await api('POST', '/auth/login', { phone: $('#a-phone').value, password: $('#a-pass').value })
          : await api('POST', '/auth/register', { inn: $('#r-inn').value, full_name: $('#r-name').value, phone: $('#r-phone').value, password: $('#r-pass').value, email: '' });
        onLoggedIn(user, t === 'register');
      } catch (ex) {
        err.textContent = ex.message;
        if (ex.field) $(`#auth-body [data-field="${ex.field}"]`)?.classList.add('err');
        btn.disabled = false;
      }
    });
  };
  paint(tab);
  $('.auth').addEventListener('click', (e) => {
    const tb = e.target.closest('[data-auth-tab]');
    if (tb) paint(tb.dataset.authTab);
    const demo = e.target.closest('[data-demo]');
    if (demo) { $('#a-phone').value = phoneFmt(demo.dataset.demo); $('#a-pass').value = DEMO_PASSWORD; $('#auth-form').requestSubmit(); }
  });
  // Фокус ставим сразу и только если пользователь ещё ничего не выбрал в окне:
  // отложенный focus() перехватывал ввод — пароль, набранный в первые 50 мс, улетал в поле телефона.
  if (!$('.auth')?.contains(document.activeElement)) $('#auth-body input')?.focus();
}

async function onLoggedIn(user, isNew) {
  ME = user;
  closeModal(); paintAuthSlot();
  await pollIsland(true);
  flash({ tone: 'green', title: isNew ? 'Кабинет создан' : `Здравствуйте, ${user.full_name.split(' ')[1] || user.full_name}`, sub: user.company.name });
  const fn = pendingAfterLogin; pendingAfterLogin = null;
  const route = (location.hash || '#/').split('/')[1];
  // после входа — сразу кабинет (или продолжаем действие, ради которого входили)
  if (fn) fn(); else if (PROTECTED.includes(route) || route === 'bank') render(); else go('#/');
}

async function logout() {
  try { await api('POST', '/auth/logout'); } catch { /* всё равно выходим локально */ }
  ME = null; island.apps = []; island.prev = {}; island.open = false;
  paintAuthSlot(); paintIsland();
  go('#/');
  toast('Вы вышли из кабинета');
}

// ---------- DYNAMIC ISLAND ----------
// Верхний «островок»: компактно показывает самое важное сейчас и раскрывается в панель управления.
const island = { ctx: null, apps: [], prev: {}, open: false, flash: null, flashT: 0 };

function setIslandCtx(ctx) { island.ctx = ctx; paintIsland(); }

function flash(f) {
  island.flash = f; island.open = false; paintIsland();
  clearTimeout(island.flashT);
  island.flashT = setTimeout(() => { island.flash = null; paintIsland(); }, 4500);
}

async function pollIsland(silent = false) {
  if (!ME) { paintIsland(); return; }
  try {
    const [apps, me] = await Promise.all([api('GET', '/applications'), api('GET', '/me')]);
    const balanceDelta = ME && me.balance !== ME.balance ? me.balance - ME.balance : 0;
    ME = me;
    for (const a of apps) {
      const was = island.prev[a.id];
      if (!silent && was && was !== a.status) {
        const msg = {
          data_check: ['blue', 'Проверяем данные', 'ЕГРЮЛ и кредитная история'],
          scoring: ['blue', 'Оцениваем риск', 'Решение через пару секунд'],
          approved: ['green', 'Кредит одобрен!', `${rub(a.decision?.amount)} · нажмите, чтобы подписать`],
          counter_offer: ['amber', 'Есть предложение', `${rub(a.decision?.amount)} на ${months(a.decision?.term_months || 0)}`],
          need_info: ['amber', 'Нужно уточнение', 'Проверьте выручку в анкете'],
          rejected: ['red', 'Отказ по заявке', 'Посмотрите причины и альтернативы'],
          issued: ['green', 'Деньги на счёте', `+${rub(a.amount)}`],
          closed: ['green', 'Кредит погашен', a.number],
        }[a.status];
        if (msg) flash({ tone: msg[0], title: msg[1], sub: msg[2], href: '#/app/' + a.id });
      }
      island.prev[a.id] = a.status;
    }
    island.apps = apps;
    if (!silent && balanceDelta && !island.flash) flash({ tone: balanceDelta > 0 ? 'green' : 'blue', title: (balanceDelta > 0 ? '+' : '−') + rub(Math.abs(balanceDelta)), sub: 'Баланс: ' + rub(me.balance) });
    paintIsland(); paintAuthSlot();
    renderHotbarBadge();
  } catch { /* сеть или 401 */ }
}

function islandCompact() {
  const f = island.flash;
  if (f) return { cls: 'is-flash tone-' + f.tone, href: f.href, html: `
    <span class="isl-dot tone-${f.tone}">${icon(f.tone === 'red' ? 'lock' : 'check', 16)}</span>
    <span class="isl-text"><b>${esc(f.title)}</b><small>${esc(f.sub || '')}</small></span>` };
  if (!ME) return { cls: 'is-guest', act: 'login', html: `
    <span class="isl-dot">${icon('lock', 14)}</span><span class="isl-text"><b>Гость</b></span><span class="isl-cta">Войти</span>` };
  const c = island.ctx;
  if (c?.type === 'apply') {
    const r = 9, L = 2 * Math.PI * r;
    return { cls: 'is-apply', html: `
      <svg class="isl-ring" width="22" height="22" viewBox="0 0 22 22"><circle cx="11" cy="11" r="${r}" stroke="rgba(255,255,255,.2)" stroke-width="3" fill="none"/><circle cx="11" cy="11" r="${r}" stroke="${c.pct === 100 ? '#3DDC84' : '#FF5A4E'}" stroke-width="3" fill="none" stroke-linecap="round" stroke-dasharray="${L}" stroke-dashoffset="${L * (1 - c.pct / 100)}" transform="rotate(-90 11 11)"/></svg>
      <span class="isl-text"><b>Анкета ${c.pct}%</b><small>${c.error ? 'не сохранено!' : c.saving ? 'сохраняем…' : 'сохранено'}</small></span>
      ${c.pct === 100 ? '<span class="isl-cta">Готово к отправке</span>' : ''}` };
  }
  const work = island.apps.find((a) => IN_PIPELINE.includes(a.status));
  if (work) return { cls: 'is-work', href: '#/app/' + work.id, html: `
    <span class="spinner spinner--isl"></span>
    <span class="isl-text"><b>${esc(work.number)} · ${STATUS[work.status][0]}</b><small>этап ${STAGE_OF[work.status] + 1} из 6</small></span>
    <span class="isl-bar"><i style="width:${((STAGE_OF[work.status] + 0.5) / 6) * 100}%"></i></span>` };
  const act = island.apps.find((a) => NEEDS_ACTION[a.status]);
  if (act) {
    const [t, cta, tone] = NEEDS_ACTION[act.status];
    return { cls: 'is-action', href: (act.status === 'need_info' ? '#/apply/' : '#/app/') + act.id, html: `
      <span class="isl-pulse tone-${tone}"></span><span class="isl-text"><b>${t}</b><small>${esc(act.number)}${act.decision?.amount ? ' · ' + rub(act.decision.amount) : ''}</small></span><span class="isl-cta tone-${tone}">${cta}</span>` };
  }
  const draft = island.apps.find((a) => a.status === 'draft');
  return { cls: 'is-idle', html: `
    <span class="isl-ava">${esc(initials(ME.full_name))}</span>
    <span class="isl-text"><b>${esc(shortCompany(ME.company.name))}</b><small>${draft ? 'есть незаконченная анкета' : 'р/с ' + maskAcc(ME.requisites.account)}</small></span>
    <span class="isl-balance">${rub(ME.balance)}</span>` };
}

function islandExpanded() {
  if (!ME) return '';
  const active = island.apps.filter((a) => !FINAL.includes(a.status)).slice(0, 3);
  const loans = island.apps.filter((a) => a.status === 'issued');
  return `
    <div class="isl-x">
      <div class="isl-x__head">
        <span class="isl-ava isl-ava--lg">${esc(initials(ME.full_name))}</span>
        <div style="flex:1;min-width:0"><b>${esc(ME.company.name)}</b><small>${esc(ME.full_name)} · ${esc(ME.position)}</small></div>
      </div>
      <div class="isl-x__bal"><small>Расчётный счёт ${esc(maskAcc(ME.requisites.account))}</small><b>${rub(ME.balance)}</b>
        ${loans.length ? `<small>Кредитов: ${loans.length} · ближайший платёж ${rub(loans[0].next_payment)}</small>` : ''}</div>
      ${active.length ? `<div class="isl-x__list">${active.map((a) => `
        <a class="isl-x__app" href="${['draft', 'need_info'].includes(a.status) ? '#/apply/' : '#/app/'}${a.id}">
          <span><b>${esc(a.number)}</b> · ${rub(a.amount)}</span>${pill(a.status)}
          <span class="mini-steps" style="margin:6px 0 0">${STAGES.map((_, i) => `<i class="${i < STAGE_OF[a.status] ? 'done' : i === STAGE_OF[a.status] ? 'active' : ''}"></i>`).join('')}</span>
        </a>`).join('')}</div>` : '<div class="isl-x__empty">Активных заявок нет</div>'}
      <div class="isl-x__acts">
        <button data-isl="new">${icon('plus', 20)}<span>Кредит</span></button>
        <button data-isl="apps">${icon('list', 20)}<span>Заявки</span></button>
        <button data-isl="profile">${icon('user', 20)}<span>Кабинет</span></button>
        <button data-isl="copy">${icon('copy', 20)}<span>Реквизиты</span></button>
        <button data-isl="logout">${icon('out', 20)}<span>Выйти</span></button>
      </div>
    </div>`;
}

function paintIsland() {
  const el = $('#island');
  // «остров» убран: события показываем карточками-уведомлениями (как в Т-Бизнесе)
  if (!el) { if (island.flash && island.flash !== island.shown) { island.shown = island.flash; showNotice(island.flash); } return; }
  const inner = $('.island__in', el);
  const open = island.open && ME && !island.flash;
  const c = open ? null : islandCompact();
  const key = open ? 'open' : c.cls + c.html;
  if (el.dataset.key === key) return;
  el.dataset.key = key;
  el.className = 'island ' + (open ? 'is-open' : c.cls);
  el.dataset.href = c?.href || '';
  el.dataset.act = c?.act || '';
  inner.innerHTML = open ? islandExpanded() : `<div class="isl-row">${c.html}</div>`;
  // морфинг: меряем новое содержимое и анимируем размеры контейнера
  const w = open ? Math.min(400, window.innerWidth - 24) : Math.min(inner.firstElementChild.scrollWidth + 16, window.innerWidth - 120);
  inner.style.width = w + 'px';
  const h = inner.firstElementChild.scrollHeight + (open ? 0 : 8);
  el.style.width = w + 'px';
  if (!open && el.parentElement.classList.contains('isl-wrap')) el.parentElement.style.width = w + 'px';
  el.style.height = Math.max(40, h) + 'px';
}

async function copyRequisites() {
  const r = ME.requisites, c = ME.company;
  const text = `${c.name}\nИНН ${c.inn}${r.kpp ? ' КПП ' + r.kpp : ''}\nОГРН ${c.ogrn}\nР/с ${r.account}\nБанк ${r.bank_name}\nБИК ${r.bik}\nК/с ${r.corr_account}\nАдрес: ${c.address}`;
  try { await navigator.clipboard.writeText(text); toast('Реквизиты скопированы'); } catch { toast('Не удалось скопировать — выделите вручную в кабинете', true); }
}

document.addEventListener('click', (e) => {
  const el = $('#island');
  if (!el) return;
  const isl = e.target.closest('[data-isl]');
  if (isl) {
    e.preventDefault();
    island.open = false;
    const a = isl.dataset.isl;
    if (a === 'new') startApply('business-dev');
    if (a === 'apps') go('#/apps');
    if (a === 'profile') go('#/profile');
    if (a === 'copy') copyRequisites();
    if (a === 'logout') return logout();
    paintIsland();
    return;
  }
  if (e.target.closest('.isl-x__app')) { island.open = false; paintIsland(); return; }
  if (el.contains(e.target)) {
    if (island.flash) { const href = island.flash.href; island.flash = null; if (href) go(href); paintIsland(); return; }
    if (el.dataset.act === 'login') return authModal();
    if (!island.open && el.dataset.href && !e.target.closest('.isl-ava, .isl-balance')) { go(el.dataset.href); return; }
    island.open = !island.open; paintIsland();
  } else if (island.open) { island.open = false; paintIsland(); }
});
window.addEventListener('resize', debounce(() => { const el = $('#island'); if (el) { el.dataset.key = ''; paintIsland(); } }, 150));

// ---------- PROFILE (личный кабинет) ----------
async function viewProfile(view) {
  await refreshMe();
  const apps = await api('GET', '/applications');
  const u = ME, c = u.company, r = u.requisites;
  const loans = apps.filter((a) => ['issued', 'closed'].includes(a.status));
  const reqRows = [
    ['Получатель', c.name], ['ИНН', c.inn], ...(r.kpp ? [['КПП', r.kpp]] : []), ['ОГРН', c.ogrn],
    ['Расчётный счёт', r.account], ['Банк', r.bank_name], ['БИК', r.bik], ['Корр. счёт', r.corr_account], ['Юр. адрес', c.address],
  ];
  view.innerHTML = `
    <section class="a-profile-head">
      <span class="a-profile-head__ava">${esc(initials(u.full_name))}</span>
      <div class="a-profile-head__txt"><h1>${esc(u.full_name)}</h1><span>${esc(u.position)} · ${esc(c.name)}</span><span>${phoneFmt(u.phone)} · ИНН ${esc(c.inn)}</span></div>
      <button class="btn btn--light" data-act="logout">${icon('out', 18)} Выйти</button>
    </section>
    <div class="profile-grid">
      <section class="card acc-card">
        <div class="row between"><span class="muted-inv small">Расчётный счёт · ${esc(r.bank_name)}</span><span class="acc-chip">${esc(maskAcc(r.account))}</span></div>
        <div class="acc-card__bal">${rub(u.balance)}</div>
        <div class="row" style="flex-wrap:wrap;gap:8px">
          <button class="btn btn--sm acc-btn" data-act="copy-req">${icon('copy', 16)} Скопировать реквизиты</button>
          <button class="btn btn--sm btn--red" data-act="start" data-product="business-dev">${icon('plus', 16)} Новый кредит</button>
        </div>
      </section>

      <section class="card">
        <h3>Контакты и вход</h3>
        <div class="kv-list">
          <div><span>Телефон (логин)</span><b>${phoneFmt(u.phone)}</b></div>
          <div><span>Эл. почта</span><b><input class="inline-input" id="p-email" value="${esc(u.email)}" placeholder="добавить"></b></div>
          <div><span>Последний вход</span><b>${u.last_login_at && !u.last_login_at.startsWith('0001') ? dateOf(u.last_login_at) : 'сейчас'}</b></div>
        </div>
        <div class="sec-note">${icon('lock', 16)} Сессия в защищённой cookie, выход после 12 ч бездействия. Без входа нельзя оформить, подписать или оплатить.</div>
      </section>

      <section class="card">
        <div class="row between"><h3>Компания</h3>${c.status === 'active' ? '<span class="pill pill--green">Действует</span>' : '<span class="pill pill--red">Ликвидирована</span>'}</div>
        <dl class="company" style="background:none;padding:0;margin-top:14px">
          <div class="company__name" style="font-size:15px">${esc(c.name)}</div>
          <div><dt>ОКВЭД</dt><dd>${esc(c.okved)} — ${esc(c.okved_name)}</dd></div>
          <div><dt>Руководитель</dt><dd>${esc(c.director)}</dd></div>
          <div><dt>Регистрация</dt><dd>${new Date(c.reg_date).toLocaleDateString('ru-RU')}</dd></div>
          <div><dt>Выручка (ФНС)</dt><dd>${rub(c.revenue_last_year)}</dd></div>
          <div style="grid-column:1/-1" class="muted small">Источник: ${esc(c.source)}</div>
        </dl>
      </section>

      <section class="card">
        <div class="row between"><h3>Банковские реквизиты</h3><button class="btn btn--light btn--sm" data-act="copy-req">${icon('copy', 16)} Все</button></div>
        <table class="req-table">${reqRows.map(([k, v]) => `<tr><td>${k}</td><td>${esc(v)}</td><td><button class="copy-btn" data-copy="${esc(v)}" aria-label="Скопировать ${k}">${icon('copy', 16)}</button></td></tr>`).join('')}</table>
      </section>

      <section class="card profile-grid__wide">
        <div class="row between"><h3>Мои кредиты</h3><a class="muted small" href="#/apps">Все заявки →</a></div>
        ${loans.length ? `<div class="grid" style="margin-top:14px">${loans.map((a) => {
          const pay = a.next_payment, paid = (a.payments || []).length;
          return `<div class="loan">
            <div><b>${esc(product(a.product_id)?.name)}</b><div class="muted small">${esc(a.number)} · ${rub(a.amount)} · ${String(a.decision.rate).replace('.', ',')}% · ${months(a.term_months)}</div></div>
            <div class="loan__prog"><div class="small">Оплачено ${paid} из ${a.term_months}</div><div class="progress progress--green"><i style="width:${(paid / a.term_months) * 100}%"></i></div></div>
            ${a.status === 'issued' ? `<button class="btn btn--red btn--sm" data-pay="${a.id}">Оплатить ${rub(pay)}</button>` : pill(a.status)}
          </div>`;
        }).join('')}</div>` : '<div class="empty" style="padding:24px">Выданных кредитов пока нет</div>'}
      </section>
    </div>`;

  $('#p-email').addEventListener('change', async (e) => {
    try { ME = await api('PATCH', '/me', { email: e.target.value }); toast('Почта сохранена'); } catch (ex) { toast(ex.message, true); }
  });
  view.addEventListener('click', async (e) => {
    if (e.target.closest('[data-act=copy-req]')) copyRequisites();
    const cp = e.target.closest('[data-copy]');
    if (cp) { try { await navigator.clipboard.writeText(cp.dataset.copy); toast('Скопировано'); } catch { toast('Не удалось скопировать', true); } }
    const pb = e.target.closest('[data-pay]');
    if (pb) { const a = apps.find((x) => x.id === pb.dataset.pay); await payLoan(a, () => render()); }
  });
}

async function payLoan(a, after) {
  return requireAuth(async () => {
    const pay = a.next_payment;
    if (!pay) return toast('По этому кредиту платежей больше нет');
    modal(`
      <h2>Оплата платежа</h2>
      <p class="muted">${esc(a.number)} · платёж ${(a.payments || []).length + 1} из ${a.term_months}</p>
      <div class="kv-list">
        <div><span>Сумма</span><b>${rub(pay)}</b></div>
        <div><span>Списание со счёта</span><b>${esc(ME.requisites.account)}</b></div>
        <div><span>Доступно</span><b>${rub(ME.balance)}</b></div>
        <div><span>Останется</span><b style="color:${ME.balance - pay < 0 ? 'var(--red)' : 'inherit'}">${rub(ME.balance - pay)}</b></div>
      </div>
      <div class="auth__err" id="pay-err"></div>
      <button class="btn btn--red btn--block" id="pay-ok" ${ME.balance < pay ? 'disabled' : ''}>${ME.balance < pay ? 'Недостаточно средств' : 'Оплатить ' + rub(pay)}</button>`);
    $('#pay-ok').addEventListener('click', async () => {
      try {
        const x = await api('POST', `/applications/${a.id}/pay`);
        closeModal(); await pollIsland(); after(x); toast('Платёж проведён');
      } catch (e) { $('#pay-err').textContent = e.message; }
    });
  }, 'Чтобы оплатить платёж, войдите в кабинет компании.');
}

// ---------- modal ----------
function modal(html) {
  $('#modal-root').innerHTML = `<div class="modal-bg" data-close><div class="modal" role="dialog" aria-modal="true">${html}</div></div>`;
}
function closeModal() { $('#modal-root').innerHTML = ''; }

// ---------- global events ----------
document.addEventListener('click', (e) => {
  const start = e.target.closest('[data-act=start]');
  if (start) startApply(start.dataset.product);
  if (e.target.matches('[data-close]')) closeModal();
  if (e.target.closest('[data-act=login]')) authModal();
  if (e.target.closest('[data-act=logout]')) logout();
  if (e.target.closest('[data-act=hot-save]')) {
    const on = $$('[data-hot-toggle]').filter((i) => i.checked).map((i) => i.dataset.hotToggle);
    store.set(HOT_KEY, on); closeModal(); renderHotbar(); toast('Панель обновлена');
  }
});
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeModal(); });
window.addEventListener('hashchange', render);
// Старт — только когда выполнены все скрипты (nav.js, cabinet.js, score.js):
// иначе быстрый ответ /me приходил раньше, чем загружался nav.js, и страница оставалась пустой
// («paintAuthSlot is not defined»).
async function boot() {
  try { ME = await api('GET', '/me'); } catch { ME = null; }
  paintAuthSlot();
  await pollIsland(true);
  setInterval(() => pollIsland(), 2500);
  render();
}
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, { once: true }); else boot();
