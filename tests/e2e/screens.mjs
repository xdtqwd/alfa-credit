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
/** 1-й уровень — предварительный расчёт без входа. */
async function level1(page, { product = 'business-dev', amount, inn = '7701234567', obligations } = {}) {
  await page.goto(BASE + '/#/apply');
  await page.waitForSelector('#ap-inn');
  await page.click(`[data-product="${product}"]`);
  if (amount) {
    await page.fill('#ap-calc [data-calc=amount-text]', String(amount));
    await page.press('#ap-calc [data-calc=amount-text]', 'Enter');
  }
  await page.selectOption('#ap-inn', inn);
  await page.waitForSelector('[data-use-fns]');
  await page.click('[data-use-fns]');
  if (obligations) await page.fill('#ap-obligations', String(obligations));
  await sleep(500);
}
const prescore = async (page) => {
  await page.click('#prescore');
  await page.waitForSelector('#prescore-result');
  await sleep(600);
};
/** Вход через Госуслуги и 2-й уровень анкеты. */
async function esia(page) {
  await page.click('#esia-go');
  await page.waitForSelector('#esia-ok');
  await page.click('#esia-ok');
  await page.waitForSelector('[data-key="consent"]');
}
const submit = async (page) => {
  await page.check('#ff-consent');
  await page.waitForFunction(() => document.querySelector('#save-state')?.textContent === 'Сохранено');
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

// одобрение: профиль → предварительный расчёт → Госуслуги → документы → полный скоринг → кредит
{
  const { ctx, page } = await session();
  await login(page, '79000000001');
  await sleep(400);
  await shot(page, 'profile');
  await level1(page);
  await top(page);
  await shot(page, 'apply');
  await prescore(page);
  await shotOf(page, 'preliminary', '#prescore-result');
  await page.click('#esia-go');
  await page.waitForSelector('.esia__person.is-on');
  await sleep(400);
  await shotOf(page, 'esia', '.modal');
  await page.click('#esia-ok');
  await page.waitForSelector('[data-key="consent"]');
  await page.click('[data-collateral=equipment]');
  await page.fill('#ff-coll-value', '4000000');
  await page.check('#ff-consent');
  await sleep(1500);
  await top(page);
  await shot(page, 'full-form');
  await page.click('#submit');
  await page.waitForSelector('.stepper');
  await top(page);
  await sleep(1200);
  await shot(page, 'progress');
  await page.waitForSelector('#decision[data-decision=approved]', { timeout: 30000 });
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

// контрпредложение — уже в предварительном расчёте, без входа
{
  const { ctx, page } = await session();
  await level1(page, { inn: '2224000012', amount: 20000000, obligations: 200000 });
  await prescore(page);
  await shotOf(page, 'counter', '#prescore-result');
  await ctx.close();
}
// отказ — на полном скоринге: просрочки видны только в кредитной истории
{
  const { ctx, page } = await session();
  await level1(page, { inn: '5401000009', obligations: 300000 });
  await prescore(page);
  await esia(page);
  await submit(page);
  await page.waitForSelector('#decision.decision--bad', { timeout: 30000 });
  await shotOf(page, 'reject', '#decision');
  await ctx.close();
}
// кабинет банка: сотрудник входит своей учётной записью и попадает в профиль
{
  const { ctx, page } = await session();
  await page.goto(BASE + '/#/');
  await page.click('[data-header=login]');
  await page.click('[data-demo="79000000005"]');
  await page.waitForSelector('.staff-queue');
  await sleep(500);
  await shot(page, 'staff-profile', { fullPage: true });
  await page.goto(BASE + '/#/bank');
  await page.waitForSelector('#bank-table table');
  await sleep(2500);
  await shot(page, 'bank');
  await shot(page, 'bank-platform', { clip: { x: 0, y: 70, width: 1440, height: 330 } });
  await ctx.close();
}

// разбор балла полного скоринга
{
  const { ctx, page } = await session();
  await level1(page, { inn: '2224000012' });
  await prescore(page);
  await esia(page);
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
