// Скриншоты для README (Playwright). Сервер должен работать на BASE_URL с чистыми данными.
//   BASE_URL=http://localhost:8080 node tests/e2e/screens.mjs   # → docs/screens/*.png
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE_URL || 'http://localhost:8080';
const OUT = process.env.SCREENS_DIR || 'docs/screens';
const DESK = { width: 1440, height: 900 };
const PHONE = { width: 390, height: 844 };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
mkdirSync(OUT, { recursive: true });

const browser = await chromium.launch();
async function session(viewport = DESK) {
  const ctx = await browser.newContext({ viewport, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  await page.addInitScript(() =>
    document.addEventListener('DOMContentLoaded', () => {
      const s = document.createElement('style');
      s.textContent = '#toasts{display:none!important}';
      document.head.append(s);
    }),
  );
  return { ctx, page };
}
const shot = (page, name, opts = {}) => page.screenshot({ path: `${OUT}/${name}.png`, ...opts });
const shotOf = async (page, name, selector) => {
  await page.locator(selector).scrollIntoViewIfNeeded();
  await sleep(400);
  await shot(page, name, { clip: await page.locator(selector).boundingBox() });
};

async function login(page, phone) {
  await page.goto(BASE + '/#/');
  await page.click('[data-header=login]');
  await page.fill('#auth-phone', phone);
  await page.fill('#auth-password', 'demo1234');
  await page.click('#auth-form button[type=submit]');
  await page.waitForSelector('#requisites');
}
async function fill(page, { revenue, obligations } = {}) {
  await page.waitForSelector('#f-revenue');
  if (revenue) await page.fill('#f-revenue', String(revenue));
  else await page.click('[data-use-fns]');
  if (obligations) await page.fill('#f-obligations', String(obligations));
  await page.check('#f-consent');
  await page.waitForFunction(() => document.querySelector('#save-state')?.textContent === 'Сохранено');
}
const newApplication = async (page) => {
  await page.click('[data-header=new-loan]');
  await page.waitForSelector('#f-revenue');
};
const submit = async (page) => {
  await page.click('#submit');
  await page.waitForSelector('.stepper');
};
const top = async (page) => {
  await page.evaluate(() => window.scrollTo(0, 0));
  await sleep(300);
};

// гость: главная и вход
{
  const { ctx, page } = await session();
  await page.goto(BASE + '/#/');
  await page.waitForSelector('.hero h1');
  await sleep(600);
  await shot(page, 'landing');
  await page.click('[data-header=login]');
  await page.waitForSelector('#auth-phone');
  await sleep(300);
  await shot(page, 'login');
  await ctx.close();
}

// одобрение: профиль → анкета → прогресс → решение → кредит
{
  const { ctx, page } = await session();
  await login(page, '79000000001');
  await sleep(400);
  await shot(page, 'profile');
  await newApplication(page);
  await fill(page);
  await top(page);
  await shot(page, 'apply');
  await submit(page);
  await top(page);
  await sleep(1200);
  await shot(page, 'progress');
  await page.waitForSelector('#decision.decision--ok', { timeout: 30000 });
  await top(page);
  await shot(page, 'approved');
  await page.click('#todo [data-action=accept]');
  await page.check('#sign-ok');
  await page.click('#sign-btn');
  await page.waitForSelector('table.schedule');
  await page.click('#todo [data-action=pay]');
  await page.click('#pay-ok');
  await page.waitForFunction(() => /Оплачено 1 из/.test(document.querySelector('#pay-line')?.textContent || ''));
  await page.goto(BASE + '/#/cabinet/credits');
  await page.waitForSelector('.loan');
  await sleep(500);
  await shot(page, 'cab-credits');
  await page.click('[data-header=menu]');
  await page.waitForSelector('.menu');
  await sleep(300);
  await shot(page, 'profile-menu');
  await ctx.close();
}

// контрпредложение и отказ
{
  const { ctx, page } = await session();
  await login(page, '79000000002');
  await page.goto(BASE + '/#/');
  await page.click('#products [data-start="business-dev"]');
  await page.waitForSelector('[data-calc=amount-text]');
  await page.click('[data-calc=amount-text]');
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.type('20000000');
  await page.keyboard.press('Enter');
  await fill(page, { obligations: 200000 });
  await submit(page);
  await page.waitForSelector('#decision.decision--warn', { timeout: 30000 });
  await shotOf(page, 'counter', '#decision');
  await ctx.close();
}
{
  const { ctx, page } = await session();
  await login(page, '79000000003');
  await newApplication(page);
  await fill(page);
  await submit(page);
  await page.waitForSelector('#decision.decision--bad', { timeout: 30000 });
  await shotOf(page, 'reject', '#decision');
  await page.goto(BASE + '/#/bank');
  await page.waitForSelector('#bank-table table');
  await sleep(2500);
  await shot(page, 'bank');
  await shot(page, 'bank-platform', { clip: { x: 0, y: 70, width: 1440, height: 330 } });
  await ctx.close();
}

// разбор балла
{
  const { ctx, page } = await session();
  await login(page, '79000000002');
  await newApplication(page);
  await fill(page);
  await submit(page);
  await page.waitForSelector('#decision.decision--ok', { timeout: 30000 });
  await shotOf(page, 'score', '.acc.score');
  await ctx.close();
}

// телефон
{
  const { ctx, page } = await session(PHONE);
  await page.goto(BASE + '/#/');
  await page.waitForSelector('.hero h1');
  await sleep(500);
  await shot(page, 'mobile-landing');
  await login(page, '79000000001');
  await sleep(500);
  await shot(page, 'mobile-cabinet');
  await ctx.close();
}

await browser.close();
console.log('Скриншоты сохранены в', OUT);
