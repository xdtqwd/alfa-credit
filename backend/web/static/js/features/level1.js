// 1-й уровень анкеты — общее для формы на главной и страницы «Предварительный расчёт»:
// черновик в браузере (заявки на сервере до входа нет), демо-список ИНН, результат расчёта
// и переход ко 2-му уровню через Госуслуги.
import { api } from '../core/api.js';
import { storage } from '../core/dom.js';
import { esc, rub } from '../core/format.js';
import { navigate } from '../core/router.js';
import { session, putApp, isStaff } from '../core/session.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { esiaLogin } from './esia.js';
import { offer, compare, reasons, scoreBlock } from './decision.js';

const KEY = 'apply';

/** Черновик 1-го уровня: кредит, компания, выручка. Общий для главной и страницы расчёта. */
export const loadDraft = () => ({
  product_id: null,
  amount: 0,
  term_months: 0,
  purpose: '',
  inn: '',
  revenue: 0,
  obligations: 0,
  ...storage.get(KEY, {}),
});
export const saveDraft = (d) => storage.set(KEY, d);

/** Компании для поля ИНН: демо-список и, если клиент вошёл, его компания. */
export async function companyOptions() {
  const list = await api('GET', '/demo/companies').catch(() => []);
  const me = session.me && !isStaff() ? session.me : null;
  if (me && !list.some((c) => c.inn === me.company.inn))
    list.unshift({ inn: me.company.inn, name: me.company.name, hint: 'ваша компания' });
  return list;
}

/** Пункт списка ИНН; withHint — с подсказкой, чем компания интересна для демонстрации. */
export const companyOption = (c, selected, withHint = true) =>
  `<option value="${esc(c.inn)}" ${c.inn === selected ? 'selected' : ''}>${esc(c.name)} · ИНН ${esc(c.inn)}${withHint && c.hint ? ` — ${esc(c.hint)}` : ''}</option>`;

/** Данные компании из реестра — под полем ИНН. */
export const companyLines = (c) =>
  `<b>${esc(c.name)}</b><span>ИНН ${esc(c.inn)} · ОГРН ${esc(c.ogrn)} · ${c.status === 'active' ? 'действует' : 'ликвидирована'}</span><span>${esc(c.address)}${c.director ? ` · руководитель ${esc(c.director)}` : ''}</span>`;

/** Подсказка под выручкой: цифра ФНС и кнопка «подставить» (data-use-fns). */
export const revenueHint = (c) =>
  c?.revenue_last_year
    ? `По данным ФНС за прошлый год: ${rub(c.revenue_last_year)} · <button type="button" class="btn--link" data-use-fns>подставить</button>`
    : '';

/** Тело запроса предварительного расчёта и создания заявки. */
export const level1Body = (d) => ({
  product_id: d.product_id,
  amount: d.amount,
  term_months: d.term_months,
  purpose: d.purpose,
  inn: d.inn,
  financials: { annual_revenue: d.revenue, monthly_obligations: d.obligations },
});

/** Клиент уже вошёл через Госуслуги и руководит этой компанией — окно ЕСИА второй раз не нужно. */
export const identified = (inn) => !!(session.me?.identity && session.me.company?.inn === inn);

const TITLES = {
  approved: ['ok', 'Предварительно одобрено'],
  counter_offer: ['warn', 'Предварительно: можем выдать на других условиях'],
  need_info: ['warn', 'Нужно уточнить данные'],
  rejected: ['bad', 'Сейчас не можем выдать кредит'],
};

/** Карточка результата предварительного расчёта с кнопкой перехода ко 2-му уровню (id="esia-go"). */
export function prescoreCard(result, inn) {
  const dec = result.decision;
  const [tone, title] = TITLES[dec.type];
  const passed = dec.type === 'approved' || dec.type === 'counter_offer';
  const terms =
    dec.type === 'counter_offer' && dec.requested
      ? `<p class="muted">Сравните, что вы просили и что предлагает банк:</p>${compare(dec.requested, dec)}`
      : dec.type === 'approved'
        ? offer(dec)
        : '';
  const llc = result.company?.type !== 'ИП';
  const next = passed
    ? `<div class="note note--box">${icon('info', 20)}<span><b>Дальше — 2-й уровень.</b> Вход через Госуслуги и документы: ${llc ? 'паспорт руководителя, ' : ''}обеспечение для суммы больше 5 млн ₽${llc ? ', протокол о крупной сделке — больше 25 млн ₽' : ''}. Затем полный скоринг с кредитной историей и окончательное решение.</span></div>
      <div class="actions"><button class="btn btn--esia" id="esia-go">${identified(inn) ? 'Продолжить — личность подтверждена' : 'Войти через Госуслуги и продолжить'}</button></div>`
    : '';
  return `<section class="card decision decision--${tone}" id="prescore-result" data-decision="${dec.type}">
    <h2>${title}</h2>${terms}${reasons('Почему так', dec.reasons)}${reasons(dec.type === 'need_info' ? 'Что сделать' : 'Что можно сделать', dec.advice)}${scoreBlock(result, 'Упрощённый скоринг', { zones: false })}${next}
  </section>`;
}

/** Начало 2-го уровня: вход через Госуслуги (если ещё не было) → заявка → анкета документов. */
export async function continueToLevel2(d) {
  if (isStaff()) {
    toast('Кредит оформляет представитель компании — вы вошли как сотрудник банка', { error: true });
    return;
  }
  try {
    if (!identified(d.inn) && !(await esiaLogin({ inn: d.inn }))) return;
    const a = await api('POST', '/applications', level1Body(d));
    putApp(a);
    navigate(`/cabinet/applications/${a.id}`);
  } catch (e) {
    toast(e.message, { error: true });
  }
}
