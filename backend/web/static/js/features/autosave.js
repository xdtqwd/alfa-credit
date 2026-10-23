// Автосохранение анкеты (и быстрой, и полной): каждое поле уходит PATCH-ем через полсекунды после ввода.
// Индикатор честный: «Сохраняем…», «Сохранено» или ошибка с повтором. Несохранённые поля не теряются
// и досылаются сами; отправить анкету с несохранёнными правками нельзя (settle).
import { api } from '../core/api.js';
import { $, debounce } from '../core/dom.js';
import { esc } from '../core/format.js';

/**
 * id — заявка; stateEl — элемент индикатора; onSaved(fresh) — ответ сервера после сохранения;
 * onSettled() — сохранение закончилось (успехом или ошибкой) — перерисовать сводку.
 */
export function createAutosave({ id, root, signal, onSaved = () => {}, onSettled = () => {} }) {
  const queue = {};
  let saving = false;
  let saveError = null;
  let retryTimer = 0;
  const pending = () => Object.keys(queue).length > 0;
  signal.addEventListener('abort', () => clearTimeout(retryTimer), { once: true });

  const paint = () => {
    const el = $('#save-state', root);
    if (!el) return;
    const state = saving || (pending() && !saveError) ? 'saving' : saveError ? 'error' : 'ok';
    el.classList.toggle('is-error', state === 'error');
    el.innerHTML =
      state === 'saving'
        ? 'Сохраняем…'
        : state === 'error'
          ? `Не сохранено: ${esc(saveError)} · <button type="button" data-retry>Повторить</button>`
          : 'Сохранено';
  };

  async function flush() {
    clearTimeout(retryTimer);
    if (!pending() || saving || signal.aborted) return;
    const patch = { ...queue };
    Object.keys(queue).forEach((k) => delete queue[k]);
    saving = true;
    paint();
    try {
      onSaved(await api('PATCH', `/applications/${id}`, patch));
      saveError = null;
    } catch (e) {
      // несохранённые поля возвращаются в очередь, не затирая более свежие правки
      for (const [k, v] of Object.entries(patch)) if (!(k in queue)) queue[k] = v;
      saveError = e.status === 401 ? 'сессия истекла, войдите снова' : e.message;
      if (e.status !== 401 && e.status !== 422 && !signal.aborted) retryTimer = setTimeout(flush, 4000);
    } finally {
      saving = false;
      if (!signal.aborted) {
        paint();
        onSettled();
        if (pending() && !saveError) flush();
      }
    }
  }
  const flushSoon = debounce(flush, 500);
  signal.addEventListener('abort', () => flushSoon.cancel(), { once: true });

  root.addEventListener(
    'click',
    (e) => {
      if (!e.target.closest('[data-retry]')) return;
      saveError = null;
      paint();
      flush();
    },
    { signal },
  );

  return {
    save(patch) {
      Object.assign(queue, patch);
      paint();
      flushSoon();
    },
    /** Досохранить всё перед отправкой; если не вышло — ошибка с понятным текстом. */
    async settle() {
      while (saving) await new Promise((r) => setTimeout(r, 100));
      flushSoon.cancel();
      await flush();
      if (saveError) throw new Error(`Анкета не сохранена (${saveError}). Проверьте связь и нажмите «Повторить».`);
    },
  };
}
