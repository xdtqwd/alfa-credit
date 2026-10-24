// Аккордеон по образцу @alfalab/core-components-accordion: заголовок + шеврон справа (свой значок — ctl),
// раскрытие анимацией высоты (CSS). Состояние переживает перерисовку экрана.
import { esc } from '../core/format.js';
import { icon } from './icons.js';

const state = new Map();
let seq = 0;

export function accordion({ header, body, open = false, key = '', cls = '', ctl = icon('chev', 20) }) {
  const id = `acc-${++seq}`;
  const isOpen = key && state.has(key) ? state.get(key) : open;
  return `
    <div class="acc ${cls} ${isOpen ? 'is-open' : ''}" ${key ? `data-acc-key="${esc(key)}"` : ''}>
      <button class="acc__head" type="button" aria-expanded="${isOpen}" aria-controls="${id}">
        <span class="acc__title">${header}</span><span class="acc__ctl">${ctl}</span>
      </button>
      <div class="acc__body" id="${id}" role="region"><div class="acc__inner"><div class="acc__content">${body}</div></div></div>
    </div>`;
}

document.addEventListener('click', (e) => {
  const head = e.target.closest('.acc__head');
  if (!head) return;
  const box = head.parentElement;
  const open = !box.classList.contains('is-open');
  box.classList.toggle('is-open', open);
  head.setAttribute('aria-expanded', String(open));
  if (box.dataset.accKey) state.set(box.dataset.accKey, open);
});
