// 1-й уровень анкеты — предварительный расчёт без входа: кредит, ИНН компании, выручка и текущие платежи.
// Упрощённый скоринг (реестр, ФНС, долговая нагрузка — без кредитной истории) считает сервер за доли секунды.
// Дальше — вход через Госуслуги: тогда создаётся заявка и открывается 2-й уровень (документы).
// Черновик хранится в браузере: заявки на сервере до входа нет.
import { api } from '../core/api.js';
import { $, $$, on, storage } from '../core/dom.js';
import { esc, fmt, rub, months, rate, digits } from '../core/format.js';
import { navigate } from '../core/router.js';
import { session, product, putApp, isStaff } from '../core/session.js';
import { toast } from '../ui/feedback.js';
import { icon } from '../ui/icons.js';
import { calcFields, bindCalc, createQuoter } from '../features/calculator.js';
import { esiaLogin } from '../features/esia.js';
import { offer, compare, reasons, scoreBlock } from '../features/decision.js';
import { pageHead, levels, paintProgress, showFieldError } from './parts.js';

const KEY = 'apply';

const TITLES = {
  approved: ['ok', 'Предварительно одобрено'],
  counter_offer: ['warn', 'Предварительно: можем выдать на других условиях'],
  need_info: ['warn', 'Нужно уточнить данные'],
  rejected: ['bad', 'Сейчас не можем выдать кредит'],
};

function layout(d, options) {
  return `<div class="container">
    ${pageHead('Предварительный расчёт', 'Без входа и документов — решение за пару секунд. Дальше — вход через Госуслуги и документы')}
    ${levels(1)}
    <div class="form-layout">
      <div class="stack-l">
        <section class="card fsec" data-sec="0">
          <div class="fsec__head"><span class="fsec__n">1</span><div><h2>Кредит</h2><p>Выберите кредит, сумму и срок</p></div></div>
          <div class="stack">
            <div class="chips" role="radiogroup" aria-label="Кредит" data-field="product_id">${session.products
              .map(
                (x) =>
                  `<button type="button" class="chip ${x.id === d.product_id ? 'is-on' : ''}" data-product="${x.id}" role="radio" aria-checked="${x.id === d.product_id}">${esc(x.name)}</button>`,
              )
              .join('')}</div>
            <div class="stack" id="ap-calc"></div>
            <div class="field" id="ap-purposes-box"><span class="field__label">Цель кредита</span><div class="chips" id="ap-purposes"></div></div>
          </div>
        </section>

        <section class="card fsec" data-sec="1">
          <div class="fsec__head"><span class="fsec__n">2</span><div><h2>Компания</h2><p>По ИНН подтянем данные из ЕГРЮЛ/ЕГРИП и выручку из ФНС</p></div></div>
          <div class="field" data-field="inn"><label for="ap-inn">ИНН компании или ИП <span class="badge-demo">демо</span></label>
            <select class="input select" id="ap-inn">
              <option value="">Выберите компанию</option>
              ${options.map((c) => `<option value="${esc(c.inn)}" ${c.inn === d.inn ? 'selected' : ''}>${esc(c.name)} · ИНН ${esc(c.inn)}${c.hint ? ` — ${esc(c.hint)}` : ''}</option>`).join('')}
            </select>
            <span class="field__hint">В демо ИНН выбирается из списка: для этих компаний заранее известны ответы реестра, ФНС и БКИ. В боевой версии ИНН вводится вручную.</span></div>
          <div class="company-box" id="ap-company" hidden></div>
        </section>

        <section class="card fsec" data-sec="2">
          <div class="fsec__head"><span class="fsec__n">3</span><div><h2>Финансы</h2><p>Две цифры — без выписок и справок</p></div></div>
          <div class="fields">
            <div class="field" data-field="financials.annual_revenue"><label for="ap-revenue">Выручка за последний год, ₽</label>
              <input class="input" id="ap-revenue" inputmode="numeric" data-money="revenue" value="${d.revenue ? fmt(d.revenue) : ''}" placeholder="например, 24 000 000">
              <span class="field__hint" id="ap-revenue-hint"></span></div>
            <div class="field" data-field="financials.monthly_obligations"><label for="ap-obligations">Платежи по другим кредитам в месяц, ₽</label>
              <input class="input" id="ap-obligations" inputmode="numeric" data-money="obligations" value="${d.obligations ? fmt(d.obligations) : ''}" placeholder="0, если нет"></div>
          </div>
        </section>

        <div id="ap-result" aria-live="polite"></div>
      </div>

      <aside class="form-aside">
        <section class="card stack" aria-label="Итог расчёта">
          <div class="calc__pay"><span>Платёж в месяц</span><b id="sum-pay">—</b></div>
          <dl class="kv">
            <div><dt>Сумма</dt><dd id="sum-amount">—</dd></div>
            <div><dt>Срок</dt><dd id="sum-term">—</dd></div>
            <div><dt>Ставка</dt><dd id="sum-rate">—</dd></div>
          </dl>
          <div class="bar"><i id="sum-progress"></i></div>
          <div class="row row--between small"><span id="sum-pct"></span><span class="muted">Без входа</span></div>
          <ul class="todo-list" id="sum-todo"></ul>
          <button class="btn btn--primary btn--block" id="prescore">Рассчитать предварительно</button>
          <p class="calc__note">Упрощённый скоринг: реестр, ФНС и долговая нагрузка. Кредитную историю банк запросит после входа через Госуслуги.</p>
        </section>
      </aside>
    </div>
  </div>`;
}

export default async function apply(root, { signal }) {
  root.className = 'page';
  const companies = await api('GET', '/demo/companies').catch(() => []);
  if (signal.aborted) return;
  const me = session.me && !isStaff() ? session.me : null;
  const d = { product_id: null, amount: 0, term_months: 0, purpose: '', inn: '', revenue: 0, obligations: 0, ...storage.get(KEY, {}) };
  if (!d.inn && me) d.inn = me.company.inn;
  let p = d.product_id ? product(d.product_id) : null;
  if (!p) d.product_id = null;
  let company = null; // компания из реестра по выбранному ИНН
  let result = null; // ответ предварительного расчёта
  const save = () => storage.set(KEY, d);

  const options = [...companies];
  if (me && !options.some((c) => c.inn === me.company.inn))
    options.unshift({ inn: me.company.inn, name: me.company.name, hint: 'ваша компания' });
  root.innerHTML = layout(d, options);

  // ---------- сводка и готовность ----------
  const quoter = createQuoter(
    (q) => {
      $('#sum-pay', root).textContent = rub(q.monthly_payment);
      $('#sum-pay', root).classList.remove('is-stale');
    },
    () => {
      $('#sum-pay', root).textContent = '—';
    },
    signal,
  );
  const refresh = () => {
    $('#sum-amount', root).textContent = p ? rub(d.amount) : '—';
    $('#sum-term', root).textContent = p ? months(d.term_months) : '—';
    $('#sum-rate', root).textContent = p ? `от ${rate(p.rate)}` : '—';
    const list = [
      ['Кредит', !!p],
      ['Компания', !!company],
      ['Выручка за год', d.revenue > 0],
    ];
    $('#prescore', root).disabled = paintProgress(root, list) < list.length;
    $$('.fsec', root).forEach((s, i) => s.classList.toggle('is-done', list[i][1]));
  };
  // любое изменение данных делает прошлый расчёт неактуальным
  const changed = () => {
    save();
    if (result) {
      result = null;
      $('#ap-result', root).innerHTML = `<p class="note">${icon('info', 20)}<span>Данные изменились — рассчитайте снова.</span></p>`;
    }
    refresh();
  };

  // ---------- кредит ----------
  const paintPurposes = () => {
    $('#ap-purposes-box', root).hidden = !p;
    if (p)
      $('#ap-purposes', root).innerHTML = p.purposes
        .map((x) => `<button type="button" class="chip ${x === d.purpose ? 'is-on' : ''}" data-purpose="${esc(x)}">${esc(x)}</button>`)
        .join('');
  };
  const paintCalc = () => {
    const box = $('#ap-calc', root);
    if (!p) {
      box.innerHTML = '<p class="muted">Выберите кредит — сумму и срок можно будет настроить ползунками.</p>';
      $('#sum-pay', root).textContent = '—';
      paintPurposes();
      return;
    }
    box.innerHTML = calcFields(p, d.amount, d.term_months);
    bindCalc(box, p, ({ amount, term }) => {
      d.amount = amount;
      d.term_months = term;
      $('#sum-pay', root).classList.add('is-stale');
      quoter(p.id, d.amount, d.term_months);
      changed();
    });
    paintPurposes();
    quoter(p.id, d.amount, d.term_months);
  };
  const pick = (id) => {
    p = product(id);
    d.product_id = p.id;
    d.amount = Math.min(p.max_amount, Math.max(p.min_amount, d.amount || p.default_amount));
    d.term_months = Math.min(p.max_term, Math.max(p.min_term, d.term_months || p.default_term));
    if (!p.purposes.includes(d.purpose)) d.purpose = p.purposes[0];
    $$('[data-product]', root).forEach((c) => {
      const isOn = c.dataset.product === id;
      c.classList.toggle('is-on', isOn);
      c.setAttribute('aria-checked', isOn);
    });
    paintCalc();
    changed();
  };

  // ---------- компания ----------
  const paintCompany = () => {
    const box = $('#ap-company', root);
    box.hidden = !company;
    if (company)
      box.innerHTML = `<b>${esc(company.name)}</b><span>ИНН ${esc(company.inn)} · ОГРН ${esc(company.ogrn)} · ${company.status === 'active' ? 'действует' : 'ликвидирована'}</span><span>${esc(company.address)}${company.director ? ` · руководитель ${esc(company.director)}` : ''}</span>`;
    $('#ap-revenue-hint', root).innerHTML = company?.revenue_last_year
      ? `По данным ФНС за прошлый год: ${rub(company.revenue_last_year)} · <button type="button" class="btn--link" data-use-fns>подставить</button>`
      : '';
  };
  const loadCompany = async () => {
    company = null;
    if (d.inn) {
      try {
        company = await api('GET', `/companies/${d.inn}`);
      } catch (e) {
        toast(e.message, { error: true });
      }
    }
    if (signal.aborted) return;
    paintCompany();
    refresh();
  };

  // ---------- результат ----------
  const identified = () => !!(session.me?.identity && session.me.company?.inn === d.inn);
  const paintResult = () => {
    const box = $('#ap-result', root);
    if (!result) return;
    const dec = result.decision;
    const [tone, title] = TITLES[dec.type];
    const passed = dec.type === 'approved' || dec.type === 'counter_offer';
    const terms =
      dec.type === 'counter_offer' && dec.requested
        ? `<p class="muted">Сравните, что вы просили и что предлагает банк:</p>${compare(dec.requested, dec)}`
        : dec.type === 'approved'
          ? offer(dec)
          : '';
    const llc = result.company?.type !== 'ИП';
    const next = passed
      ? `<div class="note note--box">${icon('info', 20)}<span><b>Дальше — 2-й уровень.</b> Вход через Госуслуги и документы: ${llc ? 'паспорт руководителя, ' : ''}обеспечение для суммы больше 5 млн ₽${llc ? ', протокол о крупной сделке — больше 25 млн ₽' : ''}. Затем полный скоринг с кредитной историей и окончательное решение.</span></div>
        <div class="actions"><button class="btn btn--esia" id="esia-go">${identified() ? 'Продолжить — личность подтверждена' : 'Войти через Госуслуги и продолжить'}</button></div>`
      : '';
    box.innerHTML = `<section class="card decision decision--${tone}" id="prescore-result" data-decision="${dec.type}">
      <h2>${title}</h2>${terms}${reasons('Почему так', dec.reasons)}${reasons(dec.type === 'need_info' ? 'Что сделать' : 'Что можно сделать', dec.advice)}${scoreBlock(result, 'Упрощённый скоринг', { zones: false })}${next}
    </section>`;
    box.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  const body = () => ({
    product_id: d.product_id,
    amount: d.amount,
    term_months: d.term_months,
    purpose: d.purpose,
    inn: d.inn,
    financials: { annual_revenue: d.revenue, monthly_obligations: d.obligations },
  });

  // ---------- начальное состояние ----------
  paintCalc();
  refresh();
  loadCompany();

  // ---------- события ----------
  on(root, 'click', '[data-product]', (_, el) => pick(el.dataset.product), signal);
  on(
    root,
    'click',
    '[data-purpose]',
    (_, el) => {
      d.purpose = el.dataset.purpose;
      paintPurposes();
      changed();
    },
    signal,
  );
  on(
    root,
    'change',
    '#ap-inn',
    (_, el) => {
      d.inn = el.value;
      changed();
      loadCompany();
    },
    signal,
  );
  on(
    root,
    'click',
    '[data-use-fns]',
    () => {
      d.revenue = company.revenue_last_year;
      $('#ap-revenue', root).value = fmt(d.revenue);
      changed();
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
      d[el.dataset.money] = v;
      changed();
    },
    signal,
  );
  on(
    root,
    'click',
    '#prescore',
    async (_, btn) => {
      btn.disabled = true;
      btn.textContent = 'Считаем…';
      try {
        result = await api('POST', '/prescore', body());
        if (signal.aborted) return;
        paintResult();
      } catch (e) {
        showFieldError(root, e);
      } finally {
        btn.textContent = 'Рассчитать предварительно';
        refresh();
      }
    },
    signal,
  );
  on(
    root,
    'click',
    '#esia-go',
    async (_, btn) => {
      if (isStaff()) {
        toast('Кредит оформляет представитель компании — вы вошли как сотрудник банка', { error: true });
        return;
      }
      btn.disabled = true;
      try {
        if (!identified() && !(await esiaLogin({ inn: d.inn }))) return;
        const a = await api('POST', '/applications', body());
        putApp(a);
        navigate(`/cabinet/applications/${a.id}`);
      } catch (e) {
        toast(e.message, { error: true });
      } finally {
        btn.disabled = false;
      }
    },
    signal,
  );
}
