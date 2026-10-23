// Кабинет → «Профиль и реквизиты»: сюда клиент попадает после входа.
import { api } from '../core/api.js';
import { on } from '../core/dom.js';
import { esc, rub, date, dateTime, phone, maskAccount } from '../core/format.js';
import { session, product, setMe, subscribe } from '../core/session.js';
import { todoFor, appPath, FINAL, statusBadge } from '../domain.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { copy, requisitesText, startApplication, payDialog } from '../features/loan.js';
import { pageHead } from './parts.js';

function requisitesRows(me) {
  const { company: c, requisites: r } = me;
  return [
    ['Получатель', c.name],
    ['ИНН', c.inn],
    ...(r.kpp ? [['КПП', r.kpp]] : []),
    ['ОГРН', c.ogrn],
    ['Расчётный счёт', r.account],
    ['Банк', r.bank_name],
    ['БИК', r.bik],
    ['Корр. счёт', r.corr_account],
    ['Юридический адрес', c.address],
  ];
}

/** Что сейчас требует внимания: активные заявки и ближайший платёж. */
function nowCard(apps) {
  const active = apps.filter((a) => !FINAL.includes(a.status)).slice(0, 3);
  const loan = apps.find((a) => a.status === 'issued');
  if (!active.length && !loan) {
    return `<section class="card"><h2 class="card__title">Сейчас</h2>
      <p class="muted">Активных заявок нет. Рассчитайте кредит и заполните анкету — это займёт пару минут.</p>
      <div><button class="btn btn--primary btn--sm" data-new-loan>${icon('plus', 20)} Оформить кредит</button></div></section>`;
  }
  return `<section class="card"><h2 class="card__title">Сейчас</h2><div class="list">
    ${active
      .map((a) => {
        const t = todoFor(a);
        return `<a class="list-item" href="#${appPath(a)}">
        <div class="list-item__main"><span class="list-item__title">${esc(a.number)} · ${esc(product(a.product_id)?.name)}</span>
          <span class="list-item__meta ${t.need ? 'is-need' : ''}">${esc(t.title)}</span></div>
        ${statusBadge(a.status, a)}</a>`;
      })
      .join('')}
    ${
      loan
        ? `<div class="list-item"><div class="list-item__main"><span class="list-item__title">Платёж ${rub(loan.next_payment)}</span>
      <span class="list-item__meta">${esc(loan.number)} · оплачено ${(loan.payments || []).length} из ${loan.term_months}</span></div>
      <button class="btn btn--secondary btn--sm" data-pay="${loan.id}">Оплатить</button></div>`
        : ''
    }
  </div></section>`;
}

export default async function profile(root, { signal }) {
  root.className = 'page';
  const draw = () => {
    const me = session.me;
    if (!me) return;
    const c = me.company;
    const r = me.requisites;
    root.innerHTML = `<div class="container">
      ${pageHead('Профиль и реквизиты', `${esc(me.full_name)} · ${esc(me.position)}`)}
      <div class="profile-layout">
        <div class="stack-l">
          <section class="card" id="requisites">
            <div class="card__head"><h2>Банковские реквизиты</h2>
              <button class="btn btn--secondary btn--sm" data-copy-all>${icon('copy', 20)} Скопировать все</button></div>
            <table class="requisites">${requisitesRows(me)
              .map(
                ([k, v]) => `
              <tr><td>${k}</td><td>${esc(v)}</td><td><button class="icon-btn" data-copy="${esc(v)}" aria-label="Скопировать: ${k}">${icon('copy', 20)}</button></td></tr>`,
              )
              .join('')}
            </table>
          </section>

          <section class="card">
            <div class="card__head"><h2>Компания</h2>${c.status === 'active' ? '<span class="status status--ok">Действует</span>' : '<span class="status status--bad">Ликвидирована</span>'}</div>
            <dl class="company-dl">
              <div class="wide"><dt>Название</dt><dd>${esc(c.name)}</dd></div>
              <div><dt>Руководитель</dt><dd>${esc(c.director)}</dd></div>
              <div><dt>Дата регистрации</dt><dd>${date(c.reg_date)}</dd></div>
              <div><dt>ОКВЭД</dt><dd>${esc(c.okved)} — ${esc(c.okved_name)}</dd></div>
              <div><dt>Выручка за год (ФНС)</dt><dd>${rub(c.revenue_last_year)}</dd></div>
              <div class="wide"><dt>Источник</dt><dd class="muted">${esc(c.source)}</dd></div>
            </dl>
          </section>

          <section class="card">
            <h2 class="card__title">Контакты и вход</h2>
            <dl class="kv">
              <div><dt>Телефон — логин</dt><dd>${phone(me.phone)}</dd></div>
              <div><dt>Эл. почта</dt><dd><input class="inline-input" id="profile-email" type="email" value="${esc(me.email)}" placeholder="добавить" aria-label="Электронная почта"></dd></div>
              <div><dt>Последний вход</dt><dd>${me.last_login_at && !me.last_login_at.startsWith('0001') ? dateTime(me.last_login_at) : 'сейчас'}</dd></div>
            </dl>
            <p class="note">${icon('lock', 20)}<span>Сессия хранится в защищённой cookie. Без входа нельзя оформить, подписать или оплатить.</span></p>
          </section>
        </div>

        <aside class="profile-side">
          <section class="account" aria-label="Расчётный счёт">
            <div class="account__head"><small>Расчётный счёт ${esc(maskAccount(r.account))}</small><small>${esc(r.bank_name)}</small></div>
            <div><div class="account__balance" id="balance">${rub(me.balance)}</div><span class="foundation"></span></div>
            <div><button class="btn btn--secondary btn--sm" data-copy-all>${icon('copy', 20)} Скопировать реквизиты</button></div>
          </section>
          ${nowCard(session.apps)}
        </aside>
      </div>
    </div>`;
  };
  draw();

  // фоновое обновление (решение банка, платёж) перерисовывает экран, но не поле, которое сейчас редактируют
  const unsubscribe = subscribe(() => {
    if (!root.contains(document.activeElement) || document.activeElement === document.body) draw();
  });
  signal.addEventListener('abort', unsubscribe, { once: true });

  on(root, 'click', '[data-copy-all]', () => copy(requisitesText(), 'Реквизиты скопированы'), signal);
  on(root, 'click', '[data-copy]', (_, el) => copy(el.dataset.copy), signal);
  on(root, 'click', '[data-new-loan]', () => startApplication('business-dev'), signal);
  on(
    root,
    'click',
    '[data-pay]',
    (_, el) => {
      const a = session.apps.find((x) => x.id === el.dataset.pay);
      if (a) payDialog(a);
    },
    signal,
  );
  on(
    root,
    'change',
    '#profile-email',
    async (_, el) => {
      try {
        setMe(await api('PATCH', '/me', { email: el.value }));
        toast('Почта сохранена');
      } catch (e) {
        toast(e.message, { error: true });
      }
    },
    signal,
  );
}
