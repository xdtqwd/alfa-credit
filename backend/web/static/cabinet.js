'use strict';
/* Кабинет после входа (в духе Т-Бизнеса): боковое меню, верхняя панель, главная с главным —
   счёт, быстрые действия, «требует внимания», заявки, кредиты, события. Уведомления — карточки справа внизу. */

Object.assign(I, {
  home: '<path d="M4 11l8-7 8 7v9a1 1 0 01-1 1h-4v-6h-6v6H5a1 1 0 01-1-1z"/>',
});

const greet = () => { const h = new Date().getHours(); return h < 5 ? 'Доброй ночи' : h < 12 ? 'Доброе утро' : h < 18 ? 'Добрый день' : 'Добрый вечер'; };
const firstName = (u) => u.full_name.split(' ')[1] || u.full_name;

// ---------- вкладки кабинета (единый стиль сайта, без бокового меню) ----------
function paintSidebar() { document.body.classList.remove('is-auth'); } // оставлено для совместимости вызовов

function cabinetTabs() { return ''; } // навигация кабинета — «таблетки» в шапке

// ---------- уведомления (вместо «острова») ----------
function showNotice(f) {
  const box = $('#notices');
  if (!box) return toast(f.title);
  const el = document.createElement('div');
  el.className = `notice tone-${f.tone || 'blue'}`;
  el.setAttribute('role', 'status');
  el.innerHTML = `<span class="notice__dot">${icon(f.tone === 'red' ? 'lock' : 'check', 16)}</span>
    <div class="notice__txt"><b>${esc(f.title)}</b>${f.sub ? `<small>${esc(f.sub)}</small>` : ''}</div>
    <button class="notice__x" aria-label="Закрыть">${icon('plus', 18)}</button>`;
  const close = () => { el.classList.add('out'); setTimeout(() => el.remove(), 250); };
  el.addEventListener('click', (e) => { if (!e.target.closest('.notice__x') && f.href) go(f.href); close(); });
  box.prepend(el);
  [...box.children].slice(3).forEach((x) => x.remove());
  setTimeout(close, 6000);
}

// ---------- минималистичный кабинет: Заявки · Кредиты · Сообщения ----------
const dShort = (iso) => new Date(iso).toLocaleDateString('ru-RU');
const dFull = (iso) => new Date(iso).toLocaleString('ru-RU', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }).replace(' г.', '');
const title = (t) => `<h1 class="mx-title">${t}<span>.</span></h1>`;
const chip = (s) => `<span class="mx-chip mx-chip--${STATUS[s][1]}">${STATUS[s][0]}</span>`;
const sideCard = (head, btn, extra = '') => `<aside class="mx-side"><section class="mx-card mx-new"><h3>${head}</h3>${btn}</section>${extra}</aside>`;
const bigBtn = (label, attrs) => `<button class="mx-btn" ${attrs}><span>${label}</span>${icon('chev', 18)}</button>`;
const accCard = () => `<section class="mx-card mx-acc"><small>Расчётный счёт ${esc(maskAcc(ME.requisites.account))}</small><b>${rub(ME.balance)}</b><a href="#/profile">Реквизиты и профиль →</a></section>`;

function cabinetPoll(view, draw) {
  const sig = () => JSON.stringify([ME?.balance, island.apps.map((a) => [a.status, a.updated_at, (a.payments || []).length])]);
  let last = sig();
  draw();
  later(() => { if (!ME || !document.body.contains(view)) return; const s = sig(); if (s !== last) { last = s; draw(); } }, 1500);
}

// ЗАЯВКИ
const APP_FILTERS = [
  ['all', 'Все', () => true],
  ['need', 'Нужно действие', (a) => todoFor(a).need && !FINAL.includes(a.status)],
  ['work', 'В работе', (a) => !FINAL.includes(a.status) && !todoFor(a).need],
  ['done', 'Завершены', (a) => FINAL.includes(a.status)],
];
let appFilter = 'all';

async function viewDashboard(view) {
  await pollIsland(true);
  const draw = () => {
    const apps = island.apps.filter((a) => !['issued', 'closed'].includes(a.status) || appFilter === 'done' || appFilter === 'all');
    const f = APP_FILTERS.find((x) => x[0] === appFilter);
    const list = apps.filter(f[2]);
    const count = (key) => apps.filter(APP_FILTERS.find((x) => x[0] === key)[2]).length;
    view.innerHTML = `${title('Заявки')}
      <div class="mx-grid">
        <section class="mx-card mx-main">
          <nav class="mx-filters">${APP_FILTERS.map(([k, t]) => `<button class="${k === appFilter ? 'on' : ''}" data-filter="${k}">${t}${k === 'need' && count('need') ? ` <i>${count('need')}</i>` : ''}</button>`).join('')}</nav>
          ${list.length ? list.map((a) => {
            const t = todoFor(a);
            const p = product(a.product_id);
            const amount = a.decision?.amount || a.amount, term = a.decision?.term_months || a.term_months;
            return `<article class="mx-item">
              <div class="mx-item__head"><span>${esc(p?.name || 'Кредит')} · Заявка ${esc(a.number)}</span>
                <button class="mx-copy" data-do="copy" data-v="${esc(a.number)}" title="Скопировать номер">${icon('copy', 14)}</button>${chip(a.status)}</div>
              <div class="mx-item__body">
                <b class="mx-item__title">${rub(amount)} на ${months(term)}</b>
                ${t.title ? `<div class="mx-item__todo ${t.need ? 'need' : ''}">${t.need ? 'Нужно: ' : ''}${esc(t.title)}</div>` : ''}
                <div class="mx-item__links">
                  <a href="${appHref(a)}">${icon('doc', 16)} Открыть заявку</a>
                  ${t.need && t.cta && !FINAL.includes(a.status) ? `<a class="mx-link-red" href="${appHref(a)}">${icon('arrow', 16)} ${t.cta.label}</a>` : ''}
                </div>
                <div class="mx-item__dates"><span>${dShort(a.created_at)}</span><span>обновлена ${dFull(a.updated_at)}</span></div>
              </div></article>`;
          }).join('') : `<div class="mx-empty">${appFilter === 'all' ? 'Заявок пока нет. Оформите кредит — анкета займёт около 3 минут.' : 'В этом разделе пусто.'}</div>`}
          <div class="mx-hint"><b>Не видите заявку?</b><p>Здесь показываются все заявки вашей компании. Если что-то не так — <a href="#" data-do="support">напишите нам</a>, поможем разобраться.</p></div>
        </section>
        ${sideCard('Новая заявка', bigBtn('Оформить кредит', 'data-do="apply" data-product="business-dev"'), accCard())}
      </div>`;
  };
  view.addEventListener('click', (e) => {
    const f = e.target.closest('[data-filter]');
    if (f) { appFilter = f.dataset.filter; draw(); }
  });
  cabinetPoll(view, draw);
}

// КРЕДИТЫ
async function viewCredits(view) {
  await pollIsland(true);
  const draw = () => {
    const loans = island.apps.filter((a) => ['issued', 'closed'].includes(a.status));
    view.innerHTML = `${title('Кредиты')}
      <div class="mx-grid">
        <section class="mx-card mx-main">
          ${loans.length ? loans.map((a) => {
            const paid = (a.payments || []).length, n = a.term_months;
            const rest = paid ? a.schedule?.[paid - 1]?.rest ?? 0 : a.amount;
            const issued = a.history.find((h) => h.status === 'issued')?.at || a.updated_at;
            const end = new Date(issued); end.setMonth(end.getMonth() + n);
            return `<article class="mx-item">
              <div class="mx-item__head"><span>${esc(product(a.product_id)?.name || 'Кредит')} · Договор ${esc(a.number)}</span>
                <button class="mx-copy" data-do="copy" data-v="${esc(a.number)}" title="Скопировать номер">${icon('copy', 14)}</button>
                <span class="mx-chip mx-chip--${a.status === 'issued' ? 'blue' : 'grey'}">${a.status === 'issued' ? 'Платёж ' + rub(a.next_payment) : 'Погашен'}</span></div>
              <div class="mx-item__body">
                <b class="mx-item__title">${a.status === 'issued' ? 'Осталось ' + rub(rest) : rub(a.amount) + ' · погашен'}</b>
                <div class="mx-item__sub">Оплачено ${paid} из ${n} · ставка ${String(a.decision.rate).replace('.', ',')}%</div>
                <div class="progress progress--green mx-progress"><i style="width:${(paid / n) * 100}%"></i></div>
                <div class="mx-item__links">
                  ${a.status === 'issued' ? `<a href="#" class="mx-link-red" data-do="pay" data-id="${a.id}">${icon('wallet', 16)} Оплатить ${rub(a.next_payment)}</a>` : ''}
                  <a href="#/app/${a.id}">${icon('doc', 16)} График и договор</a>
                </div>
                <div class="mx-item__dates"><span>${dShort(issued)}</span><span>${dShort(end.toISOString())}</span></div>
              </div></article>`;
          }).join('') : '<div class="mx-empty">Выданных кредитов пока нет. Когда вы подпишете договор, кредит появится здесь вместе с графиком и кнопкой оплаты.</div>'}
          <div class="mx-hint"><b>Платёж списывается с расчётного счёта</b><p>Оплатить можно в любой момент — график пересчитается автоматически.</p></div>
        </section>
        ${sideCard('Новый кредит', bigBtn('Оформить кредит', 'data-do="apply" data-product="business-dev"'), accCard())}
      </div>`;
  };
  cabinetPoll(view, draw);
}

// СООБЩЕНИЯ
let msgLimit = 6;
async function viewMessages(view) {
  await pollIsland(true);
  msgLimit = 6;
  const all = () => island.apps.flatMap((a) => a.history.filter((h) => h.actor !== 'client').map((h) => ({ a, h })))
    .sort((x, y) => new Date(y.h.at) - new Date(x.h.at));
  const draw = () => {
    const seen = store.get(seenKey(), 0);
    const items = all();
    view.innerHTML = `${title('Сообщения')}
      <div class="mx-grid">
        <section class="mx-card mx-main">
          ${items.length ? items.slice(0, msgLimit).map(({ a, h }) => `
            <a class="mx-msg ${new Date(h.at).getTime() > seen ? 'unread' : ''}" href="${appHref(a)}">
              <span class="mx-msg__ic">₽</span>
              <span class="mx-msg__txt"><b>${STATUS[h.status][0]}</b><small>Заявка ${esc(a.number)} · ${esc(h.comment)}</small></span>
              <time>${dFull(h.at)}</time>
            </a>`).join('') : '<div class="mx-empty">Сообщений пока нет. Здесь появятся решения банка и изменения по заявкам.</div>'}
          ${items.length > msgLimit ? `<div class="mx-more"><button data-more>Показать ещё ${Math.min(6, items.length - msgLimit)} сообщений</button></div>` : ''}
        </section>
        ${sideCard('Новое обращение', bigBtn('Обращение в поддержку', 'data-do="support"'))}
      </div>`;
    store.set(seenKey(), Date.now());
    paintNav(); paintBell();
  };
  view.addEventListener('click', (e) => { if (e.target.closest('[data-more]')) { msgLimit += 6; draw(); } });
  cabinetPoll(view, draw);
}
