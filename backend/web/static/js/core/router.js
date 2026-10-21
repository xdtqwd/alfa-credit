// Маршрутизация по hash: #/путь. Каждый экран получает свой корень и AbortSignal —
// при уходе со страницы снимаются его обработчики и таймеры, поздние ответы сервера игнорируются.

const routes = [];
const redirects = [];

/** Регистрирует экран. path вида '/cabinet/applications/:id'. */
export function route(path, page, meta = {}) {
  const keys = [];
  const re = new RegExp(
    `^${path.replace(/:(\w+)/g, (_, k) => {
      keys.push(k);
      return '([^/]+)';
    })}/?$`,
  );
  routes.push({ re, keys, page, meta });
}

/** Старые адреса ведут на новые — ссылки из закладок и писем не ломаются. */
export function redirect(from, to) {
  const re = new RegExp(`^${from.replace(/:(\w+)/g, '([^/]+)')}/?$`);
  redirects.push({ re, to });
}

export const currentPath = () => decodeURIComponent((location.hash || '#/').slice(1).split('?')[0]) || '/';

export function match(path) {
  for (const r of redirects) {
    const m = path.match(r.re);
    if (m) return match(r.to.replace(/\$(\d)/g, (_, i) => m[+i]));
  }
  for (const r of routes) {
    const m = path.match(r.re);
    if (m) return { path, page: r.page, meta: r.meta, params: Object.fromEntries(r.keys.map((k, i) => [k, m[i + 1]])) };
  }
  return null;
}

let pendingScroll = null;
/** Переход. scroll — id секции, к которой прокрутить после отрисовки. */
export function navigate(path, { scroll = null } = {}) {
  pendingScroll = scroll;
  if (currentPath() === path) window.dispatchEvent(new HashChangeEvent('hashchange'));
  else location.hash = `#${path}`;
}
export function takeScroll() {
  const s = pendingScroll;
  pendingScroll = null;
  return s;
}
