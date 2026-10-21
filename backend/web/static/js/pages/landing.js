// Главная: продуктовая страница кредитов для бизнеса. Калькулятор — на первом экране.
import { $, $$, on, storage } from '../core/dom.js';
import { esc, fmt, months, rate, rub } from '../core/format.js';
import { session, product } from '../core/session.js';
import { accordion } from '../ui/accordion.js';
import { heroArt, productArt } from '../ui/art.js';
import { icon } from '../ui/icons.js';
import { calcFields, bindCalc, createQuoter, quoteRows } from '../features/calculator.js';
import { startApplication } from '../features/loan.js';

// «Как получить»: три шага, как на alfabank.ru. Кольцо над карточкой заполняется по мере продвижения.
const STEPS = [
  ['Заполните анкету', 'Данные компании подставятся из профиля — останутся выручка и согласие'],
  ['Получите решение за 15 секунд', 'Банк проверит компанию и покажет условия прямо на странице'],
  ['Подпишите и получите деньги', 'Договор подписывается в кабинете, деньги приходят на расчётный счёт'],
];

/** Кольцо шага: серый пунктир и красная дуга на долю пройденного пути. */
const ring = (i, n) => {
  const len = 2 * Math.PI * 33;
  return `<svg class="road__ring" viewBox="0 0 72 72" aria-hidden="true">
    <circle cx="36" cy="36" r="33" fill="none" stroke="#C9CBCE" stroke-width="2" stroke-dasharray="3 4"/>
    <circle cx="36" cy="36" r="33" fill="none" stroke="#EF3124" stroke-width="2.5" stroke-linecap="round"
      stroke-dasharray="${(len * (i + 1)) / n} ${len}" transform="rotate(-90 36 36)"/>
  </svg>`;
};

const BENEFITS = [
  ['bolt', 'Решение за 15 секунд', 'Банк проверяет компанию и считает нагрузку автоматически — ответ приходит, пока открыта страница.'],
  ['doc', 'Без документов', 'Данные компании берём из ЕГРЮЛ, выручку — из налоговой. Не нужно ничего сканировать.'],
  ['percent', 'Понятное решение', 'Показываем балл, плюсы и минусы заявки. При других условиях — сравнение с тем, что вы просили.'],
  ['wallet', 'Всё в одном кабинете', 'Подписание, график и платежи — там же, где заявка. Деньги приходят на расчётный счёт.'],
];

const REQUIREMENTS = [
  'ИП или ООО, зарегистрированные в России',
  'Компания действует по данным ЕГРЮЛ/ЕГРИП',
  'Нет текущих просрочек по кредитам',
  'Платежи по всем кредитам — не больше 50% месячной выручки',
];

const FAQ = [
  [
    'Кому доступен кредит?',
    'ИП и ООО, зарегистрированным в России. Компания должна действовать по данным ЕГРЮЛ/ЕГРИП, а платежи по всем кредитам не должны превышать половину месячной выручки.',
  ],
  [
    'Какие документы нужны?',
    'Никаких. Данные компании берём из ЕГРЮЛ, выручку можно подставить из данных ФНС, договор подписывается простой электронной подписью в кабинете.',
  ],
  ['Сколько ждать решения?', 'Обычно 10–20 секунд после отправки анкеты. Страницу можно закрыть — статус сохранится в кабинете.'],
  [
    'Что если банк предложит другие условия?',
    'Покажем таблицу «Вы просили / Банк предлагает» с суммой, сроком, ставкой, платежом и переплатой и объясним причину на цифрах из анкеты. Принять или отказаться — решаете вы.',
  ],
  ['Можно ли погасить досрочно?', 'Да. Платежи вносятся с расчётного счёта прямо в кабинете.'],
];

export default async function landing(root, { signal, scroll }) {
  const products = session.products;
  const saved = storage.get('calc', {});
  let p = product(saved.product) || products[0];
  let amount = saved.product === p.id && saved.amount ? saved.amount : p.default_amount;
  let term = saved.product === p.id && saved.term ? saved.term : p.default_term;

  const minRate = Math.min(...products.map((x) => x.rate));

  root.className = 'page page--landing';
  root.innerHTML = `
    <section class="hero">
      <span class="glint glint--1"></span><span class="glint glint--2"></span><span class="glint glint--3"></span><span class="glint glint--4"></span>
      <div class="container hero__in">
          <div class="hero__text">
            <h1>Кредиты для бизнеса от ${rate(minRate)}</h1>
            <p class="hero__lead">Деньги на развитие бизнеса — решение за 15 секунд, без визита в банк</p>
            <button class="btn btn--primary btn--lg" data-start="business-dev">Оформить заявку</button>
          </div>
          ${heroArt()}
      </div>
    </section>

    <div class="landing-body">

    <section class="section" id="calc">
      <div class="container">
        <div class="section__head"><h2 class="title-f">Рассчитайте кредит</h2></div>
        <div class="calc">
          <div class="calc__form">
            <div class="chips" role="radiogroup" aria-label="Кредит">${products
              .map(
                (x) => `
              <button class="chip ${x.id === p.id ? 'is-on' : ''}" data-calc-product="${x.id}" role="radio" aria-checked="${x.id === p.id}">${esc(x.name)}</button>`,
              )
              .join('')}
            </div>
            <div class="stack" id="calc-fields"></div>
          </div>
          <div class="calc__result">
            <div class="calc__pay"><span>Платёж в месяц</span><b id="calc-pay">—</b></div>
            <div id="calc-rows"></div>
            <button class="btn btn--primary btn--block" data-calc-apply>Оформить онлайн</button>
            <p class="calc__note">Расчёт предварительный. Параметры перенесутся в анкету — вводить заново не нужно.</p>
          </div>
        </div>
      </div>
    </section>

    <section class="section" id="benefits">
      <div class="container">
        <div class="benefits">${BENEFITS.map(
          ([ic, t, d]) => `
          <div class="benefit"><span class="benefit__ic">${icon(ic, 24)}</span><h3>${t}</h3><p>${d}</p></div>`,
        ).join('')}
        </div>
      </div>
    </section>

    <section class="section" id="products">
      <div class="container">
        <div class="section__head"><h2 class="title-f">Выберите кредит</h2></div>
        <div class="products">${products
          .map(
            (x) => `
          <article class="product tile" data-product="${x.id}">
            ${x.badge ? `<span class="product__badge">${esc(x.badge)}</span>` : ''}
            <h3 class="tile__title">${esc(x.name)}</h3>
            <p class="tile__text">${esc(x.description)}</p>
            ${productArt(x.id)}
            <dl class="kv">
              <div><dt>Ставка</dt><dd>от ${rate(x.rate)}</dd></div>
              <div><dt>Сумма</dt><dd>до ${fmt(x.max_amount)} ₽</dd></div>
              <div><dt>Срок</dt><dd>${x.min_term === x.max_term ? months(x.max_term) : `до ${months(x.max_term)}`}</dd></div>
            </dl>
            <div class="product__actions">
              <button class="btn btn--primary btn--sm" data-start="${x.id}">Оформить</button>
              <button class="btn btn--secondary btn--sm" data-calc-pick="${x.id}">Рассчитать</button>
            </div>
          </article>`,
          )
          .join('')}
        </div>
      </div>
    </section>

    <section class="section" id="how">
      <div class="container">
        <h2 class="section__title-c">Как получить кредит</h2>
        <ol class="road">${STEPS.map(
          ([t, d], i) => `
          <li class="road__step">
            <span class="road__dot">${ring(i, STEPS.length)}<b>${i + 1}</b></span>
            <div class="road__card"><h3>${t}</h3><p>${d}</p></div>
          </li>`,
        ).join('')}
        </ol>
      </div>
    </section>

    <section class="section" id="requirements">
      <div class="container grid-2">
        <div class="card card--flat">
          <h2 class="card__title">Требования</h2>
          <ul class="checklist">${REQUIREMENTS.map((r) => `<li>${icon('checkCircle', 20)}<span>${r}</span></li>`).join('')}</ul>
        </div>
        <div class="card card--flat">
          <h2 class="card__title">Документы</h2>
          <p class="callout">Не нужны</p>
          <p class="muted">Данные компании берём из ЕГРЮЛ, выручку — из данных налоговой. Договор подписывается простой электронной подписью в кабинете.</p>
        </div>
      </div>
    </section>

    <section class="section" id="faq">
      <div class="container">
        <h2 class="section__title-l">Частые вопросы</h2>
        <div class="faq-list">${FAQ.map(([q, a], i) => accordion({ key: `faq-${i}`, header: q, body: `<p>${a}</p>`, cls: 'faq' })).join('')}</div>
      </div>
    </section>
    </div>`;

  // --- калькулятор ---
  const pay = $('#calc-pay', root);
  const quoter = createQuoter(
    (q) => {
      pay.textContent = rub(q.monthly_payment);
      pay.classList.remove('is-stale');
      $('#calc-rows', root).innerHTML = quoteRows(q);
    },
    () => {
      pay.textContent = '—';
    },
    signal,
  );

  const recalc = () => {
    pay.classList.add('is-stale');
    quoter(p.id, amount, term);
    storage.set('calc', { product: p.id, amount, term });
  };
  const paintFields = () => {
    const box = $('#calc-fields', root);
    box.innerHTML = calcFields(p, amount, term);
    bindCalc(box, p, (v) => {
      ({ amount, term } = v);
      recalc();
    });
    recalc();
  };
  const pick = (id) => {
    p = product(id);
    amount = p.default_amount;
    term = p.default_term;
    $$('[data-calc-product]', root).forEach((c) => {
      const active = c.dataset.calcProduct === id;
      c.classList.toggle('is-on', active);
      c.setAttribute('aria-checked', active);
    });
    paintFields();
  };
  paintFields();

  // --- действия ---
  on(root, 'click', '[data-calc-product]', (_, el) => pick(el.dataset.calcProduct), signal);
  on(
    root,
    'click',
    '[data-calc-pick]',
    (_, el) => {
      pick(el.dataset.calcPick);
      $('#calc', root).scrollIntoView({ behavior: 'smooth' });
    },
    signal,
  );
  on(root, 'click', '[data-calc-apply]', () => startApplication(p.id, amount, term), signal);
  on(root, 'click', '[data-start]', (_, el) => startApplication(el.dataset.start), signal);

  if (scroll) requestAnimationFrame(() => $(`#${scroll}`, root)?.scrollIntoView({ behavior: 'smooth' }));
}
