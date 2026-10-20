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
const annuity = (a, rate, n) => { const r = rate / 100 / 12; return r ? (a * r) / (1 - Math.pow(1 + r, -n)) : a / n; };
const months = (n) => { const m = n % 10, h = n % 100; const w = m === 1 && h !== 11 ? 'месяц' : m >= 2 && m <= 4 && (h < 10 || h >= 20) ? 'месяца' : 'месяцев'; return `${n} ${w}`; };
const timeOf = (iso) => new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
const dateOf = (iso) => new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const debounce = (fn, ms) => { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; };
const store = {
  get(k, d) { try { const v = localStorage.getItem(k); return v ? JSON.parse(v) : d; } catch { return d; } },
  set(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* приватный режим */ } },
};

// Демо-идентификация клиента (в проде — OIDC / Альфа ID на API Gateway)
const CLIENT_ID = (() => {
  let id = store.get('client_id');
  if (!id) { id = 'c-' + Math.random().toString(36).slice(2) + Date.now().toString(36); store.set('client_id', id); }
  return id;
})();

async function api(method, path, body, headers = {}) {
  const res = await fetch('/api/v1' + path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Client-ID': CLIENT_ID, ...headers },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const e = new Error(data?.error?.message || res.statusText);
    e.status = res.status; e.field = data?.error?.field;
    throw e;
  }
  return data;
}

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
};
const icon = (n, size = 24) => `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${I[n]}</svg>`;

// ---------- domain dictionaries ----------
const STATUS = {
  draft: ['Черновик', 'grey'], submitted: ['Отправлена', 'blue'], data_check: ['Проверка данных', 'blue'],
  scoring: ['Оценка риска', 'blue'], approved: ['Одобрено', 'green'], counter_offer: ['Есть предложение', 'amber'],
  need_info: ['Нужны уточнения', 'amber'], rejected: ['Отказ', 'red'], signing: ['Подписание', 'blue'],
  issued: ['Кредит выдан', 'green'], cancelled: ['Отменена', 'grey'],
};
const pill = (s) => `<span class="pill pill--${STATUS[s][1]}">${STATUS[s][0]}</span>`;
const STAGES = ['Анкета', 'Проверка данных', 'Оценка риска', 'Решение', 'Подписание', 'Деньги на счёте'];
const STAGE_OF = { draft: 0, need_info: 0, submitted: 1, data_check: 1, scoring: 2, approved: 3, counter_offer: 3, rejected: 3, signing: 4, issued: 5, cancelled: 0 };
const IN_PIPELINE = ['submitted', 'data_check', 'scoring'];
const FINAL = ['issued', 'cancelled', 'rejected'];
const DOING = {
  submitted: 'Заявка принята, ставим в очередь на проверку…',
  data_check: 'Проверяем компанию в ЕГРЮЛ/ЕГРИП и кредитную историю в БКИ…',
  scoring: 'Считаем долговую нагрузку и принимаем решение…',
};
const DEMO_INN = [
  ['7701234567', 'одобрение'], ['2224000012', 'контрпредложение при сумме от 15 млн'],
  ['5401000009', 'отказ — просрочки'], ['6600000000', 'отказ — ликвидирована'], ['222400000012', 'ИП'],
];

let PRODUCTS = [];
const product = (id) => PRODUCTS.find((p) => p.id === id);
let timers = [];
const later = (fn, ms) => timers.push(setInterval(fn, ms));
const clearTimers = () => { timers.forEach(clearInterval); timers = []; };

// ---------- hot bar ----------
const HOT = [
  { id: 'credit', t: 'Взять кредит', i: 'plus', cls: 'hot--primary', act: () => startApply('business-dev') },
  { id: 'calc', t: 'Калькулятор', i: 'calc', act: () => { go('#/'); setTimeout(() => $('#calc')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 80); } },
  { id: 'apps', t: 'Мои заявки', i: 'list', act: () => go('#/apps') },
  { id: 'overdraft', t: 'Овердрафт', i: 'bolt', act: () => startApply('overdraft') },
  { id: 'working', t: 'Оборотный кредит', i: 'cycle', act: () => startApply('working-capital') },
  { id: 'equipment', t: 'Оборудование', i: 'truck', act: () => startApply('equipment') },
  { id: 'docs', t: 'Документы', i: 'doc', act: () => toast('Документы появятся после подписания договора') },
  { id: 'bank', t: 'Кабинет банка', i: 'bank', act: () => go('#/bank') },
];
const hotEnabled = () => store.get('hotbar', HOT.map((h) => h.id));

async function renderHotbar() {
  let active = 0;
  try { active = (await api('GET', '/applications')).filter((a) => !FINAL.includes(a.status)).length; } catch { /* офлайн */ }
  const on = hotEnabled();
  $('#hotbar').innerHTML = HOT.filter((h) => on.includes(h.id)).map((h) => `
    <button class="hot ${h.cls || ''}" data-hot="${h.id}">
      <span class="hot__ic">${icon(h.i)}</span>${h.t}
      ${h.id === 'apps' && active ? `<span class="hot__badge">${active}</span>` : ''}
    </button>`).join('') + `
    <button class="hot hot--ghost" data-hot="_edit"><span class="hot__ic">${icon('gear')}</span>Настроить</button>`;
}

function editHotbar() {
  const on = hotEnabled();
  modal(`
    <h2>Быстрые действия</h2>
    <p class="muted">Выберите, что показывать в панели — как на главном экране банка.</p>
    ${HOT.map((h) => `<label class="toggle-row"><span class="row">${icon(h.i, 20)} ${h.t}</span>
      <input type="checkbox" data-hot-toggle="${h.id}" ${on.includes(h.id) ? 'checked' : ''}></label>`).join('')}
    <div class="row" style="margin-top:20px"><button class="btn btn--red btn--block" data-act="hot-save">Сохранить</button></div>`);
}

// ---------- router ----------
function go(hash) { if (location.hash === hash) render(); else location.hash = hash; }

async function render() {
  clearTimers();
  closeModal();
  const h = location.hash || '#/';
  const [, route, id] = h.split('/');
  $$('[data-nav]').forEach((a) => a.classList.toggle('active', a.dataset.nav === (route || 'home') || (route === 'app' && a.dataset.nav === 'apps')));
  const old = $('#view');
  const view = old.cloneNode(false); // сбрасываем обработчики предыдущего экрана
  old.replaceWith(view);
  view.innerHTML = '<div class="skeleton"></div>';
  window.scrollTo(0, 0);
  try {
    if (!PRODUCTS.length) PRODUCTS = await api('GET', '/products');
    if (route === 'apply') await viewApply(view, id);
    else if (route === 'app') await viewStatus(view, id);
    else if (route === 'apps') await viewApps(view);
    else if (route === 'bank') await viewBank(view);
    else await viewHome(view);
  } catch (e) {
    view.innerHTML = `<div class="card empty"><h2>Не удалось загрузить</h2><p>${esc(e.message)}</p><a class="btn btn--red" href="#/">На главную</a></div>`;
  }
  renderHotbar();
}

async function startApply(productId, amount, term) {
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
    <div class="calc__field">
      <div class="calc__label"><span>Срок</span><span>от ${p.min_term} до ${p.max_term} мес.</span></div>
      <input class="amount-input" data-calc="term-text" value="${months(term)}" readonly tabindex="-1" aria-hidden="true">
      <input type="range" data-calc="term" min="${p.min_term}" max="${p.max_term}" step="1" value="${term}" ${p.min_term === p.max_term ? 'disabled' : ''} style="--p:${pct(term, p.min_term, p.max_term)}%" aria-label="Срок кредита">
      <div class="range-scale"><span>${months(p.min_term)}</span><span>${months(p.max_term)}</span></div>
    </div>
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

// ---------- HOME ----------
async function viewHome(view) {
  const apps = await api('GET', '/applications');
  const active = apps.find((a) => !FINAL.includes(a.status));
  const st = store.get('calc', { product: 'business-dev' });
  let p = product(st.product) || PRODUCTS[0];
  let amount = st.amount && st.product === p.id ? st.amount : p.default_amount;
  let term = st.term && st.product === p.id ? st.term : p.default_term;

  view.innerHTML = `
    ${active ? activeBanner(active) : ''}
    <section class="hero">
      <div class="hero__promo">
        <div>
          <h1>Кредит для бизнеса без визита в банк</h1>
          <p>Рассчитайте платёж и сразу заполните анкету — данные компании подтянем по ИНН. Решение приходит здесь же, в кабинете.</p>
        </div>
        <div>
          <div class="hero__facts"><div><b>~3 мин</b><span>на анкету</span></div><div><b>~15 сек</b><span>до решения</span></div><div><b>0</b><span>звонков и визитов</span></div></div>
          <button class="btn" style="margin-top:22px" data-act="start" data-product="business-dev">Оформить кредит</button>
        </div>
      </div>
      <div class="card" id="calc">
        <div class="row between"><h2>Онлайн-калькулятор</h2></div>
        <div class="chips" style="margin-top:14px">${PRODUCTS.map((x) => `<button class="chip ${x.id === p.id ? 'on' : ''}" data-calc-product="${x.id}">${esc(x.name)}</button>`).join('')}</div>
        <div id="calc-body"></div>
      </div>
    </section>

    <div class="section-title"><h2>Кредитные продукты</h2><span class="muted small">ставки демонстрационные</span></div>
    <div class="grid products">
      ${PRODUCTS.map((x) => `
        <article class="card product">
          ${x.badge ? `<span class="badge">${esc(x.badge)}</span>` : '<span style="height:22px"></span>'}
          <h3>${esc(x.name)}</h3>
          <div class="product__rate">от ${String(x.rate).replace('.', ',')}% <small>годовых</small></div>
          <div class="muted small">${esc(x.description)}</div>
          <div class="small">до ${fmt(x.max_amount)} ₽ · до ${months(x.max_term)}</div>
          <button class="btn btn--light" data-act="start" data-product="${x.id}">Оформить</button>
        </article>`).join('')}
    </div>`;

  const body = $('#calc-body');
  const paintCalc = () => {
    body.innerHTML = `<div class="calc" style="grid-template-columns:1fr;gap:0">
      ${calcBlock(p, amount, term)}
      <div class="calc__result" style="margin-top:20px">
        <div class="row between"><span>Ежемесячный платёж</span><span class="pill pill--red" style="background:rgba(239,49,36,.18);color:#FF8A80">${String(p.rate).replace('.', ',')}%</span></div>
        <div class="big" id="calc-pay"></div>
        <div class="kv"><span>Переплата</span><b id="calc-over"></b></div>
        <button class="btn btn--red btn--block" data-act="calc-apply">Перейти к анкете →</button>
        <small>Без отдельной заявки — параметры перенесутся в анкету</small>
      </div></div>`;
    const upd = () => {
      const pay = annuity(amount, p.rate, term);
      $('#calc-pay').textContent = rub(pay);
      $('#calc-over').textContent = rub(pay * term - amount);
      store.set('calc', { product: p.id, amount, term });
    };
    bindCalc(body, p, (v) => { amount = v.amount; term = v.term; upd(); });
    upd();
  };
  paintCalc();

  view.addEventListener('click', (e) => {
    const chip = e.target.closest('[data-calc-product]');
    if (chip) {
      p = product(chip.dataset.calcProduct); amount = p.default_amount; term = p.default_term;
      $$('[data-calc-product]').forEach((c) => c.classList.toggle('on', c === chip));
      paintCalc();
    }
    if (e.target.closest('[data-act=calc-apply]')) startApply(p.id, amount, term);
  });
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

  const checklist = () => {
    const c = a.company && a.company.status === 'active';
    return [
      ['Сумма и срок', true],
      ['Компания по ИНН', !!c],
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
          <div class="fsec__head"><span class="fsec__n">2</span><div><h3>Компания</h3><div class="muted small">Введите ИНН — остальное заполним из ЕГРЮЛ/ЕГРИП</div></div></div>
          <div class="fsec__body">
            <div class="field" data-field="inn"><label for="inn">ИНН организации или ИП</label>
              <input id="inn" inputmode="numeric" maxlength="12" placeholder="10 или 12 цифр" value="${esc(a.inn)}" autocomplete="off"></div>
            <div class="chips" style="margin-top:10px">${DEMO_INN.map(([inn, t]) => `<button class="chip chip--demo" data-inn="${inn}" title="${esc(t)}">${inn} · ${esc(t)}</button>`).join('')}</div>
            <div id="company"></div>
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
            <div class="field" data-field="contact.phone"><label for="tel">Телефон</label><input id="tel" data-contact="phone" inputmode="tel" value="${esc(a.contact.phone)}" placeholder="+7 900 000-00-00" autocomplete="tel"></div>
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
        <div class="fill-meta"><span id="s-pct"></span><span class="saved" id="s-saved">Сохранено</span></div>
        <ul class="todo" id="s-todo"></ul>
        <button class="btn btn--red btn--block" id="submit">Отправить на решение</button>
        <p class="muted small" style="text-align:center;margin:10px 0 0">Решение — примерно через 15 секунд, прямо здесь</p>
      </aside>
    </div>`;

  // --- calculator in form ---
  const paintPurposes = () => {
    $('#purposes').innerHTML = p.purposes.map((x) => `<button class="chip ${x === a.purpose ? 'on' : ''}" data-purpose="${esc(x)}">${esc(x)}</button>`).join('');
  };
  const paintCalc = () => {
    $('#apply-calc').innerHTML = calcBlock(p, a.amount, a.term_months);
    bindCalc($('#apply-calc'), p, (v) => { a.amount = v.amount; a.term_months = v.term; queue({ amount: v.amount, term_months: v.term }); refresh(); });
    paintPurposes();
  };
  paintCalc();

  // --- autosave ---
  const indicator = () => { const s = $('#s-saved'); s.classList.toggle('saving', saving > 0); s.textContent = saving > 0 ? 'Сохраняем…' : 'Сохранено'; };
  const flush = debounce(async () => {
    const patch = { ...saveQueue }; Object.keys(saveQueue).forEach((k) => delete saveQueue[k]);
    if (!Object.keys(patch).length) return;
    saving++; indicator();
    try {
      const fresh = await api('PATCH', '/applications/' + id, patch);
      if ('inn' in patch) { a.company = fresh.company; paintCompany(); }
    } catch (e) { toast(e.message, true); } finally { saving--; indicator(); refresh(); }
  }, 500);
  const queue = (patch) => { Object.assign(saveQueue, patch); saving || indicator(); flush(); };

  // --- company ---
  const paintCompany = () => {
    const box = $('#company');
    const c = a.company;
    if (!c) {
      box.innerHTML = a.inn && [10, 12].includes(a.inn.length) ? '<div class="company company--bad"><div class="company__name">Компания не найдена в реестре</div><div class="small">Проверьте ИНН</div></div>' : '';
      $('#rev-hint').innerHTML = '';
      return;
    }
    box.innerHTML = `
      <dl class="company ${c.status !== 'active' ? 'company--bad' : ''}">
        <div class="company__name"><span>${esc(c.name)}</span>${c.status === 'active' ? '<span class="pill pill--green">Действует</span>' : '<span class="pill pill--red">Ликвидирована</span>'}</div>
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

  const setInn = (v) => {
    a.inn = digits(v).slice(0, 12); $('#inn').value = a.inn;
    if ([10, 12].includes(a.inn.length)) queue({ inn: a.inn });
    else if (a.company) { a.company = null; paintCompany(); queue({ inn: a.inn }); }
    refresh();
  };

  // --- summary / progress ---
  const refresh = () => {
    const pay = annuity(a.amount, p.rate, a.term_months);
    $('#s-pay').textContent = rub(pay);
    $('#s-amount').textContent = rub(a.amount);
    $('#s-term').textContent = months(a.term_months);
    $('#s-over').textContent = rub(pay * a.term_months - a.amount);
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
      paintCalc(); refresh();
    }
    const pur = t.closest('[data-purpose]');
    if (pur) { a.purpose = pur.dataset.purpose; queue({ purpose: a.purpose }); paintPurposes(); }
    const demo = t.closest('[data-inn]');
    if (demo) setInn(demo.dataset.inn);
    if (t.closest('[data-act=use-fns]') && a.company) {
      a.financials.annual_revenue = a.company.revenue_last_year;
      $('#rev').value = fmt(a.financials.annual_revenue);
      queue({ financials: { ...a.financials } }); refresh();
    }
  });
  $('#inn').addEventListener('input', (e) => setInn(e.target.value));
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
      await new Promise((r) => setTimeout(r, saving || Object.keys(saveQueue).length ? 700 : 0));
      await api('PATCH', '/applications/' + id, {
        product_id: a.product_id, amount: a.amount, term_months: a.term_months, purpose: a.purpose,
        inn: a.inn, financials: a.financials, contact: a.contact, consent: a.consent,
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
      <section class="card">
        <div class="row between"><h3>Прогресс заявки</h3><span class="muted small">${secs !== null ? `Решение принято за ${secs} сек` : ''}</span></div>
        <div class="tracker">${STAGES.map((s, i) => {
          let cls = i < idx ? 'done' : i === idx ? 'active' : '';
          if (a.status === 'issued' && i === idx) cls = 'done';
          if (a.status === 'rejected' && i === idx) cls = 'fail';
          if (working && i === idx) cls += ' spin';
          const mark = cls.startsWith('done') ? '✓' : cls === 'fail' ? '✕' : i + 1;
          return `<div class="tstep ${cls}"><span class="tstep__dot">${mark}</span>${s}</div>`;
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
      if (act === 'accept') { a = await api('POST', `/applications/${id}/accept`); draw(a); signModal(a, draw); }
      if (act === 'sign-open') signModal(a, draw);
      if (act === 'cancel' && confirmCancel()) { a = await api('POST', `/applications/${id}/cancel`); draw(a); toast('Заявка отменена'); }
      if (act === 'fix') go('#/apply/' + id);
      if (act === 'retry') { store.set('calc', { product: a.product_id, amount: Math.round(a.amount / 2 / 10000) * 10000, term: a.term_months }); go('#/'); }
    } catch (err) { toast(err.message, true); }
  });
}

const confirmCancel = () => true; // без браузерных диалогов: отмена обратима созданием новой заявки за 1 клик

function decisionCard(a) {
  const d = a.decision;
  const box = (cls, title, body) => `<section class="card decision decision--${cls}">${title}${body}</section>`;
  const offer = (dd, req) => `<div class="offer">
      <div><span>Сумма</span><b>${rub(dd.amount)}</b>${req && dd.amount !== req.amount ? `<s>${rub(req.amount)}</s>` : ''}</div>
      <div><span>Срок</span><b>${months(dd.term_months)}</b>${req && dd.term_months !== req.term_months ? `<s>${months(req.term_months)}</s>` : ''}</div>
      <div><span>Ставка</span><b>${String(dd.rate).replace('.', ',')}%</b></div>
      <div><span>Платёж в месяц</span><b>${rub(dd.monthly_payment)}</b></div></div>`;
  switch (a.status) {
    case 'submitted': case 'data_check': case 'scoring':
      return box('blue', '<h2>Рассматриваем заявку</h2>', '<p class="muted">Ничего делать не нужно — статус обновится сам. Обычно это занимает 10–20 секунд.</p>');
    case 'approved':
      return box('green', '<h2>Кредит одобрен</h2>', `${offer(d)}<ul class="reasons">${d.reasons.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--red" data-act="accept">Принять и подписать</button><button class="btn btn--light" data-act="cancel">Отказаться</button></div>`);
    case 'counter_offer':
      return box('amber', '<h2>Можем выдать на других условиях</h2>', `${offer(d, a)}<ul class="reasons">${d.reasons.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--red" data-act="accept">Принять предложение</button><button class="btn btn--light" data-act="cancel">Отказаться</button></div>`);
    case 'need_info':
      return box('amber', '<h2>Нужно уточнить данные</h2>', `<ul class="reasons">${(d?.reasons || []).map((r) => `<li>${esc(r)}</li>`).join('')}</ul>
        <button class="btn btn--red" style="margin-top:20px" data-act="fix">Исправить анкету</button>`);
    case 'rejected':
      return box('red', '<h2>Сейчас не можем выдать кредит</h2>', `<p class="muted" style="margin:8px 0 0">Что повлияло на решение:</p><ul class="reasons">${(d?.reasons || []).map((r) => `<li>${esc(r)}</li>`).join('')}</ul>
        <div class="row" style="margin-top:20px;flex-wrap:wrap"><button class="btn btn--dark" data-act="retry">Рассчитать меньшую сумму</button></div>`);
    case 'signing':
      return box('blue', '<h2>Документы готовы к подписанию</h2>', `${offer(d)}<button class="btn btn--red" data-act="sign-open">Подписать документы</button>`);
    case 'issued': {
      const rate = d.rate, n = a.term_months; let rest = a.amount; const pay = annuity(a.amount, rate, n);
      const rows = []; for (let i = 1; i <= Math.min(6, n); i++) { const int = rest * rate / 1200; const princ = pay - int; rest -= princ; rows.push(`<tr><td>${i}-й месяц</td><td>${rub(pay)}</td><td>${rub(princ)}</td><td>${rub(int)}</td><td>${rub(Math.max(rest, 0))}</td></tr>`); }
      return box('green', '<h2>Деньги на расчётном счёте</h2>', `<p class="muted">Договор подписан, ${rub(a.amount)} зачислены (симуляция). Первый платёж — через месяц.</p>${offer(d)}
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
    <label class="check-box"><input type="checkbox" id="sign-ok"><span class="small">Ознакомился с условиями и подписываю документы простой электронной подписью</span></label>
    <div class="row" style="margin-top:18px"><button class="btn btn--red btn--block" id="sign-btn" disabled>Подписать</button></div>`);
  $('#sign-ok').addEventListener('change', (e) => ($('#sign-btn').disabled = !e.target.checked));
  $('#sign-btn').addEventListener('click', async () => {
    try {
      const fresh = await api('POST', `/applications/${a.id}/sign`, { confirm: true });
      closeModal(); draw(fresh); renderHotbar(); toast('Готово! Деньги на счёте');
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
        <div class="muted small">${dateOf(a.updated_at)}</div>
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
    <div id="bank-body"><div class="skeleton"></div></div>`;
  const load = async () => draw(await api('GET', '/bank/applications', null, { 'X-Bank-Token': token }));
  await load();
  later(() => load().catch(() => {}), 2000);
}

// ---------- modal ----------
function modal(html) {
  $('#modal-root').innerHTML = `<div class="modal-bg" data-close><div class="modal" role="dialog" aria-modal="true">${html}</div></div>`;
}
function closeModal() { $('#modal-root').innerHTML = ''; }

// ---------- global events ----------
document.addEventListener('click', (e) => {
  const hot = e.target.closest('[data-hot]');
  if (hot) { if (hot.dataset.hot === '_edit') editHotbar(); else HOT.find((h) => h.id === hot.dataset.hot)?.act(); }
  const start = e.target.closest('[data-act=start]');
  if (start) startApply(start.dataset.product);
  if (e.target.matches('[data-close]')) closeModal();
  if (e.target.closest('[data-act=hot-save]')) {
    const on = $$('[data-hot-toggle]').filter((i) => i.checked).map((i) => i.dataset.hotToggle);
    store.set('hotbar', on); closeModal(); renderHotbar(); toast('Панель обновлена');
  }
});
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeModal(); });
window.addEventListener('hashchange', render);
render();
