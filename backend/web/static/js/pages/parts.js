// Общие части страниц кабинета.
import { $, $$ } from '../core/dom.js';
import { esc } from '../core/format.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';

export const pageHead = (title, sub = '', actions = '') => `
  <div class="page-head">
    <div><h1>${esc(title)}</h1>${sub ? `<p class="page-head__sub">${sub}</p>` : ''}</div>
    ${actions ? `<div class="row row--wrap">${actions}</div>` : ''}
  </div>`;

export const backLink = (href, label) => `<a class="back" href="#${href}">${icon('back', 20)}${esc(label)}</a>`;

/** Прогресс заполнения анкеты в боковой сводке; list — пары [пункт, выполнен]. Возвращает число выполненных. */
export function paintProgress(root, list) {
  const done = list.filter(([, ok]) => ok).length;
  const pct = `${Math.round((done / list.length) * 100)}%`;
  $('#sum-progress', root).style.width = pct;
  $('#sum-pct', root).textContent = `Заполнено ${pct}`;
  $('#sum-todo', root).innerHTML = list.map(([t, ok]) => `<li class="${ok ? 'is-done' : ''}">${t}</li>`).join('');
  return done;
}

/** Ошибка отправки анкеты: уведомление и подсветка поля, на которое указал сервер. */
export function showFieldError(root, e) {
  toast(e.message, { error: true });
  $$('.field--error', root).forEach((x) => x.classList.remove('field--error'));
  const field = e.field && $(`[data-field="${e.field}"]`, root);
  if (field) {
    field.classList.add('field--error');
    field.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }
}

export const empty = (title, text, action = '') => `<div class="empty"><h3>${esc(title)}</h3><p>${text}</p>${action}</div>`;

/** Этапы заявки: шесть шагов от анкеты до денег на счёте. */
export function stepper(a, stages, stageOf, busy) {
  const idx = stageOf[a.status];
  return `<ol class="stepper">${stages
    .map((label, i) => {
      let cls = i < idx ? 'is-done' : i === idx ? 'is-current' : '';
      if (i === idx && ['issued', 'closed'].includes(a.status)) cls = 'is-done';
      if (i === idx && a.status === 'rejected') cls = 'is-failed';
      if (i === idx && busy) cls += ' is-busy';
      const mark = cls.startsWith('is-done') ? icon('check', 18) : cls === 'is-failed' ? icon('cross', 18) : i + 1;
      return `<li class="step ${cls}"><span class="step__dot">${mark}</span><span>${label}</span></li>`;
    })
    .join('')}</ol>`;
}

/** Два уровня анкеты: где клиент сейчас и что будет дальше. */
export function levels(current = 1) {
  const step = (n, title, text) =>
    `<li class="levels__item ${n === current ? 'is-current' : n < current ? 'is-done' : ''}"><span class="levels__n">${n < current ? icon('check', 16) : n}</span><div><b>${title}</b><small>${text}</small></div></li>`;
  return `<ol class="levels" aria-label="Уровни анкеты">
    ${step(1, 'Предварительный расчёт', 'Без входа: кредит, ИНН и выручка → решение за секунды')}
    ${step(2, 'Госуслуги и документы', 'Вход через Госуслуги, паспорт, обеспечение → окончательное решение')}
  </ol>`;
}

/** Серия и номер паспорта с закрытой серединой: «45 12 ••••78». */
export const maskPassport = (p) =>
  p?.series?.length === 4 && p?.number?.length === 6 ? `${p.series.slice(0, 2)} ${p.series.slice(2)} ••••${p.number.slice(4)}` : '—';
