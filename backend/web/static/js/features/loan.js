// Действия с кредитом: оформить заявку, подписать договор, оплатить платёж, скопировать реквизиты.
import { api } from '../core/api.js';
import { $, copyText, storage } from '../core/dom.js';
import { esc, rub, months, rate } from '../core/format.js';
import { navigate } from '../core/router.js';
import { session, putApp, refresh, product, isStaff } from '../core/session.js';
import { openModal, toast } from '../ui/feedback.js';

/**
 * «Оформить» — открывает 1-й уровень анкеты (предварительный расчёт, без входа). Кредит из карточки
 * продукта или с параметрами калькулятора сохраняется; без продукта клиент выбирает кредит сам.
 */
export function startApplication(productId = null, amount = 0, term = 0, inn = '') {
  if (isStaff()) {
    toast('Кредит оформляет представитель компании — вы вошли как сотрудник банка', { error: true });
    return;
  }
  const prev = storage.get('apply', {});
  const p = productId ? product(productId) : null;
  storage.set('apply', {
    ...prev,
    product_id: p?.id || null,
    amount: p ? amount || p.default_amount : 0,
    term_months: p ? term || p.default_term : 0,
    inn: inn || prev.inn || '',
  });
  navigate('/apply');
}

/** Подписание простой электронной подписью. Возвращает обновлённую заявку или null. */
export function signDialog(a) {
  const d = a.decision;
  const r = session.me.requisites;
  return new Promise((resolve) => {
    let result = null;
    const { el, close } = openModal(
      `
      <h2>Подписание документов</h2>
      <p class="modal__lead">Простая электронная подпись — без визита в офис и без SMS.</p>
      <div class="doc-list">
        ${['Кредитный договор', 'График платежей', 'Заявление на зачисление средств']
          .map(
            (name) => `
          <div class="doc"><span class="doc__ic">PDF</span><div>${name}<small>${esc(a.number)} · ${rub(d.amount)}, ${months(d.term_months)}, ${rate(d.rate)}</small></div></div>`,
          )
          .join('')}
      </div>
      <dl class="kv">
        <div><dt>Получатель</dt><dd>${esc(session.me.company.name)}</dd></div>
        <div><dt>Расчётный счёт</dt><dd>${esc(r.account)}</dd></div>
        <div><dt>БИК</dt><dd>${esc(r.bik)}</dd></div>
      </dl>
      <label class="check" id="sign-consent"><input type="checkbox" id="sign-ok"><span>Ознакомился с условиями и подписываю документы простой электронной подписью</span></label>
      <button class="btn btn--primary btn--block" id="sign-btn" disabled>Подписать</button>`,
      { onClose: () => resolve(result) },
    );
    el.classList.add('stack');
    $('#sign-ok', el).addEventListener('change', (e) => {
      $('#sign-btn', el).disabled = !e.target.checked;
    });
    $('#sign-btn', el).addEventListener('click', async (e) => {
      const btn = e.currentTarget; // после await currentTarget уже null
      btn.disabled = true;
      try {
        result = await api('POST', `/applications/${a.id}/sign`, { confirm: true });
        putApp(result);
        await refresh({ quiet: true }).catch(() => {});
        close();
        toast('Готово! Деньги на расчётном счёте');
      } catch (ex) {
        toast(ex.message, { error: true });
        btn.disabled = false;
      }
    });
  });
}

/** Оплата очередного платежа с расчётного счёта. Возвращает обновлённую заявку или null. */
export function payDialog(a) {
  const pay = a.next_payment;
  const me = session.me;
  const enough = me.balance >= pay;
  return new Promise((resolve) => {
    let result = null;
    const { el, close } = openModal(
      `
      <h2>Оплата платежа</h2>
      <p class="modal__lead">${esc(a.number)} · платёж ${(a.payments || []).length + 1} из ${a.term_months}</p>
      <dl class="kv">
        <div><dt>Сумма</dt><dd>${rub(pay)}</dd></div>
        <div><dt>Списание со счёта</dt><dd>${esc(me.requisites.account)}</dd></div>
        <div><dt>Доступно</dt><dd>${rub(me.balance)}</dd></div>
        <div><dt>Останется</dt><dd class="${enough ? '' : 'text-bad'}">${rub(me.balance - pay)}</dd></div>
      </dl>
      <p class="field__hint text-bad" id="pay-error" role="alert"></p>
      <button class="btn btn--primary btn--block" id="pay-ok" ${enough ? '' : 'disabled'}>${enough ? `Оплатить ${rub(pay)}` : 'Недостаточно средств'}</button>`,
      { onClose: () => resolve(result) },
    );
    el.classList.add('stack');
    $('#pay-ok', el).addEventListener('click', async (e) => {
      const btn = e.currentTarget; // после await currentTarget уже null
      btn.disabled = true;
      try {
        result = await api('POST', `/applications/${a.id}/pay`);
        putApp(result);
        await refresh({ quiet: true }).catch(() => {});
        close();
        toast('Платёж проведён');
      } catch (ex) {
        $('#pay-error', el).textContent = ex.message;
        btn.disabled = false;
      }
    });
  });
}

/** Реквизиты компании одним текстом — для договоров и счетов. */
export function requisitesText(me = session.me) {
  const { company: c, requisites: r } = me;
  return [
    c.name,
    `ИНН ${c.inn}${r.kpp ? ` КПП ${r.kpp}` : ''}`,
    `ОГРН ${c.ogrn}`,
    `Р/с ${r.account}`,
    `Банк ${r.bank_name}`,
    `БИК ${r.bik}`,
    `К/с ${r.corr_account}`,
    `Адрес: ${c.address}`,
  ].join('\n');
}

export async function copy(text, done = 'Скопировано') {
  try {
    await copyText(text);
    toast(done);
  } catch {
    toast('Не удалось скопировать — выделите текст вручную', { error: true });
  }
}
