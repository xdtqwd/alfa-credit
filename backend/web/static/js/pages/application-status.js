// Заявка после отправки: что нужно от клиента, этапы, решение банка, проверки и история.
import { api } from '../core/api.js';
import { on, every, storage } from '../core/dom.js';
import { esc, rub, months, rate, time } from '../core/format.js';
import { navigate } from '../core/router.js';
import { product, putApp } from '../core/session.js';
import { STATUS, STAGES, STAGE_OF, IN_PIPELINE, DOING, todoFor, statusBadge } from '../domain.js';
import { toast } from '../ui/feedback.js';
import { accordion } from '../ui/accordion.js';
import { icon } from '../ui/icons.js';
import { signDialog, payDialog } from '../features/loan.js';
import { backLink, pageHead, stepper } from './parts.js';

// ---------- блоки решения ----------

const offer = (d) => `
  <dl class="offer">
    <div><dt>Сумма</dt><dd>${rub(d.amount)}</dd></div>
    <div><dt>Срок</dt><dd>${months(d.term_months)}</dd></div>
    <div><dt>Ставка</dt><dd>${rate(d.rate)}</dd></div>
    <div><dt>Платёж в месяц</dt><dd>${rub(d.monthly_payment)}</dd></div>
    <div><dt>Переплата</dt><dd>${rub(d.overpayment)}</dd></div>
  </dl>`;

// Главная кнопка («Принять», «Подписать», «Оплатить», «Исправить») — одна, в блоке «Что нужно от вас» сверху;
// в карточке решения остаются только объяснение и «Отказаться».

/** «Вы просили / Банк предлагает / Разница» — все цифры с сервера. */
function compare(req, off) {
  const rows = [
    ['Сумма', 'amount', rub, 'more'],
    ['Срок', 'term_months', months, ''],
    ['Ставка', 'rate', rate, 'less'],
    ['Платёж в месяц', 'monthly_payment', rub, 'less'],
    ['Переплата', 'overpayment', rub, 'less'],
    ['Всего вернуть', 'total', rub, 'less'],
  ];
  const delta = (key, better) => {
    const dv = off[key] - req[key];
    if (!dv) return '<span class="delta--same">без изменений</span>';
    const sign = dv > 0 ? '+' : '−';
    const abs = Math.abs(dv);
    const text =
      key === 'rate'
        ? `${sign}${String(abs.toFixed(1)).replace('.', ',')} п.п.`
        : key === 'term_months'
          ? `${sign}${abs} мес.`
          : `${sign}${rub(abs)}`;
    const good = better === 'less' ? dv < 0 : better === 'more' ? dv > 0 : null;
    return `<span class="${good === null ? '' : good ? 'delta--good' : 'delta--bad'}">${text}</span>`;
  };
  return `<div class="table-wrap"><table class="table compare">
    <thead><tr><th><span class="sr-only">Параметр</span></th><th>Вы просили</th><th>Банк предлагает</th><th>Разница</th></tr></thead>
    <tbody>${rows
      .map(
        ([label, key, f, better]) => `
      <tr class="${off[key] !== req[key] ? 'is-changed' : ''}"><td>${label}</td><td>${f(req[key])}</td><td>${f(off[key])}</td><td>${delta(key, better)}</td></tr>`,
      )
      .join('')}
    </tbody></table></div>`;
}

const reasons = (title, list) =>
  list?.length ? `<h3>${title}</h3><ul class="reasons">${list.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>` : '';

// шкала балльной модели — пороги с сервера
let SCALE = { base: 600, reject_below: 500, approve_from: 650, max_dti_percent: 50 };
api('GET', '/scoring/scale')
  .then((s) => {
    SCALE = s;
  })
  .catch(() => {});

/** «Как банк оценил заявку»: шкала с зонами и факторы с плюсами и минусами. */
function scoreBlock(a) {
  if (a.score == null || !a.factors?.length) return '';
  const [MIN, MAX] = [350, 850];
  const at = (v) => Math.max(0, Math.min(100, ((v - MIN) / (MAX - MIN)) * 100));
  const r = at(SCALE.reject_below);
  const g = at(SCALE.approve_from);
  const zone =
    a.score >= SCALE.approve_from
      ? ['зона одобрения', 'ok']
      : a.score >= SCALE.reject_below
        ? ['зона других условий', 'warn']
        : ['зона отказа', 'bad'];
  const factors = [...a.factors].sort((x, y) => y.impact - x.impact);
  const body = `
    <div class="scale" role="img" aria-label="Балл ${a.score}: отказ ниже ${SCALE.reject_below}, одобрение от ${SCALE.approve_from}">
      <span class="scale__mark" style="left:${at(a.score)}%"><b>${a.score}</b></span>
      <div class="scale__track"><i class="z-bad" style="width:${r}%"></i><i class="z-warn" style="width:${g - r}%"></i><i class="z-ok" style="width:${100 - g}%"></i></div>
      <div class="scale__legend">
        <span style="width:${r}%">Отказ<small>ниже ${SCALE.reject_below}</small></span>
        <span style="width:${g - r}%">Другие условия<small>${SCALE.reject_below}–${SCALE.approve_from - 1}</small></span>
        <span style="width:${100 - g}%">Одобрение<small>от ${SCALE.approve_from}*</small></span>
      </div>
    </div>
    <p class="calc__note">* Для одобрения нужен ещё и посильный платёж: вместе с текущими — не больше ${SCALE.max_dti_percent}% месячной выручки.</p>
    <ul class="factors">
      <li><span class="factor__pts">${SCALE.base}</span><div><b>Стартовый балл</b><small>С него начинается оценка любой заявки.</small></div></li>
      ${factors.map((f) => `<li class="${f.impact >= 0 ? 'factor--plus' : 'factor--minus'}"><span class="factor__pts">${f.impact > 0 ? '+' : '−'}${Math.abs(f.impact)}</span><div><b>${esc(f.name)}</b><small>${esc(f.explain)}</small></div></li>`).join('')}
      <li class="factor--total"><span class="factor__pts">${a.score}</span><div><b>Итоговый балл</b><small>${zone[0]}</small></div></li>
    </ul>`;
  return accordion({
    key: `score-${a.id}`,
    open: true,
    cls: 'score',
    header: `Как банк оценил заявку <span class="status status--${zone[1]}">${a.score} баллов · ${zone[0]}</span>`,
    body,
  });
}

function decisionCard(a) {
  const d = a.decision;
  const card = (tone, title, body) =>
    `<section class="card decision decision--${tone}" id="decision" data-decision="${a.status}"><h2>${title}</h2>${body}</section>`;
  const cancel = '<button class="btn btn--ghost" data-action="cancel">Отказаться</button>';
  switch (a.status) {
    case 'submitted':
    case 'data_check':
    case 'scoring':
      return card(
        'info',
        'Рассматриваем заявку',
        `<p class="muted">Вы запросили ${rub(a.amount)} на ${months(a.term_months)}${a.quote ? ` — ориентировочный платёж ${rub(a.quote.monthly_payment)} в месяц` : ''}. Статус обновится сам.</p>`,
      );
    case 'approved':
      return card(
        'ok',
        'Кредит одобрен',
        `${d.requested && d.requested.rate !== d.rate ? compare(d.requested, d) : offer(d)}${reasons('Почему так', d.reasons)}${scoreBlock(a)}
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
      return card('warn', 'Нужно уточнить данные', `${reasons('Почему так', d.reasons)}${reasons('Что сделать', d.advice)}`);
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
        '<p class="muted">Новую заявку можно оформить за пару минут — данные компании подтянутся снова.</p><div class="actions"><a class="btn btn--primary" href="#/">Оформить кредит</a></div>',
      );
    default:
      return '';
  }
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
  const sent = a.history.find((h) => h.status === 'submitted');
  const decided = a.history.find((h) => ['approved', 'counter_offer', 'rejected', 'need_info'].includes(h.status));
  const secs = sent && decided ? Math.round((new Date(decided.at) - new Date(sent.at)) / 1000) : null;
  return `<div class="container stack-l">
    <div>
      ${backLink('/cabinet/applications', 'Все заявки')}
      ${pageHead(`Заявка ${a.number}`, `${esc(p?.name)} · ${rub(a.amount)} на ${months(a.term_months)}${a.company ? ` · ${esc(a.company.name)}` : ''}`, statusBadge(a.status))}
    </div>
    ${todoNotice(a)}
    <section class="card stepper-box">
      <div class="card__head"><h2>Прогресс заявки</h2>${secs !== null ? `<span class="small muted">Решение принято за ${secs} с</span>` : ''}</div>
      ${stepper(a, STAGES, STAGE_OF, busy)}
      ${busy ? `<p class="note">${icon('clock', 20)}<span>${DOING[a.status]}</span></p>` : ''}
    </section>
    <div class="app-layout">
      <div class="stack-l">
        ${decisionCard(a)}
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
          .map((h) => `<li><time>${time(h.at)}</time><b>${STATUS[h.status][0]}</b><span>${esc(h.comment)}</span></li>`)
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
          draw(await api('POST', `/applications/${a.id}/accept`));
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
        if (action === 'retry') {
          storage.set('calc', { product: a.product_id, amount: Math.round(a.amount / 2 / 10000) * 10000, term: a.term_months });
          navigate('/', { scroll: 'calc' });
        }
      } catch (e) {
        toast(e.message, { error: true });
      }
    },
    signal,
  );
}
