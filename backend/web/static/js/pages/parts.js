// Общие части страниц кабинета.
import { esc } from '../core/format.js';
import { icon } from '../ui/icons.js';

export const pageHead = (title, sub = '', actions = '') => `
  <div class="page-head">
    <div><h1>${esc(title)}</h1>${sub ? `<p class="page-head__sub">${sub}</p>` : ''}</div>
    ${actions ? `<div class="row row--wrap">${actions}</div>` : ''}
  </div>`;

export const backLink = (href, label) => `<a class="back" href="#${href}">${icon('back', 20)}${esc(label)}</a>`;

export const empty = (title, text, action = '') => `<div class="empty"><h3>${esc(title)}</h3><p>${text}</p>${action}</div>`;

/** Этапы заявки: шесть шагов от анкеты до денег на счёте. */
export function stepper(a, stages, stageOf, busy) {
  const idx = stageOf[a.status];
  return `<ol class="stepper">${stages.map((label, i) => {
    let cls = i < idx ? 'is-done' : i === idx ? 'is-current' : '';
    if (i === idx && ['issued', 'closed'].includes(a.status)) cls = 'is-done';
    if (i === idx && a.status === 'rejected') cls = 'is-failed';
    if (i === idx && busy) cls += ' is-busy';
    const mark = cls.startsWith('is-done') ? icon('check', 18) : cls === 'is-failed' ? icon('cross', 18) : i + 1;
    return `<li class="step ${cls}"><span class="step__dot">${mark}</span><span>${label}</span></li>`;
  }).join('')}</ol>`;
}
