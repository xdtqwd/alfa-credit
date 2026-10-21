// Шапка: одна для всего сайта, два режима.
//   site    — разделы продуктовой страницы и «Услуги для бизнеса», справа «Войти» или аватар;
//   cabinet — вкладки кабинета «Профиль · Заявки · Кредиты · Сообщения», справа «Новый кредит» и аватар.
// «Услуги для бизнеса» — мега-меню как на alfabank.ru: панель во всю ширину, слева заголовок и ссылка
// на все услуги, справа колонки ссылок. Открывается наведением (и фокусом с клавиатуры) на чистом CSS.
// Только для компьютера — в меню-бургере телефона его нет.
// Меню профиля открывается по клику и закрывается кликом мимо, Esc или переходом.
import { esc, initials } from '../core/format.js';
import { currentPath, navigate } from '../core/router.js';
import { session, unreadCount } from '../core/session.js';
import { openAuth, logout } from '../features/auth.js';
import { copy, requisitesText, startApplication } from '../features/loan.js';
import { icon, logoSign } from './icons.js';

const SITE_LINKS = [
  ['products', 'Кредиты'],
  ['calc', 'Калькулятор'],
  ['how', 'Как получить'],
  ['faq', 'Вопросы'],
];
// Внешние ссылки на сайт Альфа-Банка — открываются в новой вкладке.
const ALFA_SME = 'https://alfabank.ru/sme/';
const A = {
  account: 'https://alfabank.ru/sme/raschetnyj-schet/ip/bystro/',
  acquiring: 'https://alfabank.ru/sme/payservice/ehkavajring-dlya-ooo/',
  paylinks: 'https://alfabank.ru/sme/payservice/internet-acquiring/servis-platezhnyh-ssylok/',
  credit: 'https://alfabank.ru/sme/profits/kredit-dlya-ooo/',
  creditcard: 'https://alfabank.ru/sme/profits/creditcard/',
  marketplace: 'https://alfabank.ru/sme/specialoffer/marketplace/',
  edo: 'https://alfabank.ru/sme/abp/services/contragents-docs/',
  changes: 'https://alfabank.ru/sme/abp/services/svizm/',
};
// Колонки мега-меню: [заголовок, ссылки [текст, адрес]].
const SERVICE_GROUPS = [
  [
    'Счёт и платежи',
    [
      ['Расчётный счёт', A.account],
      ['Эквайринг', A.acquiring],
      ['Платёжные ссылки', A.paylinks],
    ],
  ],
  [
    'Финансирование',
    [
      ['Кредит для ООО', A.credit],
      ['Кредитная бизнес-карта', A.creditcard],
    ],
  ],
  [
    'Сервисы',
    [
      ['Селлерам маркетплейсов', A.marketplace],
      ['Документооборот (ЭДО)', A.edo],
      ['Изменение данных ИП и ООО', A.changes],
    ],
  ],
];
const CABINET_TABS = [
  ['profile', '/cabinet', 'Профиль'],
  ['applications', '/cabinet/applications', 'Заявки'],
  ['credits', '/cabinet/credits', 'Кредиты'],
  ['messages', '/cabinet/messages', 'Сообщения'],
];

let el = null;
let view = { area: 'site', tab: '', platinum: false };
let menuOpen = false;
let signature = '';

function model() {
  const me = session.me;
  return {
    ...view,
    me: me && { name: me.full_name, position: me.position, company: me.company.name, inn: me.company.inn },
    unread: unreadCount(),
    menuOpen,
  };
}

function navHtml(m) {
  if (m.area === 'cabinet') {
    return CABINET_TABS.map(
      ([key, href, label]) => `
      <a class="nav__link ${m.tab === key ? 'is-active' : ''}" href="#${href}" ${m.tab === key ? 'aria-current="page"' : ''}>${label}${key === 'messages' && m.unread ? `<span class="nav__badge">${m.unread}</span>` : ''}</a>`,
    ).join('');
  }
  return `${SITE_LINKS.map(([id, label]) => `<a class="nav__link" href="#/" data-section="${id}">${label}</a>`).join('')}
    <div class="nav__dd">
      <button class="nav__link nav__more" type="button" aria-haspopup="true" aria-controls="services">Услуги для бизнеса ${icon('chev', 20)}</button>
      ${servicesHtml()}
    </div>`;
}

const external = (href) => `href="${href}" target="_blank" rel="noopener noreferrer"`;

function servicesHtml() {
  return `<div class="services" id="services">
    <div class="container services__in">
      <div class="services__side">
        <span class="services__heading">Услуги для бизнеса</span>
        <a class="services__all" ${external(ALFA_SME)}>Все услуги для бизнеса ${icon('forward', 20)}</a>
      </div>
      <div class="services__cols">${SERVICE_GROUPS.map(
        ([head, links]) => `
        <div class="services__col"><span class="services__title">${head}</span>
          ${links.map(([label, href]) => `<a class="services__link" ${external(href)}>${label}</a>`).join('')}
        </div>`,
      ).join('')}
      </div>
    </div>
  </div>`;
}

function menuHtml(me) {
  return `
    <div class="menu" role="menu">
      <a class="menu__head" href="#/cabinet" role="menuitem" title="Профиль и реквизиты">
        <span class="avatar avatar--l">${esc(initials(me.name))}</span>
        <span><b>${esc(me.name)}</b><small>${esc(me.position)}</small><small>${esc(me.company)} · ИНН ${esc(me.inn)}</small></span>
      </a>
      <div class="menu__sep"></div>
      <a class="menu__item" href="#/cabinet/applications" role="menuitem">${icon('doc', 20)} Заявки</a>
      <a class="menu__item" href="#/cabinet/credits" role="menuitem">${icon('wallet', 20)} Кредиты</a>
      <button class="menu__item" data-header="copy-requisites" role="menuitem">${icon('copy', 20)} Скопировать реквизиты</button>
      <div class="menu__sep"></div>
      <button class="menu__item" data-header="logout" role="menuitem">${icon('out', 20)} Выйти</button>
    </div>`;
}

function rightHtml(m) {
  if (!m.me) return `<button class="btn btn--primary btn--sm" data-header="login">Войти</button>`;
  return `
    ${m.area === 'cabinet' ? `<button class="btn btn--primary btn--sm" data-header="new-loan">${icon('plus', 20)}<span class="hide-s">Новый кредит</span></button>` : ''}
    <div class="menu-wrap">
      <button class="avatar-btn" data-header="menu" aria-haspopup="menu" aria-expanded="${m.menuOpen}" aria-label="Меню профиля">
        <span class="avatar">${esc(initials(m.me.name))}</span>
      </button>
      ${m.menuOpen ? menuHtml(m.me) : ''}
    </div>`;
}

/** Перерисовывает шапку, только если что-то в ней изменилось (фоновый опрос не трогает открытое меню). */
export function renderHeader(next = {}) {
  view = { ...view, ...next };
  if (!session.me) menuOpen = false;
  const m = model();
  const sig = JSON.stringify(m);
  if (sig === signature) return;
  signature = sig;
  el.className = `header header--${m.area}${m.platinum ? ' header--platinum' : ''}`;
  el.innerHTML = `
    <div class="container header__in">
      <a class="logo" href="#/" aria-label="Альфа Бизнес — на главную">${logoSign()}</a>
      <nav class="nav" aria-label="${m.area === 'cabinet' ? 'Разделы кабинета' : 'Разделы страницы'}">${navHtml(m)}</nav>
      <div class="header__right">
        ${rightHtml(m)}
        ${m.area === 'site' ? `<button class="header__burger" data-header="burger" aria-label="Разделы">${icon('menu', 24)}</button>` : ''}
      </div>
    </div>`;
}

const setMenu = (open) => {
  if (menuOpen === open) return;
  menuOpen = open;
  renderHeader();
};

export function mountHeader(root) {
  el = root;
  renderHeader();

  el.addEventListener('click', async (e) => {
    if (e.target.closest('.logo') && currentPath() === '/') {
      // уже на главной — просто наверх
      e.preventDefault();
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    const section = e.target.closest('[data-section]');
    if (section) {
      e.preventDefault();
      el.classList.remove('is-open');
      navigate('/', { scroll: section.dataset.section });
      return;
    }
    const action = e.target.closest('[data-header]')?.dataset.header;
    if (action === 'menu') return setMenu(!menuOpen);
    if (action === 'burger') return el.classList.toggle('is-open');
    if (action === 'login') {
      if (await openAuth()) navigate('/cabinet');
      return;
    }
    if (action === 'new-loan') return startApplication('business-dev');
    if (action === 'copy-requisites') {
      setMenu(false);
      return copy(requisitesText(), 'Реквизиты скопированы');
    }
    if (action === 'logout') {
      setMenu(false);
      await logout();
      navigate('/');
      return;
    }
    if (e.target.closest('.menu a')) setMenu(false);
    if (e.target.closest('.services a')) {
      document.activeElement?.blur();
      el.classList.remove('is-open');
    } // переход по пункту меню; клик по пустому месту меню его не закрывает
  });

  document.addEventListener('click', (e) => {
    if (menuOpen && !e.target.closest('.menu-wrap')) setMenu(false);
    if (el.classList.contains('is-open') && !el.contains(e.target)) el.classList.remove('is-open');
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      setMenu(false);
      if (el.contains(document.activeElement)) document.activeElement.blur();
    }
  });
  window.addEventListener('hashchange', () => {
    menuOpen = false;
    el.classList.remove('is-open');
  });
}
