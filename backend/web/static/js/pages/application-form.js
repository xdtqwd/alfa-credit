// Быстрая анкета (уровень 1): 4 коротких блока и сводка справа → предварительное решение за ~15 секунд.
// Подробности о бизнесе и обеспечении банк спросит в полной анкете (application-full.js) — только после
// предварительного одобрения. У одноуровневого продукта (овердрафт) решение по ней сразу окончательное.
// Всё сохраняется автоматически (features/autosave.js).
import { api } from '../core/api.js';
import { $, $$, on } from '../core/dom.js';
import { esc, fmt, rub, months, rate, digits, phone } from '../core/format.js';
import { navigate } from '../core/router.js';
import { session, product, putApp } from '../core/session.js';
import { icon } from '../ui/icons.js';
import { calcFields, bindCalc, createQuoter } from '../features/calculator.js';
import { createAutosave } from '../features/autosave.js';
import { backLink, pageHead, paintProgress, showFieldError } from './parts.js';

const POSITIONS = [
  'Генеральный директор',
  'Собственник бизнеса',
  'Финансовый директор',
  'Главный бухгалтер',
  'Индивидуальный предприниматель',
];

const checklist = (a) => [
  ['Сумма и срок', true],
  ['Компания', !!a.company],
  ['Выручка за год', a.financials.annual_revenue > 0],
  ['ФИО контактного лица', (a.contact.full_name || '').trim().length >= 5],
  ['Телефон', digits(a.contact.phone).length >= 10],
  ['Согласие на запрос в БКИ', a.consent],
];

const submitLabel = (p) => (p.express ? 'Отправить на решение' : 'Получить предварительное решение');

/** Два уровня анкеты: где клиент сейчас и что будет дальше. У овердрафта уровень один. */
export function levels(p, current = 1) {
  if (p.express) return '';
  const step = (n, title, text) =>
    `<li class="levels__item ${n === current ? 'is-current' : n < current ? 'is-done' : ''}"><span class="levels__n">${n < current ? icon('check', 16) : n}</span><div><b>${title}</b><small>${text}</small></div></li>`;
  return `<ol class="levels" aria-label="Уровни анкеты">
    ${step(1, 'Быстрая анкета', '2 минуты → предварительное решение')}
    ${step(2, 'Полная анкета', 'О бизнесе и обеспечении → окончательное решение')}
  </ol>`;
}

function layout(a, p) {
  const me = session.me;
  const reasons = a.status === 'need_info' ? a.decision?.reasons || [] : [];
  return `<div class="container">
    ${backLink('/cabinet/applications', 'Все заявки')}
    ${pageHead(`Анкета · ${p.name}`, `Заявка ${esc(a.number)} · всё сохраняется автоматически, можно вернуться позже`)}
    ${levels(p)}
    ${
      reasons.length
        ? `<div class="notice notice--action notice--block"><span class="notice__ic">${icon('alert', 20)}</span>
      <div class="notice__txt"><small>Нужно уточнение</small><b>Исправьте данные и отправьте снова</b>${reasons.map((r) => `<span>${esc(r)}</span>`).join('')}</div></div>`
        : ''
    }
    <div class="form-layout">
      <div class="stack-l">
        <section class="card fsec" data-sec="0">
          <div class="fsec__head"><span class="fsec__n">1</span><div><h2>Сумма и срок</h2></div></div>
          <div class="stack">
            <div class="chips" role="radiogroup" aria-label="Кредит">${session.products
              .map(
                (x) => `
              <button class="chip ${x.id === p.id ? 'is-on' : ''}" data-product="${x.id}" role="radio" aria-checked="${x.id === p.id}">${esc(x.name)}</button>`,
              )
              .join('')}</div>
            <div class="stack" id="form-calc"></div>
            <div class="field"><span class="field__label">Цель кредита</span><div class="chips" id="form-purposes"></div></div>
          </div>
        </section>

        <section class="card fsec" data-sec="1">
          <div class="fsec__head"><span class="fsec__n">2</span><div><h2>Компания</h2><p>Из профиля, сверено с ЕГРЮЛ/ЕГРИП — вводить ничего не нужно</p></div></div>
          <div class="company-box" id="form-company"></div>
          <p class="note">${icon('card', 20)}<span>Деньги поступят на расчётный счёт ${esc(me.requisites.account)}, ${esc(me.requisites.bank_name)}. <a class="btn--link" href="#/cabinet">Реквизиты</a></span></p>
        </section>

        <section class="card fsec" data-sec="2">
          <div class="fsec__head"><span class="fsec__n">3</span><div><h2>Финансы</h2><p>Две цифры — без выписок и справок</p></div></div>
          <div class="fields">
            <div class="field" data-field="financials.annual_revenue"><label for="f-revenue">Выручка за последний год, ₽</label>
              <input class="input" id="f-revenue" inputmode="numeric" data-money="annual_revenue" value="${a.financials.annual_revenue ? fmt(a.financials.annual_revenue) : ''}" placeholder="например, 24 000 000">
              <span class="field__hint" id="f-revenue-hint"></span></div>
            <div class="field"><label for="f-obligations">Платежи по другим кредитам в месяц, ₽</label>
              <input class="input" id="f-obligations" inputmode="numeric" data-money="monthly_obligations" value="${a.financials.monthly_obligations ? fmt(a.financials.monthly_obligations) : ''}" placeholder="0, если нет"></div>
          </div>
        </section>

        <section class="card fsec" data-sec="3">
          <div class="fsec__head"><span class="fsec__n">4</span><div><h2>Контактное лицо и согласие</h2></div></div>
          <div class="fields">
            <div class="field field--wide" data-field="contact.full_name"><label for="f-name">ФИО</label>
              <input class="input" id="f-name" data-contact="full_name" value="${esc(a.contact.full_name)}" autocomplete="name"></div>
            <div class="field"><label for="f-position">Должность</label>
              <input class="input" id="f-position" data-contact="position" value="${esc(a.contact.position)}" list="f-positions"></div>
            <datalist id="f-positions">${POSITIONS.map((x) => `<option>${x}</option>`).join('')}</datalist>
            <div class="field" data-field="contact.phone"><label for="f-phone">Телефон</label>
              <input class="input" id="f-phone" data-contact="phone" inputmode="tel" value="${esc(phone(a.contact.phone))}" placeholder="+7 (900) 000-00-00" autocomplete="tel"></div>
            <div class="field field--wide"><label for="f-email">Эл. почта <span class="muted">(необязательно)</span></label>
              <input class="input" id="f-email" data-contact="email" type="email" value="${esc(a.contact.email)}" autocomplete="email"></div>
            <label class="check field--wide" data-field="consent"><input type="checkbox" id="f-consent" ${a.consent ? 'checked' : ''}>
              <span>Согласен на обработку персональных данных и запрос кредитной истории компании в бюро кредитных историй</span></label>
          </div>
        </section>
      </div>

      <aside class="form-aside">
        <section class="card stack" aria-label="Итог по заявке">
          <div class="calc__pay"><span>Платёж в месяц</span><b id="sum-pay">—</b></div>
          <dl class="kv">
            <div><dt>Сумма</dt><dd id="sum-amount"></dd></div>
            <div><dt>Срок</dt><dd id="sum-term"></dd></div>
            <div><dt>Ставка</dt><dd>от ${rate(p.rate)}</dd></div>
            <div><dt>Переплата</dt><dd id="sum-over"></dd></div>
          </dl>
          <div class="bar"><i id="sum-progress"></i></div>
          <div class="row row--between small"><span id="sum-pct"></span><span class="save-state" id="save-state" aria-live="polite">Сохранено</span></div>
          <ul class="todo-list" id="sum-todo"></ul>
          <button class="btn btn--primary btn--block" id="submit">${submitLabel(p)}</button>
          <p class="calc__note">${p.express ? 'Решение — примерно через 15 секунд, прямо на этой странице' : 'Предварительное решение — примерно через 15 секунд. Подробности о бизнесе спросим, только если одобрим'}</p>
        </section>
      </aside>
    </div>
  </div>`;
}

export default function applicationForm(root, a, { signal }) {
  let p = product(a.product_id);
  const id = a.id;
  root.innerHTML = layout(a, p);

  // ---------- автосохранение ----------
  const autosave = createAutosave({
    id,
    root,
    signal,
    // расчёт с сервера принимаем, только если параметры не успели измениться
    onSaved: (fresh) => {
      if (fresh.quote && fresh.amount === a.amount && fresh.term_months === a.term_months && fresh.product_id === a.product_id)
        a.quote = fresh.quote;
    },
    onSettled: () => refresh(),
  });
  const save = (patch) => autosave.save(patch);

  // ---------- сводка ----------
  const paintSummary = () => {
    const q = a.quote;
    const pay = $('#sum-pay', root);
    pay.textContent = q ? rub(q.monthly_payment) : '—';
    pay.classList.toggle('is-stale', !q || q.amount !== a.amount || q.term_months !== a.term_months);
    $('#sum-amount', root).textContent = rub(a.amount);
    $('#sum-term', root).textContent = months(a.term_months);
    $('#sum-over', root).textContent = q ? rub(q.overpayment) : '—';
  };
  const refresh = () => {
    paintSummary();
    const list = checklist(a);
    $('#submit', root).disabled = paintProgress(root, list) < list.length;
    const sections = [true, list[1][1], list[2][1], list[3][1] && list[4][1] && list[5][1]];
    $$('.fsec', root).forEach((s, i) => s.classList.toggle('is-done', sections[i]));
  };

  // ---------- блоки ----------
  const quoter = createQuoter(
    (q) => {
      a.quote = q;
      paintSummary();
    },
    () => {
      a.quote = null;
      paintSummary();
    },
    signal,
  );
  const paintPurposes = () => {
    $('#form-purposes', root).innerHTML = p.purposes
      .map((x) => `<button class="chip ${x === a.purpose ? 'is-on' : ''}" data-purpose="${esc(x)}">${esc(x)}</button>`)
      .join('');
  };
  const paintCalc = () => {
    const box = $('#form-calc', root);
    box.innerHTML = calcFields(p, a.amount, a.term_months);
    bindCalc(box, p, ({ amount, term }) => {
      a.amount = amount;
      a.term_months = term;
      $('#sum-pay', root).classList.add('is-stale');
      quoter(p.id, a.amount, a.term_months);
      save({ amount, term_months: term });
      refresh();
    });
    paintPurposes();
  };
  const paintCompany = () => {
    const c = a.company;
    $('#form-company', root).innerHTML = c
      ? `<b>${esc(c.name)}</b><span>ИНН ${esc(c.inn)} · ОГРН ${esc(c.ogrn)} · ${c.status === 'active' ? 'действует' : 'ликвидирована'}</span><span>${esc(c.address)}</span>`
      : '<span>Компания не найдена в реестре</span>';
    $('#f-revenue-hint', root).innerHTML = c?.revenue_last_year
      ? `По данным ФНС за прошлый год: ${rub(c.revenue_last_year)} · <button type="button" class="btn--link" data-use-fns>подставить</button>`
      : '';
  };
  paintCalc();
  paintCompany();
  quoter(p.id, a.amount, a.term_months);
  refresh();

  // ---------- события ----------
  on(
    root,
    'click',
    '[data-product]',
    (_, el) => {
      p = product(el.dataset.product);
      a.product_id = p.id;
      a.amount = Math.min(p.max_amount, Math.max(p.min_amount, a.amount));
      a.term_months = Math.min(p.max_term, Math.max(p.min_term, a.term_months));
      a.purpose = p.purposes[0];
      $$('[data-product]', root).forEach((c) => {
        const isOn = c === el;
        c.classList.toggle('is-on', isOn);
        c.setAttribute('aria-checked', isOn);
      });
      $('h1', root).textContent = `Анкета · ${p.name}`;
      save({ product_id: p.id, amount: a.amount, term_months: a.term_months, purpose: a.purpose });
      paintCalc();
      quoter(p.id, a.amount, a.term_months);
      refresh();
    },
    signal,
  );
  on(
    root,
    'click',
    '[data-purpose]',
    (_, el) => {
      a.purpose = el.dataset.purpose;
      save({ purpose: a.purpose });
      paintPurposes();
    },
    signal,
  );
  on(
    root,
    'click',
    '[data-use-fns]',
    () => {
      a.financials.annual_revenue = a.company.revenue_last_year;
      $('#f-revenue', root).value = fmt(a.financials.annual_revenue);
      save({ financials: { ...a.financials } });
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '[data-money]',
    (_, el) => {
      const v = +digits(el.value);
      el.value = v ? fmt(v) : '';
      a.financials[el.dataset.money] = v;
      save({ financials: { ...a.financials } });
      refresh();
    },
    signal,
  );
  on(
    root,
    'input',
    '[data-contact]',
    (_, el) => {
      if (el.dataset.contact === 'phone') el.value = phone(el.value);
      a.contact[el.dataset.contact] = el.value;
      save({ contact: { ...a.contact } });
      refresh();
    },
    signal,
  );
  on(
    root,
    'change',
    '#f-consent',
    (_, el) => {
      a.consent = el.checked;
      save({ consent: a.consent });
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
        // сначала досохраняем анкету — нельзя отправлять, если правки не дошли до сервера
        await autosave.settle();
        await api('PATCH', `/applications/${id}`, {
          product_id: a.product_id,
          amount: a.amount,
          term_months: a.term_months,
          purpose: a.purpose,
          financials: a.financials,
          contact: a.contact,
          consent: a.consent,
        });
        putApp(await api('POST', `/applications/${id}/submit`));
        navigate(`/cabinet/applications/${id}`);
      } catch (e) {
        showFieldError(root, e);
        btn.disabled = false;
        btn.textContent = submitLabel(p);
      }
    },
    signal,
  );
}
