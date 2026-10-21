// Главная: продуктовая страница кредитов для бизнеса. Калькулятор — на первом экране.
import { $, $$, on, storage } from '../core/dom.js';
import { esc, fmt, months, rate, rub } from '../core/format.js';
import { session, product } from '../core/session.js';
import { accordion } from '../ui/accordion.js';
import { icon } from '../ui/icons.js';
import { calcFields, bindCalc, createQuoter, quoteRows } from '../features/calculator.js';
import { startApplication } from '../features/loan.js';

const STEPS = [
  ['Рассчитайте платёж', 'Выберите кредит, сумму и срок в калькуляторе.'],
  ['Заполните анкету', 'Компания и контакты уже подставлены из профиля — останутся выручка и согласие.'],
  ['Получите решение', 'Через 10–20 секунд: одобрение, другие условия или понятное объяснение.'],
  ['Подпишите онлайн', 'Договор подписывается в кабинете, деньги приходят на расчётный счёт.'],
];

const REQUIREMENTS = [
  'ИП или ООО, зарегистрированные в России',
  'Компания действует по данным ЕГРЮЛ/ЕГРИП',
  'Нет текущих просрочек по кредитам',
  'Платежи по всем кредитам — не больше 50% месячной выручки',
];

const FAQ = [
  ['Кому доступен кредит?', 'ИП и ООО, зарегистрированным в России. Компания должна действовать по данным ЕГРЮЛ/ЕГРИП, а платежи по всем кредитам не должны превышать половину месячной выручки.'],
  ['Какие документы нужны?', 'Никаких. Данные компании берём из ЕГРЮЛ, выручку можно подставить из данных ФНС, договор подписывается простой электронной подписью в кабинете.'],
  ['Сколько ждать решения?', 'Обычно 10–20 секунд после отправки анкеты. Страницу можно закрыть — статус сохранится в кабинете.'],
  ['Что если банк предложит другие условия?', 'Покажем таблицу «Вы просили / Банк предлагает» с суммой, сроком, ставкой, платежом и переплатой и объясним причину на цифрах из анкеты. Принять или отказаться — решаете вы.'],
  ['Можно ли погасить досрочно?', 'Да. Платежи вносятся с расчётного счёта прямо в кабинете.'],
];

export default async function landing(root, { signal, scroll }) {
  const products = session.products;
  const saved = storage.get('calc', {});
  let p = product(saved.product) || products[0];
  let amount = saved.product === p.id && saved.amount ? saved.amount : p.default_amount;
  let term = saved.product === p.id && saved.term ? saved.term : p.default_term;

  const maxAmount = Math.max(...products.map((x) => x.max_amount));
  const minRate = Math.min(...products.map((x) => x.rate));
  const maxTerm = Math.max(...products.map((x) => x.max_term));

  root.className = 'page page--landing';
  root.innerHTML = `
    <section class="hero">
      <div class="container hero__in">
        <div>
          <p class="hero__crumbs">Малому бизнесу и ИП · Кредиты</p>
          <h1>Кредит для бизнеса онлайн</h1>
          <p class="hero__lead">Анкета за 3 минуты без визита в банк и документов. Решение за 15 секунд, деньги — сразу на расчётный счёт.</p>
          <div class="facts">
            <div><b>до ${fmt(maxAmount / 1e6)} млн ₽</b><span>сумма</span></div>
            <div><b>от ${rate(minRate)}</b><span>ставка годовых</span></div>
            <div><b>до ${Math.round(maxTerm / 12)} лет</b><span>срок</span></div>
            <div><b>15 секунд</b><span>решение</span></div>
          </div>
          <div class="row row--wrap">
            <button class="btn btn--primary" data-start="business-dev">Оформить онлайн</button>
            <button class="btn btn--ghost" data-scroll="how">Как получить</button>
          </div>
        </div>
        <div class="calc" id="calc">
          <div class="chips" role="radiogroup" aria-label="Кредит">${products.map((x) => `
            <button class="chip ${x.id === p.id ? 'is-on' : ''}" data-calc-product="${x.id}" role="radio" aria-checked="${x.id === p.id}">${esc(x.name)}</button>`).join('')}
          </div>
          <div class="stack" id="calc-fields"></div>
          <div class="calc__result">
            <div class="calc__pay"><span>Платёж в месяц</span><b id="calc-pay">—</b></div>
            <div id="calc-rows"></div>
            <button class="btn btn--primary btn--block" data-calc-apply>Оформить онлайн</button>
            <p class="calc__note">Расчёт предварительный. Параметры перенесутся в анкету — вводить заново не нужно.</p>
          </div>
        </div>
      </div>
    </section>

    <section class="section" id="products">
      <div class="container">
        <div class="section__head"><h2 class="title-f">Выберите кредит</h2></div>
        <div class="products">${products.map((x) => `
          <article class="product" data-product="${x.id}">
            ${x.badge ? `<span class="product__badge">${esc(x.badge)}</span>` : ''}
            <h3>${esc(x.name)}</h3>
            <p>${esc(x.description)}</p>
            <dl class="kv">
              <div><dt>Ставка</dt><dd>от ${rate(x.rate)}</dd></div>
              <div><dt>Сумма</dt><dd>до ${fmt(x.max_amount)} ₽</dd></div>
              <div><dt>Срок</dt><dd>${x.min_term === x.max_term ? months(x.max_term) : `до ${months(x.max_term)}`}</dd></div>
            </dl>
            <div class="product__actions">
              <button class="btn btn--primary btn--sm" data-start="${x.id}">Оформить</button>
              <button class="btn btn--secondary btn--sm" data-calc-pick="${x.id}">Рассчитать</button>
            </div>
          </article>`).join('')}
        </div>
      </div>
    </section>

    <section class="section" id="how">
      <div class="container">
        <div class="section__head"><h2 class="title-f">Как получить кредит</h2></div>
        <ol class="steps">${STEPS.map(([t, d]) => `<li><h3>${t}</h3><p>${d}</p></li>`).join('')}</ol>
      </div>
    </section>

    <section class="section" id="requirements">
      <div class="container grid-2">
        <div class="card card--flat">
          <h3 class="card__title">Требования</h3>
          <ul class="checklist">${REQUIREMENTS.map((r) => `<li>${icon('checkCircle', 20)}<span>${r}</span></li>`).join('')}</ul>
        </div>
        <div class="card card--flat">
          <h3 class="card__title">Документы</h3>
          <p class="callout">Не нужны</p>
          <p class="muted">Данные компании берём из ЕГРЮЛ, выручку — из данных налоговой. Договор подписывается простой электронной подписью в кабинете.</p>
        </div>
      </div>
    </section>

    <section class="section" id="faq">
      <div class="container">
        <div class="section__head"><h2 class="title-f">Частые вопросы</h2></div>
        <div>${FAQ.map(([q, a], i) => accordion({ key: `faq-${i}`, header: q, body: `<p>${a}</p>` })).join('')}</div>
      </div>
    </section>`;

  // --- калькулятор ---
  const pay = $('#calc-pay', root);
  const quoter = createQuoter((q) => {
    pay.textContent = rub(q.monthly_payment);
    pay.classList.remove('is-stale');
    $('#calc-rows', root).innerHTML = quoteRows(q);
  }, () => { pay.textContent = '—'; }, signal);

  const recalc = () => {
    pay.classList.add('is-stale');
    quoter(p.id, amount, term);
    storage.set('calc', { product: p.id, amount, term });
  };
  const paintFields = () => {
    const box = $('#calc-fields', root);
    box.innerHTML = calcFields(p, amount, term);
    bindCalc(box, p, (v) => { ({ amount, term } = v); recalc(); });
    recalc();
  };
  const pick = (id) => {
    p = product(id);
    amount = p.default_amount;
    term = p.default_term;
    $$('[data-calc-product]', root).forEach((c) => { const on = c.dataset.calcProduct === id; c.classList.toggle('is-on', on); c.setAttribute('aria-checked', on); });
    paintFields();
  };
  paintFields();

  // --- действия ---
  on(root, 'click', '[data-calc-product]', (_, el) => pick(el.dataset.calcProduct), signal);
  on(root, 'click', '[data-calc-pick]', (_, el) => { pick(el.dataset.calcPick); $('#calc', root).scrollIntoView({ behavior: 'smooth', block: 'center' }); }, signal);
  on(root, 'click', '[data-calc-apply]', () => startApplication(p.id, amount, term), signal);
  on(root, 'click', '[data-start]', (_, el) => startApplication(el.dataset.start), signal);
  on(root, 'click', '[data-scroll]', (_, el) => $(`#${el.dataset.scroll}`, root)?.scrollIntoView({ behavior: 'smooth' }), signal);

  if (scroll) requestAnimationFrame(() => $(`#${scroll}`, root)?.scrollIntoView({ behavior: 'smooth', block: scroll === 'calc' ? 'center' : 'start' }));
}
