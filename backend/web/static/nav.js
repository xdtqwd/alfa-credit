'use strict';
/* Навигация: мега-меню в шапке, поиск (⌘K), уведомления, меню профиля и динамичный хот-бар
   с выбором услуги при наведении. Использует общие хелперы и состояние из app.js. */

Object.assign(I, {
  search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>',
  bell: '<path d="M6 16V11a6 6 0 0112 0v5l2 2H4z"/><path d="M10 20a2 2 0 004 0"/>',
  chev: '<path d="M6 9l6 6 6-6"/>',
  wallet: '<rect x="3" y="6" width="18" height="14" rx="3"/><path d="M3 10h18M16 15h2"/><path d="M6 6l9-3 1 3"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 015 .5c0 1.5-2.5 2-2.5 3.5M12 17h.01"/>',
  menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  arrow: '<path d="M5 12h14M13 6l6 6-6 6"/>',
  box: '<path d="M3 7l9-4 9 4-9 4z"/><path d="M3 7v10l9 4 9-4V7"/>',
  gearSm: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M21 12h-3M6 12H3"/>',
});

const canHover = window.matchMedia('(hover: hover) and (pointer: fine)').matches;
const appHref = (a) => (['draft', 'need_info'].includes(a.status) ? '#/apply/' : '#/app/') + a.id;
const rate = (r) => String(r).replace('.', ',') + '%';
const activeApps = () => (ME ? island.apps.filter((a) => !FINAL.includes(a.status)) : []);
const loansIssued = () => (ME ? island.apps.filter((a) => a.status === 'issued') : []);
const nextPay = (a) => a.next_payment; // считает сервер
const miniSteps = (a) => `<span class="mini-steps">${STAGES.map((_, i) => `<i class="${i < STAGE_OF[a.status] ? 'done' : i === STAGE_OF[a.status] ? 'active' : ''}"></i>`).join('')}</span>`;

const PURPOSES = [
  ['box', 'Закупить товар или сырьё', 'working-capital'],
  ['truck', 'Купить оборудование, транспорт', 'equipment'],
  ['bolt', 'Закрыть кассовый разрыв', 'overdraft'],
  ['plus', 'Развивать бизнес', 'business-dev'],
];

// ---------- общие блоки панелей ----------
const lockBox = (text) => `
  <div class="lockbox"><span class="lockbox__ic">${icon('lock', 22)}</span>
    <div><b>Нужен вход</b><div class="muted small">${esc(text)}</div></div>
    <button class="btn btn--red btn--sm" data-do="login">Войти</button></div>`;

const panelHead = (title, sub) => `<div class="pn-head"><b>${title}</b>${sub ? `<span class="muted small">${sub}</span>` : ''}</div>`;

function productRows() {
  return PRODUCTS.map((p) => `
    <button class="pn-item" data-do="apply" data-product="${p.id}">
      <span class="pn-ic">${icon({ 'business-dev': 'plus', 'working-capital': 'cycle', equipment: 'truck', overdraft: 'bolt' }[p.id] || 'plus', 20)}</span>
      <span class="pn-txt"><b>${esc(p.name)}</b><small>от ${rate(p.rate)} · до ${fmt(p.max_amount)} ₽</small></span>
      <span class="pn-side">≈ ${rub(p.default_payment)}<small>в месяц</small></span>
    </button>`).join('');
}

function appRows(limit = 5) {
  const apps = island.apps.slice(0, limit);
  if (!apps.length) return `<div class="pn-empty">Заявок пока нет — оформите кредит за пару минут</div>`;
  return apps.map((a) => `
    <a class="pn-app" href="${appHref(a)}">
      <span class="pn-app__top"><b>${esc(a.number)}</b><span class="muted small">${esc(product(a.product_id)?.name || '')} · ${rub(a.amount)}</span>${pill(a.status)}</span>
      ${FINAL.includes(a.status) ? '' : miniSteps(a)}
    </a>`).join('');
}

function loanRows() {
  const loans = loansIssued();
  if (!loans.length) return `<div class="pn-empty">Выданных кредитов нет — платежи появятся после подписания договора</div>`;
  return loans.map((a) => {
    const paid = (a.payments || []).length;
    return `<div class="pn-loan">
      <span class="pn-txt"><b>${esc(a.number)} · ${esc(product(a.product_id)?.name || '')}</b><small>платёж ${paid + 1} из ${a.term_months} · ${rate(a.decision.rate)}</small></span>
      <button class="btn btn--red btn--sm" data-do="pay" data-id="${a.id}">${rub(nextPay(a))}</button></div>`;
  }).join('');
}

function reqRows() {
  const r = ME.requisites, c = ME.company;
  const rows = [['ИНН', c.inn], ...(r.kpp ? [['КПП', r.kpp]] : []), ['Р/с', r.account], ['БИК', r.bik], ['Банк', r.bank_name]];
  return rows.map(([k, v]) => `<div class="pn-req"><span>${k}</span><b>${esc(v)}</b><button class="copy-btn" data-do="copy" data-v="${esc(v)}" aria-label="Скопировать ${k}">${icon('copy', 15)}</button></div>`).join('');
}

function docRows() {
  const signed = island.apps.filter((a) => ['issued', 'closed', 'signing'].includes(a.status));
  if (!signed.length) return `<div class="pn-empty">Документы появятся, когда банк одобрит заявку</div>`;
  return signed.flatMap((a) => {
    const docs = a.status === 'signing' ? ['Кредитный договор (на подпись)'] : ['Кредитный договор', 'График платежей'];
    return docs.map((d) => `<a class="pn-item" href="#/app/${a.id}"><span class="doc__ic">PDF</span><span class="pn-txt"><b>${d}</b><small>${esc(a.number)} · ${rub(a.amount)}</small></span></a>`);
  }).join('');
}

// ---------- панели хот-бара ----------
const FLY = {
  credit: () => `${panelHead('Что нужно бизнесу?', 'выберите — анкета откроется сразу')}${productRows()}
    <div class="pn-foot"><button class="pn-link" data-do="calc">${icon('calc', 16)} Сравнить в калькуляторе</button></div>`,
  overdraft: () => {
    const p = product('overdraft');
    return `${panelHead('Овердрафт', `${rate(p.rate)} · решение за минуту · платите только за использованное`)}
      <div class="pn-label">Сколько нужно?</div>
      <div class="pn-chips">${[300_000, 1_000_000, 3_000_000, 5_000_000].map((v) => `<button class="chip" data-do="apply" data-product="overdraft" data-amount="${v}">${fmt(v)} ₽</button>`).join('')}</div>
      <div class="pn-foot"><button class="pn-link" data-do="apply" data-product="overdraft">Своя сумма ${icon('arrow', 16)}</button></div>`;
  },
  calc: () => `${panelHead('Рассчитать платёж', 'выберите продукт')}
    ${PRODUCTS.map((p) => `<button class="pn-item" data-do="calc" data-product="${p.id}"><span class="pn-txt"><b>${esc(p.name)}</b><small>${esc(p.description)}</small></span><span class="pn-side">${rate(p.rate)}</span></button>`).join('')}`,
  apps: () => (ME ? `${panelHead('Мои заявки', activeApps().length ? `${activeApps().length} в работе` : '')}${appRows()}
    <div class="pn-foot"><a class="pn-link" href="#/apps">Все заявки ${icon('arrow', 16)}</a><button class="pn-link pn-link--red" data-do="apply" data-product="business-dev">${icon('plus', 16)} Новая</button></div>` : lockBox('Войдите, чтобы видеть свои заявки и их прогресс')),
  pay: () => (ME ? `${panelHead('Платежи по кредитам', 'списание с расчётного счёта ' + maskAcc(ME.requisites.account))}${loanRows()}
    <div class="pn-foot"><span class="muted small">Доступно: <b>${rub(ME.balance)}</b></span></div>` : lockBox('Оплата доступна только после входа в кабинет компании')),
  req: () => (ME ? `${panelHead(esc(ME.company.name), 'реквизиты для счетов и договоров')}${reqRows()}
    <div class="pn-foot"><button class="pn-link pn-link--red" data-do="copy-req">${icon('copy', 16)} Скопировать все</button><a class="pn-link" href="#/profile">Профиль ${icon('arrow', 16)}</a></div>` : lockBox('Реквизиты компании видны только после входа')),
  docs: () => (ME ? `${panelHead('Документы', '')}${docRows()}` : lockBox('Договоры и графики платежей доступны после входа')),
  help: () => `${panelHead('Поддержка', 'отвечаем внутри кабинета, без звонков')}
    <button class="pn-item" data-do="support"><span class="pn-ic">${icon('chat', 20)}</span><span class="pn-txt"><b>Написать менеджеру</b><small>ответ в среднем за 5 минут</small></span></button>
    <details class="pn-faq"><summary>Какие документы нужны?</summary><p>Никаких: данные компании берём из ЕГРЮЛ, а выручку можно подставить из данных ФНС.</p></details>
    <details class="pn-faq"><summary>Сколько ждать решения?</summary><p>Обычно 10–20 секунд после отправки анкеты. Статус обновляется сам.</p></details>
    <details class="pn-faq"><summary>Что если отказали?</summary><p>Мы покажем причины и предложим меньшую сумму или другой продукт.</p></details>`,
  ctx: () => {
    const a = ctxApp();
    if (!a) return '';
    return `${panelHead(`${esc(a.number)} · ${esc(product(a.product_id)?.name || '')}`, `${rub(a.amount)} на ${months(a.term_months)}`)}
      <div class="pn-ctx">${pill(a.status)}${miniSteps(a)}<div class="muted small">этап ${STAGE_OF[a.status] + 1} из 6: ${STAGES[STAGE_OF[a.status]]}</div></div>
      <div class="pn-foot"><a class="btn btn--red btn--sm" href="${appHref(a)}">${ctxLabel(a)[0]} ${icon('arrow', 16)}</a></div>`;
  },
};

// ---------- хот-бар ----------
const HOT = [
  { id: 'credit', t: 'Взять кредит', i: 'plus', cls: 'hot--primary', act: () => startApply('business-dev') },
  { id: 'overdraft', t: 'Овердрафт', i: 'bolt', act: () => startApply('overdraft') },
  { id: 'calc', t: 'Калькулятор', i: 'calc', act: () => openCalc() },
  { id: 'apps', t: 'Мои заявки', i: 'list', auth: true, act: () => go('#/apps') },
  { id: 'pay', t: 'Платежи', i: 'wallet', auth: true, act: () => go('#/profile') },
  { id: 'req', t: 'Реквизиты', i: 'card', auth: true, act: () => go('#/profile') },
  { id: 'docs', t: 'Документы', i: 'doc', auth: true, act: () => go('#/profile') },
  { id: 'help', t: 'Поддержка', i: 'help', act: null },
  { id: 'bank', t: 'Кабинет банка', i: 'bank', act: () => go('#/bank') },
];
const HOT_KEY = 'hotbar_v2';
const hotEnabled = () => store.get(HOT_KEY, HOT.map((h) => h.id));

// Контекстная плитка: появляется первой, когда по заявке что-то происходит.
function ctxApp() {
  if (!ME) return null;
  return island.apps.find((a) => NEEDS_ACTION[a.status]) || island.apps.find((a) => IN_PIPELINE.includes(a.status)) || island.apps.find((a) => a.status === 'draft');
}
function ctxLabel(a) {
  if (NEEDS_ACTION[a.status]) return [NEEDS_ACTION[a.status][1], NEEDS_ACTION[a.status][2]];
  if (IN_PIPELINE.includes(a.status)) return ['Следить', 'blue'];
  return ['Дозаполнить', 'amber'];
}

let hotSig = '';
function renderHotbar() {
  if (!$('#hotbar')) return; // панель быстрых действий отключена
  const on = hotEnabled();
  const ctx = ctxApp();
  const n = activeApps().length;
  const tiles = [];
  if (ctx) {
    const [label, tone] = ctxLabel(ctx);
    tiles.push(`<button class="hot hot--ctx tone-${tone}" data-hot="ctx"><span class="hot__ic">${IN_PIPELINE.includes(ctx.status) ? '<span class="spinner"></span>' : icon('arrow')}</span>${label}<small>${esc(ctx.number)}</small></button>`);
  }
  for (const h of HOT.filter((x) => on.includes(x.id))) {
    tiles.push(`<button class="hot ${h.cls || ''}" data-hot="${h.id}" aria-haspopup="true">
      <span class="hot__ic">${icon(h.i)}${h.auth && !ME ? `<span class="hot__lock">${icon('lock', 11)}</span>` : ''}</span>${h.t}
      ${h.id === 'apps' && n ? `<span class="hot__badge">${n}</span>` : ''}</button>`);
  }
  tiles.push(`<button class="hot hot--ghost" data-hot="_edit"><span class="hot__ic">${icon('gear')}</span>Настроить</button>`);
  const html = tiles.join('');
  hotSig = html;
  $('#hotbar').innerHTML = html;
}

// вызывается из app.js после каждого опроса сервера
function renderHotbarBadge() {
  const prev = hotSig;
  renderHotbar();
  if (prev !== hotSig && flyId) { const t = $(`[data-hot="${flyId}"]`); t ? showFly(flyId, t, true) : hideFly(); }
  refreshPanels();
}

function editHotbar() {
  const on = hotEnabled();
  modal(`
    <h2>Быстрые действия</h2>
    <p class="muted">Выберите, что показывать в панели. Плитка текущей заявки появляется сама, когда она нужна.</p>
    ${HOT.map((h) => `<label class="toggle-row"><span class="row">${icon(h.i, 20)} ${h.t}</span>
      <input type="checkbox" data-hot-toggle="${h.id}" ${on.includes(h.id) ? 'checked' : ''}></label>`).join('')}
    <div class="row" style="margin-top:20px"><button class="btn btn--red btn--block" data-act="hot-save">Сохранить</button></div>`);
}

// ---------- flyout (портал, чтобы не обрезался прокруткой хот-бара) ----------
let flyId = null;
const flyEl = (() => { const d = document.createElement('div'); d.id = 'flyout'; d.className = 'flyout'; d.setAttribute('role', 'menu'); document.body.appendChild(d); return d; })();

function showFly(id, tile, keep = false) {
  const fn = id === 'ctx' ? FLY.ctx : FLY[id];
  if (!fn) { hideFly(); return; }
  const html = fn();
  if (!html) { hideFly(); return; }
  if (!keep || flyEl.innerHTML !== html) flyEl.innerHTML = html;
  $$('.hot.on').forEach((t) => t.classList.remove('on'));
  tile.classList.add('on');
  flyId = id;
  flyEl.classList.add('show');
  const r = tile.getBoundingClientRect();
  const w = flyEl.offsetWidth;
  flyEl.style.left = Math.max(8, Math.min(r.left + r.width / 2 - 60, window.innerWidth - w - 8)) + 'px';
  flyEl.style.top = r.bottom + 6 + 'px';
  flyEl.style.setProperty('--ax', Math.max(16, r.left + r.width / 2 - parseFloat(flyEl.style.left)) + 'px');
}
function hideFly() {
  flyId = null; flyEl.classList.remove('show');
  $$('.hot.on').forEach((t) => t.classList.remove('on'));
}
window.addEventListener('scroll', () => flyId && hideFly(), { passive: true });
$('#hotbar')?.addEventListener('scroll', () => flyId && hideFly(), { passive: true });

// ---------- мега-меню и выпадающие панели шапки ----------
const DD = {
  bell: () => {
    const list = notifications();
    return `${panelHead('Уведомления', list.length ? '' : '')}
      ${list.length ? list.map((n) => `<a class="pn-note ${n.unread ? 'unread' : ''}" href="${n.href}"><span class="pn-note__dot tone-${n.tone}"></span>
        <span class="pn-txt"><b>${esc(n.title)}</b><small>${esc(n.text)}</small></span><time>${timeOf(n.at).slice(0, 5)}</time></a>`).join('')
        : '<div class="pn-empty">Пока тихо. Здесь появятся решения банка и изменения по заявкам.</div>'}`;
  },
  guest: () => `
    ${panelHead('Кабинет компании', 'вход по телефону, регистрация — по ИНН')}
    <button class="pn-item" data-do="login"><span class="pn-ic">${icon('user', 20)}</span><span class="pn-txt"><b>Войти</b><small>по номеру телефона и паролю</small></span></button>
    <button class="pn-item" data-do="register"><span class="pn-ic">${icon('plus', 20)}</span><span class="pn-txt"><b>Зарегистрироваться</b><small>компания и реквизиты подтянутся по ИНН</small></span></button>
    <div class="pn-sep"></div>
    <div class="muted small" style="padding:4px 10px 8px">Без входа можно смотреть продукты и калькулятор. Оформить, подписать и оплатить — только после входа.</div>`,
  me: () => `
    <a class="pn-me" href="#/" title="Открыть личный кабинет"><span class="profile__ava profile__ava--lg">${esc(initials(ME.full_name))}</span>
      <div><b>${esc(ME.full_name)}</b><small>${esc(ME.position)}</small><small>${esc(ME.company.name)} · ИНН ${esc(ME.company.inn)}</small></div>
      <span class="pn-me__go">${icon('chev', 18)}</span></a>
    <a class="pn-item" href="#/profile"><span class="pn-ic">${icon('user', 20)}</span><span class="pn-txt"><b>Профиль и реквизиты</b><small>данные компании и счёта</small></span></a>
    <button class="pn-item" data-do="copy-req"><span class="pn-ic">${icon('copy', 20)}</span><span class="pn-txt"><b>Скопировать реквизиты</b><small>р/с ${esc(maskAcc(ME.requisites.account))}</small></span></button>
    <a class="pn-item" href="#/credits"><span class="pn-ic">${icon('wallet', 20)}</span><span class="pn-txt"><b>Кредиты</b><small>платежи и графики</small></span></a>
    <a class="pn-item" href="#/products"><span class="pn-ic">${icon('plus', 20)}</span><span class="pn-txt"><b>Продукты и калькулятор</b><small>все кредиты для бизнеса</small></span></a>
    <div class="pn-sep"></div>
    <button class="pn-item" data-do="logout"><span class="pn-ic">${icon('out', 20)}</span><span class="pn-txt"><b>Выйти</b></span></button>`,
};

let ddOpen = null;
function openDD(key) {
  const host = $(`.dd[data-dd="${key}"]`);
  if (!host || !DD[key]) return;
  if (ddOpen !== key) closeDD();
  $('.dd__panel', host).innerHTML = DD[key]();
  placePanel(host);
  host.classList.add('open');
  $('.dd__btn', host)?.setAttribute('aria-expanded', 'true');
  ddOpen = key;
  if (key === 'bell') markNotificationsSeen();
}
// На телефоне панель — шторка во всю ширину под шапкой. Позицию считаем от реальной шапки:
// в кабинете шапка не «липкая» и без backdrop-filter, поэтому CSS-отступ от неё уводил меню за экран.
const isPhone = () => matchMedia('(max-width: 640px)').matches;
function placePanel(host) {
  const panel = $('.dd__panel', host);
  if (!panel) return;
  if (!isPhone()) { panel.style.top = panel.style.maxHeight = ''; return; }
  const top = Math.max(8, Math.round(($('.ahdr')?.getBoundingClientRect().bottom || 56) + 8));
  panel.style.top = top + 'px';
  panel.style.maxHeight = `calc(100dvh - ${top + 12}px)`;
}
function closeDD() {
  $$('.dd.open').forEach((d) => { d.classList.remove('open'); $('.dd__btn', d)?.setAttribute('aria-expanded', 'false'); });
  ddOpen = null;
}
function refreshPanels() {
  if (ddOpen && ddOpen !== 'bell') {
    const p = $(`.dd[data-dd="${ddOpen}"] .dd__panel`);
    const html = DD[ddOpen]?.();
    if (p && html !== undefined && p.innerHTML !== html) p.innerHTML = html;
  }
}

// ---------- уведомления (из истории заявок) ----------
const seenKey = () => 'notif_seen_' + (ME?.id || 'guest');
function notifications() {
  if (!ME) return [];
  const seen = store.get(seenKey(), 0);
  const tone = { approved: 'green', issued: 'green', closed: 'green', counter_offer: 'amber', need_info: 'amber', rejected: 'red' };
  return island.apps.flatMap((a) => a.history.filter((h) => h.actor !== 'client').map((h) => ({
    at: h.at, title: `${STATUS[h.status][0]} · ${a.number}`, text: h.comment, href: appHref(a),
    tone: tone[h.status] || 'blue', unread: new Date(h.at).getTime() > seen,
  }))).sort((x, y) => new Date(y.at) - new Date(x.at)).slice(0, 12);
}
function markNotificationsSeen() {
  store.set(seenKey(), Date.now());
  setTimeout(paintBell, 1500);
}
function paintBell() {
  const b = $('#bell-dd .dd__btn');
  if (!b) return;
  const n = notifications().filter((x) => x.unread).length;
  b.querySelector('.hdr-badge')?.remove();
  if (n) b.insertAdjacentHTML('beforeend', `<span class="hdr-badge">${n > 9 ? '9+' : n}</span>`);
}

// Шапка клиента («таблетки» + аватар) — только в личном кабинете; на страницах сайта — обычная шапка.
const CAB_ROUTES = ['', 'apps', 'app', 'apply', 'credits', 'messages', 'profile'];
function inCabinet() {
  const route = (location.hash || '#/').split('/')[1] || '';
  return !!ME && CAB_ROUTES.includes(route);
}

// ---------- шапка (как у Сбера: верхняя строка + «пилюля» + полноэкранное меню) ----------
function paintNav() {
  const nav = $('#hdrnav');
  if (!nav) return;
  const route = (location.hash || '#/').split('/')[1] || '';
  const landing = route === 'products' || (!route && !ME);
  if (inCabinet()) {
    // кабинет: три «таблетки» — только самое важное
    const unread = notifications().filter((x) => x.unread).length;
    const pillLink = (href, label, routes, badge = '') => `<a href="${href}" class="mx-pill ${routes.includes(route) ? 'active' : ''}">${label}${badge}</a>`;
    nav.classList.add('is-pills');
    nav.innerHTML = pillLink('#/', 'Заявки', ['', 'apps', 'app', 'apply']) + pillLink('#/credits', 'Кредиты', ['credits']) +
      pillLink('#/messages', 'Сообщения', ['messages'], unread && route !== 'messages' ? `<i class="mx-pill__badge">${unread > 99 ? '99+' : unread}</i>` : '');
    return;
  }
  nav.classList.remove('is-pills');
  nav.innerHTML = `
    <a href="#/products" class="${landing ? 'active' : ''}">Кредиты</a>
    <a href="#" data-do="apply" data-product="overdraft">Овердрафт</a>
    <a href="#" data-do="calc">Калькулятор</a>
    <a href="#" data-do="goto" data-sec="how">Как получить</a>
    <a href="#" data-do="goto" data-sec="faq">Вопросы</a>
    <button data-do="menu">Ещё ${icon('chev', 16)}</button>`;
}

// Красная «разделённая» кнопка: основное действие + стрелка с меню (как «СберБанк Онлайн ▾»)
function ctaSplit(inMenu = false) {
  const more = inMenu ? '' : `<div class="dd" data-dd="${ME ? 'me' : 'guest'}"><button class="dd__btn cta-more" aria-label="Ещё" aria-haspopup="true" aria-expanded="false">${icon('chev', 18)}</button><div class="dd__panel dd__panel--right"></div></div>`;
  return ME
    ? `<div class="cta-split"><a class="cta-main" href="#/" title="Личный кабинет"><span class="cta-ava">${esc(initials(ME.full_name))}</span><span class="cta-txt">Личный кабинет</span></a>${more}</div>`
    : `<div class="cta-split"><button class="cta-main" data-do="login"><span class="cta-txt">Войти в кабинет</span></button>${more}</div>`;
}

function paintAuthSlot() {
  const slot = $('#auth-slot');
  if (!slot) return;
  // после входа справа везде аватар с меню профиля (и на сайте, и в кабинете)
  const html = ME
    ? `<div class="dd" data-dd="me"><button class="dd__btn ahdr__user" aria-haspopup="true" aria-expanded="false" title="${esc(ME.full_name)}"><span class="ahdr__ava">${icon('user', 20)}</span></button><div class="dd__panel dd__panel--right"></div></div>`
    : `<button class="btn btn--red ahdr__login" data-do="login">Войти</button>`;
  // перерисовываем только при реальном изменении — иначе фоновый опрос пересоздаёт и закрывает открытое меню
  if (slot.dataset.html !== html) { if (ddOpen === 'me') closeDD(); slot.innerHTML = html; slot.dataset.html = html; }
  $('#bell-dd').hidden = true; // уведомления — в разделе «Сообщения»
  document.body.classList.toggle('is-cab', inCabinet());
  paintNav(); paintBell();
  if (ddOpen === 'me' && !ME) closeDD();
}

// ---------- поиск / командная палитра ----------
function paletteItems() {
  const items = [];
  const add = (group, ic, title, sub, run, kw = '') => items.push({ group, ic, title, sub, run, hay: (title + ' ' + sub + ' ' + kw).toLowerCase() });
  add('Действия', 'plus', 'Взять кредит', 'анкета откроется сразу', () => startApply('business-dev'), 'деньги займ оформить');
  add('Действия', 'bolt', 'Оформить овердрафт', 'решение за минуту', () => startApply('overdraft'), 'кассовый разрыв');
  add('Действия', 'calc', 'Калькулятор', 'платёж и переплата', () => openCalc(), 'рассчитать');
  if (ME) {
    add('Действия', 'list', 'Мои заявки', `${activeApps().length} в работе`, () => go('#/apps'), 'статус прогресс');
    add('Действия', 'user', 'Личный кабинет', ME.company.name, () => go('#/profile'), 'профиль счёт баланс');
    add('Действия', 'copy', 'Скопировать реквизиты', `р/с ${maskAcc(ME.requisites.account)}`, () => copyRequisites(), 'реквизиты инн бик счёт');
    loansIssued().forEach((a) => add('Действия', 'wallet', `Оплатить платёж ${a.number}`, rub(nextPay(a)), () => payLoan(a, () => render()), 'оплата платёж'));
    add('Действия', 'out', 'Выйти', '', () => logout(), 'logout');
  } else {
    add('Действия', 'user', 'Войти', 'или зарегистрироваться по ИНН', () => authModal(), 'вход login регистрация');
  }
  add('Действия', 'bank', 'Кабинет сотрудника банка', 'конвейер заявок', () => go('#/bank'), 'менеджер');
  PRODUCTS.forEach((p) => add('Продукты', 'plus', p.name, `от ${rate(p.rate)} · до ${fmt(p.max_amount)} ₽`, () => startApply(p.id), p.description + ' ' + p.purposes.join(' ')));
  if (ME) island.apps.forEach((a) => add('Заявки', 'doc', `${a.number} · ${product(a.product_id)?.name || ''}`, `${STATUS[a.status][0]} · ${rub(a.amount)}`, () => go(appHref(a))));
  return items;
}

function openPalette() {
  closeDD(); hideFly();
  const items = paletteItems();
  modal(`<div class="pal">
    <label class="pal__in">${icon('search', 20)}<input id="pal-q" placeholder="Продукт, заявка, реквизиты или действие" autocomplete="off" aria-label="Поиск"><kbd>Esc</kbd></label>
    <div class="pal__list" id="pal-list" role="listbox"></div></div>`);
  $('.modal').classList.add('modal--pal');
  const q = $('#pal-q'), list = $('#pal-list');
  let shown = [], sel = 0;
  const paint = () => {
    const words = q.value.toLowerCase().trim().split(/\s+/).filter(Boolean);
    shown = items.filter((it) => words.every((w) => it.hay.includes(w))).slice(0, 14);
    sel = Math.min(sel, Math.max(0, shown.length - 1));
    let g = '';
    list.innerHTML = shown.length ? shown.map((it, i) => {
      const head = it.group !== g ? `<div class="pal__group">${(g = it.group)}</div>` : '';
      return `${head}<button class="pal__item ${i === sel ? 'sel' : ''}" data-pi="${i}" role="option"><span class="pn-ic">${icon(it.ic, 18)}</span><span class="pn-txt"><b>${esc(it.title)}</b><small>${esc(it.sub)}</small></span></button>`;
    }).join('') : '<div class="pn-empty">Ничего не нашли. Попробуйте «кредит», «реквизиты» или номер заявки.</div>';
  };
  const run = (i) => { const it = shown[i]; if (!it) return; closeModal(); it.run(); };
  q.addEventListener('input', () => { sel = 0; paint(); });
  q.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, shown.length - 1); paint(); list.querySelector('.sel')?.scrollIntoView({ block: 'nearest' }); }
    if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); paint(); list.querySelector('.sel')?.scrollIntoView({ block: 'nearest' }); }
    if (e.key === 'Enter') { e.preventDefault(); run(sel); }
  });
  list.addEventListener('click', (e) => { const b = e.target.closest('[data-pi]'); if (b) run(+b.dataset.pi); });
  paint();
  q.focus(); // сразу, без задержки — иначе первые символы поиска могли потеряться
}

function openCalc(productId) {
  if (productId) store.set('calc', { product: productId });
  try { sessionStorage.setItem('scrollTo', 'calc'); } catch { /* недоступно */ }
  go('#/products');
}

// ---------- события ----------
function runDo(ds) {
  switch (ds.do) {
    case 'apply': return startApply(ds.product, +ds.amount || 0);
    case 'calc': return openCalc(ds.product);
    case 'copy-req': return requireAuth(() => copyRequisites(), 'Войдите, чтобы скопировать реквизиты компании.');
    case 'copy': return navigator.clipboard.writeText(ds.v).then(() => toast('Скопировано: ' + ds.v), () => toast('Не удалось скопировать', true));
    case 'pay': return requireAuth(() => { const a = island.apps.find((x) => x.id === ds.id); if (a) payLoan(a, () => render()); }, 'Чтобы оплатить платёж, войдите.');
    case 'login': return authModal();
    case 'register': return authModal('', 'register');
    case 'menu': return mmEl.hidden || !mmEl.classList.contains('open') ? openMenu() : closeMenu();
    case 'goto': {
      const route = (location.hash || '#/').split('/')[1] || '';
      const onLanding = route === 'products' || (!route && !ME);
      if (onLanding && $('#' + ds.sec)) return $('#' + ds.sec).scrollIntoView({ behavior: 'smooth', block: 'start' });
      try { sessionStorage.setItem('scrollTo', ds.sec); } catch { /* недоступно */ }
      return go('#/products');
    }
    case 'menu-close': return closeMenu();
    case 'faq': return modal(`<h2>Частые вопросы</h2>${FLY.help().replace(/<button class="pn-item"[\s\S]*?<\/button>/, '')}`);
    case 'go-auth': return requireAuth(() => go(ds.href), ds.reason || 'Войдите, чтобы открыть этот раздел.');
    case 'logout': return logout();
    case 'palette': return openPalette();
    case 'hot-edit': return editHotbar();
    case 'support': return toast('Менеджер ответит в чате кабинета в течение 5 минут (демо)');
  }
}

let hoverT;
const STICKY_DD = 'me';
document.addEventListener('mouseover', (e) => {
  if (!canHover || $('#modal-root').firstChild) return;
  const dd = e.target.closest('.dd');
  const tile = e.target.closest('.hot[data-hot]');
  clearTimeout(hoverT);
  if (ddOpen === STICKY_DD) return; // меню профиля закрывается только кликом мимо него
  if (dd) {
    if (dd.dataset.dd === STICKY_DD) return; // меню профиля открывается только по клику
    if (flyId) hideFly();
    if (ddOpen !== dd.dataset.dd) hoverT = setTimeout(() => openDD(dd.dataset.dd), ddOpen ? 40 : 120);
    return;
  }
  if (tile && tile.dataset.hot !== '_edit') {
    if (ddOpen) closeDD();
    if (flyId !== tile.dataset.hot) hoverT = setTimeout(() => showFly(tile.dataset.hot, tile), flyId ? 40 : 140);
    return;
  }
  if (e.target.closest('#flyout')) return;
  if (ddOpen || flyId) hoverT = setTimeout(() => { closeDD(); hideFly(); }, 260);
});

document.addEventListener('click', (e) => {
  const doEl = e.target.closest('[data-do]');
  if (doEl) {
    e.preventDefault(); closeDD(); hideFly();
    if (doEl.closest('#megamenu') && !['menu-close'].includes(doEl.dataset.do)) closeMenu();
    runDo(doEl.dataset); return;
  }
  if (e.target.closest('#megamenu a[href]')) { closeMenu(); return; }
  if (!mmEl.hidden && !e.target.closest('#megamenu')) closeMenu();
  if (e.target.closest('.dd__panel a, #flyout a')) { closeDD(); hideFly(); return; }
  const btn = e.target.closest('.dd__btn');
  if (btn) {
    const k = btn.closest('.dd').dataset.dd;
    hideFly();
    if (ddOpen === k && (!canHover || k === STICKY_DD)) closeDD(); else openDD(k);
    return;
  }
  const tile = e.target.closest('.hot[data-hot]');
  if (tile) {
    const id = tile.dataset.hot;
    if (id === '_edit') { hideFly(); return editHotbar(); }
    if (id === 'ctx') { const a = ctxApp(); hideFly(); if (a) go(appHref(a)); return; }
    const h = HOT.find((x) => x.id === id);
    // на тач-устройствах первый тап открывает выбор, на десктопе клик — основное действие
    if (!canHover && flyId !== id && FLY[id]) { closeDD(); showFly(id, tile); return; }
    hideFly();
    if (!h?.act) { if (FLY[id]) showFly(id, tile); return; }
    if (h.auth && !ME) return requireAuth(h.act, 'Войдите, чтобы открыть раздел «' + h.t + '».');
    return h.act();
  }
  if (!e.target.closest('.dd__panel, #flyout')) { closeDD(); hideFly(); }
});

document.addEventListener('keydown', (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); openPalette(); }
  else if (e.key === '/' && !e.target.closest('input, textarea, select') && !$('#modal-root').firstChild) { e.preventDefault(); openPalette(); }
  else if (e.key === 'Escape') { closeDD(); hideFly(); closeMenu(); }
});
window.addEventListener('hashchange', () => { closeDD(); hideFly(); paintAuthSlot(); });
// на телефоне resize прилетает и при скрытии адресной строки — закрываем меню только при смене ширины
let lastW = innerWidth;
window.addEventListener('resize', debounce(() => {
  if (innerWidth === lastW) { const h = ddOpen && $(`.dd[data-dd="${ddOpen}"]`); if (h) placePanel(h); return; }
  lastW = innerWidth; closeDD(); hideFly();
}, 100));

// ---------- меню «Ещё»: скруглённая панель под шапкой сайта ----------
const mmEl = $('#megamenu');
function menuHTML() {
  const n = activeApps().length;
  const authLink = (label, href, reason, extra = '') => ME
    ? `<a href="${href}">${label}${extra}</a>`
    : `<a href="#" data-do="go-auth" data-href="${href}" data-reason="${esc(reason)}">${label} <span class="mm__lock">${icon('lock', 14)}</span></a>`;
  const now = ME ? (() => {
    const t = ctxApp();
    const todo = t ? todoFor(t) : null;
    return `
      <div class="mm-card mm-card--bal"><small>Расчётный счёт ${esc(maskAcc(ME.requisites.account))}</small><b>${rub(ME.balance)}</b><small>${esc(ME.company.name)}</small></div>
      ${t ? `<a class="mm-card" href="${appHref(t)}"><span class="row between"><b>${esc(t.number)} · ${esc(product(t.product_id)?.name || '')}</b>${pill(t.status)}</span>
        ${FINAL.includes(t.status) ? '' : miniSteps(t)}
        <span class="mm-todo ${todo.need ? 'need' : ''}"><small>${todo.need ? 'Что нужно от вас' : 'От вас ничего не нужно'}</small>${esc(todo.title)}</span></a>`
        : `<div class="mm-card"><b>Активных заявок нет</b><span class="muted small">Рассчитайте платёж и сразу заполните анкету — без отдельной заявки.</span><button class="btn btn--red btn--sm" data-do="apply" data-product="business-dev">Оформить кредит</button></div>`}`;
  })() : `
      <div class="mm-card mm-card--guest"><span class="lockbox__ic">${icon('lock', 22)}</span><b>Войдите в кабинет компании</b>
        <span class="muted small">Оформление, подписание, оплата и реквизиты доступны только после входа.</span>
        <div class="row" style="gap:8px;flex-wrap:wrap"><button class="btn btn--red btn--sm" data-do="login">Войти</button><button class="btn btn--light btn--sm" data-do="register">Регистрация по ИНН</button></div></div>`;
  return `
    <div class="mm__in">
      <div class="mm__cols">
        <div class="mm__col">
          <div class="mm__group"><h4>Кредиты</h4>
            ${PRODUCTS.map((p) => `<a href="#" data-do="apply" data-product="${p.id}">${esc(p.name)} <span class="mm__hint">от ${rate(p.rate)}</span></a>`).join('')}
            <a href="#" data-do="calc">Онлайн-калькулятор</a></div>
          <div class="mm__group"><h4>Подобрать по задаче</h4>
            ${PURPOSES.map(([, t, pid]) => `<a href="#" data-do="apply" data-product="${pid}">${t}</a>`).join('')}</div>
        </div>
        <div class="mm__col">
          <div class="mm__group"><h4>Мой бизнес</h4>
            ${authLink('Личный кабинет', '#/profile', 'Войдите, чтобы открыть личный кабинет.')}
            ${authLink('Мои заявки', '#/apps', 'Войдите, чтобы увидеть свои заявки.', n ? ` <span class="mnav__badge">${n}</span>` : '')}
            ${authLink('Платежи по кредитам', '#/profile', 'Оплата доступна только после входа.')}
            ${authLink('Реквизиты компании', '#/profile', 'Реквизиты видны только после входа.')}
            ${authLink('Документы', '#/profile', 'Документы доступны после входа.')}</div>
          <div class="mm__group"><h4>Поддержка и сервисы</h4>
            <a href="#" data-do="support">Чат с менеджером</a>
            <a href="#" data-do="faq">Частые вопросы</a>
            <a href="#" data-do="palette">Поиск по кабинету <span class="mm__hint">Ctrl K</span></a>
            <a href="#/bank">Кабинет сотрудника банка</a></div>
        </div>
        <div class="mm__col">
          <div class="mm__group"><h4>Сейчас</h4>${now}</div>
        </div>
      </div>
    </div>`;
}
let mmT = 0;
function openMenu() {
  closeDD(); hideFly();
  clearTimeout(mmT);
  mmEl.innerHTML = menuHTML();
  mmEl.hidden = false;
  mmEl.style.top = Math.round($('.ahdr').getBoundingClientRect().bottom + 8) + 'px';
  $$('[data-do=menu]').forEach((b) => b.setAttribute('aria-expanded', 'true'));
  requestAnimationFrame(() => mmEl.classList.add('open'));
}
function closeMenu() {
  if (mmEl.hidden) return;
  mmEl.classList.remove('open');
  $$('[data-do=menu]').forEach((b) => b.setAttribute('aria-expanded', 'false'));
  mmT = setTimeout(() => { mmEl.hidden = true; mmEl.innerHTML = ''; }, 220);
}
window.addEventListener('hashchange', closeMenu);

// Логотип всегда ведёт на главную сайта; если уже там — плавно наверх.
document.querySelector('.ahdr__logo')?.addEventListener('click', (e) => {
  const route = (location.hash || '#/').split('/')[1] || '';
  const onHome = route === 'products' || (!route && !ME);
  if (onHome) { e.preventDefault(); closeDD(); closeMenu(); window.scrollTo({ top: 0, behavior: 'smooth' }); }
});
