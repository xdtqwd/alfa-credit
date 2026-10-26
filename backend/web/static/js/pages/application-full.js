// 2-й уровень анкеты: открывается после входа через Госуслуги. Личность уже подтверждена; клиент прикладывает
// паспорт руководителя (ООО), протокол об одобрении крупной сделки (ООО, больше 25 млн ₽), обеспечение
// (обязательно больше 5 млн ₽) и даёт согласие на запрос в БКИ. Дальше — полный скоринг и окончательное решение.
// Всё сохраняется автоматически (features/autosave.js).
import { api } from '../core/api.js';
import { $, $$, on } from '../core/dom.js';
import { esc, fmt, rub, months, rate, digits, date } from '../core/format.js';
import { navigate } from '../core/router.js';
import { product, putApp } from '../core/session.js';
import { icon } from '../ui/icons.js';
import { createAutosave } from '../features/autosave.js';
import { backLink, pageHead, paintProgress, showFieldError, levels, maskPassport } from './parts.js';

let RULES = null; // { required_from, protocol_from, kinds: [{type, name, discount}] }
const rules = async () => (RULES ??= await api('GET', '/security-rules'));

const kindOf = (type) => RULES.kinds.find((k) => k.type === type);
const liquid = (c) => (kindOf(c.type) && c.value > 0 ? Math.floor(c.value * kindOf(c.type).discount) : 0);
const isLLC = (a) => a.company?.type !== 'ИП';
const needsProtocol = (a) => isLLC(a) && a.amount > RULES.protocol_from;

const passportDone = (p) =>
  !!p &&
  /^\d{4}$/.test(p.series) &&
  /^\d{6}$/.test(p.number) &&
  !!p.issued_at &&
  /^\d{6}$/.test(digits(p.division_code)) &&
  (p.issued_by || '').trim().length >= 5;

/** Обеспечение достаточно для отправки: до порога — не нужно; выше — залог или поручитель. */
const securityDone = (a, f) => {
  if (f.guarantor && !(f.guarantor.full_name.trim().length >= 5 && digits(f.guarantor.inn).length === 12)) return false;
  if (f.collateral.type !== 'none' && !(f.collateral.value > 0)) return false;
  return a.amount <= RULES.required_from || f.collateral.type !== 'none' || !!f.guarantor;
};

/** Пункты готовности: [ключ блока, пункт, выполнен]. */
const checklist = (a, f) =>
  [
    isLLC(a) && ['passport', 'Паспорт руководителя', passportDone(f.director_passport)],
    needsProtocol(a) && ['protocol', 'Протокол о крупной сделке', !!f.protocol?.name],
    ['security', a.amount > RULES.required_from ? 'Залог или поручитель' : 'Обеспечение (по желанию)', securityDone(a, f)],
    ['consent', 'Согласие на запрос в БКИ', !!f.consent],
  ].filter(Boolean);

function securityHint(a) {
  const from = rub(RULES.required_from);
  return a.amount > RULES.required_from
    ? `Сумма больше ${from} — нужен залог, покрывающий хотя бы половину кредита (${rub(a.amount / 2)}), или поручитель без просрочек.`
    : `Для суммы до ${from} обеспечение не обязательно. Залог на всю сумму снизит ставку на 1 п.п.`;
}

function collateralInfo(a, c) {
  if (c.type === 'none' || !(c.value > 0)) return '';
  const l = liquid(c);
  const share = Math.round((l / a.amount) * 100);
  const k = kindOf(c.type);
  const tone = l >= a.amount ? 'text-ok' : a.amount > RULES.required_from && l * 2 < a.amount ? 'text-bad' : '';
  return `В зачёт банк примет ${rub(l)} (${Math.round(k.discount * 100)}% оценки) — <b class="${tone}">${share}% кредита</b>${
    l >= a.amount ? '. Залог покрывает весь кредит — ставка будет ниже на 1 п.п.' : ''
  }`;
}

const fileSize = (n) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1).replace('.', ',')} МБ` : `${Math.max(1, Math.round(n / 1024))} КБ`);

function layout(a, p) {
  const f = a.full_form;
  const pre = a.preliminary || {};
  const id = a.identity;
  const need = a.status === 'need_info' ? a.decision : null;
  const g = f.guarantor;
  const dp = f.director_passport || {};
  const fromESIA = id && a.company?.director && id.full_name.toLowerCase() === a.company.director.toLowerCase();
  let n = 0;
  const sec = (key, title, text, body, extra = '') =>
    `<section class="card fsec ${extra}" data-key="${key}"><div class="fsec__head"><span class="fsec__n">${++n}</span><div><h2>${title}</h2><p>${text}</p></div></div>${body}</section>`;
  return `<div class="container">
    ${backLink('/cabinet/applications', 'Все заявки')}
    ${pageHead(`Документы · ${p.name}`, `Заявка ${esc(a.number)} · предварительно ${pre.type === 'counter_offer' ? 'одобрено на других условиях' : 'одобрено'}: ${rub(a.amount)} на ${months(a.term_months)}`)}
    ${levels(2)}
    ${
      need
        ? `<div class="notice notice--action notice--block"><span class="notice__ic">${icon('alert', 20)}</span>
      <div class="notice__txt"><small>Нужно уточнение</small><b>Исправьте данные и отправьте снова</b>${(need.reasons || []).map((r) => `<span>${esc(r)}</span>`).join('')}${(need.advice || []).map((r) => `<span>${esc(r)}</span>`).join('')}</div></div>`
        : ''
    }
    <div class="form-layout">
      <div class="stack-l">
        ${sec(
          'identity',
          'Личность подтверждена через Госуслуги',
          'Эти данные банк получил из ЕСИА — вводить их не нужно',
          id
            ? `<dl class="kv identity">
            <div><dt>ФИО</dt><dd>${esc(id.full_name)}</dd></div>
            <div><dt>Дата рождения</dt><dd>${id.birth_date ? date(id.birth_date) : '—'}</dd></div>
            <div><dt>Паспорт</dt><dd>${maskPassport(id.passport)}</dd></div>
            <div><dt>СНИЛС</dt><dd>${esc((id.snils || '').replace(/^(\d{3})-\d{3}-\d{3}/, '$1-•••-•••'))}</dd></div>
          </dl><p class="note">${icon('lock', 20)}<span>${esc(id.source || 'Госуслуги')} · ${a.company ? `руководитель ${esc(a.company.name)} по ЕГРЮЛ` : ''}</span></p>`
            : '<p class="text-bad">Личность не подтверждена — войдите через Госуслуги заново.</p>',
          'is-done',
        )}

        ${
          isLLC(a)
            ? sec(
                'passport',
                'Паспорт руководителя',
                fromESIA
                  ? 'Руководитель по ЕГРЮЛ — вы: паспорт подставили из Госуслуг, проверьте'
                  : `Руководитель по ЕГРЮЛ — ${esc(a.company?.director || '')}: укажите его паспорт`,
                `<div class="fields" data-passport>
            <div class="field" data-field="full_form.director_passport.series"><label for="ff-p-series">Серия</label>
              <input class="input" id="ff-p-series" data-pass="series" inputmode="numeric" maxlength="4" value="${esc(dp.series || '')}" placeholder="0000"></div>
            <div class="field" data-field="full_form.director_passport.number"><label for="ff-p-number">Номер</label>
              <input class="input" id="ff-p-number" data-pass="number" inputmode="numeric" maxlength="6" value="${esc(dp.number || '')}" placeholder="000000"></div>
            <div class="field" data-field="full_form.director_passport.issued_at"><label for="ff-p-issued">Дата выдачи</label>
              <input class="input" id="ff-p-issued" data-pass="issued_at" type="date" value="${esc(dp.issued_at || '')}"></div>
            <div class="field" data-field="full_form.director_passport.division_code"><label for="ff-p-code">Код подразделения</label>
              <input class="input" id="ff-p-code" data-pass="division_code" inputmode="numeric" maxlength="7" value="${esc(dp.division_code || '')}" placeholder="000-000"></div>
            <div class="field field--wide" data-field="full_form.director_passport.issued_by"><label for="ff-p-by">Кем выдан</label>
              <input class="input" id="ff-p-by" data-pass="issued_by" value="${esc(dp.issued_by || '')}"></div>
          </div>`,
              )
            : ''
        }

        ${
          needsProtocol(a)
            ? sec(
                'protocol',
                'Решение об одобрении крупной сделки',
                `Кредит больше ${rub(RULES.protocol_from)} для ООО — крупная сделка: нужен протокол или решение участников`,
                `<div class="field" data-field="full_form.protocol">
            <input class="sr-only" id="ff-protocol" type="file" accept=".pdf,.jpg,.jpeg,.png">
            <label class="file" for="ff-protocol">${icon('doc', 20)}<span id="ff-protocol-name">${f.protocol?.name ? `${esc(f.protocol.name)} · ${fileSize(f.protocol.size)}` : 'Выбрать файл: PDF, JPG или PNG до 20 МБ'}</span></label>
            <span class="field__hint">Демо: файл остаётся на вашем устройстве, банк сохраняет только имя и размер.</span></div>`,
              )
            : ''
        }

        ${sec(
          'security',
          'Обеспечение',
          esc(securityHint(a)),
          `<div class="fields">
            <div class="field field--wide" data-field="full_form.collateral.type"><span class="field__label" id="ff-coll-l">Залог</span>
              <div class="chips" role="radiogroup" aria-labelledby="ff-coll-l">
                ${[{ type: 'none', name: 'Без залога' }, ...RULES.kinds]
                  .map(
                    (k) =>
                      `<button type="button" class="chip ${f.collateral.type === k.type ? 'is-on' : ''}" data-collateral="${k.type}" role="radio" aria-checked="${f.collateral.type === k.type}">${esc(k.name)}</button>`,
                  )
                  .join('')}
              </div></div>
            <div class="field ${f.collateral.type === 'none' ? 'is-hidden' : ''}" data-field="full_form.collateral.value" id="ff-coll-value-box"><label for="ff-coll-value">Оценочная стоимость, ₽</label>
              <input class="input" id="ff-coll-value" inputmode="numeric" value="${f.collateral.value ? fmt(f.collateral.value) : ''}" placeholder="например, 12 000 000"></div>
            <div class="field ${f.collateral.type === 'none' ? 'is-hidden' : ''}" id="ff-coll-desc-box"><label for="ff-coll-desc">Что именно <span class="muted">(необязательно)</span></label>
              <input class="input" id="ff-coll-desc" value="${esc(f.collateral.description)}" placeholder="адрес, марка, модель"></div>
            <p class="field__hint field--wide" id="ff-coll-info" aria-live="polite">${collateralInfo(a, f.collateral)}</p>

            <label class="check field--wide"><input type="checkbox" id="ff-guarantor" ${g ? 'checked' : ''}>
              <span>Поручительство собственника или участника компании</span></label>
            <div class="field ${g ? '' : 'is-hidden'}" data-field="full_form.guarantor.full_name" id="ff-g-name-box"><label for="ff-g-name">ФИО поручителя</label>
              <input class="input" id="ff-g-name" value="${esc(g?.full_name || '')}" autocomplete="off"></div>
            <div class="field ${g ? '' : 'is-hidden'}" data-field="full_form.guarantor.inn" id="ff-g-inn-box"><label for="ff-g-inn">ИНН поручителя</label>
              <input class="input" id="ff-g-inn" inputmode="numeric" maxlength="12" value="${esc(g?.inn || '')}" placeholder="12 цифр"></div>
          </div>`,
        )}

        ${sec(
          'consent',
          'Согласие',
          'Кредитную историю, выписки и отчётность банк запросит сам — загружать ничего не нужно',
          `<label class="check" data-field="full_form.consent"><input type="checkbox" id="ff-consent" ${f.consent ? 'checked' : ''}>
            <span>Согласен на запрос кредитной истории компании${g ? ' и поручителя' : ''} в бюро кредитных историй, выписок по счетам в других банках и бухгалтерской отчётности в ФНС</span></label>`,
        )}
      </div>

      <aside class="form-aside">
        <section class="card stack" aria-label="Предварительные условия">
          <div class="calc__pay"><span>Платёж в месяц</span><b>${rub(pre.monthly_payment)}</b></div>
          <dl class="kv">
            <div><dt>Сумма</dt><dd>${rub(a.amount)}</dd></div>
            <div><dt>Срок</dt><dd>${months(a.term_months)}</dd></div>
            <div><dt>Ставка</dt><dd>${pre.rate ? rate(pre.rate) : '—'}</dd></div>
            <div><dt>Переплата</dt><dd>${rub(pre.overpayment)}</dd></div>
          </dl>
          <div class="bar"><i id="sum-progress"></i></div>
          <div class="row row--between small"><span id="sum-pct"></span><span class="save-state" id="save-state" aria-live="polite">Сохранено</span></div>
          <ul class="todo-list" id="sum-todo"></ul>
          <button class="btn btn--primary btn--block" id="submit">Отправить на полный скоринг</button>
          <p class="calc__note">Условия предварительные — без кредитной истории. Окончательное решение — примерно через 15 секунд после отправки</p>
        </section>
      </aside>
    </div>
  </div>`;
}

export default async function applicationFull(root, a, { signal }) {
  await rules();
  if (signal.aborted) return;
  const p = product(a.product_id);
  const id = a.id;
  const f = a.full_form;
  f.collateral ??= { type: 'none', value: 0, description: '' };
  f.collateral.type ||= 'none';
  root.innerHTML = layout(a, p);

  const autosave = createAutosave({ id, root, signal, onSettled: () => refresh() });
  const save = () => autosave.save({ full_form: JSON.parse(JSON.stringify(f)) });

  const refresh = () => {
    const list = checklist(a, f);
    $('#submit', root).disabled =
      paintProgress(
        root,
        list.map(([, title, ok]) => [title, ok]),
      ) < list.length;
    for (const [key, , ok] of list) $(`[data-key="${key}"]`, root)?.classList.toggle('is-done', ok);
    $('#ff-coll-info', root).innerHTML = collateralInfo(a, f.collateral);
  };
  refresh();

  const radio = (attr, value) =>
    $$(`[${attr}]`, root).forEach((c) => {
      const isOn = c.getAttribute(attr) === value;
      c.classList.toggle('is-on', isOn);
      c.setAttribute('aria-checked', isOn);
    });
  const show = (ids, visible) => ids.forEach((x) => $(x, root).classList.toggle('is-hidden', !visible));

  on(
    root,
    'input',
    '[data-pass]',
    (_, el) => {
      const key = el.dataset.pass;
      if (key === 'series' || key === 'number') el.value = digits(el.value);
      if (key === 'division_code') {
        const v = digits(el.value).slice(0, 6);
        el.value = v.length > 3 ? `${v.slice(0, 3)}-${v.slice(3)}` : v;
      }
      f.director_passport = { ...(f.director_passport || {}), [key]: el.value };
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'change',
    '#ff-protocol',
    (_, el) => {
      const file = el.files?.[0];
      if (!file) return;
      f.protocol = { name: file.name, size: file.size };
      $('#ff-protocol-name', root).textContent = `${file.name} · ${fileSize(file.size)}`;
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'click',
    '[data-collateral]',
    (_, el) => {
      f.collateral.type = el.dataset.collateral;
      radio('data-collateral', f.collateral.type);
      show(['#ff-coll-value-box', '#ff-coll-desc-box'], f.collateral.type !== 'none');
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '#ff-coll-value',
    (_, el) => {
      f.collateral.value = +digits(el.value) || 0;
      el.value = f.collateral.value ? fmt(f.collateral.value) : '';
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '#ff-coll-desc',
    (_, el) => {
      f.collateral.description = el.value;
      save();
    },
    signal,
  );
  on(
    root,
    'change',
    '#ff-guarantor',
    (_, el) => {
      f.guarantor = el.checked ? { full_name: $('#ff-g-name', root).value, inn: digits($('#ff-g-inn', root).value) } : undefined;
      show(['#ff-g-name-box', '#ff-g-inn-box'], el.checked);
      if (el.checked) $('#ff-g-name', root).focus();
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '#ff-g-name, #ff-g-inn',
    (_, el) => {
      if (!f.guarantor) return;
      if (el.id === 'ff-g-inn') el.value = digits(el.value).slice(0, 12);
      f.guarantor = { full_name: $('#ff-g-name', root).value, inn: $('#ff-g-inn', root).value };
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'change',
    '#ff-consent',
    (_, el) => {
      f.consent = el.checked;
      save();
      refresh();
    },
    signal,
  );

  on(
    root,
    'click',
    '#submit',
    async (_, btn) => {
      btn.disabled = true;
      btn.textContent = 'Отправляем…';
      try {
        await autosave.settle();
        await api('PATCH', `/applications/${id}`, { full_form: f });
        putApp(await api('POST', `/applications/${id}/submit`));
        navigate(`/cabinet/applications/${id}`);
      } catch (e) {
        showFieldError(root, e);
        btn.disabled = false;
        btn.textContent = 'Отправить на полный скоринг';
      }
    },
    signal,
  );
}
