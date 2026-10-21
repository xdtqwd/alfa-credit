// Кабинет → «Заявки»: все заявки компании с фильтрами.
import { on, storage } from '../core/dom.js';
import { esc, dateTime } from '../core/format.js';
import { session, product, subscribe } from '../core/session.js';
import { todoFor, appPath, appSummary, statusBadge, FINAL } from '../domain.js';
import { startApplication } from '../features/loan.js';
import { pageHead, empty } from './parts.js';

const FILTERS = [
  ['all', 'Все', () => true],
  ['action', 'Нужно действие', (a) => todoFor(a).need && !FINAL.includes(a.status)],
  ['work', 'В работе', (a) => !FINAL.includes(a.status) && !todoFor(a).need],
  ['done', 'Завершены', (a) => FINAL.includes(a.status)],
];

const item = (a) => {
  const t = todoFor(a);
  return `
    <a class="list-item" href="#${appPath(a)}" data-app="${esc(a.number)}">
      <div class="list-item__main">
        <span class="list-item__title">${esc(a.number)} · ${esc(appSummary(a, product(a.product_id)))}</span>
        <span class="list-item__meta ${t.need && !FINAL.includes(a.status) ? 'is-need' : ''}">${esc(t.title)}</span>
      </div>
      <div class="list-item__side">${statusBadge(a.status)}<span class="xsmall muted">${dateTime(a.updated_at)}</span></div>
    </a>`;
};

export default async function applications(root, { signal }) {
  root.className = 'page';
  let filter = storage.get('apps_filter', 'all');

  const draw = () => {
    const apps = session.apps;
    const [, , test] = FILTERS.find(([k]) => k === filter) || FILTERS[0];
    const list = apps.filter(test);
    root.innerHTML = `<div class="container">
      ${pageHead('Заявки', 'Все заявки вашей компании и что по каждой нужно сделать')}
      <section class="card">
        <nav class="tabs" aria-label="Фильтр заявок">${FILTERS.map(
          ([k, label, f]) => `
          <button class="tab ${k === filter ? 'is-on' : ''}" data-filter="${k}">${label}<span class="tab__count">${apps.filter(f).length}</span></button>`,
        ).join('')}
        </nav>
        ${
          list.length
            ? `<div class="list">${list.map(item).join('')}</div>`
            : empty(
                filter === 'all' ? 'Заявок пока нет' : 'Здесь пусто',
                filter === 'all'
                  ? 'Рассчитайте кредит и сразу заполните анкету — это займёт пару минут.'
                  : 'В этом разделе сейчас нет заявок.',
                filter === 'all' ? '<button class="btn btn--primary" data-new-loan>Оформить кредит</button>' : '',
              )
        }
      </section>
    </div>`;
  };
  draw();

  const unsubscribe = subscribe(draw);
  signal.addEventListener('abort', unsubscribe, { once: true });
  on(
    root,
    'click',
    '[data-filter]',
    (_, el) => {
      filter = el.dataset.filter;
      storage.set('apps_filter', filter);
      draw();
    },
    signal,
  );
  on(root, 'click', '[data-new-loan]', () => startApplication('business-dev'), signal);
}
