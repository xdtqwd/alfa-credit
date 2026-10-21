// Состояние сессии: текущий пользователь, его заявки и справочник продуктов.
// Экраны подписываются на изменения; фоновое обновление идёт, пока клиент в кабинете.
import { api } from './api.js';
import { storage } from './dom.js';
import { bankEvents } from '../domain.js';

export const session = {
  me: null,
  apps: [],
  products: [],
};

const listeners = new Set();
/** Подписка на изменения сессии; возвращает функцию отписки. */
export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
const emit = (changes = []) => listeners.forEach((fn) => fn(changes));

export const product = (id) => session.products.find((p) => p.id === id);

export async function init() {
  const [products, me] = await Promise.all([api('GET', '/products'), api('GET', '/me').catch(() => null)]);
  session.products = products;
  session.me = me;
  if (me) await refresh({ quiet: true });
}

/**
 * Обновляет пользователя и заявки. Возвращает список изменений статусов,
 * чтобы показать уведомление («Кредит одобрен»).
 */
export async function refresh({ quiet = false } = {}) {
  if (!session.me) return [];
  const [me, apps] = await Promise.all([api('GET', '/me'), api('GET', '/applications')]);
  const before = new Map(session.apps.map((a) => [a.id, a.status]));
  const changes = quiet ? [] : apps.filter((a) => before.has(a.id) && before.get(a.id) !== a.status);
  session.me = me;
  session.apps = apps;
  emit(changes);
  return changes;
}

export function signIn(user) {
  session.me = user;
  session.apps = [];
  emit();
}

export function signOut() {
  session.me = null;
  session.apps = [];
  emit();
}

export function setMe(user) {
  session.me = user;
  emit();
}

/** Обновить одну заявку в кэше после действия (не дожидаясь фонового опроса). */
export function putApp(app) {
  const i = session.apps.findIndex((a) => a.id === app.id);
  if (i >= 0) session.apps[i] = app;
  else session.apps.unshift(app);
  emit();
}

// ---------- непрочитанные сообщения ----------
const seenKey = () => `messages_seen_${session.me?.id}`;
export const unreadCount = () => {
  if (!session.me) return 0;
  const seen = storage.get(seenKey(), 0);
  return bankEvents(session.apps).filter((e) => new Date(e.at).getTime() > seen).length;
};
export const lastSeen = () => storage.get(seenKey(), 0);
export function markMessagesSeen() {
  storage.set(seenKey(), Date.now());
  emit();
}

// ---------- фоновое обновление ----------
let timer = 0;
/** Опрос раз в 3 с, пока клиент вошёл и вкладка видна. Сервер отвечает 304, если ничего не изменилось. */
export function startPolling(onChanges) {
  clearInterval(timer);
  timer = setInterval(async () => {
    if (!session.me || document.visibilityState !== 'visible') return;
    try {
      const changes = await refresh();
      if (changes.length) onChanges(changes);
    } catch {
      /* сеть: повторим на следующем тике */
    }
  }, 3000);
}
