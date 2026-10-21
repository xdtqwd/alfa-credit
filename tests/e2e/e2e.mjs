// Браузерные e2e-тесты интерфейса (Playwright, без тест-раннера).
// Запуск: сервер должен работать на BASE_URL (по умолчанию http://localhost:8080)
//   PIPELINE_STEP_DELAY=1s make run   # в одном терминале
//   make e2e                          # в другом (нужны Node 18+ и `npm i -g playwright`)
import { chromium, devices } from 'playwright';

const BASE = process.env.BASE_URL || 'http://localhost:8080';
const PASS = 'demo1234';
const results = [];
let browser;

const fmtRub = (n) => new Intl.NumberFormat('ru-RU').format(n) + ' ₽';
const norm = (s) => (s || '').replace(/\s+/g, ' ').trim();
function assert(cond, msg) { if (!cond) throw new Error(msg); }

async function test(name, fn) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error' && !/401|Failed to load resource/.test(m.text())) errors.push(m.text()); });
  const t0 = Date.now();
  try {
    await fn(page, ctx);
    assert(errors.length === 0, 'ошибки в консоли: ' + errors.join(' | '));
    results.push({ name, ok: true, ms: Date.now() - t0 });
    console.log(`  ✓ ${name} (${Date.now() - t0} мс)`);
  } catch (e) {
    results.push({ name, ok: false, err: e.message });
    console.log(`  ✗ ${name}\n      ${e.message}`);
    await diagnose(page, errors).catch((d) => console.log('      (диагностика не удалась: ' + d.message + ')'));
    await page.screenshot({ path: `e2e-fail-${results.length}.png`, fullPage: true }).catch(() => {});
  } finally {
    await ctx.close();
  }
}

// При падении печатаем, что было на самом деле: адрес, видимый статус, ошибки консоли и — если открыта
// заявка — её статус и историю с сервера. Тогда упавший тест в CI объясняет себя без артефактов.
async function diagnose(page, errors) {
  if (errors.length) console.log('      консоль: ' + errors.join(' | ').slice(0, 400));
  const url = page.url();
  const shown = await page.evaluate(() => [...document.querySelectorAll('.decision h2, .todo-banner b, .pill, .tstep.active .tstep__t, .now-doing')]
    .map((e) => e.textContent.trim()).filter(Boolean).slice(0, 6).join(' · ')).catch(() => '');
  console.log(`      страница: ${url}${shown ? ' — ' + shown : ''}`);
  const m = url.match(/#\/(?:app|apply)\/([0-9a-f]+)/);
  if (!m) return;
  const a = (await apiAs(page, 'GET', '/applications/' + m[1])).json;
  if (!a) return;
  const t = (x) => new Date(x).toISOString().slice(11, 23);
  console.log(`      заявка ${a.number}: статус ${a.status}, сумма ${a.amount}, балл ${a.score ?? '—'}, сейчас ${t(Date.now())}`);
  for (const h of (a.history || []).slice(-6)) console.log(`        ${t(h.at)} ${h.status} (${h.actor}) ${h.comment || ''}`.slice(0, 160));
}

async function login(page, phone) {
  await page.goto(BASE + '/');
  await page.click('#auth-slot [data-do=login]');
  await page.fill('#a-phone', phone);
  await page.fill('#a-pass', PASS);
  await page.click('#auth-form button[type=submit]');
  await page.waitForSelector('#auth-slot .ahdr__user');
}

async function fillAndSubmit(page, { revenue, obligations } = {}) {
  await page.waitForSelector('#rev');
  if (revenue) await page.fill('#rev', String(revenue)); else await page.click('[data-act=use-fns]');
  if (obligations) await page.fill('#obl', String(obligations));
  await page.check('#consent');
  await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено', null, { timeout: 5000 });
  await page.click('#submit');
  await page.waitForSelector('.tracker');
}

async function apiAs(page, method, path, body) {
  return page.evaluate(async ([m, p, b]) => {
    const r = await fetch('/api/v1' + p, { method: m, headers: { 'Content-Type': 'application/json' }, body: b ? JSON.stringify(b) : undefined });
    return { status: r.status, json: await r.json().catch(() => null) };
  }, [method, path, body]);
}

(async () => {
  browser = await chromium.launch();
  console.log(`E2E против ${BASE}\n`);

  await test('Гость видит продуктовую страницу, кабинетные вкладки скрыты', async (page) => {
    await page.goto(BASE + '/');
    await page.waitForSelector('.a-hero h1');
    assert(norm(await page.textContent('.a-hero h1')).includes('Кредит для бизнеса'), 'нет заголовка');
    assert(await page.locator('.cab-tabs').count() === 0, 'вкладки кабинета видны гостю');
    assert(await page.locator('#auth-slot [data-do=login]').isVisible(), 'нет кнопки «Войти»');
  });

  await test('Без входа нельзя оформить: появляется окно входа, после входа — сразу анкета', async (page) => {
    await page.goto(BASE + '/');
    await page.click('.a-prod [data-act=start][data-product="working-capital"]');
    await page.waitForSelector('#a-phone');
    assert(norm(await page.textContent('.auth__reason')).includes('войдите'), 'нет объяснения, зачем вход');
    await page.click('[data-demo="79000000001"]');
    await page.waitForURL(/#\/apply\//);
    assert(norm(await page.textContent('h1')).includes('Оборотный кредит'), 'открыта не та анкета');
  });

  await test('Калькулятор показывает расчёт сервера (единый источник)', async (page) => {
    await page.goto(BASE + '/');
    await page.waitForFunction(() => /\d/.test(document.querySelector('#calc-pay')?.textContent || ''));
    const shown = norm(await page.textContent('#calc-pay'));
    const q = await apiAs(page, 'POST', '/calc', { product_id: 'business-dev', amount: 3000000, term_months: 24 });
    assert(shown === norm(fmtRub(q.json.monthly_payment)), `на экране ${shown}, сервер ${q.json.monthly_payment}`);
    const over = norm(await page.textContent('#calc-over'));
    assert(over === norm(fmtRub(q.json.overpayment)), 'переплата не совпадает с сервером');
  });

  await test('После входа — минималистичный кабинет: «Заявки · Кредиты · Сообщения», счёт, новая заявка', async (page) => {
    await login(page, '79000000001');
    await page.waitForSelector('.mx-title');
    assert(norm(await page.textContent('.mx-title')).startsWith('ЗАЯВКИ') || norm(await page.textContent('.mx-title')).toUpperCase().startsWith('ЗАЯВКИ'), 'не страница заявок');
    assert(norm(await page.textContent('a.mx-pill.active')) === 'Заявки', 'активна не «Заявки»');
    assert(norm(await page.textContent('.mx-acc')).includes('Расчётный счёт'), 'нет счёта');
    for (const [href, t] of [['#/credits', 'КРЕДИТЫ'], ['#/messages', 'СООБЩЕНИЯ']]) {
      await page.click(`a.mx-pill[href="${href}"]`);
      await page.waitForFunction((x) => document.querySelector('.mx-title')?.textContent.toUpperCase().startsWith(x), t);
    }
  });

  await test('Одобрение → сравнение → подписание → выдача → оплата', async (page) => {
    await login(page, '79000000001');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    await page.waitForSelector('.decision--green', { timeout: 20000 });
    assert(norm(await page.textContent('.todo-banner')).includes('Что нужно от вас'), 'нет блока «Что нужно от вас»');
    const before = (await apiAs(page, 'GET', '/me')).json.balance;
    await page.click('.todo-banner [data-act=accept]');
    await page.check('#sign-ok');
    await page.click('#sign-btn');
    await page.waitForSelector('.schedule');
    const after = (await apiAs(page, 'GET', '/me')).json.balance;
    const id = page.url().split('/').pop();
    const app = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(after - before === app.amount, `баланс вырос на ${after - before}, а выдано ${app.amount}`);
    assert(norm(await page.textContent('.todo-banner')).includes(norm(fmtRub(app.next_payment))), 'в «Что нужно от вас» не тот платёж');
    await page.click('.todo-banner [data-act=pay]');
    await page.click('#pay-ok');
    await page.waitForFunction(() => /Оплачено 1 из/.test(document.querySelector('.pay-row')?.textContent || ''));
    const paid = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(paid.payments[0].amount === app.schedule[0].payment, 'списано не по графику');
  });

  await test('Контрпредложение: таблица «Вы просили / Банк предлагает» и объяснение на цифрах анкеты', async (page) => {
    await login(page, '79000000002');
    await page.goto(BASE + '/#/products');
    await page.click('.a-prod [data-act=start][data-product="business-dev"]');
    await page.waitForSelector('[data-calc=amount-text]');
    await page.click('[data-calc=amount-text]');
    await page.keyboard.type('20000000');
    await page.keyboard.press('Enter');
    await fillAndSubmit(page, { obligations: 200000 });
    await page.waitForSelector('.decision--amber', { timeout: 20000 });
    const table = norm(await page.textContent('table.cmp'));
    for (const col of ['Вы просили', 'Банк предлагает', 'Разница', 'Платёж в месяц', 'Переплата', 'Ставка', 'Срок']) assert(table.includes(col), 'в таблице нет: ' + col);
    const why = norm(await page.textContent('.decision'));
    assert(why.includes('1 500 000 ₽') && why.includes('50%'), 'объяснение не опирается на выручку из анкеты');
  });

  await test('Уточнение: выручка сильно выше ФНС → «Исправьте данные», после правки — снова в конвейер', async (page) => {
    await login(page, '79000000002');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page, { revenue: 50000000 });
    await page.waitForSelector('.decision--amber', { timeout: 20000 });
    assert(norm(await page.textContent('.todo-banner')).includes('Исправьте данные'), 'нет подсказки, что делать');
    await page.click('.todo-banner [data-act=fix]');
    await page.waitForSelector('#rev');
    await page.click('[data-act=use-fns]');
    await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено');
    await page.click('#submit');
    await page.waitForSelector('.decision--green, .decision--amber, .decision--red', { timeout: 20000 });
    assert(await page.locator('.decision--green').count() === 1, 'после исправления ожидали одобрение');
  });

  await test('Отказ объясняется и предлагает, что сделать', async (page) => {
    await login(page, '79000000003');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    await page.waitForSelector('.decision--red', { timeout: 20000 });
    const txt = norm(await page.textContent('.decision'));
    assert(txt.includes('Почему так') && txt.includes('просрочк'), 'нет причины отказа');
    assert(txt.includes('Что можно сделать'), 'нет советов');
  });

  await test('Автосохранение честно показывает ошибку без сети и досохраняет после восстановления', async (page, ctx) => {
    await login(page, '79000000001');
    await page.click('.mx-new [data-do=apply]');
    await page.waitForSelector('#rev');
    await ctx.setOffline(true);
    await page.fill('#obl', '123000');
    await page.waitForFunction(() => document.querySelector('#s-saved')?.classList.contains('err'), null, { timeout: 5000 });
    assert(norm(await page.textContent('#s-saved')).includes('Не сохранено'), 'ошибка не показана');
    await ctx.setOffline(false);
    await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено', null, { timeout: 10000 });
    const id = page.url().split('/').pop();
    const a = (await apiAs(page, 'GET', '/applications/' + id)).json;
    assert(a.financials.monthly_obligations === 123000, 'правка не дошла до сервера');
  });

  await test('Шапка и страницы не вылезают за экран (десктоп и телефон)', async (page, ctx) => {
    for (const [w, h] of [[1440, 900], [1024, 768], [390, 844]]) {
      await page.setViewportSize({ width: w, height: h });
      for (const route of ['#/', '#/products']) {
        await page.goto(BASE + '/' + route);
        await page.waitForTimeout(300);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        assert(overflow <= 1, `горизонтальная прокрутка ${overflow}px на ${w}px (${route})`);
      }
    }
    await login(page, '79000000001');
    for (const w of [1440, 1100, 390]) {
      await page.setViewportSize({ width: w, height: 900 });
      await page.waitForTimeout(200);
      const overlap = await page.evaluate(() => {
        const nav = document.querySelector('.ahdr__nav'), right = document.querySelector('.ahdr__right');
        if (!nav || getComputedStyle(nav).display === 'none') return 0;
        const last = [...nav.children].pop();
        return Math.max(nav.getBoundingClientRect().right, last ? last.getBoundingClientRect().right : 0) - right.getBoundingClientRect().left;
      });
      assert(overlap <= 0, `разделы шапки наезжают на значки на ${w}px`);
    }
  });

  await test('Уход со страницы до ответа калькулятора не ломает скрипт (регрессия)', async (page) => {
    await login(page, '79000000001');
    // задерживаем расчёт, чтобы ответ гарантированно пришёл уже после перехода в анкету
    await page.route('**/api/v1/calc', async (route) => { await new Promise((r) => setTimeout(r, 800)); await route.continue(); });
    await page.goto(BASE + '/#/products');
    await page.waitForSelector('.a-prod [data-act=start]');
    await page.click('.a-prod [data-act=start][data-product="business-dev"]');
    await page.waitForSelector('#rev');
    await page.waitForTimeout(1500); // ответы калькулятора приходят на уже закрытую страницу
    await page.unroute('**/api/v1/calc');
  });

  await test('Быстрый ввод в окне входа не теряет символы (регрессия: перехват фокуса)', async (page) => {
    for (let i = 0; i < 5; i++) {
      await page.goto(BASE + '/');
      await page.click('#auth-slot [data-do=login]');
      await page.click('#a-pass');
      await page.keyboard.type('demo1234'); // сразу после открытия окна
      await page.waitForTimeout(120);
      assert(await page.inputValue('#a-pass') === 'demo1234', 'пароль потерялся при наборе');
      assert(await page.inputValue('#a-phone') === '', 'символы пароля улетели в поле телефона');
    }
  });

  await test('Клик по логотипу возвращает на главную (гость и после входа)', async (page) => {
    await page.goto(BASE + '/#/products');
    await page.evaluate(() => window.scrollTo(0, 2000));
    await page.click('.ahdr__logo');
    await page.waitForFunction(() => window.scrollY < 50);
    await login(page, '79000000001');
    await page.waitForSelector('.mx-title');
    await page.click('.ahdr__logo');
    await page.waitForSelector('.a-hero h1');
    assert(page.url().endsWith('#/products'), 'логотип ведёт не на главную: ' + page.url());
    // на сайте после входа — разделы сайта, а справа аватар с тем же меню профиля, что и в кабинете
    assert(await page.locator('.ahdr__nav.is-pills').count() === 0, 'на главной не должно быть шапки клиента');
    assert(norm(await page.textContent('.ahdr__nav')).includes('Как получить'), 'на главной нет обычных разделов сайта');
    assert(await page.locator('#auth-slot .ahdr__login').count() === 0, 'на главной после входа не должно быть кнопки — только аватар');
    await page.click('#auth-slot .ahdr__user');
    await page.click('.dd[data-dd="me"] .pn-me');
    await page.waitForSelector('.ahdr__nav.is-pills a.mx-pill.active');
    assert(await page.locator('#auth-slot .ahdr__user').count() === 1, 'в кабинете справа должен быть аватар');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(BASE + '/#/');
    await page.waitForSelector('.mx-title');
    assert(await page.locator('.ahdr__logo').isVisible(), 'на телефоне в кабинете нет логотипа');
  });

  await test('Меню профиля: по клику, не закрывается само, ФИО ведёт в кабинет', async (page) => {
    await login(page, '79000000001');
    await page.goto(BASE + '/#/credits');
    await page.waitForSelector('.mx-title');
    const open = () => page.locator('.dd[data-dd="me"].open').count();
    await page.hover('#auth-slot .ahdr__user');
    await page.waitForTimeout(400);
    assert(await open() === 0, 'меню профиля не должно открываться наведением');
    await page.click('#auth-slot .ahdr__user');
    await page.waitForSelector('.dd[data-dd="me"].open .pn-me');
    await page.mouse.move(10, 600); // увели мышь
    await page.waitForTimeout(6000); // дольше двух циклов фонового опроса (2,5 с)
    assert(await open() === 1, 'меню закрылось само, без клика (регрессия: фоновый опрос перерисовывал шапку)');
    await page.click('.dd[data-dd="me"] .pn-sep'); // клик внутри меню
    assert(await open() === 1, 'клик внутри меню не должен его закрывать');
    assert(!norm(await page.textContent('.dd[data-dd="me"] .dd__panel')).includes('счёт, заявки, кредиты'), 'пункт «Личный кабинет» должен быть убран');
    await page.mouse.click(10, 600); // клик мимо
    await page.waitForTimeout(100);
    assert(await open() === 0, 'клик мимо меню должен его закрыть');
    await page.click('#auth-slot .ahdr__user');
    await page.click('.dd[data-dd="me"] .pn-me');
    await page.waitForFunction(() => location.hash === '#/');
    await page.waitForSelector('.mx-title');
    assert(await open() === 0, 'после перехода меню должно закрыться');
  });

  await test('Меню «Ещё» — панель под шапкой: шапка сайта видна, клик мимо закрывает', async (page) => {
    await login(page, '79000000001');
    await page.goto(BASE + '/#/products');
    await page.waitForSelector('.a-hero h1');
    await page.click('.ahdr__nav [data-do=menu]');
    await page.waitForSelector('#megamenu.open .mm__cols');
    const hdr = await page.locator('.ahdr').boundingBox();
    const mm = await page.locator('#megamenu').boundingBox();
    const vp = page.viewportSize();
    assert(await page.locator('.ahdr__logo').isVisible(), 'шапка сайта должна оставаться видимой');
    assert(mm.y >= hdr.y + hdr.height, 'панель должна начинаться под шапкой');
    assert(mm.width < vp.width && mm.height < vp.height, 'панель не должна закрывать весь экран');
    assert(parseFloat(await page.$eval('#megamenu', (e) => getComputedStyle(e).borderTopLeftRadius)) >= 16, 'панель должна быть скруглённой');
    assert(await page.locator('#megamenu .mm__top').count() === 0, 'в панели не должно быть второй шапки');
    await page.mouse.click(vp.width / 2, vp.height - 5);
    await page.waitForFunction(() => document.getElementById('megamenu').hidden);
    await page.click('.ahdr__nav [data-do=menu]');
    await page.waitForSelector('#megamenu.open');
    await page.click('.ahdr__nav [data-do=menu]'); // повторный клик по «Ещё» закрывает
    await page.waitForFunction(() => document.getElementById('megamenu').hidden);
  });

  await test('Овердрафт: срок фиксированный — без ползунка, платёж считается', async (page) => {
    await login(page, '79000000001');
    const a = (await apiAs(page, 'POST', '/applications', { product_id: 'overdraft' })).json;
    await page.goto(BASE + '/#/apply/' + a.id);
    await page.waitForSelector('#apply-calc .calc__field--fixed');
    assert(await page.locator('#apply-calc input[type=range][data-calc=term]').count() === 0, 'у овердрафта не должно быть ползунка срока');
    assert(await page.locator('#apply-calc input[type=range][data-calc=amount]').count() === 1, 'ползунок суммы должен остаться');
    assert(norm(await page.inputValue('#apply-calc [data-calc=term-text]')) === '12 месяцев', 'срок овердрафта — 12 месяцев');
    await page.click('[data-prod="business-dev"]');
    await page.waitForSelector('#apply-calc input[type=range][data-calc=term]');
  });

  await test('Телефон: меню профиля открывается касанием на экране (кабинет и главная), не закрывается при прокрутке', async (page, ctx) => {
    const mctx = await ctx.browser().newContext({ ...devices['iPhone 13'] });
    const m = await mctx.newPage();
    try {
      await login(m, '79000000001');
      const vp = m.viewportSize();
      const check = async (where) => {
        await m.tap('#auth-slot .ahdr__user');
        await m.waitForSelector('.dd[data-dd="me"].open .pn-me');
        const r = await m.locator('.dd[data-dd="me"] .dd__panel').boundingBox();
        assert(r && r.y >= 0 && r.y + 60 < vp.height && r.x >= 0 && r.x + r.width <= vp.width, `${where}: меню за пределами экрана ${JSON.stringify(r)}`);
        assert(await m.locator('.dd[data-dd="me"] .pn-me').isVisible(), `${where}: блок с ФИО не виден`);
      };
      await m.goto(BASE + '/#/'); await m.waitForSelector('.mx-title');
      await check('кабинет');
      await m.mouse.wheel(0, 300); await m.waitForTimeout(400);
      assert(await m.locator('.dd[data-dd="me"].open').count() === 1, 'меню закрылось при прокрутке');
      await m.tap('.mx-title');
      await m.waitForFunction(() => !document.querySelector('.dd[data-dd="me"].open'));
      await m.goto(BASE + '/#/products'); await m.waitForSelector('.a-hero h1');
      await check('главная');
      await m.tap('.dd[data-dd="me"] .pn-me');
      await m.waitForSelector('.mx-title');
    } finally { await mctx.close(); }
  });

  await test('Разбор балла: одобрение — шкала с отметкой и плюсы в факторах', async (page) => {
    await login(page, '79000000001');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    await page.waitForSelector('.decision--green', { timeout: 20000 });
    const score = norm(await page.textContent('.acc--score'));
    assert(score.includes('зона одобрения') && score.includes('Стартовый балл') && /\+\d+/.test(score), 'нет разбора балла: ' + score.slice(0, 120));
    assert(await page.locator('.acc--score .sbar__mark').isVisible(), 'на шкале нет отметки балла');
    assert(await page.locator('a[href="#/scenarios"], .a-scn-cta').count() === 0, 'ссылки на «Сценарии проверки» должны быть убраны');
  });

  await test('Разбор балла: отказ — минусы в факторах; аккордеон сворачивается', async (page) => {
    await login(page, '79000000003');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    await page.waitForSelector('.decision--red', { timeout: 20000 });
    assert(await page.locator('.acc--score .f-minus').count() >= 1, 'в разборе нет минусов');
    assert(norm(await page.textContent('.acc--score')).includes('зона отказа'), 'нет зоны отказа');
    const head = page.locator('.acc--score > .acc__head');
    assert(await head.getAttribute('aria-expanded') === 'true', 'разбор должен быть раскрыт');
    await head.click();
    await page.waitForFunction(() => getComputedStyle(document.querySelector('.acc--score > .acc__body')).visibility === 'hidden');
    assert(await head.getAttribute('aria-expanded') === 'false', 'aria-expanded не обновился');
  });

  await test('Узкие экраны: меню «Ещё» и прогресс заявки не вылезают и не наезжают', async (page) => {
    await login(page, '79000000003');
    await page.click('.mx-new [data-do=apply]');
    await page.waitForSelector('#rev');
    const id = page.url().split('/').pop();
    for (const w of [768, 390]) {
      await page.setViewportSize({ width: w, height: 900 });
      await page.goto(BASE + '/#/products'); await page.waitForSelector('.a-hero');
      await page.waitForTimeout(2700); // заявка попала в «Сейчас»
      await page.locator('[data-do=menu]:visible').first().click();
      await page.waitForSelector('#megamenu.open .mm-card');
      const bad = await page.evaluate(() => { const m = document.getElementById('megamenu').getBoundingClientRect(); return [...document.querySelectorAll('#megamenu .mm-card')].filter((c) => { const r = c.getBoundingClientRect(); return r.right > m.right + 1 || r.left < m.left - 1; }).length; });
      assert(bad === 0, `${w}px: карточки «Сейчас» вылезают за меню`);
      await page.keyboard.press('Escape');
    }
    await page.goto(BASE + '/#/apply/' + id); await page.waitForSelector('#rev');
    await page.click('[data-act=use-fns]'); await page.check('#consent');
    await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено');
    await page.click('#submit'); await page.waitForSelector('.tracker');
    const overlap = await page.evaluate(() => { const ls = [...document.querySelectorAll('.tstep__t')].map((e) => e.getBoundingClientRect()); return ls.some((a, i) => i && a.left < ls[i - 1].right - 1 && a.top < ls[i - 1].bottom - 1 && ls[i - 1].top < a.bottom - 1); });
    assert(!overlap, 'подписи этапов наезжают друг на друга');
  });

  await test('Платформа: действия с деньгами идут с Idempotency-Key, кабинет банка показывает инфраструктуру', async (page) => {
    await login(page, '79000000001');
    const keys = [];
    page.on('request', (r) => { if (r.method() === 'POST' && /\/applications(\/[^/]+\/(submit|accept|sign|pay))?$/.test(r.url())) keys.push(r.headers()['idempotency-key']); });
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    assert(keys.length >= 2 && keys.every((k) => k && k.length >= 16), 'POST-действия должны идти с Idempotency-Key: ' + JSON.stringify(keys));
    assert(new Set(keys).size === keys.length, 'у каждого действия свой ключ');
    await page.goto(BASE + '/#/bank');
    await page.waitForSelector('#bank-plat .plat__chip');
    const plat = norm(await page.textContent('#bank-plat'));
    assert(plat.includes('Хранилище') && plat.includes('Шина событий'), 'нет состояния платформы: ' + plat);
  });

  await test('Чужую заявку открыть нельзя', async (page, ctx) => {
    await login(page, '79000000001');
    const mine = (await apiAs(page, 'POST', '/applications', { product_id: 'business-dev' })).json;
    const other = await ctx.browser().newContext();
    const p2 = await other.newPage();
    await login(p2, '79000000002');
    const r = await apiAs(p2, 'GET', '/applications/' + mine.id);
    await other.close();
    assert(r.status === 404, `чужая заявка вернула ${r.status}`);
  });

  await browser.close();
  const failed = results.filter((r) => !r.ok);
  console.log(`\n${results.length - failed.length}/${results.length} прошли`);
  process.exit(failed.length ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(2); });
