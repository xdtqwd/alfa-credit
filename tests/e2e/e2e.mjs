// Браузерные e2e-тесты интерфейса (Playwright, без тест-раннера).
// Запуск: сервер должен работать на BASE_URL (по умолчанию http://localhost:8080)
//   PIPELINE_STEP_DELAY=1s make run   # в одном терминале
//   make e2e                          # в другом (нужны Node 18+ и `npm i -g playwright`)
import { Buffer } from 'node:buffer';
import { chromium, devices } from 'playwright';

const BASE = process.env.BASE_URL || 'http://localhost:8080';
const PASS = 'demo1234';
const DECISION = 20000; // сколько ждём решения банка, мс
const results = [];
let browser;

const fmtRub = (n) => new Intl.NumberFormat('ru-RU').format(n) + ' ₽';
const norm = (s) => (s || '').replace(/\s+/g, ' ').trim();
function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

async function test(name, fn) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => {
    if (m.type() === 'error' && !/401|Failed to load resource/.test(m.text())) errors.push(m.text());
  });
  const t0 = Date.now();
  try {
    await fn(page, ctx);
    assert(errors.length === 0, 'ошибки в консоли: ' + errors.join(' | '));
    results.push({ name, ok: true });
    console.log(`  ✓ ${name} (${Date.now() - t0} мс)`);
  } catch (e) {
    results.push({ name, ok: false });
    console.log(`  ✗ ${name}\n      ${e.message}`);
    await diagnose(page, errors).catch((d) => console.log(`      (диагностика не удалась: ${d.message})`));
    await page.screenshot({ path: `e2e-fail-${results.length}.png`, fullPage: true }).catch(() => {});
  } finally {
    await ctx.close();
  }
}

// При падении печатаем, что было на самом деле: адрес, видимый статус и — если открыта заявка —
// её статус и историю с сервера. Тогда упавший тест в CI объясняет себя без артефактов.
async function diagnose(page, errors) {
  if (errors.length) console.log('      консоль: ' + errors.join(' | ').slice(0, 400));
  const url = page.url();
  const shown = await page
    .evaluate(() =>
      [...document.querySelectorAll('#decision h2, #prescore-result h2, #todo b, .page-head .status, .step.is-current')]
        .map((e) => e.textContent.trim())
        .filter(Boolean)
        .slice(0, 6)
        .join(' · '),
    )
    .catch(() => '');
  console.log(`      страница: ${url}${shown ? ' — ' + shown : ''}`);
  const id = url.match(/#\/cabinet\/applications\/([0-9a-f]+)/)?.[1];
  if (!id) return;
  const a = (await apiAs(page, 'GET', '/applications/' + id)).json;
  if (!a) return;
  const t = (x) => new Date(x).toISOString().slice(11, 23);
  console.log(`      заявка ${a.number}: статус ${a.status}, сумма ${a.amount}, балл ${a.score ?? '—'}, сейчас ${t(Date.now())}`);
  for (const h of (a.history || []).slice(-6)) console.log(`        ${t(h.at)} ${h.status} (${h.actor}) ${h.comment || ''}`.slice(0, 160));
}

async function apiAs(page, method, path, body) {
  return page.evaluate(
    async ([m, p, b]) => {
      const r = await fetch('/api/v1' + p, {
        method: m,
        headers: { 'Content-Type': 'application/json' },
        body: b ? JSON.stringify(b) : undefined,
      });
      return { status: r.status, json: await r.json().catch(() => null) };
    },
    [method, path, body],
  );
}

/** Вход через окно в шапке; после входа клиент попадает в «Профиль и реквизиты». */
async function login(page, phone) {
  await page.goto(BASE + '/#/');
  await page.click('[data-header=login]');
  await page.fill('#auth-phone', phone);
  await page.fill('#auth-password', PASS);
  await page.click('#auth-form button[type=submit]');
  await page.waitForFunction(() => location.hash === '#/cabinet');
  await page.waitForSelector('#requisites');
}

const saved = (page, timeout = 5000) =>
  page.waitForFunction(() => document.querySelector('#save-state')?.textContent === 'Сохранено', null, { timeout });

/** Сумма в калькуляторе: вписать числом и применить. */
async function setAmount(page, box, amount) {
  await page.fill(`${box} [data-calc=amount-text]`, String(amount));
  await page.press(`${box} [data-calc=amount-text]`, 'Enter');
}

/**
 * 1-й уровень — предварительный расчёт без входа (страница #/apply уже открыта).
 * Возвращает тип предварительного решения.
 */
async function level1(page, { product = 'business-dev', amount, inn = '7701234567', revenue, obligations } = {}) {
  await page.waitForSelector('#ap-inn');
  if (product && !(await page.locator(`[data-product="${product}"].is-on`).count())) await page.click(`[data-product="${product}"]`);
  if (amount) await setAmount(page, '#ap-calc', amount);
  await page.selectOption('#ap-inn', inn);
  await page.waitForSelector('#ap-company:not([hidden])');
  if (revenue) await page.fill('#ap-revenue', String(revenue));
  else await page.click('[data-use-fns]');
  if (obligations) await page.fill('#ap-obligations', String(obligations));
  await page.click('#prescore');
  await page.waitForSelector('#prescore-result');
  return page.getAttribute('#prescore-result', 'data-decision');
}

async function prescore(page, opts) {
  await page.goto(BASE + '/#/apply');
  return level1(page, opts);
}

/** Начало 2-го уровня: вход через Госуслуги из результата расчёта (учётка руководителя выбрана сразу). */
async function esia(page) {
  await page.click('#esia-go');
  const step = await Promise.race([
    page.waitForSelector('#esia-ok').then(() => 'modal'),
    page.waitForSelector('[data-key="consent"]').then(() => 'form'),
  ]);
  if (step === 'modal') await page.click('#esia-ok');
  await page.waitForSelector('[data-key="consent"]');
}

/** 2-й уровень: обеспечение, протокол, согласие → полный скоринг. */
async function level2(page, { guarantorInn, collateral, protocol } = {}) {
  await page.waitForSelector('[data-key="consent"]');
  if (collateral) {
    await page.click(`[data-collateral=${collateral.type}]`);
    await page.fill('#ff-coll-value', String(collateral.value));
  }
  if (guarantorInn) {
    if (!(await page.isChecked('#ff-guarantor'))) await page.check('#ff-guarantor');
    await page.fill('#ff-g-name', 'Кузнецов Олег Петрович');
    await page.fill('#ff-g-inn', guarantorInn);
  }
  if (protocol)
    await page.setInputFiles('#ff-protocol', {
      name: 'Протокол ОСУ №3.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from('%PDF-1.4 demo'),
    });
  if (!(await page.isChecked('#ff-consent'))) await page.check('#ff-consent');
  await saved(page);
  await page.click('#submit');
  await page.waitForSelector('.stepper');
}

/** Весь путь клиента до окончательного решения. */
async function applyFull(page, opts = {}) {
  await prescore(page, opts);
  await esia(page);
  await level2(page, opts);
}

const decision = (page, tone) => page.waitForSelector(`#decision.decision--${tone}`, { timeout: DECISION });
const menuOpen = (page) => page.locator('.menu-wrap .menu').count();

(async () => {
  browser = await chromium.launch();
  console.log(`E2E против ${BASE}\n`);

  await test('Гость видит продуктовую страницу: разделы сайта и «Войти», вкладок кабинета нет', async (page) => {
    await page.goto(BASE + '/');
    await page.waitForSelector('.hero h1');
    assert(norm(await page.textContent('.hero h1')) === 'Деньги, когда они нужны бизнесу', 'нет заголовка из макета');
    assert((await page.locator('.header--cabinet').count()) === 0, 'гостю показана шапка кабинета');
    assert(await page.locator('[data-header=login]').isVisible(), 'нет кнопки «Войти»');
  });

  await test('«Оформить» ведёт на 1-й уровень без входа: из карточки — с кредитом, с первого экрана — без, из калькулятора — с его условиями', async (page) => {
    await page.goto(BASE + '/');
    await page.click('#products [data-start="working-capital"]');
    await page.waitForSelector('#ap-inn');
    assert(page.url().endsWith('#/apply'), 'открыт не 1-й уровень: ' + page.url());
    assert((await page.locator('#auth-phone').count()) === 0, 'расчёт не должен требовать входа');
    assert((await page.locator('[data-product="working-capital"].is-on').count()) === 1, 'кредит из карточки не сохранился');
    await page.goto(BASE + '/');
    await page.click('.lp-hero [data-start]');
    await page.waitForSelector('#ap-inn');
    assert((await page.locator('[data-product].is-on').count()) === 0, 'с первого экрана кредит выбирает сам клиент');
    assert(await page.isDisabled('#prescore'), 'без кредита, компании и выручки рассчитывать нечего');
    await page.goto(BASE + '/');
    await setAmount(page, '#calc', 4000000);
    await page.click('#calc [data-calc-apply]');
    await page.waitForSelector('[data-product="business-dev"].is-on');
    assert(norm(await page.inputValue('#ap-calc [data-calc=amount-text]')) === '4 000 000 ₽', 'сумма калькулятора не перенеслась');
    assert(norm(await page.textContent('.badge-demo')) === 'демо', 'ИНН из списка должен быть помечен как демо');
  });

  await test('Калькулятор показывает расчёт сервера (единый источник)', async (page) => {
    await page.goto(BASE + '/');
    await page.waitForFunction(() => /\d/.test(document.querySelector('#calc-pay')?.textContent || ''));
    const shown = norm(await page.textContent('#calc-pay'));
    const q = await apiAs(page, 'POST', '/calc', { product_id: 'business-dev', amount: 3000000, term_months: 24 });
    assert(shown === norm(fmtRub(q.json.monthly_payment)), `на экране ${shown}, сервер ${q.json.monthly_payment}`);
    assert(norm(await page.textContent('#calc-over')) === norm(fmtRub(q.json.overpayment)), 'переплата не совпадает с сервером');
  });

  await test('Главная по макету: «Рассчитать» переносит продукт в калькулятор, ссылки подвала ведут в разделы и кабинет банка', async (page) => {
    await page.goto(BASE + '/');
    await page.waitForFunction(() => /\d/.test(document.querySelector('#calc-pay')?.textContent || ''));
    assert((await page.locator('#products .lp-product').count()) === 4, 'должно быть четыре карточки кредитов');
    await page.click('#products [data-calc-pick="working-capital"]');
    await page.waitForFunction(() => document.querySelector('#calc').getBoundingClientRect().top < 200);
    const q = await apiAs(page, 'POST', '/calc', { product_id: 'working-capital', amount: 1000000, term_months: 12 });
    await page.waitForFunction(
      (t) => document.querySelector('#calc-pay')?.textContent.replace(/\s+/g, ' ').trim() === t,
      norm(fmtRub(q.json.monthly_payment)),
    );
    assert(
      norm(await page.textContent('#calc-rate')) === `от ${String(q.json.rate).replace('.', ',')}%`,
      'ставка калькулятора — не с сервера',
    );
    await page.click('.footer [data-section=faq]');
    await page.waitForFunction(() => document.getElementById('faq').getBoundingClientRect().top < 300);
    await page.click('.footer a[href="#/bank"]');
    await page.waitForSelector('.bank-gate');
    assert(norm(await page.textContent('.footer')).includes('Кабинет администратора'), 'в подвале нет ссылки на кабинет администратора');
  });

  await test('Срок можно вписать числом: ползунок и платёж следуют, лишнее прижимается к границам', async (page) => {
    await page.goto(BASE + '/');
    const term = '#calc [data-calc=term-text]';
    const typeTerm = async (v) => {
      await page.click(term);
      await page.keyboard.type(String(v));
      await page.keyboard.press('Enter');
    };
    await page.waitForFunction(() => /\d/.test(document.querySelector('#calc-pay')?.textContent || ''));
    await typeTerm(36);
    assert(norm(await page.inputValue(term)) === '36 месяцев', 'срок не применился: ' + (await page.inputValue(term)));
    assert((await page.inputValue('#calc [data-calc=term]')) === '36', 'ползунок не сдвинулся');
    const q = await apiAs(page, 'POST', '/calc', { product_id: 'business-dev', amount: 3000000, term_months: 36 });
    await page.waitForFunction(
      (t) => document.querySelector('#calc-pay')?.textContent.replace(/\s+/g, ' ').trim() === t,
      norm(fmtRub(q.json.monthly_payment)),
    );
    await typeTerm(999);
    assert(norm(await page.inputValue(term)) === '60 месяцев', 'срок больше максимума не прижат к 60');
    await typeTerm(1);
    assert(norm(await page.inputValue(term)) === '6 месяцев', 'срок меньше минимума не прижат к 6');
  });

  await test('Вход ведёт в «Профиль и реквизиты»; вкладки кабинета переключаются', async (page) => {
    await login(page, '79000000001');
    assert((await page.locator('.header--cabinet .nav__link.is-active').textContent()) === 'Профиль', 'активна не вкладка «Профиль»');
    const req = norm(await page.textContent('#requisites'));
    for (const k of ['ИНН', 'Расчётный счёт', 'БИК', 'Корр. счёт']) assert(req.includes(k), 'в реквизитах нет: ' + k);
    const me = (await apiAs(page, 'GET', '/me')).json;
    assert(req.includes(me.requisites.account), 'не тот расчётный счёт');
    assert(norm(await page.textContent('#balance')) === norm(fmtRub(me.balance)), 'баланс не совпадает с сервером');
    for (const [href, title] of [
      ['/cabinet/applications', 'Заявки'],
      ['/cabinet/credits', 'Кредиты'],
      ['/cabinet/messages', 'Сообщения'],
      ['/cabinet', 'Профиль'],
    ]) {
      await page.click(`.header--cabinet .nav__link[href="#${href}"]`);
      await page.waitForFunction((t) => document.querySelector('.page-head h1')?.textContent.startsWith(t), title);
    }
  });

  await test('Два уровня: расчёт без входа → Госуслуги → документы → полный скоринг → подписание → выдача → оплата', async (page) => {
    assert((await prescore(page)) === 'approved', 'предварительный расчёт должен одобрить');
    const pre = norm(await page.textContent('#prescore-result'));
    assert(pre.includes('Предварительно одобрено') && pre.includes('без кредитной истории'), 'не объяснено, что расчёт предварительный');
    assert((await page.locator('#prescore-result .scale').count()) === 0, 'у упрощённого скоринга не должно быть шкалы порогов');
    await page.click('#esia-go');
    await page.waitForSelector('.esia__person.is-on');
    assert(
      norm(await page.textContent('.esia__person.is-on')).includes('Смирнов'),
      'учётка руководителя выбранной компании должна быть выбрана сразу',
    );
    assert(norm(await page.textContent('.modal')).includes('имитация'), 'окно Госуслуг должно честно называться демо');
    await page.click('#esia-ok');
    await page.waitForSelector('[data-key="consent"]');
    assert(norm(await page.textContent('[data-key="identity"]')).includes('Смирнов Алексей Викторович'), 'нет личности из Госуслуг');
    assert((await page.inputValue('#ff-p-number')) === '345678', 'паспорт руководителя не подставлен из Госуслуг');
    assert((await page.locator('[data-key="protocol"]').count()) === 0, 'протокол нужен только для суммы больше 25 млн');
    await level2(page);
    await page.waitForSelector('#decision[data-decision=approved] .actions', { timeout: DECISION });
    const table = norm(await page.textContent('table.compare'));
    assert(
      table.includes('Предварительно') && table.includes('Окончательно') && table.includes('−1,0 п.п.'),
      'нет сравнения с расчётом: ' + table,
    );
    assert(norm(await page.textContent('#view')).includes('Документы 2-го уровня'), 'после отправки не видно приложенного');
    const before = (await apiAs(page, 'GET', '/me')).json.balance;
    await page.click('#todo [data-action=accept]');
    await page.check('#sign-ok');
    await page.click('#sign-btn');
    await page.waitForSelector('table.schedule');
    const after = (await apiAs(page, 'GET', '/me')).json.balance;
    const id = page.url().split('/').pop();
    const app = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(after - before === app.amount, `баланс вырос на ${after - before}, а выдано ${app.amount}`);
    assert(norm(await page.textContent('#todo')).includes(norm(fmtRub(app.next_payment))), 'в «Что нужно от вас» не тот платёж');
    await page.click('#todo [data-action=pay]');
    await page.click('#pay-ok');
    await page.waitForFunction(() => /Оплачено 1 из/.test(document.querySelector('#pay-line')?.textContent || ''));
    const paid = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(paid.payments[0].amount === app.schedule[0].payment, 'списано не по графику');
    await page.click('.header--cabinet .nav__link[href="#/cabinet/credits"]');
    await page.waitForSelector(`.loan[data-loan="${app.number}"]`);
  });

  await test('Контрпредложение уже на 1-м уровне: таблица «Вы просили / Банк предлагает» и объяснение на цифрах', async (page) => {
    const type = await prescore(page, { inn: '2224000012', amount: 20000000, obligations: 200000 });
    assert(type === 'counter_offer', 'ожидалось контрпредложение, а не ' + type);
    const table = norm(await page.textContent('#prescore-result table.compare'));
    for (const col of ['Вы просили', 'Банк предлагает', 'Разница', 'Платёж в месяц', 'Переплата', 'Ставка', 'Срок'])
      assert(table.includes(col), 'в таблице нет: ' + col);
    const why = norm(await page.textContent('#prescore-result'));
    assert(why.includes('1 500 000 ₽') && why.includes('50%'), 'объяснение не опирается на выручку из анкеты');
    assert(await page.locator('#esia-go').isVisible(), 'с другими условиями тоже можно перейти ко 2-му уровню');
  });

  await test('Уточнение на 1-м уровне: выручка сильно выше ФНС → «Что сделать», после правки — пересчёт', async (page) => {
    assert((await prescore(page, { inn: '2224000012', revenue: 50000000 })) === 'need_info', 'ожидалось уточнение');
    assert(norm(await page.textContent('#prescore-result')).includes('Что сделать'), 'нет подсказки, что делать');
    assert((await page.locator('#esia-go').count()) === 0, 'с неуточнённой выручкой дальше не пускаем');
    await page.click('[data-use-fns]');
    await page.waitForSelector('#ap-result .note');
    assert(norm(await page.textContent('#ap-result')).includes('рассчитайте снова'), 'старый расчёт должен сброситься');
    await page.click('#prescore');
    await page.waitForSelector('#prescore-result[data-decision=approved]');
  });

  await test('2-й уровень: поручитель с просрочками → уточнение, залог на всю сумму → ставка ниже', async (page) => {
    await prescore(page, { amount: 8000000 });
    await esia(page);
    assert(norm(await page.textContent('[data-key="security"]')).includes('нужен залог'), 'для 8 млн не объяснено, что нужно обеспечение');
    await page.reload(); // после перезагрузки открывается та же анкета 2-го уровня
    await level2(page, { guarantorInn: '540100000009' });
    await page.waitForSelector('#decision[data-decision=need_info]', { timeout: DECISION });
    assert(norm(await page.textContent('#decision')).includes('просрочки'), 'не объяснено, почему не подошёл поручитель');
    await page.click('#todo [data-action=fix]');
    await page.waitForSelector('[data-key="consent"]');
    await page.click('[data-collateral=realty]');
    await page.fill('#ff-coll-value', '15000000');
    assert(norm(await page.textContent('#ff-coll-info')).includes('ниже на 1 п.п.'), 'не подсказано, что залог снизит ставку');
    await saved(page);
    await page.click('#submit');
    await page.waitForSelector('#decision[data-decision=approved]', { timeout: DECISION });
    const table = norm(await page.textContent('table.compare'));
    assert(
      table.includes('Предварительно') && table.includes('Окончательно') && table.includes('−2,0 п.п.'),
      'нет сравнения ставок: ' + table,
    );
  });

  await test('ООО и больше 25 млн: без протокола о крупной сделке не отправить', async (page) => {
    await prescore(page, { amount: 26000000 });
    await esia(page);
    await page.waitForSelector('[data-key="protocol"]');
    await page.click('[data-collateral=realty]');
    await page.fill('#ff-coll-value', '40000000');
    await page.check('#ff-consent');
    await saved(page);
    assert(await page.isDisabled('#submit'), 'без протокола отправлять нельзя');
    await page.setInputFiles('#ff-protocol', {
      name: 'Протокол ОСУ №3.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from('%PDF-1.4 demo'),
    });
    assert(norm(await page.textContent('#ff-protocol-name')).includes('Протокол ОСУ №3.pdf'), 'не видно выбранного файла');
    await saved(page);
    await page.click('#submit');
    await page.waitForSelector('#decision', { timeout: DECISION });
    await page.waitForFunction(() => !/Считаем/.test(document.querySelector('#decision h2')?.textContent || ''), null, {
      timeout: DECISION,
    });
    assert(norm(await page.textContent('.checks')).includes('Решение об одобрении крупной сделки'), 'протокол не попал в проверки банка');
  });

  await test('Госуслуги: учётка не руководителя выбранной компании — понятная ошибка, заявка не создаётся', async (page) => {
    await prescore(page);
    await page.click('#esia-go');
    await page.click('[data-person="esia-2"]');
    await page.click('#esia-ok');
    await page.waitForSelector('.toast--error');
    assert(norm(await page.textContent('.toast--error')).includes('не руководит'), 'нет объяснения, почему нельзя');
    assert(page.url().endsWith('#/apply'), 'должны остаться на расчёте');
    assert((await apiAs(page, 'GET', '/applications')).json.length === 0, 'заявка не должна создаться');
  });

  await test('Отказ на полном скоринге объясняется: просрочки в БКИ видны только после входа', async (page) => {
    assert((await prescore(page, { inn: '5401000009', obligations: 300000 })) === 'approved', 'без кредитной истории расчёт одобряет');
    await esia(page);
    await level2(page);
    await decision(page, 'bad');
    const txt = norm(await page.textContent('#decision'));
    assert(txt.includes('Почему так') && txt.includes('просрочк'), 'нет причины отказа');
    assert(txt.includes('Что можно сделать'), 'нет советов');
  });

  await test('Автосохранение 2-го уровня честно показывает ошибку без сети и досохраняет после восстановления', async (page, ctx) => {
    await prescore(page);
    await esia(page);
    await ctx.setOffline(true);
    await page.check('#ff-consent');
    await page.waitForFunction(() => document.querySelector('#save-state')?.classList.contains('is-error'), null, { timeout: 5000 });
    assert(norm(await page.textContent('#save-state')).includes('Не сохранено'), 'ошибка не показана');
    await ctx.setOffline(false);
    await saved(page, 10000);
    const id = page.url().split('/').pop();
    const a = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(a.full_form.consent === true, 'правка не дошла до сервера');
  });

  await test('Страницы и шапка не вылезают за экран (десктоп, планшет, телефон)', async (page) => {
    const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    for (const [w, h] of [
      [1440, 900],
      [1024, 768],
      [390, 844],
    ]) {
      await page.setViewportSize({ width: w, height: h });
      await page.goto(BASE + '/#/');
      await page.waitForSelector('.hero h1');
      assert((await overflow()) <= 1, `горизонтальная прокрутка на главной, ${w}px`);
      await page.goto(BASE + '/#/apply');
      await page.waitForSelector('#ap-inn');
      assert((await overflow()) <= 1, `горизонтальная прокрутка на расчёте, ${w}px`);
    }
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page, '79000000001');
    for (const w of [1440, 1100, 768, 390]) {
      await page.setViewportSize({ width: w, height: 900 });
      for (const r of ['/cabinet', '/cabinet/applications', '/cabinet/credits']) {
        await page.goto(BASE + '/#' + r);
        await page.waitForSelector('.page-head h1');
        assert((await overflow()) <= 1, `горизонтальная прокрутка на ${r}, ${w}px`);
      }
      const overlap = await page.evaluate(() => {
        const links = [...document.querySelectorAll('.nav__link')].filter((l) => l.offsetParent);
        const right = document.querySelector('.header__right').getBoundingClientRect();
        // на телефоне вкладки — отдельной строкой под шапкой, сравниваем только то, что в одном ряду
        const row = links.map((l) => l.getBoundingClientRect()).filter((r) => r.top < right.bottom && right.top < r.bottom);
        return row.length ? Math.max(...row.map((r) => r.right)) - right.left : 0;
      });
      assert(overlap <= 0, `вкладки шапки наезжают на кнопки справа на ${w}px`);
    }
  });

  await test('Уход со страницы до ответа калькулятора не ломает скрипт (регрессия)', async (page) => {
    await login(page, '79000000001');
    // задерживаем расчёт, чтобы ответ гарантированно пришёл уже после перехода в анкету
    await page.route('**/api/v1/calc', async (route) => {
      await new Promise((r) => setTimeout(r, 800));
      await route.continue();
    });
    await page.goto(BASE + '/#/');
    await page.click('#products [data-start="business-dev"]');
    await page.waitForSelector('#ap-inn');
    await page.waitForTimeout(1500); // ответы калькулятора приходят на уже закрытую страницу
    await page.unroute('**/api/v1/calc');
  });

  await test('Быстрый ввод в окне входа не теряет символы (регрессия: перехват фокуса)', async (page) => {
    for (let i = 0; i < 5; i++) {
      await page.goto(BASE + '/#/');
      await page.click('[data-header=login]');
      await page.click('#auth-password');
      await page.keyboard.type(PASS); // сразу после открытия окна
      await page.waitForTimeout(120);
      assert((await page.inputValue('#auth-password')) === PASS, 'пароль потерялся при наборе');
      assert((await page.inputValue('#auth-phone')) === '', 'символы пароля улетели в поле телефона');
      await page.keyboard.press('Escape');
    }
  });

  await test('Логотип ведёт на главную; на главной после входа — шапка сайта и аватар, ФИО в меню ведёт в кабинет', async (page) => {
    await page.goto(BASE + '/#/');
    await page.evaluate(() => window.scrollTo(0, 2000));
    await page.click('.logo');
    await page.waitForFunction(() => window.scrollY < 50);
    await login(page, '79000000001');
    await page.click('.logo');
    await page.waitForSelector('.hero h1');
    assert((await page.locator('.header--cabinet').count()) === 0, 'на главной не должно быть шапки кабинета');
    assert(norm(await page.textContent('.nav')).includes('Как получить'), 'на главной нет разделов сайта');
    assert((await page.locator('[data-header=login]').count()) === 0, 'после входа вместо «Войти» должен быть аватар');
    await page.click('[data-header=menu]');
    await page.click('.menu__head');
    await page.waitForSelector('.header--cabinet #requisites, #requisites');
    assert(page.url().endsWith('#/cabinet'), 'ФИО в меню ведёт не в профиль: ' + page.url());
  });

  await test('Меню профиля: по клику, не закрывается само и от клика внутри, закрывается кликом мимо', async (page) => {
    await login(page, '79000000001');
    await page.goto(BASE + '/#/cabinet/credits');
    await page.waitForSelector('.page-head h1');
    await page.hover('[data-header=menu]');
    await page.waitForTimeout(300);
    assert((await menuOpen(page)) === 0, 'меню не должно открываться наведением');
    await page.click('[data-header=menu]');
    await page.waitForSelector('.menu .menu__head');
    await page.mouse.move(10, 600);
    await page.waitForTimeout(7000); // дольше двух циклов фонового опроса
    assert((await menuOpen(page)) === 1, 'меню закрылось само (фоновый опрос перерисовал шапку)');
    await page.click('.menu .menu__sep');
    assert((await menuOpen(page)) === 1, 'клик внутри меню не должен его закрывать');
    assert(!norm(await page.textContent('.menu')).includes('Личный кабинет'), 'пункт «Личный кабинет» должен быть убран');
    await page.mouse.click(10, 600);
    await page.waitForFunction(() => !document.querySelector('.menu-wrap .menu'));
    await page.click('[data-header=menu]');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('.menu-wrap .menu'));
  });

  await test('Телефон: разделы сайта — скруглённая панель под шапкой, клик мимо закрывает', async (page) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(BASE + '/#/');
    await page.waitForSelector('.hero h1');
    await page.click('[data-header=burger]');
    await page.waitForSelector('.header.is-open .nav__link');
    const hdr = await page.locator('.header__in').boundingBox();
    const nav = await page.locator('.header .nav').boundingBox();
    assert(await page.locator('.logo').isVisible(), 'шапка должна оставаться видимой');
    assert(nav.y >= hdr.y + hdr.height - 1, 'панель должна начинаться под шапкой');
    assert(nav.height < 844, 'панель не должна закрывать весь экран');
    assert(
      parseFloat(await page.$eval('.header .nav', (e) => getComputedStyle(e).borderBottomLeftRadius)) >= 16,
      'панель должна быть скруглённой',
    );
    await page.mouse.click(195, 800);
    await page.waitForFunction(() => !document.querySelector('.header.is-open'));
    await page.click('[data-header=burger]');
    await page.click('.header .nav__link[data-section=faq]');
    await page.waitForFunction(
      () => !document.querySelector('.header.is-open') && document.getElementById('faq').getBoundingClientRect().top < 300,
    );
  });

  await test('«Услуги бизнеса»: список ссылок на alfabank.ru открывается наведением, в новой вкладке', async (page) => {
    await page.goto(BASE + '/#/');
    await page.waitForSelector('.hero h1');
    assert((await page.locator('.topbar').count()) === 0, 'полоски сегментов над шапкой быть не должно');
    const panel = page.locator('#services');
    assert(!(await panel.isVisible()), 'панель не должна быть видна без наведения');
    await page.hover('.nav__more');
    await page.waitForFunction(() => getComputedStyle(document.querySelector('#services')).visibility === 'visible');
    assert((await page.locator('#services .art, #services img').count()) === 0, 'в меню услуг не должно быть картинок — только список');
    const links = await page.$$eval('#services a', (as) => as.map((a) => ({ href: a.href, target: a.target, rel: a.rel })));
    assert(links.length >= 6, 'мало услуг в списке');
    assert(
      links.every((l) => l.href.startsWith('https://alfabank.ru/') && l.target === '_blank' && l.rel.includes('noopener')),
      'ссылки должны вести на alfabank.ru в новой вкладке: ' + JSON.stringify(links[0]),
    );
    await page.waitForTimeout(250); // дождались конца анимации появления
    const hdr = await page.locator('.header').boundingBox();
    const box = await panel.boundingBox();
    assert(box.y >= hdr.y + hdr.height - 2 && box.height < 500, 'панель должна быть под шапкой и не закрывать экран');
    await page.hover('#services .services__link'); // курсор переходит на панель — она не закрывается
    await page.waitForTimeout(300);
    assert(await panel.isVisible(), 'панель закрылась при переходе курсора на неё');
    await page.mouse.move(720, 880); // увели курсор
    await page.waitForFunction(() => getComputedStyle(document.querySelector('#services')).visibility === 'hidden');
    const [panelBg, headerBg, panelBlur] = await page.evaluate(() => [
      ...['#services', '.header'].map((q) => getComputedStyle(document.querySelector(q)).backgroundColor),
      getComputedStyle(document.querySelector('#services')).backdropFilter,
    ]);
    // панель — продолжение фона страницы: полупрозрачная платина с размытием, без сплошной заливки
    assert(
      /^rgba\(232, 234, 238, 0\.7\d*\)$/.test(panelBg) && panelBlur.includes('blur'),
      `панель услуг должна сливаться с фоном: ${panelBg} ${panelBlur}`,
    );
    assert(headerBg === 'rgba(0, 0, 0, 0)', 'платиновая шапка прозрачна и лежит на первом экране без шва: ' + headerBg);
    await page.evaluate(() => window.scrollTo(0, 800));
    await page.waitForFunction(() => document.querySelector('.header').getBoundingClientRect().bottom <= 0);
  });

  await test('Овердрафт: срок фиксированный — без ползунка, платёж считается', async (page) => {
    await page.goto(BASE + '/#/apply');
    await page.click('[data-product="overdraft"]');
    await page.waitForSelector('#ap-calc [data-calc-fixed]');
    assert((await page.locator('#ap-calc input[type=range][data-calc=term]').count()) === 0, 'у овердрафта не должно быть ползунка срока');
    assert((await page.locator('#ap-calc input[type=range][data-calc=amount]').count()) === 1, 'ползунок суммы должен остаться');
    assert(norm(await page.inputValue('#ap-calc [data-calc=term-text]')) === '12 месяцев', 'срок овердрафта — 12 месяцев');
    await page.waitForFunction(() => /\d/.test(document.querySelector('#sum-pay')?.textContent || ''));
    await page.click('[data-product="business-dev"]');
    await page.waitForSelector('#ap-calc input[type=range][data-calc=term]');
  });

  await test('Телефон: меню профиля открывается касанием, помещается в экран, не закрывается при прокрутке', async (page, ctx) => {
    const mctx = await ctx.browser().newContext({ ...devices['iPhone 13'] });
    const m = await mctx.newPage();
    try {
      await login(m, '79000000001');
      const vp = m.viewportSize();
      const check = async (where) => {
        await m.tap('[data-header=menu]');
        await m.waitForSelector('.menu .menu__head');
        const r = await m.locator('.menu').boundingBox();
        assert(
          r && r.y >= 0 && r.y + 60 < vp.height && r.x >= 0 && r.x + r.width <= vp.width,
          `${where}: меню за пределами экрана ${JSON.stringify(r)}`,
        );
      };
      await check('кабинет');
      await m.mouse.wheel(0, 300);
      await m.waitForTimeout(400);
      assert((await menuOpen(m)) === 1, 'меню закрылось при прокрутке');
      await m.touchscreen.tap(vp.width / 2, vp.height - 40); // касание мимо меню
      await m.waitForFunction(() => !document.querySelector('.menu-wrap .menu'));
      await m.goto(BASE + '/#/');
      await m.waitForSelector('.hero h1');
      await check('главная');
      await m.tap('.menu__head');
      await m.waitForSelector('#requisites');
    } finally {
      await mctx.close();
    }
  });

  await test('Разбор балла: одобрение — шкала с отметкой и плюсы в факторах', async (page) => {
    await applyFull(page);
    await decision(page, 'ok');
    const score = norm(await page.textContent('.acc.score'));
    assert(
      score.includes('зона одобрения') && score.includes('Стартовый балл') && score.includes('Рейтинг БКИ') && /\+\d+/.test(score),
      'нет разбора балла: ' + score.slice(0, 120),
    );
    assert(await page.locator('.acc.score .scale__mark').isVisible(), 'на шкале нет отметки балла');
  });

  await test('Разбор балла: отказ — минусы в факторах; аккордеон сворачивается', async (page) => {
    await applyFull(page, { inn: '5401000009', obligations: 300000 });
    await decision(page, 'bad');
    assert((await page.locator('.acc.score .factor--minus').count()) >= 1, 'в разборе нет минусов');
    assert(norm(await page.textContent('.acc.score')).includes('зона отказа'), 'нет зоны отказа');
    const head = page.locator('.acc.score > .acc__head');
    assert((await head.getAttribute('aria-expanded')) === 'true', 'разбор должен быть раскрыт');
    await head.click();
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.acc.score > .acc__body')).visibility === 'hidden');
    assert((await head.getAttribute('aria-expanded')) === 'false', 'aria-expanded не обновился');
  });

  await test('Узкие экраны: этапы заявки и список заявок не наезжают друг на друга', async (page) => {
    await applyFull(page, { inn: '5401000009', obligations: 300000 });
    for (const w of [1024, 768, 390]) {
      await page.setViewportSize({ width: w, height: 900 });
      await page.waitForTimeout(150);
      const overlap = await page.evaluate(() => {
        const ls = [...document.querySelectorAll('.step > span:last-child')].map((e) => e.getBoundingClientRect());
        return ls.some((a, i) => i && a.left < ls[i - 1].right - 1 && a.top < ls[i - 1].bottom - 1 && ls[i - 1].top < a.bottom - 1);
      });
      assert(!overlap, `${w}px: подписи этапов наезжают друг на друга`);
      const wide = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      assert(wide <= 1, `${w}px: страница заявки шире экрана на ${wide}px`);
    }
    await page.goto(BASE + '/#/cabinet/applications');
    await page.waitForSelector('.list-item');
    const out = await page.evaluate(
      () => [...document.querySelectorAll('.list-item')].filter((e) => e.scrollWidth > e.clientWidth + 1).length,
    );
    assert(out === 0, 'содержимое строк списка заявок вылезает за край');
  });

  await test('Платформа: действия с деньгами идут с Idempotency-Key, клиенту кабинет банка закрыт', async (page) => {
    const keys = [];
    page.on('request', (r) => {
      if (r.method() === 'POST' && /\/applications(\/[^/]+\/(submit|accept|sign|pay))?$/.test(r.url()))
        keys.push(r.headers()['idempotency-key']);
    });
    await applyFull(page);
    assert(
      keys.length >= 2 && keys.every((k) => k && k.length >= 16),
      'POST-действия должны идти с Idempotency-Key: ' + JSON.stringify(keys),
    );
    assert(new Set(keys).size === keys.length, 'у каждого действия свой ключ');
    // клиенту кабинет банка закрыт — и на экране, и в API
    await page.goto(BASE + '/#/bank');
    await page.waitForSelector('.bank-gate');
    assert((await apiAs(page, 'GET', '/bank/applications')).status === 403, 'API кабинета банка должно отвечать клиенту 403');
  });

  await test('Сотрудник банка: последний в списке входа, после входа — профиль, очередь заявок, кабинет компании закрыт', async (page) => {
    await page.goto(BASE + '/#/');
    await page.click('[data-header=login]');
    await page.waitForSelector('.demo__item');
    const demo = await page.$$eval('.demo__item', (els) => els.map((e) => e.dataset.demo));
    assert(demo.at(-1) === '79000000005', 'сотрудник банка должен быть последним в списке: ' + demo.join(', '));
    await page.click('.demo__item--staff');
    await page.waitForFunction(() => location.hash === '#/bank/profile');
    await page.waitForSelector('.staff-badge');
    const prof = norm(await page.textContent('#view'));
    for (const t of ['Соколова Анна Дмитриевна', 'Кредитный аналитик', 'АБ-04217', 'Журнал аудита по заявке']) {
      assert(prof.includes(t), `в профиле нет «${t}»`);
    }
    assert((await page.locator('.perm.is-on').count()) === 3, 'у демо-сотрудника три права кабинета банка');
    await page.waitForSelector('.staff-queue');
    const tabs = await page.$$eval('.header--cabinet .nav__link', (els) => els.map((e) => e.textContent.trim()));
    assert(tabs.join('|') === 'Очередь заявок|Профиль', 'вкладки кабинета банка: ' + tabs.join('|'));
    assert((await page.locator('[data-header=new-loan]').count()) === 0, 'у сотрудника не должно быть «Нового кредита»');

    await page.click('.nav__link[href="#/bank"]');
    await page.waitForSelector('#bank-platform .platform');
    const plat = norm(await page.textContent('#bank-platform'));
    assert(plat.includes('Хранилище') && plat.includes('Шина событий'), 'нет состояния платформы: ' + plat);
    await page.waitForSelector('#bank-table table, #bank-table .empty');

    // роли не смешиваются
    await page.goto(BASE + '/#/cabinet/applications');
    await page.waitForFunction(() => location.hash === '#/bank/profile');
    assert((await apiAs(page, 'GET', '/applications')).status === 403, 'сотруднику API заявок клиента должно отвечать 403');
    await page.goto(BASE + '/#/');
    await page.click('#products [data-start="working-capital"]');
    await page.waitForSelector('.toast--error');
    assert((await page.locator('#auth-phone').count()) === 0, 'сотруднику не должно открываться окно входа клиента');
    assert(norm(await page.textContent('.toast--error')).includes('сотрудник банка'), 'нет объяснения, почему нельзя оформить');
  });

  await test('Главная: «Залог» в «Вам понадобится» — только для кредита больше 5 млн, с подсказкой по наведению', async (page) => {
    await page.goto(BASE + '/#/');
    await page.waitForSelector('#calc [data-calc=amount-text]');
    await page.fill('#calc [data-calc=amount-text]', '3000000');
    await page.press('#calc [data-calc=amount-text]', 'Enter');
    await page.waitForFunction(() => document.querySelector('#need-collateral')?.hidden === true);
    await page.fill('#calc [data-calc=amount-text]', '10000000');
    await page.press('#calc [data-calc=amount-text]', 'Enter');
    await page.waitForSelector('#need-collateral', { state: 'visible' });
    await page.hover('#need-collateral');
    await page.waitForSelector('#tip-collateral', { state: 'visible' });
    assert(norm(await page.textContent('#tip-collateral')).includes('5 млн'), 'в подсказке нет порога суммы');
  });

  await test('Клавиатура: меню услуг по фокусу и Esc, вопросы по Enter, ползунок стрелками, окно держит и возвращает фокус', async (page) => {
    await page.goto(BASE + '/#/');
    await page.waitForSelector('.hero h1');
    const visibility = () => page.$eval('#services', (e) => getComputedStyle(e).visibility);
    await page.focus('.nav__more');
    await page.waitForFunction(() => getComputedStyle(document.querySelector('#services')).visibility === 'visible');
    await page.keyboard.press('Tab');
    assert(await page.evaluate(() => !!document.activeElement.closest('#services')), 'Tab с «Услуг» должен вести в меню услуг');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => getComputedStyle(document.querySelector('#services')).visibility === 'hidden');
    assert((await visibility()) === 'hidden', 'Esc должен закрывать меню услуг');

    await page.focus('.acc.faq .acc__head');
    await page.keyboard.press('Enter');
    assert(await page.$eval('.acc.faq', (e) => e.classList.contains('is-open')), 'вопрос должен раскрываться по Enter');

    await page.focus('#calc [data-calc=term]');
    await page.keyboard.press('ArrowRight');
    assert(norm(await page.inputValue('#calc [data-calc=term-text]')) === '25 месяцев', 'срок должен меняться стрелками');

    await page.focus('[data-header=login]');
    await page.keyboard.press('Enter');
    await page.waitForSelector('#auth-phone');
    assert(
      (await page.getAttribute('.modal', 'aria-label')) === 'Вход в кредитный кабинет',
      'у окна входа должно быть имя для экранных дикторов',
    );
    for (let i = 0; i < 20; i++) {
      await page.keyboard.press('Tab');
      assert(await page.evaluate(() => !!document.activeElement.closest('.modal')), 'фокус ушёл из окна входа');
    }
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => document.activeElement?.dataset.header === 'login');
  });

  await test('Чужую заявку открыть нельзя', async (page, ctx) => {
    await page.goto(BASE + '/#/');
    await apiAs(page, 'POST', '/auth/esia', { person_id: 'esia-1' });
    const mine = (
      await apiAs(page, 'POST', '/applications', {
        product_id: 'business-dev',
        amount: 3000000,
        term_months: 24,
        inn: '7701234567',
        financials: { annual_revenue: 84000000 },
      })
    ).json;
    assert(mine?.id, 'заявка не создалась');
    const other = await ctx.browser().newContext();
    const p2 = await other.newPage();
    await login(p2, '79000000002');
    const r = await apiAs(p2, 'GET', '/applications/' + mine.id);
    await other.close();
    assert(r.status === 404, `чужая заявка вернула ${r.status}`);
  });

  await test('Старые адреса и кабинет без входа: редирект и окно входа, после входа — тот же экран', async (page) => {
    await page.goto(BASE + '/#/credits');
    await page.waitForSelector('#auth-phone');
    assert(page.url().endsWith('#/cabinet/credits'), 'старый адрес не перенаправлен: ' + page.url());
    await page.click('[data-demo="79000000001"]');
    await page.waitForFunction(() => document.querySelector('.page-head h1')?.textContent === 'Кредиты');
  });

  await browser.close();
  const failed = results.filter((r) => !r.ok);
  console.log(`\n${results.length - failed.length}/${results.length} прошли`);
  process.exit(failed.length ? 1 : 0);
})().catch((e) => {
  console.error(e);
  process.exit(2);
});
