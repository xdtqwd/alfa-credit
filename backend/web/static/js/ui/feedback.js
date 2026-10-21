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

/**
 * Модальное окно. Закрывается крестиком, кликом по фону и Esc.
 * Возвращает { el, close }; onClose вызывается при любом закрытии.
 */
export function openModal(html, { onClose } = {}) {
  closeModal();
  const root = $('#modal-root');
  root.innerHTML = `
    <div class="modal-bg" data-modal-bg>
      <div class="modal" role="dialog" aria-modal="true">
        <button class="icon-btn modal__close" data-modal-close aria-label="Закрыть">${icon('cross', 20)}</button>
        ${html}
      </div>
    </div>`;
  document.body.classList.add('has-modal');
  const ac = new AbortController();
  const close = () => {
    if (current !== handle) return;
    current = null;
    ac.abort();
    root.innerHTML = '';
    document.body.classList.remove('has-modal');
    onClose?.();
  };
  root.addEventListener('click', (e) => {
    if (e.target.matches('[data-modal-bg]') || e.target.closest('[data-modal-close]')) close();
  }, { signal: ac.signal });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); }, { signal: ac.signal });
  const handle = { el: $('.modal', root), close };
  current = handle;
  return handle;
}

export function closeModal() {
  current?.close();
}
