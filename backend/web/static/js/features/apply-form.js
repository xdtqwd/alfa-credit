// Форма 1-го уровня на главной — под калькулятором, в стиле анкет alfabank.ru: серый блок, белая карточка,
// заполненные поля с подписью внутри и линией снизу. Поля — те же, что на странице «Предварительный расчёт».
// Кредит, сумма и срок общие с калькулятором: меняются в любом из двух мест.
import { api } from '../core/api.js';
import { $, on } from '../core/dom.js';
import { esc, fmt, months, shortRub, digits } from '../core/format.js';
import { session, isStaff } from '../core/session.js';
import { icon } from '../ui/icons.js';
import {
  loadDraft,
  saveDraft,
  companyOptions,
  companyOption,
  companyLines,
  revenueHint,
  level1Body,
  prescoreCard,
  continueToLevel2,
} from './level1.js';

const REQUIRED = 'Поле обязательно для заполнения';

const field = (id, label, control, cls = '') => `
  <div class="af-field ${cls}" data-af="${id}">
    ${control}
    <label class="af-field__label" for="af-${id}">${label}</label>
  </div>`;
const hint = (id, text = '') => `<p class="af-hint" id="af-${id}-hint">${text}</p>`;

export const applyFormHtml = () => `
  <section class="lp-section lp-section--apply" id="apply-form">
    <div class="lp-container">
      <div class="af">
        <form class="af__card" id="af-form" novalidate>
          <h2 class="af__title">Заполните заявку на кредит для бизнеса</h2>
          <p class="af__lead">Без входа и документов — предварительное решение за пару секунд. Дальше — вход через Госуслуги и документы.</p>

          <h3 class="af__h">Кредит</h3>
          ${field('product', 'Кредит', '<select class="af-field__control" id="af-product"></select>', 'af-field--select is-filled')}
          ${field('amount', 'Сумма', '<input class="af-field__control" id="af-amount" inputmode="numeric" autocomplete="off" placeholder=" ">')}
          ${hint('amount')}
          ${field('term', 'Срок', '<select class="af-field__control" id="af-term"></select>', 'af-field--select is-filled')}
          ${hint('term')}
          ${field('purpose', 'Цель кредита', '<select class="af-field__control" id="af-purpose"></select>', 'af-field--select is-filled')}

          <h3 class="af__h">Компания</h3>
          ${field('inn', 'ИНН компании или ИП', '<select class="af-field__control" id="af-inn"><option value=""></option></select>', 'af-field--select')}
          ${hint('inn', '<span class="badge-demo">демо</span> ИНН выбирается из списка: для этих компаний заранее известны ответы реестра, ФНС и БКИ')}
          <div class="af-company" id="af-company" hidden></div>

          <h3 class="af__h">Финансы</h3>
          ${field('revenue', 'Выручка за последний год, ₽', '<input class="af-field__control" id="af-revenue" inputmode="numeric" autocomplete="off" placeholder=" ">')}
          ${hint('revenue')}
          ${field('obligations', 'Платежи по кредитам в месяц, ₽', '<input class="af-field__control" id="af-obligations" inputmode="numeric" autocomplete="off" placeholder=" ">')}
          ${hint('obligations', '0, если нет')}

          <button type="submit" class="lp-btn af__submit" id="af-prescore">Рассчитать предварительно</button>
          <p class="af-legal">Упрощённый скоринг: реестр, сверка с ФНС и долговая нагрузка. Кредитную историю банк запросит после входа через Госуслуги — на полном скоринге условия могут измениться.</p>
          <div class="af__result" id="af-result" aria-live="polite"></div>
        </form>
      </div>
    </div>
  </section>`;

/** Сроки для списка: границы продукта, частые значения и текущий срок калькулятора. */
const termOptions = (p, term) =>
  [...new Set([p.min_term, 3, 6, 12, 18, 24, 36, 48, 60, p.max_term, term])]
    .filter((m) => m >= p.min_term && m <= p.max_term)
    .sort((a, b) => a - b);

/**
 * Подключает форму. credit() — текущие кредит, сумма и срок калькулятора; setCredit(patch) меняет их там.
 * Возвращает sync() — вызывать, когда калькулятор изменился сам.
 */
export function bindApplyForm(root, { signal, credit, setCredit }) {
  const d = loadDraft();
  let company = null;
  let result = null;
  const $f = (id) => $(`#af-${id}`, root);

  // подсказки под полями: при ошибке на их месте — текст ошибки, после исправления — снова подсказка
  const hints = {};
  const setError = (id, text) => {
    $(`[data-af="${id}"]`, root).classList.toggle('is-error', !!text);
    const h = $f(`${id}-hint`);
    if (!h) return;
    if (text && !h.classList.contains('is-error')) hints[id] = h.innerHTML;
    h.classList.toggle('is-error', !!text);
    if (text) h.textContent = text;
    else if (hints[id] !== undefined) h.innerHTML = hints[id];
  };
  const filled = (id) => $(`[data-af="${id}"]`, root).classList.toggle('is-filled', !!$f(id).value);

  // ---------- кредит: общий с калькулятором ----------
  const paintCredit = () => {
    const { p, amount, term } = credit();
    Object.assign(d, { product_id: p.id, amount, term_months: term });
    if (!p.purposes.includes(d.purpose)) d.purpose = p.purposes[0];
    $f('product').innerHTML = session.products
      .map((x) => `<option value="${x.id}" ${x.id === p.id ? 'selected' : ''}>${esc(x.name)}</option>`)
      .join('');
    if (document.activeElement !== $f('amount')) $f('amount').value = `${fmt(amount)} ₽`;
    $f('amount-hint').textContent = `От ${shortRub(p.min_amount).replace(' ₽', '')} до ${shortRub(p.max_amount)}`;
    $f('amount-hint').classList.remove('is-error');
    $('[data-af="amount"]', root).classList.remove('is-error');
    const fixed = p.min_term === p.max_term;
    $f('term').innerHTML = termOptions(p, term)
      .map((m) => `<option value="${m}" ${m === term ? 'selected' : ''}>${months(m)}</option>`)
      .join('');
    $f('term').disabled = fixed;
    $f('term-hint').textContent = fixed ? `Лимит открывается на ${months(p.min_term)}, потом его можно продлить` : '';
    $f('purpose').innerHTML = p.purposes.map((x) => `<option ${x === d.purpose ? 'selected' : ''}>${esc(x)}</option>`).join('');
    saveDraft(d);
  };

  // ---------- компания ----------
  const paintCompany = () => {
    const box = $f('company');
    box.hidden = !company;
    if (company) box.innerHTML = companyLines(company);
    $f('revenue-hint').innerHTML = revenueHint(company);
    $f('revenue-hint').classList.remove('is-error');
    $('[data-af="revenue"]', root).classList.remove('is-error');
    delete hints.revenue;
  };
  const loadCompany = async () => {
    company = null;
    if (d.inn) company = await api('GET', `/companies/${d.inn}`).catch(() => null);
    if (!signal.aborted) paintCompany();
  };

  // любое изменение данных делает прошлый расчёт неактуальным
  const changed = () => {
    saveDraft(d);
    if (result) {
      result = null;
      $f('result').innerHTML = `<p class="note">${icon('info', 20)}<span>Данные изменились — рассчитайте снова.</span></p>`;
    }
  };

  // ---------- начальное состояние ----------
  const me = session.me && !isStaff() ? session.me : null;
  if (!d.inn && me) d.inn = me.company.inn;
  paintCredit();
  if (d.revenue) $f('revenue').value = fmt(d.revenue);
  if (d.obligations) $f('obligations').value = fmt(d.obligations);
  companyOptions().then((list) => {
    if (signal.aborted) return;
    $f('inn').insertAdjacentHTML('beforeend', list.map((c) => companyOption(c, d.inn, false)).join(''));
    if (!list.some((c) => c.inn === d.inn)) d.inn = '';
    $f('inn').value = d.inn;
    filled('inn');
    loadCompany();
  });

  // ---------- события ----------
  on(
    root,
    'change',
    '#af-product',
    (_, el) => {
      setCredit({ product: el.value });
      changed();
    },
    signal,
  );
  on(
    root,
    'input',
    '#af-amount',
    (_, el) => {
      const v = +digits(el.value);
      el.value = v ? fmt(v) : '';
    },
    signal,
  );
  const applyAmount = () => {
    const v = +digits($f('amount').value);
    if (!v) return setError('amount', REQUIRED);
    const { p } = credit();
    setCredit({ amount: Math.min(p.max_amount, Math.max(p.min_amount, v)) });
    changed();
  };
  on(root, 'focusout', '#af-amount', applyAmount, signal);
  on(
    root,
    'focusin',
    '#af-amount',
    (_, el) => {
      el.value = fmt(credit().amount);
    },
    signal,
  );
  on(
    root,
    'change',
    '#af-term',
    (_, el) => {
      setCredit({ term: +el.value });
      changed();
    },
    signal,
  );
  on(
    root,
    'change',
    '#af-purpose',
    (_, el) => {
      d.purpose = el.value;
      changed();
    },
    signal,
  );
  on(
    root,
    'change',
    '#af-inn',
    (_, el) => {
      d.inn = el.value;
      filled('inn');
      setError('inn', '');
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
      $f('revenue').value = fmt(d.revenue);
      setError('revenue', '');
      changed();
    },
    signal,
  );
  on(
    root,
    'input',
    '#af-revenue, #af-obligations',
    (_, el) => {
      const v = +digits(el.value);
      el.value = v ? fmt(v) : '';
      d[el.id === 'af-revenue' ? 'revenue' : 'obligations'] = v;
      if (el.id === 'af-revenue') setError('revenue', '');
      changed();
    },
    signal,
  );
  on(
    root,
    'submit',
    '#af-form',
    async (e) => {
      e.preventDefault();
      if (document.activeElement === $f('amount')) applyAmount();
      // обязательные поля подсвечиваем все сразу, как в анкетах alfabank.ru
      const missing = [!d.inn && 'inn', !(d.revenue > 0) && 'revenue'].filter(Boolean);
      missing.forEach((id) => setError(id, REQUIRED));
      if (!d.revenue && company) {
        hints.revenue = revenueHint(company);
        $f('revenue-hint').innerHTML = `${REQUIRED} · ${hints.revenue}`;
      }
      if (missing.length) {
        $f(missing[0]).focus();
        return;
      }
      const btn = $f('prescore');
      btn.disabled = true;
      btn.textContent = 'Считаем…';
      try {
        result = await api('POST', '/prescore', level1Body(d));
        if (signal.aborted) return;
        $f('result').innerHTML = prescoreCard(result, d.inn);
        $f('result').scrollIntoView({ behavior: 'smooth', block: 'start' });
      } catch (ex) {
        const id = { inn: 'inn', 'financials.annual_revenue': 'revenue', amount: 'amount', term_months: 'term' }[ex.field];
        if (id) setError(id, ex.message);
        else $f('result').innerHTML = `<p class="note text-bad">${esc(ex.message)}</p>`;
      } finally {
        btn.disabled = false;
        btn.textContent = 'Рассчитать предварительно';
      }
    },
    signal,
  );
  on(
    root,
    'click',
    '#esia-go',
    async (_, btn) => {
      btn.disabled = true;
      await continueToLevel2(d);
      btn.disabled = false;
    },
    signal,
  );

  return {
    /** Калькулятор изменился сам: обновить поля кредита и сбросить устаревший расчёт. */
    sync() {
      paintCredit();
      changed();
    },
  };
}
