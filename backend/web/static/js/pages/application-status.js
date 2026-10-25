// Заявка после отправки: что нужно от клиента, этапы, решение банка, проверки и история.
import { api } from '../core/api.js';
import { on, every } from '../core/dom.js';
import { esc, rub, months, time, dateTime } from '../core/format.js';
import { navigate } from '../core/router.js';
import { product, putApp } from '../core/session.js';
import { STATUS, IN_PIPELINE, DOING, todoFor, statusBadge, stagesFor } from '../domain.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { offer, compare, reasons, scoreBlock } from '../features/decision.js';
import { signDialog, payDialog, startApplication } from '../features/loan.js';
import { backLink, pageHead, stepper, maskPassport } from './parts.js';

// Главная кнопка («Принять», «Подписать», «Оплатить», «Исправить») — одна, в блоке «Что нужно от вас» сверху;
// в карточке решения остаются только объяснение и «Отказаться».

function decisionCard(a) {
  const d = a.decision;
  const pre = a.preliminary;
  const card = (tone, title, body) =>
    `<section class="card decision decision--${tone}" id="decision" data-decision="${a.status}"><h2>${title}</h2>${body}</section>`;
  const cancel = '<button class="btn btn--ghost" data-action="cancel">Отказаться</button>';
  switch (a.status) {
    case 'submitted':
    case 'data_check':
    case 'scoring':
    case 'verification':
      return card(
        'info',
        'Считаем полный скоринг',
        `<p class="muted">Предварительно ${pre?.type === 'counter_offer' ? 'одобрено на других условиях' : 'одобрено'}: ${rub(a.amount)} на ${months(a.term_months)}${pre ? `, ставка от ${String(pre.rate).replace('.', ',')}%` : ''}. Сейчас банк запрашивает кредитную историю и проверяет документы${a.full_form?.guarantor ? ' и поручителя' : ''} — окончательное решение придёт сюда же.</p>`,
      );
    case 'approved':
      // окончательное решение сравниваем с предварительным расчётом 1-го уровня
      return card(
        'ok',
        'Кредит одобрен',
        `${d.requested && d.requested.rate !== d.rate ? compare(d.requested, d, ['Предварительно', 'Окончательно']) : offer(d)}${reasons('Почему так', d.reasons)}${scoreBlock(a)}
        <div class="actions">${cancel}</div>`,
      );
    case 'counter_offer':
      return card(
        'warn',
        'Можем выдать на других условиях',
        `<p class="muted">Сравните, что вы просили и что предлагает банк:</p>${d.requested ? compare(d.requested, d) : offer(d)}${reasons('Почему так', d.reasons)}${scoreBlock(a)}
        <div class="actions">${cancel}</div>`,
      );
    case 'need_info':
      return card('warn', 'Нужно уточнить данные', `${reasons('Почему так', d?.reasons)}${reasons('Что сделать', d?.advice)}`);
    case 'rejected':
      return card(
        'bad',
        'Сейчас не можем выдать кредит',
        `${reasons('Почему так', d.reasons)}${reasons('Что можно сделать', d.advice)}${scoreBlock(a)}
        <div class="actions"><button class="btn btn--secondary" data-action="retry">Рассчитать меньшую сумму</button></div>`,
      );
    case 'signing':
      return card('info', 'Документы готовы к подписанию', offer(d));
    case 'issued':
    case 'closed': {
      const paid = (a.payments || []).length;
      const rows = (a.schedule || []).slice(Math.max(0, paid - 1), paid + 5);
      return card(
        'ok',
        a.status === 'closed' ? 'Кредит полностью погашен' : 'Деньги на расчётном счёте',
        `
        <p class="muted">Договор подписан, ${rub(a.amount)} зачислены на расчётный счёт (симуляция).</p>${offer(d)}
        <div class="pay-line" id="pay-line">
          <b>Оплачено ${paid} из ${a.term_months} платежей</b><div class="bar bar--ok"><i style="width:${(paid / a.term_months) * 100}%"></i></div>
        </div>
        <h3>График платежей</h3>
        <div class="table-wrap"><table class="table schedule"><thead><tr><th>Платёж</th><th class="num">Сумма</th><th class="num">Долг</th><th class="num">Проценты</th><th class="num">Остаток</th></tr></thead><tbody>
          ${rows.map((r) => `<tr class="${r.n <= paid ? 'is-paid' : r.n === paid + 1 ? 'is-next' : ''}"><td>${r.n}-й месяц${r.n <= paid ? ' · оплачен' : r.n === paid + 1 ? ' · следующий' : ''}</td><td class="num">${rub(r.payment)}</td><td class="num">${rub(r.principal)}</td><td class="num">${rub(r.interest)}</td><td class="num">${rub(r.rest)}</td></tr>`).join('')}
        </tbody></table></div>`,
      );
    }
    case 'cancelled':
      return card(
        'info',
        'Заявка отменена',
        '<p class="muted">Новый предварительный расчёт — пара минут, без входа.</p><div class="actions"><a class="btn btn--primary" href="#/apply">Рассчитать кредит</a></div>',
      );
    case 'draft':
      return card(
        'info',
        'Анкета из прошлой версии',
        '<p class="muted">Теперь кредит оформляется в два уровня: предварительный расчёт без входа, затем вход через Госуслуги и документы. Начните новый расчёт — данные компании подтянутся сами.</p>',
      );
    default:
      return '';
  }
}

const COLLATERAL = {
  realty: 'Недвижимость',
  vehicle: 'Транспорт и спецтехника',
  equipment: 'Оборудование',
  deposit: 'Депозит в Альфа-Банке',
};

/** Что клиент приложил на 2-м уровне — после отправки. */
function fullSummary(a) {
  const f = a.full_form;
  if (!f || a.status === 'full_form') return '';
  const c = f.collateral || {};
  const id = a.identity;
  return `<section class="card"><h2 class="card__title">Документы 2-го уровня</h2><dl class="kv full-summary">
    ${id ? `<div><dt>Госуслуги</dt><dd>${esc(id.full_name)}, паспорт ${maskPassport(id.passport)}</dd></div>` : ''}
    ${f.director_passport ? `<div><dt>Паспорт руководителя</dt><dd>${maskPassport(f.director_passport)}</dd></div>` : ''}
    ${f.protocol ? `<div><dt>Протокол о крупной сделке</dt><dd>${esc(f.protocol.name)}</dd></div>` : ''}
    <div><dt>Залог</dt><dd>${c.type && c.type !== 'none' ? `${esc(COLLATERAL[c.type])}, ${rub(c.value)}` : 'Без залога'}</dd></div>
    <div><dt>Поручитель</dt><dd>${f.guarantor ? esc(f.guarantor.full_name) : 'Нет'}</dd></div>
  </dl>${id?.verified_at ? `<p class="note">${icon('lock', 20)}<span>Личность подтверждена через Госуслуги ${dateTime(id.verified_at)}</span></p>` : ''}</section>`;
}

function todoNotice(a) {
  const t = todoFor(a);
  if (!t.title) return '';
  return `<section class="notice ${t.need ? 'notice--action' : ''}" id="todo">
    <span class="notice__ic">${icon(t.need ? 'arrow' : 'check', 20)}</span>
    <div class="notice__txt"><small>${t.need ? 'Что нужно от вас' : 'От вас ничего не нужно'}</small><b>${esc(t.title)}</b><span>${esc(t.text)}</span></div>
    ${t.action ? `<button class="btn btn--primary" data-action="${t.action[0]}">${t.action[1]}</button>` : ''}
  </section>`;
}

function layout(a) {
  const p = product(a.product_id);
  const busy = IN_PIPELINE.includes(a.status);
  const { stages, idx } = stagesFor(a);
  const sent = a.history.find((h) => h.status === 'submitted');
  const decided = a.history.find((h) => ['approved', 'counter_offer', 'rejected', 'need_info'].includes(h.status));
  const secs = sent && decided ? Math.round((new Date(decided.at) - new Date(sent.at)) / 1000) : null;
  return `<div class="container stack-l">
    <div>
      ${backLink('/cabinet/applications', 'Все заявки')}
      ${pageHead(`Заявка ${a.number}`, `${esc(p?.name)} · ${rub(a.amount)} на ${months(a.term_months)}${a.company ? ` · ${esc(a.company.name)}` : ''}`, statusBadge(a.status, a))}
    </div>
    ${todoNotice(a)}
    <section class="card stepper-box">
      <div class="card__head"><h2>Прогресс заявки</h2>${secs !== null ? `<span class="small muted">Полный скоринг — за ${secs} с</span>` : ''}</div>
      ${stepper(a, stages, { [a.status]: idx }, busy)}
      ${busy ? `<p class="note">${icon('clock', 20)}<span>${DOING[a.status]}</span></p>` : ''}
    </section>
    <div class="app-layout">
      <div class="stack-l">
        ${decisionCard(a)}
        ${fullSummary(a)}
        ${
          a.checks.length
            ? `<section class="card"><h2 class="card__title">Проверки банка</h2><ul class="checks">${a.checks
                .map(
                  (c) => `
          <li class="${c.result}"><i>${c.result === 'ok' ? '✓' : c.result === 'warn' ? '!' : '✕'}</i><div>${esc(c.name)}<small>${esc(c.details)}</small></div></li>`,
                )
                .join('')}</ul></section>`
            : ''
        }
      </div>
      <aside class="card">
        <h2 class="card__title">История</h2>
        <ul class="timeline">${[...a.history]
          .reverse()
          .map((h) => `<li><time>${time(h.at)}</time><b>${STATUS[h.status]?.[0] || h.status}</b><span>${esc(h.comment)}</span></li>`)
          .join('')}</ul>
      </aside>
    </div>
  </div>`;
}

export default function applicationStatus(root, initial, { signal }) {
  let a = initial;
  const draw = (next) => {
    a = next;
    putApp(a);
    root.innerHTML = layout(a);
  };
  draw(a);

  // пока банк думает — следим за статусом (сервер отвечает 304, если ничего не изменилось)
  let last = `${a.status}|${a.updated_at}`;
  every(
    1500,
    async () => {
      if (!IN_PIPELINE.includes(a.status)) return;
      try {
        const fresh = await api('GET', `/applications/${a.id}`);
        const sig = `${fresh.status}|${fresh.updated_at}`;
        if (sig !== last && !signal.aborted) {
          last = sig;
          draw(fresh);
        }
      } catch {
        /* сеть: повторим */
      }
    },
    signal,
  );

  on(
    root,
    'click',
    '[data-action]',
    async (_, el) => {
      const action = el.dataset.action;
      try {
        if (action === 'accept') {
          const next = await api('POST', `/applications/${a.id}/accept`);
          putApp(next);
          draw(next);
          const signed = await signDialog(a);
          if (signed && !signal.aborted) draw(signed);
        }
        if (action === 'sign') {
          const signed = await signDialog(a);
          if (signed && !signal.aborted) draw(signed);
        }
        if (action === 'pay') {
          const paid = await payDialog(a);
          if (paid && !signal.aborted) draw(paid);
        }
        if (action === 'cancel') {
          draw(await api('POST', `/applications/${a.id}/cancel`));
          toast('Заявка отменена');
        }
        if (action === 'fix') navigate(`/cabinet/applications/${a.id}`);
        if (action === 'retry') startApplication(a.product_id, Math.round(a.amount / 2 / 10000) * 10000, a.term_months, a.inn);
        if (action === 'restart') startApplication(a.product_id, a.amount, a.term_months, a.inn);
      } catch (e) {
        toast(e.message, { error: true });
      }
    },
    signal,
  );
}
