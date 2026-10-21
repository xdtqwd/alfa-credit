// Мелкие помощники для работы с DOM и временем жизни экранов.

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

/**
 * Делегирование: обработчик срабатывает для элементов, подходящих под selector внутри root.
 * signal снимает обработчик, когда экран закрывается.
 */
export function on(root, type, selector, handler, signal) {
  root.addEventListener(type, (e) => {
    const el = e.target.closest(selector);
    if (el && root.contains(el)) handler(e, el);
  }, { signal });
}

/** setInterval, который сам останавливается вместе с экраном. */
export function every(ms, fn, signal) {
  const id = setInterval(fn, ms);
  signal.addEventListener('abort', () => clearInterval(id), { once: true });
}

export function debounce(fn, ms) {
  let t;
  const d = (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
  d.cancel = () => clearTimeout(t);
  return d;
}

/** localStorage, который не падает в приватном режиме. Только для удобств (фильтры, «прочитано»). */
export const storage = {
  get(key, fallback) {
    try { const v = localStorage.getItem(key); return v ? JSON.parse(v) : fallback; } catch { return fallback; }
  },
  set(key, value) {
    try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* недоступно */ }
  },
};

export async function copyText(text) {
  await navigator.clipboard.writeText(text);
}
