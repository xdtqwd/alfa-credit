// Кабинет банка → «Профиль сотрудника»: служебная карточка, права в кабинете банка, контакты и вход.
// Права и должность ведёт банк (кадровая система) — сотрудник меняет только почту.
import { api } from '../core/api.js';
import { on } from '../core/dom.js';
import { esc, phone, dateTime, initials } from '../core/format.js';
import { session, setMe, subscribe } from '../core/session.js';
import { IN_PIPELINE, DECIDED } from '../domain.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { pageHead } from './parts.js';

// Права кабинета банка: [код, название, что именно открывает].
const PERMISSIONS = [
  ['applications.view', 'Очередь заявок и решения', 'Статусы, балл скоринга, итоги проверок ЕГРЮЛ, ФНС и БКИ'],
  ['audit.view', 'Журнал аудита по заявке', 'Кто и когда отправлял, проверял и менял заявку'],
  ['platform.view', 'Состояние платформы', 'Хранилище, шина событий, очередь outbox, кэши'],
];

const lastLogin = (me) => (me.last_login_at && !me.last_login_at.startsWith('0001') ? dateTime(me.last_login_at) : 'сейчас');

function accessCard(e) {
  const has = new Set(e.permissions || []);
  return `<section class="card">
    <div class="card__head"><h2>Доступ в кабинете банка</h2><span class="status status--info">Роль: сотрудник банка</span></div>
    <ul class="perms">
      ${PERMISSIONS.map(
        ([code, title, text]) => `
        <li class="perm ${has.has(code) ? 'is-on' : 'is-off'}">
          <span class="perm__mark">${icon(has.has(code) ? 'check' : 'cross', 18)}</span>
          <span><b>${title}</b><small>${text}</small></span>
          <span class="perm__state">${has.has(code) ? 'Есть' : 'Нет'}</span>
        </li>`,
      ).join('')}
      <li class="perm is-off">
        <span class="perm__mark">${icon('cross', 18)}</span>
        <span><b>Действия от имени клиента</b><small>Оформление, подписание и оплата — только у представителя компании</small></span>
        <span class="perm__state">Нет</span>
      </li>
    </ul>
    <p class="note">${icon('shield', 20)}<span>Права выдаёт администратор банка. Сервер проверяет их при каждом запросе — изменение действует сразу, без повторного входа.</span></p>
  </section>`;
}

/** Сводка по очереди — что ждёт сотрудника прямо сейчас. */
function queueCard(rows) {
  if (!rows) return '';
  const today = new Date().toDateString();
  const decidedToday = rows.filter((r) =>
    r.history.some((h) => DECIDED.includes(h.status) && new Date(h.at).toDateString() === today),
  ).length;
  const items = [
    ['Всего заявок', rows.length],
    ['В конвейере', rows.filter((r) => IN_PIPELINE.includes(r.status)).length],
    ['Ждут клиента', rows.filter((r) => r.status === 'need_info').length],
    ['Решений сегодня', decidedToday],
  ];
  return `<section class="card">
    <h2 class="card__title">Очередь сейчас</h2>
    <div class="staff-queue">${items.map(([label, value]) => `<div><span>${label}</span><b>${value}</b></div>`).join('')}</div>
    <a class="btn btn--primary btn--sm" href="#/bank">${icon('list', 20)} Открыть очередь заявок</a>
  </section>`;
}

export default async function staffProfile(root, { signal }) {
  root.className = 'page';
  let rows = null;

  const draw = () => {
    const me = session.me;
    if (!me) return;
    const e = me.employee || {};
    root.innerHTML = `<div class="container">
      ${pageHead('Профиль сотрудника', `${esc(me.full_name)} · ${esc(me.position)}`)}
      <div class="profile-layout">
        <div class="stack-l">
          <section class="card">
            <h2 class="card__title">Служебные данные</h2>
            <dl class="company-dl">
              <div class="wide"><dt>ФИО</dt><dd>${esc(me.full_name)}</dd></div>
              <div><dt>Должность</dt><dd>${esc(me.position)}</dd></div>
              <div><dt>Табельный номер</dt><dd>${esc(e.personnel_no || '—')}</dd></div>
              <div class="wide"><dt>Подразделение</dt><dd>${esc(e.department || '—')}</dd></div>
              <div class="wide"><dt>Офис</dt><dd>${esc(e.branch || '—')}</dd></div>
            </dl>
          </section>

          ${accessCard(e)}

          <section class="card">
            <h2 class="card__title">Контакты и вход</h2>
            <dl class="kv">
              <div><dt>Телефон — логин</dt><dd>${phone(me.phone)}</dd></div>
              <div><dt>Рабочая почта</dt><dd><input class="inline-input" id="staff-email" type="email" value="${esc(me.email)}" placeholder="добавить" aria-label="Рабочая почта"></dd></div>
              <div><dt>Последний вход</dt><dd>${lastLogin(me)}</dd></div>
            </dl>
            <p class="note">${icon('lock', 20)}<span>Сессия хранится в защищённой cookie. Должность и права меняет отдел кадров, не сотрудник.</span></p>
          </section>
        </div>

        <aside class="profile-side">
          <section class="account staff-badge" aria-label="Служебное удостоверение">
            <div class="staff-badge__top"><small>Альфа-Банк · сотрудник</small><small>№ ${esc(e.personnel_no || '—')}</small></div>
            <div class="staff-badge__who">
              <span class="avatar avatar--l avatar--staff">${esc(initials(me.full_name))}</span>
              <span><b>${esc(me.full_name)}</b><small>${esc(me.position)}</small></span>
            </div>
            <div><small>${esc(e.department || '')}</small><span class="foundation"></span></div>
          </section>
          ${queueCard(rows)}
        </aside>
      </div>
    </div>`;
  };
  draw();

  const unsubscribe = subscribe(() => {
    if (!root.contains(document.activeElement) || document.activeElement === document.body) draw();
  });
  signal.addEventListener('abort', unsubscribe, { once: true });

  on(
    root,
    'change',
    '#staff-email',
    async (_, el) => {
      try {
        setMe(await api('PATCH', '/me', { email: el.value }));
        toast('Почта сохранена');
      } catch (err) {
        toast(err.message, { error: true });
      }
    },
    signal,
  );

  // сводка по очереди — если у сотрудника есть право на неё; без права карточки просто нет
  if ((session.me.employee?.permissions || []).includes('applications.view')) {
    try {
      rows = await api('GET', '/bank/applications?limit=200');
      if (!signal.aborted) draw();
    } catch {
      /* профиль показываем и без сводки */
    }
  }
}
