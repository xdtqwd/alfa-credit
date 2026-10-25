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
