// Калькулятор: сумма и срок — ползунком или числом с клавиатуры, платёж считает сервер (единый источник расчёта).
import { quote } from '../core/api.js';
import { $, debounce } from '../core/dom.js';
import { fmt, rub, months, digits } from '../core/format.js';

const pct = (v, min, max) => (max === min ? 100 : ((v - min) / (max - min)) * 100);

/** Разметка полей суммы и срока. У продукта с фиксированным сроком (овердрафт) — без ползунка. */
export function calcFields(p, amount, term) {
  const fixed = p.min_term === p.max_term;
  return `
    <div class="calc__field">
      <div class="calc__label"><span>Сумма кредита</span><span>от ${fmt(p.min_amount)} до ${fmt(p.max_amount)} ₽</span></div>
      <input class="input input--lg" data-calc="amount-text" inputmode="numeric" value="${fmt(amount)} ₽" aria-label="Сумма кредита">
      <input class="range" type="range" data-calc="amount" min="${p.min_amount}" max="${p.max_amount}" step="10000" value="${amount}" style="--p:${pct(amount, p.min_amount, p.max_amount)}%" aria-label="Сумма кредита, ползунок">
      <div class="range-scale"><span>${fmt(p.min_amount)}</span><span>${fmt(p.max_amount)}</span></div>
    </div>
    ${fixed ? `
    <div class="calc__field" data-calc-fixed>
      <div class="calc__label"><span>Срок</span><span>фиксированный</span></div>
      <input class="input input--lg" data-calc="term-text" value="${months(p.min_term)}" readonly tabindex="-1">
      <input type="hidden" data-calc="term" value="${p.min_term}">
      <p class="calc__note">Лимит открывается на ${months(p.min_term)}, потом его можно продлить.</p>
    </div>` : `
    <div class="calc__field">
      <div class="calc__label"><span>Срок</span><span>от ${months(p.min_term)} до ${months(p.max_term)}</span></div>
      <input class="input input--lg" data-calc="term-text" inputmode="numeric" value="${months(term)}" aria-label="Срок кредита, месяцев">
      <input class="range" type="range" data-calc="term" min="${p.min_term}" max="${p.max_term}" step="1" value="${term}" style="--p:${pct(term, p.min_term, p.max_term)}%" aria-label="Срок кредита, ползунок">
      <div class="range-scale"><span>${months(p.min_term)}</span><span>${months(p.max_term)}</span></div>
    </div>`}`;
}

/**
 * Связывает текстовое поле с ползунком: тянешь ползунок — меняется текст, вводишь число — двигается ползунок.
 * При фокусе остаётся голое число и выделяется целиком; значение применяется по Enter или уходу с поля
 * и прижимается к границам продукта.
 */
function linkField(text, range, show, onChange) {
  const paint = () => range.style.setProperty('--p', `${pct(+range.value, +range.min, +range.max)}%`);
  const apply = (v) => {
    range.value = Math.min(+range.max, Math.max(+range.min, v || +range.min));
    text.value = show(+range.value);
    paint();
    onChange();
  };
  range.addEventListener('input', () => apply(+range.value));

  let justFocused = false;
  text.addEventListener('focus', () => { text.value = fmt(range.value); text.select(); justFocused = true; });
  text.addEventListener('mouseup', (e) => { if (justFocused) { e.preventDefault(); justFocused = false; } });
  text.addEventListener('input', () => { const v = +digits(text.value); text.value = v ? fmt(v) : ''; });
  text.addEventListener('blur', () => apply(+digits(text.value)));
  text.addEventListener('keydown', (e) => { if (e.key === 'Enter') text.blur(); });
}

/** Подключает поля калькулятора. onChange({ amount, term }) — на каждое изменение. */
export function bindCalc(root, p, onChange) {
  const amount = $('[data-calc=amount]', root);
  const term = $('[data-calc=term]', root);
  const emit = () => onChange({ amount: +amount.value, term: +term.value });
  linkField($('[data-calc=amount-text]', root), amount, (v) => `${fmt(v)} ₽`, emit);
  if (term.type === 'range') linkField($('[data-calc=term-text]', root), term, months, emit); // у овердрафта срок фиксирован
}

/**
 * Живой расчёт: побеждает последний запрос, промежуточные ответы отбрасываются;
 * после закрытия экрана (signal) ответы игнорируются.
 */
export function createQuoter(onQuote, onError, signal) {
  let seq = 0;
  const run = debounce(async (productId, amount, term, my) => {
    if (signal.aborted) return;
    try {
      const q = await quote(productId, amount, term);
      if (my === seq && !signal.aborted) onQuote(q);
    } catch (e) {
      if (my === seq && !signal.aborted) onError?.(e);
    }
  }, 60);
  signal.addEventListener('abort', () => run.cancel(), { once: true });
  return (productId, amount, term) => run(productId, amount, term, ++seq);
}

/** Блок результата расчёта (используется на главной). */
export const quoteRows = (q) => `
  <div class="kv">
    <div><span>Ставка</span><b>от ${String(q.rate).replace('.', ',')}%</b></div>
    <div><span>Переплата</span><b>${rub(q.overpayment)}</b></div>
    <div><span>Всего вернуть</span><b>${rub(q.total)}</b></div>
  </div>`;
