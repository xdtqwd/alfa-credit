// Блоки решения банка: условия, сравнение «было / стало», причины и разбор балла.
// Показываются и в предварительном расчёте (1-й уровень), и на странице заявки.
import { api } from '../core/api.js';
import { esc, rub, months, rate } from '../core/format.js';
import { accordion } from '../ui/accordion.js';

export const offer = (d) => `
  <dl class="offer">
    <div><dt>Сумма</dt><dd>${rub(d.amount)}</dd></div>
    <div><dt>Срок</dt><dd>${months(d.term_months)}</dd></div>
    <div><dt>Ставка</dt><dd>${rate(d.rate)}</dd></div>
    <div><dt>Платёж в месяц</dt><dd>${rub(d.monthly_payment)}</dd></div>
    <div><dt>Переплата</dt><dd>${rub(d.overpayment)}</dd></div>
  </dl>`;

/** «Вы просили / Банк предлагает / Разница» — все цифры с сервера. heads — подписи колонок. */
export function compare(req, off, heads = ['Вы просили', 'Банк предлагает']) {
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
    <thead><tr><th><span class="sr-only">Параметр</span></th><th>${heads[0]}</th><th>${heads[1]}</th><th>Разница</th></tr></thead>
    <tbody>${rows
      .map(
        ([label, key, f, better]) => `
      <tr class="${off[key] !== req[key] ? 'is-changed' : ''}"><td>${label}</td><td>${f(req[key])}</td><td>${f(off[key])}</td><td>${delta(key, better)}</td></tr>`,
      )
      .join('')}
    </tbody></table></div>`;
}

export const reasons = (title, list) =>
  list?.length ? `<h3>${title}</h3><ul class="reasons">${list.map((r) => `<li>${esc(r)}</li>`).join('')}</ul>` : '';

// шкала балльной модели — пороги с сервера
let SCALE = { base: 600, reject_below: 500, approve_from: 650, max_dti_percent: 50 };
api('GET', '/scoring/scale')
  .then((s) => {
    SCALE = s;
  })
  .catch(() => {});

/**
 * «Как банк оценил заявку»: шкала с зонами и факторы с плюсами и минусами.
 * a — заявка или предварительный расчёт (score, factors); title — заголовок блока.
 * zones: false — без шкалы порогов (предварительный расчёт: без кредитной истории пороги не применяются).
 */
export function scoreBlock(a, title = 'Как банк оценил заявку', { zones = true } = {}) {
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
  const list = (total) => `
    <ul class="factors">
      <li><span class="factor__pts">${SCALE.base}</span><div><b>Стартовый балл</b><small>С него начинается оценка любой заявки.</small></div></li>
      ${factors.map((f) => `<li class="${f.impact >= 0 ? 'factor--plus' : 'factor--minus'}"><span class="factor__pts">${f.impact > 0 ? '+' : '−'}${Math.abs(f.impact)}</span><div><b>${esc(f.name)}</b><small>${esc(f.explain)}</small></div></li>`).join('')}
      <li class="factor--total"><span class="factor__pts">${a.score}</span><div><b>Итоговый балл</b><small>${total}</small></div></li>
    </ul>`;
  if (!zones)
    return accordion({
      key: `score-${a.id || 'prescore'}`,
      open: true,
      cls: 'score',
      header: `${title} <span class="status">${a.score} баллов</span>`,
      body: `<p class="calc__note">Без кредитной истории балл неполный, поэтому решение пока по посильности платежа: вместе с текущими — не больше ${SCALE.max_dti_percent}% месячной выручки. Пороги балла применит полный скоринг.</p>${list('без кредитной истории')}`,
    });
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
    ${list(zone[0])}`;
  return accordion({
    key: `score-${a.id || 'prescore'}`,
    open: true,
    cls: 'score',
    header: `${title} <span class="status status--${zone[1]}">${a.score} баллов · ${zone[0]}</span>`,
    body,
  });
}
