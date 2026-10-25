// Вход, регистрация по ИНН и выход. Без входа нельзя оформить, подписать или оплатить.
import { api } from '../core/api.js';
import { $, $$, debounce } from '../core/dom.js';
import { esc, phone, digits } from '../core/format.js';
import { navigate } from '../core/router.js';
import { signIn, signOut, refresh, isStaff } from '../core/session.js';
import { openModal, toast } from '../ui/feedback.js';
import { icon, logoSign } from '../ui/icons.js';

const DEMO_PASSWORD = 'demo1234';
const DEMO_ACCOUNTS = [
  ['79000000001', 'ООО «Северный ветер»', 'одобрение'],
  ['79000000002', 'ООО «Алтайские сыроварни»', 'другие условия от 15 млн'],
  ['79000000003', 'ООО «Сибирский кран»', 'отказ: просрочки'],
  ['79000000004', 'ООО «Закрытый горизонт»', 'отказ: ликвидирована'],
];
// Сотрудник банка — последним: другой кабинет, другая роль.
const DEMO_STAFF = ['79000000005', 'Соколова Анна Дмитриевна', 'кредитный аналитик · кабинет банка'];

const demoItem = ([ph, name, hint], cls = '') => `
      <button type="button" class="demo__item ${cls}" data-demo="${ph}"><b>${esc(name)}</b><small>${phone(ph)} · ${esc(hint)}</small></button>`;

const loginForm = () => `
  <form id="auth-form" class="stack" novalidate>
    <div class="field" data-field="phone"><label for="auth-phone">Телефон</label>
      <input class="input" id="auth-phone" inputmode="tel" autocomplete="username" placeholder="+7 (900) 000-00-00" required></div>
    <div class="field" data-field="password"><label for="auth-password">Пароль</label>
      <input class="input" id="auth-password" type="password" autocomplete="current-password" required></div>
    <p class="field__hint" id="auth-error" role="alert"></p>
    <button class="btn btn--primary btn--block" type="submit">Войти</button>
  </form>
  <div class="demo">
    <p class="small muted">Демо-аккаунты, пароль <code>${DEMO_PASSWORD}</code></p>
    ${DEMO_ACCOUNTS.map((a) => demoItem(a)).join('')}
    <p class="small muted demo__group">Сотрудник банка</p>
    ${demoItem(DEMO_STAFF, 'demo__item--staff')}
  </div>`;

const registerForm = () => `
  <form id="auth-form" class="stack" novalidate>
    <div class="field" data-field="inn"><label for="reg-inn">ИНН компании или ИП</label>
      <input class="input" id="reg-inn" inputmode="numeric" maxlength="12" placeholder="10 или 12 цифр" required>
      <span class="field__hint" id="reg-company">Компанию, реквизиты и руководителя подтянем из ЕГРЮЛ</span></div>
    <div class="field" data-field="full_name"><label for="reg-name">ФИО представителя</label>
      <input class="input" id="reg-name" autocomplete="name" required></div>
    <div class="field" data-field="phone"><label for="reg-phone">Телефон — это ваш логин</label>
      <input class="input" id="reg-phone" inputmode="tel" autocomplete="username" placeholder="+7 (900) 000-00-00" required></div>
    <div class="field" data-field="password"><label for="reg-password">Пароль</label>
      <input class="input" id="reg-password" type="password" autocomplete="new-password" minlength="8" required>
      <span class="field__hint">Не короче 8 символов</span></div>
    <p class="field__hint" id="auth-error" role="alert"></p>
    <button class="btn btn--primary btn--block" type="submit">Создать кабинет</button>
  </form>`;

/**
 * Окно входа. Возвращает клиента после успешного входа или null, если окно закрыли.
 * Сотрудник банка сразу уходит в кабинет банка — продолжать клиентское действие
 * (оформить, подписать) за него нечего, поэтому для вызывающего это тоже null.
 * reason — зачем нужен вход (показывается сверху).
 */
export function openAuth({ reason = '', tab = 'login' } = {}) {
  return new Promise((resolve) => {
    let done = false;
    const { el, close } = openModal(
      `
      <div class="auth__brand">${logoSign()}<div><b>Альфа Бизнес</b><small>Вход в кредитный кабинет</small></div></div>
      ${reason ? `<p class="auth__reason">${icon('lock', 20)}<span>${esc(reason)}</span></p>` : ''}
      <div class="tabs auth__tabs" role="tablist">
        <button class="tab" data-auth-tab="login" role="tab">Вход</button>
        <button class="tab" data-auth-tab="register" role="tab">Регистрация</button>
      </div>
      <div id="auth-body"></div>`,
      {
        label: 'Вход в кредитный кабинет',
        onClose: () => {
          if (!done) resolve(null);
        },
      },
    );

    const body = $('#auth-body', el);
    const show = (t) => {
      $$('[data-auth-tab]', el).forEach((b) => {
        b.classList.toggle('is-on', b.dataset.authTab === t);
        b.setAttribute('aria-selected', b.dataset.authTab === t);
      });
      body.innerHTML = t === 'login' ? loginForm() : registerForm();
      bindForm(t);
      // фокус сразу и только если пользователь ещё никуда не нажал — иначе ввод улетал бы в другое поле
      if (!el.contains(document.activeElement) || document.activeElement.closest('[data-auth-tab]')) $('input', body)?.focus();
    };

    const bindForm = (t) => {
      $$('#auth-phone, #reg-phone', body).forEach((i) =>
        i.addEventListener('input', () => {
          i.value = phone(i.value);
        }),
      );
      const inn = $('#reg-inn', body);
      inn?.addEventListener(
        'input',
        debounce(async () => {
          inn.value = digits(inn.value);
          const hint = $('#reg-company', body);
          if (![10, 12].includes(inn.value.length)) {
            hint.textContent = 'Компанию, реквизиты и руководителя подтянем из ЕГРЮЛ';
            return;
          }
          try {
            const c = await api('GET', `/companies/${inn.value}`);
            hint.innerHTML = `<b class="text-ok">${esc(c.name)}</b> · ${esc(c.address)}`;
            const name = $('#reg-name', body);
            if (!name.value) name.value = c.director;
          } catch (e) {
            hint.innerHTML = `<span class="text-bad">${esc(e.message)}</span>`;
          }
        }, 300),
      );

      $('#auth-form', body).addEventListener('submit', async (e) => {
        e.preventDefault();
        const btn = $('button[type=submit]', body);
        const err = $('#auth-error', body);
        err.textContent = '';
        $$('.field--error', body).forEach((f) => f.classList.remove('field--error'));
        btn.disabled = true;
        try {
          const user =
            t === 'login'
              ? await api('POST', '/auth/login', { phone: $('#auth-phone', body).value, password: $('#auth-password', body).value })
              : await api('POST', '/auth/register', {
                  inn: $('#reg-inn', body).value,
                  full_name: $('#reg-name', body).value,
                  phone: $('#reg-phone', body).value,
                  password: $('#reg-password', body).value,
                  email: '',
                });
          signIn(user);
          await refresh({ quiet: true }).catch(() => {});
          done = true;
          close();
          toast(t === 'login' ? `Здравствуйте, ${user.full_name.split(' ')[1] || user.full_name}` : 'Кабинет создан');
          if (isStaff(user)) {
            resolve(null);
            navigate('/bank/profile');
            return;
          }
          resolve(user);
        } catch (ex) {
          err.textContent = ex.message;
          if (ex.field) $(`[data-field="${ex.field}"]`, body)?.classList.add('field--error');
          btn.disabled = false;
        }
      });
    };

    el.addEventListener('click', (e) => {
      const tb = e.target.closest('[data-auth-tab]');
      if (tb) show(tb.dataset.authTab);
      const demo = e.target.closest('[data-demo]');
      if (demo) {
        $('#auth-phone', body).value = phone(demo.dataset.demo);
        $('#auth-password', body).value = DEMO_PASSWORD;
        $('#auth-form', body).requestSubmit();
      }
    });
    show(tab);
  });
}

export async function logout() {
  try {
    await api('POST', '/auth/logout');
  } catch {
    /* выходим локально в любом случае */
  }
  signOut();
  toast('Вы вышли из кабинета');
}
