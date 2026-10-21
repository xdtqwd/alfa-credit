// Запись демо-видео работающего приложения (Playwright, без внешних сервисов).
// Показывает связанный процесс: клиент подаёт заявку → банковский конвейер проверяет и решает →
// клиент видит результат, подписывает, получает деньги и платит; плюс отказ с объяснением.
//
// Запуск (сервер уже работает на BASE_URL):
//   BASE_URL=http://localhost:8080 node tests/e2e/demo.mjs        # → demo/alfa-credit-demo.webm
// Без Node — через Docker, см. README, раздел «Демонстрация».
import { chromium } from 'playwright';
import { mkdirSync, readdirSync, renameSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.DEMO_DIR || 'demo';
const SIZE = { width: 1280, height: 800 };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Подпись внизу кадра — объясняет зрителю, что происходит.
async function caption(page, text, ms = 2200) {
  await page.evaluate((t) => {
    let el = document.getElementById('__demo_caption');
    if (!el) {
      el = document.createElement('div');
      el.id = '__demo_caption';
      Object.assign(el.style, {
        position: 'fixed', left: '50%', bottom: '24px', transform: 'translateX(-50%)', zIndex: 99999,
        maxWidth: '900px', padding: '14px 22px', borderRadius: '16px', background: 'rgba(17,24,39,.92)',
        color: '#fff', font: '600 18px/1.4 system-ui, sans-serif', textAlign: 'center',
        boxShadow: '0 12px 32px rgba(0,0,0,.25)', pointerEvents: 'none', transition: 'opacity .25s',
      });
      document.body.appendChild(el);
    }
    el.textContent = t; el.style.opacity = t ? '1' : '0';
  }, text);
  if (ms) await sleep(ms);
}

async function login(page, phone) {
  await page.click('#auth-slot [data-do=login]');
  await page.waitForSelector('#a-phone');
  await page.type('#a-phone', phone, { delay: 40 });
  await page.type('#a-pass', 'demo1234', { delay: 40 });
  await page.click('#auth-form button[type=submit]');
  await page.waitForSelector('#auth-slot .ahdr__user');
}

async function fillAndSubmit(page) {
  await page.waitForSelector('#rev');
  await caption(page, 'Анкета уже заполнена из профиля и ЕГРЮЛ — осталось подставить выручку из ФНС и дать согласие');
  await page.click('[data-act=use-fns]');
  await sleep(600);
  await page.check('#consent');
  await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено', null, { timeout: 8000 });
  await caption(page, 'Отправляем — без отдельного шага «оставить заявку»', 1400);
  await page.click('#submit');
  await page.waitForSelector('.tracker');
}

(async () => {
  rmSync(join(OUT, '_raw'), { recursive: true, force: true });
  mkdirSync(join(OUT, '_raw'), { recursive: true });
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: SIZE, recordVideo: { dir: join(OUT, '_raw'), size: SIZE } });
  const page = await ctx.newPage();
  const t0 = Date.now();
  try {
    // 1. Гость: продуктовая страница и калькулятор
    await page.goto(BASE + '/#/products');
    await page.waitForSelector('.a-hero h1');
    await caption(page, 'Умный кредитный кабинет предпринимателя · Альфа Хак 2026', 2500);
    await page.click('.ahdr__nav [data-do=calc]');
    await sleep(900);
    await caption(page, 'Калькулятор: платёж считает сервер (Go) — единый источник расчёта');
    const range = page.locator('#calc [data-calc=amount]');
    if (await range.count()) {
      const box = await range.boundingBox();
      await page.mouse.move(box.x + box.width * 0.15, box.y + box.height / 2);
      await page.mouse.down();
      for (let i = 1; i <= 10; i++) { await page.mouse.move(box.x + box.width * (0.15 + i * 0.02), box.y + box.height / 2); await sleep(70); }
      await page.mouse.up();
      await sleep(900);
    }

    // 2. Оформить без входа нельзя → вход → сразу анкета
    await caption(page, 'Оформить без входа нельзя: вход по телефону, и сразу открывается анкета', 1500);
    await page.click('#calc [data-act=calc-apply]');
    await page.waitForSelector('#a-phone');
    await page.type('#a-phone', '79000000001', { delay: 40 });
    await page.type('#a-pass', 'demo1234', { delay: 40 });
    await page.click('#auth-form button[type=submit]');
    await fillAndSubmit(page);

    // 3. Банковская логика: конвейер и кабинет банка
    await caption(page, 'Банк обрабатывает заявку: проверка данных (ЕГРЮЛ, ФНС, БКИ) → оценка риска → решение', 1200);
    const appUrl = page.url();
    await page.goto(BASE + '/#/bank');
    await page.waitForSelector('table.bank');
    await caption(page, 'Кабинет сотрудника банка: та же заявка идёт по конвейеру в реальном времени', 0);
    await page.waitForFunction(() => /Одобрено|Контрпредложение|Отказ|Уточнение/.test(document.querySelector('table.bank tbody tr')?.textContent || ''), null, { timeout: 30000 });
    await caption(page, 'Скоринг принял решение: балл, условия и причина видны банку', 3000);

    // 4. Клиент видит результат, подписывает, получает деньги и платит
    await page.goto(appUrl);
    await page.waitForSelector('.decision--green', { timeout: 30000 });
    await caption(page, 'Клиент сразу видит решение и что от него нужно — простыми словами', 3000);
    await page.click('.todo-banner [data-act=accept]');
    await page.waitForSelector('#sign-ok');
    await caption(page, 'Подписание простой электронной подписью прямо в кабинете', 1600);
    await page.check('#sign-ok');
    await page.click('#sign-btn');
    await page.waitForSelector('.schedule');
    await caption(page, 'Деньги зачислены на расчётный счёт, график платежей построен', 3000);
    await page.click('.todo-banner [data-act=pay]');
    await page.waitForSelector('#pay-ok');
    await sleep(800);
    await page.click('#pay-ok');
    await page.waitForFunction(() => /Оплачено 1 из/.test(document.querySelector('.pay-row')?.textContent || ''), null, { timeout: 10000 });
    await caption(page, 'Первый платёж списан со счёта строго по графику', 2500);

    // 5. Личный кабинет
    await page.goto(BASE + '/#/credits');
    await page.waitForSelector('.mx-title');
    await caption(page, 'Минималистичный личный кабинет: заявки, кредиты, сообщения', 3000);
    await page.click('#auth-slot .ahdr__user');
    await page.waitForSelector('.dd[data-dd="me"].open');
    await caption(page, 'Профиль компании, реквизиты, выход', 2200);
    await page.click('[data-do=logout]');
    await page.waitForSelector('#auth-slot [data-do=login]');

    // 6. Отказ с понятным объяснением
    await caption(page, 'Другой сценарий: у компании просрочки в БКИ', 1800);
    await login(page, '79000000003');
    await page.click('.mx-new [data-do=apply]');
    await fillAndSubmit(page);
    await page.waitForSelector('.decision--red', { timeout: 30000 });
    await page.locator('.decision').scrollIntoViewIfNeeded();
    await caption(page, 'Отказ не «чёрный ящик»: причина на данных анкеты и что можно сделать', 4500);
    await caption(page, 'Без платных API и подписок · один Docker-контейнер · обычный ПК', 3000);
    await caption(page, '', 0);
  } finally {
    const video = page.video();
    await ctx.close();
    await browser.close();
    const raw = await video?.path();
    if (raw) {
      const dst = join(OUT, 'alfa-credit-demo.webm');
      renameSync(raw, dst);
      rmSync(join(OUT, '_raw'), { recursive: true, force: true });
      console.log(`Видео: ${dst} (${Math.round((Date.now() - t0) / 1000)} с)`);
    } else {
      console.log('Видео не записано', readdirSync(join(OUT, '_raw')));
    }
  }
})().catch((e) => { console.error(e); process.exit(1); });
