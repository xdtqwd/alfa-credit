// Клиент REST API /api/v1. Сессия — HttpOnly cookie, JS её не видит.

/**
 * Действия с деньгами и статусом заявки идут с Idempotency-Key: если связь оборвалась после отправки,
 * запрос повторяется с тем же ключом, и сервер не выполнит его второй раз (не будет второй заявки
 * или двойного платежа), а вернёт сохранённый ответ.
 */
const IDEMPOTENT = /^\/applications(\/[^/]+\/(submit|accept|sign|pay|cancel))?$/;

const newKey = () =>
  crypto.randomUUID
    ? crypto.randomUUID()
    : [...crypto.getRandomValues(new Uint8Array(16))].map((b) => b.toString(16).padStart(2, '0')).join('');

export class ApiError extends Error {
  constructor(message, { status = 0, field = '', code = '', offline = false } = {}) {
    super(message);
    Object.assign(this, { status, field, code, offline });
  }
}

let onUnauthorized = () => {};
/** Что делать, если сессия истекла посреди работы (выход из кабинета). */
export const setUnauthorizedHandler = (fn) => {
  onUnauthorized = fn;
};

export async function api(method, path, body, headers = {}) {
  if (navigator.onLine === false) throw new ApiError('Нет подключения к интернету', { offline: true });
  const idempotent = method === 'POST' && IDEMPOTENT.test(path);
  const hdrs = { 'Content-Type': 'application/json', ...headers };
  if (idempotent && !hdrs['Idempotency-Key']) hdrs['Idempotency-Key'] = newKey();

  const send = () =>
    fetch(`/api/v1${path}`, {
      method,
      credentials: 'same-origin',
      headers: hdrs,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  let res;
  try {
    res = await send();
  } catch {
    if (!idempotent) throw new ApiError('Нет связи с сервером', { offline: true });
    await new Promise((r) => setTimeout(r, 700)); // повтор безопасен: тот же Idempotency-Key
    try {
      res = await send();
    } catch {
      throw new ApiError('Нет связи с сервером', { offline: true });
    }
  }

  const data = res.status === 204 ? null : await res.json().catch(() => null);
  if (!res.ok) {
    const e = data?.error || {};
    // 401 по токену кабинета банка — не про сессию клиента, выходить из кабинета не нужно
    if (res.status === 401 && !path.startsWith('/auth/') && !hdrs['X-Bank-Token']) onUnauthorized();
    throw new ApiError(e.message || res.statusText, { status: res.status, field: e.field, code: e.code });
  }
  return data;
}

/** Расчёт платежа — только на сервере (единый источник). Одинаковые запросы не повторяются. */
const quotes = new Map();
export function quote(productId, amount, term) {
  const key = `${productId}|${amount}|${term}`;
  if (!quotes.has(key)) {
    if (quotes.size > 300) quotes.clear();
    quotes.set(
      key,
      api('POST', '/calc', { product_id: productId, amount, term_months: term }).catch((e) => {
        quotes.delete(key);
        throw e;
      }),
    );
  }
  return quotes.get(key);
}
