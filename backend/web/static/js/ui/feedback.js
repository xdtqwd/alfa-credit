// Обратная связь: всплывающие уведомления и модальные окна.
import { $ } from '../core/dom.js';
import { esc } from '../core/format.js';
import { icon } from './icons.js';

/** Уведомление внизу справа. link — { href, label } для перехода (например, к заявке). */
export function toast(message, { error = false, link = null, ms = 4000 } = {}) {
  const box = $('#toasts');
  const el = document.createElement('div');
  el.className = `toast${error ? ' toast--error' : ''}`;
  el.setAttribute('role', error ? 'alert' : 'status');
  el.innerHTML = `<span>${esc(message)}</span>${link ? `<a href="#${esc(link.href)}">${esc(link.label)}</a>` : ''}`;
  box.append(el);
  [...box.children].slice(0, -3).forEach((x) => x.remove());
  setTimeout(() => el.remove(), ms);
}

let current = null;

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select, textarea, [tabindex]:not([tabindex="-1"])';

/** Tab по кругу внутри окна: с последнего элемента — на первый и обратно. */
function keepFocusInside(e, box) {
  const items = [...box.querySelectorAll(FOCUSABLE)].filter((x) => x.offsetParent !== null);
  if (!items.length) return;
  const [first, last] = [items[0], items[items.length - 1]];
  if (e.shiftKey && (document.activeElement === first || !box.contains(document.activeElement))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}

/**
 * Модальное окно. Закрывается крестиком, кликом по фону и Esc.
 * Возвращает { el, close }; onClose вызывается при любом закрытии.
 * Имя окна для экранных дикторов — label или первый заголовок h2 внутри.
 * Фокус не уходит из окна по Tab и после закрытия возвращается туда, откуда окно открыли.
 */
export function openModal(html, { onClose, label = '' } = {}) {
  closeModal();
  const opener = document.activeElement;
  const root = $('#modal-root');
  root.innerHTML = `
    <div class="modal-bg" data-modal-bg>
      <div class="modal" role="dialog" aria-modal="true" ${label ? `aria-label="${esc(label)}"` : 'aria-labelledby="modal-title"'}>
        <button class="icon-btn modal__close" data-modal-close aria-label="Закрыть">${icon('cross', 20)}</button>
        ${html}
      </div>
    </div>`;
  if (!label) $('.modal h2', root)?.setAttribute('id', 'modal-title');
  document.body.classList.add('has-modal');
  const ac = new AbortController();
  const close = () => {
    if (current !== handle) return;
    current = null;
    ac.abort();
    root.innerHTML = '';
    document.body.classList.remove('has-modal');
    if (opener?.isConnected) opener.focus();
    onClose?.();
  };
  root.addEventListener(
    'click',
    (e) => {
      if (e.target.matches('[data-modal-bg]') || e.target.closest('[data-modal-close]')) close();
    },
    { signal: ac.signal },
  );
  document.addEventListener(
    'keydown',
    (e) => {
      if (e.key === 'Escape') close();
      if (e.key === 'Tab') keepFocusInside(e, handle.el);
    },
    { signal: ac.signal },
  );
  const handle = { el: $('.modal', root), close };
  current = handle;
  return handle;
}

export function closeModal() {
  current?.close();
}
