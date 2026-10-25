// Полная анкета (уровень 2): открывается после того, как клиент принял предварительные условия.
// Три блока — о бизнесе, обеспечение, согласие на выписки — и сводка справа с предварительными условиями.
// Сумма и срок уже согласованы и здесь не меняются. Правила залога (дисконты, порог) — с сервера.
import { api } from '../core/api.js';
import { $, $$, on } from '../core/dom.js';
import { esc, fmt, rub, months, rate, digits } from '../core/format.js';
import { navigate } from '../core/router.js';
import { product, putApp } from '../core/session.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { createAutosave } from '../features/autosave.js';
import { levels } from './application-form.js';
import { backLink, pageHead } from './parts.js';

let RULES = null; // { required_from, kinds: [{type, name, discount}], premises: [{type, name}] }
const rules = async () => (RULES ??= await api('GET', '/security-rules'));

const kindOf = (type) => RULES.kinds.find((k) => k.type === type);
const liquid = (c) => (kindOf(c.type) && c.value > 0 ? Math.floor(c.value * kindOf(c.type).discount) : 0);

/** Обеспечение достаточно для отправки: до порога — не нужно; выше — залог или поручитель. */
const securityDone = (a, f) => {
  if (f.guarantor && !(f.guarantor.full_name.trim().length >= 5 && digits(f.guarantor.inn).length === 12)) return false;
  if (f.collateral.type !== 'none' && !(f.collateral.value > 0)) return false;
  return a.amount <= RULES.required_from || f.collateral.type !== 'none' || !!f.guarantor;
};

const checklist = (a, f) => [
  ['Сотрудники', f.employees > 0],
  ['Где работает бизнес', !!f.premises],
  ['На что пойдут деньги', f.purpose_details.trim().length >= 20],
  [a.amount > RULES.required_from ? 'Залог или поручитель' : 'Обеспечение (по желанию)', securityDone(a, f)],
  ['Согласие на выписки и отчётность', f.statements_consent],
];

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

function layout(a, p) {
  const f = a.full_form;
  const pre = a.preliminary || a.decision;
  const need = a.status === 'need_info' ? a.decision : null;
  const g = f.guarantor;
  return `<div class="container">
    ${backLink('/cabinet/applications', 'Все заявки')}
    ${pageHead(`Полная анкета · ${p.name}`, `Заявка ${esc(a.number)} · предварительно одобрено ${rub(a.amount)} на ${months(a.term_months)}`)}
    ${levels(p, 2)}
    ${
      need
        ? `<div class="notice notice--action notice--block"><span class="notice__ic">${icon('alert', 20)}</span>
      <div class="notice__txt"><small>Нужно уточнение</small><b>Уточните обеспечение и отправьте снова</b>${(need.reasons || []).map((r) => `<span>${esc(r)}</span>`).join('')}${(need.advice || []).map((r) => `<span>${esc(r)}</span>`).join('')}</div></div>`
        : ''
    }
    <div class="form-layout">
      <div class="stack-l">
        <section class="card fsec" data-sec="0">
          <div class="fsec__head"><span class="fsec__n">1</span><div><h2>О бизнесе</h2><p>Чтобы окончательно оценить, как бизнес справится с кредитом</p></div></div>
          <div class="fields">
            <div class="field" data-field="full_form.employees"><label for="ff-employees">Сотрудников, включая руководителя</label>
              <input class="input" id="ff-employees" inputmode="numeric" value="${f.employees || ''}" placeholder="например, 12"></div>
            <div class="field field--wide" data-field="full_form.premises"><span class="field__label" id="ff-premises-l">Где работает бизнес</span>
              <div class="chips" role="radiogroup" aria-labelledby="ff-premises-l">${RULES.premises
                .map(
                  (x) =>
                    `<button type="button" class="chip ${f.premises === x.type ? 'is-on' : ''}" data-premises="${x.type}" role="radio" aria-checked="${f.premises === x.type}">${esc(x.name)}</button>`,
                )
                .join('')}</div></div>
            <div class="field field--wide" data-field="full_form.purpose_details"><label for="ff-purpose">На что пойдут деньги</label>
              <textarea class="input textarea" id="ff-purpose" rows="3" placeholder="${esc(a.purpose)}: например, ремонт второго зала и закупка мебели">${esc(f.purpose_details)}</textarea>
              <span class="field__hint">Пара предложений своими словами — для кредитного аналитика</span></div>
          </div>
        </section>

        <section class="card fsec" data-sec="1">
          <div class="fsec__head"><span class="fsec__n">2</span><div><h2>Обеспечение</h2><p>${esc(securityHint(a))}</p></div></div>
          <div class="fields">
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
          </div>
        </section>

        <section class="card fsec" data-sec="2">
          <div class="fsec__head"><span class="fsec__n">3</span><div><h2>Согласие</h2><p>Выписки и отчётность банк запросит сам — загружать ничего не нужно</p></div></div>
          <label class="check" data-field="full_form.statements_consent"><input type="checkbox" id="ff-consent" ${f.statements_consent ? 'checked' : ''}>
            <span>Согласен на запрос выписок по счетам в других банках и бухгалтерской отчётности в ФНС${f.guarantor ? ', а также кредитной истории поручителя в БКИ' : ''}</span></label>
        </section>
      </div>

      <aside class="form-aside">
        <section class="card stack" aria-label="Предварительные условия">
          <div class="calc__pay"><span>Платёж в месяц</span><b>${rub(pre.monthly_payment)}</b></div>
          <dl class="kv">
            <div><dt>Сумма</dt><dd>${rub(a.amount)}</dd></div>
            <div><dt>Срок</dt><dd>${months(a.term_months)}</dd></div>
            <div><dt>Ставка</dt><dd>${rate(pre.rate)}</dd></div>
            <div><dt>Переплата</dt><dd>${rub(pre.overpayment)}</dd></div>
          </dl>
          <div class="bar"><i id="sum-progress"></i></div>
          <div class="row row--between small"><span id="sum-pct"></span><span class="save-state" id="save-state" aria-live="polite">Сохранено</span></div>
          <ul class="todo-list" id="sum-todo"></ul>
          <button class="btn btn--primary btn--block" id="submit">Отправить на решение</button>
          <p class="calc__note">Условия предварительные. Окончательное решение — примерно через 15 секунд после отправки</p>
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
  f.purpose_details ??= '';
  root.innerHTML = layout(a, p);

  const autosave = createAutosave({ id, root, signal, onSettled: () => refresh() });
  const save = () => autosave.save({ full_form: JSON.parse(JSON.stringify(f)) });

  const refresh = () => {
    const list = checklist(a, f);
    const done = list.filter(([, ok]) => ok).length;
    $('#sum-progress', root).style.width = `${Math.round((done / list.length) * 100)}%`;
    $('#sum-pct', root).textContent = `Заполнено ${Math.round((done / list.length) * 100)}%`;
    $('#sum-todo', root).innerHTML = list.map(([t, ok]) => `<li class="${ok ? 'is-done' : ''}">${t}</li>`).join('');
    $('#submit', root).disabled = done < list.length;
    const sections = [list[0][1] && list[1][1] && list[2][1], list[3][1], list[4][1]];
    $$('.fsec', root).forEach((s, i) => s.classList.toggle('is-done', sections[i]));
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
    '#ff-employees',
    (_, el) => {
      f.employees = +digits(el.value) || 0;
      el.value = f.employees || '';
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'click',
    '[data-premises]',
    (_, el) => {
      f.premises = el.dataset.premises;
      radio('data-premises', f.premises);
      save();
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '#ff-purpose',
    (_, el) => {
      f.purpose_details = el.value;
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
      f.statements_consent = el.checked;
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
        toast(e.message, { error: true });
        $$('.field--error', root).forEach((x) => x.classList.remove('field--error'));
        const field = e.field && $(`[data-field="${e.field}"]`, root);
        if (field) {
          field.classList.add('field--error');
          field.scrollIntoView({ behavior: 'smooth', block: 'center' });
        }
        btn.disabled = false;
        btn.textContent = 'Отправить на решение';
      }
    },
    signal,
  );
}
