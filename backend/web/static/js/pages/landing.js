// Главная: продуктовая страница кредитов для бизнеса — вёрстка по макету Figma «УККП 2» (design/figma-main.json).
// Все действия прежние: калькулятор считает сервер, «Оформить» открывает анкету продукта,
// «Рассчитать» переносит продукт в калькулятор, вопросы раскрываются.
import { $, on, storage } from '../core/dom.js';
import { esc, fmt, months, rate, rub, shortRub } from '../core/format.js';
import { session, product } from '../core/session.js';
import { accordion } from '../ui/accordion.js';
import { calcFields, bindCalc, createQuoter } from '../features/calculator.js';
import { startApplication } from '../features/loan.js';

const IMG = 'img/landing/';

// Иконки — оригинальные файлы из архива макета; размер — как у слоя в Figma (молния 42, остальные 40).
const BENEFITS = [
  ['bolt.svg', 42, 'Быстрое решение', 'Мы проверяем компанию и считаем нагрузку автоматически – ответ приходит, пока страница открыта.'],
  [
    'doc.svg',
    40,
    'Без лишних документов',
    'Данные компании берем из ЕГРЮЛ, выручку – из налоговой. Не нужно сканировать пачки документов.',
  ],
  [
    'scales.svg',
    40,
    'Прозрачный процесс',
    'Показываем решение системы, плюсы и минусы заявки. При других условиях – сравнение с тем, что вы просили.',
  ],
  [
    'wallet.svg',
    40,
    'Всё в одном кабинете',
    'Подписание, графики и платежи – там же, где и заявка. Деньги приходят сразу на расчетный счет',
  ],
];

// Картинки продуктов из макета; у продукта без картинки карточка остаётся без иллюстрации.
const PRODUCT_IMG = {
  'business-dev': 'business-dev.webp',
  'working-capital': 'working-capital.webp',
  equipment: 'equipment.webp',
  overdraft: 'overdraft.webp',
};

// Названия карточек — из макета (в каталоге продукт называется «На оборудование и транспорт»).
const PRODUCT_TITLE = { equipment: 'На транспорт и оборудование' };

// Подписи карточек — из макета; у овердрафта в макете повторён текст «Кредита на развитие»,
// поэтому для него остаётся описание продукта из каталога.
const PRODUCT_TEXT = {
  'business-dev': 'На запуск направления, ремонт, маркетинг и любые другие цели бизнеса',
  'working-capital': 'На закупку товара и сырья, кассовые разрывы, зарплаты сотрудникам',
  equipment: 'Покупка оборудования, спецтехники и перевозки',
};

const STEPS = [
  ['Заполните анкету', 'Получите предварительный расчет. Данные компании подтянутся автоматически.'],
  ['Пройдите проверку', 'Мы проверим компанию и покажем условия прямо на странице.'],
  ['Получите деньги', 'Подпишите договор и деньги поступят на расчетный счет.'],
];

const REQUIREMENTS = [
  'ИП или ООО, зарегистрированные в России',
  'Компания действует по правилам ЕГРЮЛ/ЕГРИП',
  'Нет текущих просрочек по кредитам',
  'Платежи по кредитам – не более 50% месячной выручки',
];

const FAQ = [
  [
    'Кому доступен кредит?',
    'ИП и ООО, зарегистрированным в России. Компания должна действовать по данным ЕГРЮЛ/ЕГРИП, а платежи по всем кредитам не должны превышать половину месячной выручки.',
  ],
  [
    'Какие документы нужны?',
    'Только паспорт. Данные компании берём из ЕГРЮЛ, выручку можно подставить из данных ФНС, договор подписывается электронной подписью в кабинете.',
  ],
  ['Сколько ждать решение?', 'Обычно 10–20 секунд после отправки анкеты. Страницу можно закрыть — статус сохранится в кабинете.'],
  [
    'Что если банк предложит другие условия?',
    'Покажем таблицу «Вы просили / Банк предлагает» с суммой, сроком, ставкой, платежом и переплатой и объясним причину на цифрах из анкеты. Принять или отказаться — решаете вы.',
  ],
  ['Можно ли погасить кредит досрочно?', 'Да. Платежи вносятся с расчётного счёта прямо в кабинете.'],
];

// шеврон вопросов — иконка «развернуть» из архива макета (слой 20×20), при раскрытии поворачивается
const EXPAND = `<img src="${IMG}expand.svg" width="20" height="20" alt="">`;

const sectionTitle = (text) => `<h2 class="lp-title">${text}</h2>`;

export default async function landing(root, { signal, scroll }) {
  const products = session.products;
  const saved = storage.get('calc', {});
  let p = product(saved.product) || products[0];
  let amount = saved.product === p.id && saved.amount ? saved.amount : p.default_amount;
  let term = saved.product === p.id && saved.term ? saved.term : p.default_term;

  root.className = 'page page--landing';
  root.innerHTML = `
    <section class="hero lp-hero platinum">
      <div class="lp-container lp-hero__in">
        <div class="lp-hero__text">
          <h1>Деньги, когда они нужны бизнесу</h1>
          <p class="lp-hero__lead">Кредит на развитие бизнеса – решение за 15 секунд, без визита в банк</p>
          <button class="lp-btn lp-btn--hero" data-start="business-dev">Оформить заявку</button>
        </div>
        <img class="lp-hero__art" src="${IMG}hero.webp" width="519" height="323" alt="" fetchpriority="high">
      </div>
    </section>

    <div class="lp-body">

    <section class="lp-section lp-section--calc" id="calc">
      <div class="lp-container">
        ${sectionTitle('Рассчитайте выгоду')}
        <div class="lp-calc">
          <div class="lp-calc__form">
            <div class="lp-calc__fields" id="calc-fields"></div>
            <div class="lp-need">
              <span class="lp-need__title">Вам понадобится</span>
              <span class="lp-need__items">
                <span class="lp-need__item"><img src="${IMG}passport.svg" width="30" height="30" alt="">Паспорт</span>
                <span class="lp-need__item"><img src="${IMG}house.svg" width="30" height="30" alt="">Залог</span>
              </span>
            </div>
          </div>
          <div class="lp-offer">
            <h3 class="lp-offer__title">Наше предложение</h3>
            <div class="lp-offer__pay"><span>Ежемесячный платеж</span><b id="calc-pay">—</b></div>
            <div class="lp-offer__row"><span>Ставка*</span><b id="calc-rate">—</b></div>
            <div class="lp-offer__row"><span>Переплата</span><b id="calc-over">—</b></div>
            <button class="lp-btn lp-btn--block" data-calc-apply>Оформить онлайн</button>
            <p class="lp-offer__note">Расчет предварительный. Параметры перенесутся в анкету – вводить заново не нужно. *Ставка от 17,99% до 55,39% годовых определяется индивидуально</p>
          </div>
        </div>
      </div>
    </section>

    <section class="lp-section lp-section--benefits" id="benefits">
      <div class="lp-container">
        <div class="lp-cards4 lp-benefits">${BENEFITS.map(
          ([img, size, t, d]) => `
          <div class="lp-benefit">
            <span class="lp-benefit__ic"><img src="${IMG}${img}" width="${size}" height="${size}" alt=""></span>
            <h3>${t}</h3>
            <p>${d}</p>
          </div>`,
        ).join('')}
        </div>
      </div>
    </section>

    <section class="lp-section lp-section--products" id="products">
      <div class="lp-container">
        ${sectionTitle('Выберите кредит')}
        <div class="lp-cards4 lp-products">${products
          .map(
            (x) => `
          <article class="lp-product" data-product="${x.id}">
            <h3 class="lp-product__title">${esc(PRODUCT_TITLE[x.id] || x.name)}</h3>
            <p class="lp-product__text">${esc(PRODUCT_TEXT[x.id] || x.description)}</p>
            <div class="lp-product__art">${PRODUCT_IMG[x.id] ? `<img src="${IMG}${PRODUCT_IMG[x.id]}" width="160" height="160" alt="" loading="lazy">` : ''}</div>
            <dl class="lp-product__kv">
              <div><dt>Ставка</dt><dd>от ${rate(x.rate)}</dd></div>
              <div><dt>Сумма</dt><dd>до ${shortRub(x.max_amount)}</dd></div>
            </dl>
            <div class="lp-product__actions">
              <button class="lp-btn lp-btn--sm" data-start="${x.id}" aria-label="Оформить: ${esc(x.name)}">Оформить</button>
              <button class="lp-btn lp-btn--sm lp-btn--gray" data-calc-pick="${x.id}" aria-label="Рассчитать: ${esc(x.name)}">Рассчитать</button>
            </div>
          </article>`,
          )
          .join('')}
        </div>
      </div>
    </section>

    <section class="lp-section lp-section--how" id="how">
      <div class="lp-container">
        <h2 class="lp-title-c">Как получить кредит</h2>
        <ol class="lp-road">${STEPS.map(
          ([t, d], i) => `
          <li class="lp-road__step">
            <img class="lp-road__ring" src="${IMG}step-${i + 1}.svg" width="64" height="64" alt="Шаг ${i + 1}">
            <div class="lp-road__card"><h3>${t}</h3><p>${d}</p></div>
          </li>`,
        ).join('')}
        </ol>
      </div>
    </section>

    <section class="lp-section lp-section--req" id="requirements">
      <div class="lp-container">
        <div class="lp-req">
          <div class="lp-req__card">
            <h2>Требования</h2>
            <ul class="lp-checklist">${REQUIREMENTS.map((r) => `<li><img src="${IMG}check.svg" width="19" height="19" alt="">${r}</li>`).join('')}</ul>
          </div>
          <div class="lp-req__card">
            <h2>Документы</h2>
            <p class="lp-req__lead">Понадобится только паспорт</p>
            <p>Данные компании берем из ЕГРЮЛ/ЕГРИП, выручку – из данных налоговой. Договор подписывается системой Госключ.</p>
          </div>
        </div>
      </div>
    </section>

    <section class="lp-section lp-section--faq" id="faq">
      <div class="lp-container">
        ${sectionTitle('Частые вопросы')}
        <div class="faq-list">${FAQ.map(([q, a], i) => accordion({ key: `faq-${i}`, header: q, body: `<p>${a}</p>`, cls: 'faq', ctl: EXPAND })).join('')}</div>
      </div>
    </section>
    </div>`;

  // --- калькулятор ---
  const pay = $('#calc-pay', root);
  const quoter = createQuoter(
    (q) => {
      pay.textContent = rub(q.monthly_payment);
      pay.classList.remove('is-stale');
      $('#calc-rate', root).textContent = `от ${String(q.rate).replace('.', ',')}%`;
      $('#calc-over', root).textContent = `${fmt(q.overpayment)} ₽`;
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
    // шкала по макету: только границы, срок — месяцами («6 месяцев», «60 месяцев»)
    box.innerHTML = calcFields(p, amount, term, { ends: true, termScale: months });
    bindCalc(box, p, (v) => {
      ({ amount, term } = v);
      recalc();
    });
    recalc();
  };
  // продукт калькулятора выбирается кнопкой «Рассчитать» в карточке продукта
  const pick = (id) => {
    p = product(id);
    amount = p.default_amount;
    term = p.default_term;
    paintFields();
  };
  paintFields();

  // --- действия ---
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
