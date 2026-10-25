// Вход через Госуслуги (ЕСИА) — начало 2-го уровня анкеты. В демо — окно-имитация с учётными записями
// руководителей демо-компаний: настоящий сайт Госуслуг не открывается, пароль не нужен.
import { api } from '../core/api.js';
import { $, $$ } from '../core/dom.js';
import { esc } from '../core/format.js';
import { signIn, refresh } from '../core/session.js';
import { openModal, toast } from '../ui/feedback.js';

const person = (p, on) => `
  <button type="button" class="esia__person ${on ? 'is-on' : ''}" data-person="${esc(p.id)}" role="radio" aria-checked="${on}">
    <b>${esc(p.full_name)}</b>
    <small>${esc(p.role || 'Представитель')} · ${esc(p.company_name || `ИНН ${p.company_inn}`)}</small>
    <small>СНИЛС ${esc(p.snils)}</small>
  </button>`;

/**
 * Окно «Вход через Госуслуги». inn — компания из 1-го уровня: учётку её руководителя выбираем сразу.
 * Возвращает пользователя после входа или null, если окно закрыли.
 */
export async function esiaLogin({ inn = '' } = {}) {
  let persons;
  try {
    persons = await api('GET', '/esia/persons');
  } catch (e) {
    toast(e.message, { error: true });
    return null;
  }
  let chosen = (persons.find((p) => p.company_inn === inn) || persons[0])?.id;
  return new Promise((resolve) => {
    let result = null;
    const { el, close } = openModal(
      `
      <div class="esia__brand"><span class="esia__mark">Госуслуги</span><span class="esia__demo">демо-вход</span></div>
      <h2>Вход через Госуслуги</h2>
      <p class="modal__lead">Это имитация входа через ЕСИА для демонстрации — настоящий сайт Госуслуг не открывается. Выберите учётную запись руководителя компании.</p>
      <div class="esia__list" role="radiogroup" aria-label="Учётная запись Госуслуг">${persons.map((p) => person(p, p.id === chosen)).join('')}</div>
      <div class="esia__consent">
        <b>Альфа-Банк получит из Госуслуг</b>
        <ul><li>ФИО и дату рождения</li><li>паспорт</li><li>СНИЛС</li><li>подтверждённый телефон</li></ul>
      </div>
      <p class="field__hint text-bad" id="esia-error" role="alert"></p>
      <button class="btn btn--esia btn--block" id="esia-ok">Предоставить доступ и войти</button>`,
      { onClose: () => resolve(result) },
    );
    el.classList.add('stack', 'esia');
    el.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-person]');
      if (!btn) return;
      chosen = btn.dataset.person;
      $$('[data-person]', el).forEach((b) => {
        const isOn = b === btn;
        b.classList.toggle('is-on', isOn);
        b.setAttribute('aria-checked', isOn);
      });
    });
    $('#esia-ok', el).addEventListener('click', async (e) => {
      const ok = e.currentTarget;
      ok.disabled = true;
      try {
        const user = await api('POST', '/auth/esia', { person_id: chosen });
        signIn(user);
        await refresh({ quiet: true }).catch(() => {});
        result = user;
        close();
        toast(`Личность подтверждена через Госуслуги: ${user.full_name}`);
      } catch (ex) {
        $('#esia-error', el).textContent = ex.message;
        ok.disabled = false;
      }
    });
  });
}
