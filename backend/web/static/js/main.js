// Точка входа: маршруты, шапка, проверка входа, уведомления о решениях банка.
import { setUnauthorizedHandler } from './core/api.js';
import { $, on } from './core/dom.js';
import { esc } from './core/format.js';
import { route, redirect, match, currentPath, navigate, takeScroll } from './core/router.js';
import { session, init, subscribe, signOut, startPolling, isStaff } from './core/session.js';
import { appPath, isPreliminary } from './domain.js';
import { closeModal, toast } from './ui/feedback.js';
import { mountHeader, renderHeader } from './ui/header.js';
import { icon } from './ui/icons.js';
import { openAuth } from './features/auth.js';
import landing from './pages/landing.js';
import profile from './pages/profile.js';
import applications from './pages/applications.js';
import application from './pages/application.js';
import credits from './pages/credits.js';
import messages from './pages/messages.js';
import bank from './pages/bank.js';
import staffProfile from './pages/staff-profile.js';

// ---------- маршруты ----------
route('/', landing, { area: 'site', platinum: true }); // главная — платиновая тема для предпринимателей
route('/cabinet', profile, { area: 'cabinet', tab: 'profile', auth: true });
route('/cabinet/applications', applications, { area: 'cabinet', tab: 'applications', auth: true });
route('/cabinet/applications/:id', application, { area: 'cabinet', tab: 'applications', auth: true });
route('/cabinet/credits', credits, { area: 'cabinet', tab: 'credits', auth: true });
route('/cabinet/messages', messages, { area: 'cabinet', tab: 'messages', auth: true });
// кабинет банка: сотрудник входит по телефону и паролю (или по служебному токену)
route('/bank', bank, { area: 'cabinet', tab: 'queue', staff: true });
route('/bank/profile', staffProfile, { area: 'cabinet', tab: 'profile', staff: true });

// старые адреса
redirect('/products', '/');
redirect('/profile', '/cabinet');
redirect('/apps', '/cabinet/applications');
redirect('/app/:id', '/cabinet/applications/$1');
redirect('/apply/:id', '/cabinet/applications/$1');
redirect('/credits', '/cabinet/credits');
redirect('/messages', '/cabinet/messages');

// ---------- отрисовка экрана ----------
let controller = null;

function freshRoot() {
  const old = $('#view');
  const root = document.createElement('main');
  root.id = 'view';
  old.replaceWith(root); // новый корень — обработчики прошлого экрана не доживут
  return root;
}

/** Кабинет закрыт без входа: объясняем и сразу предлагаем войти; после входа — тот же экран. */
function gate(root, signal) {
  root.className = 'page';
  root.innerHTML = `<div class="container"><section class="card empty">
    <h3>Войдите в кабинет компании</h3>
    <p>Заявки, кредиты, реквизиты и оплата доступны только после входа — так данные и деньги компании под защитой.</p>
    <button class="btn btn--primary" data-gate-login>${icon('lock', 20)} Войти</button>
  </section></div>`;
  const ask = async () => {
    if ((await openAuth({ reason: 'Кабинет компании доступен после входа.' })) && !signal.aborted) render();
  };
  on(root, 'click', '[data-gate-login]', ask, signal);
  ask();
}

async function render() {
  const path = currentPath();
  const m = match(path);
  if (!m) return navigate('/');
  if (m.path !== path) history.replaceState(null, '', `#${m.path}`);

  controller?.abort();
  controller = new AbortController();
  const { signal } = controller;
  // роли не смешиваются: сотрудник банка не открывает кабинет компании, а профиль сотрудника — только для сотрудника
  if (m.meta.auth && isStaff()) return navigate('/bank/profile');
  if (m.path === '/bank/profile' && !isStaff()) return navigate('/bank');

  closeModal();
  // гостю и клиенту на странице кабинета банка — обычная шапка сайта, а не вкладки чужого кабинета
  const area = m.meta.staff && !isStaff() ? 'site' : m.meta.area;
  renderHeader({ area, tab: m.meta.tab || '', platinum: !!m.meta.platinum });
  const root = freshRoot();
  const scroll = takeScroll();
  if (!scroll) window.scrollTo(0, 0);

  if (m.meta.auth && !session.me) return gate(root, signal);
  root.className = 'page';
  root.innerHTML = '<div class="container"><div class="skeleton"></div></div>';
  try {
    await m.page(root, { params: m.params, signal, scroll });
  } catch (e) {
    if (signal.aborted) return;
    root.innerHTML = `<div class="container"><section class="card empty">
      <h3>Не удалось загрузить страницу</h3><p>${esc(e.message)}</p><a class="btn btn--primary" href="#/">На главную</a></section></div>`;
  }
}

// ---------- уведомления о решениях банка ----------
const STATUS_NEWS = {
  approved: 'Кредит одобрен',
  counter_offer: 'Банк предлагает другие условия',
  need_info: 'Банку нужно уточнение',
  rejected: 'Отказ по заявке',
  issued: 'Деньги на расчётном счёте',
  closed: 'Кредит погашен',
};
function announce(changes) {
  for (const a of changes) {
    // предварительное решение зовёт к полной анкете, а не к подписанию
    const text = isPreliminary(a)
      ? `${a.status === 'approved' ? 'Предварительно одобрено' : 'Предварительно: другие условия'} — заполните полную анкету`
      : STATUS_NEWS[a.status];
    if (!text || currentPath() === appPath(a)) continue; // на странице заявки статус и так обновляется
    toast(`${text} · ${a.number}`, { link: { href: appPath(a), label: 'Открыть' }, ms: 6000 });
  }
}

// ---------- запуск ----------
async function boot() {
  mountHeader($('#header'));
  subscribe(() => renderHeader());
  setUnauthorizedHandler(() => {
    if (!session.me) return;
    signOut();
    toast('Сессия истекла, войдите снова', { error: true });
    const meta = match(currentPath())?.meta;
    if (meta?.auth || meta?.staff) render();
  });
  // ссылки на разделы главной из подвала
  on(document.body, 'click', '.footer [data-section]', (e, el) => {
    e.preventDefault();
    navigate('/', { scroll: el.dataset.section });
  });

  try {
    await init();
  } catch {
    $('#view').innerHTML =
      '<div class="container page"><section class="card empty"><h3>Сервис временно недоступен</h3><p>Обновите страницу через минуту.</p></section></div>';
    return;
  }
  startPolling(announce);
  window.addEventListener('hashchange', render);
  render();
}

boot();
