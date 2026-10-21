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
  return { ctx, page: await ctx.newPage() };
}
const shot = (page, name, opts = {}) => page.screenshot({ path: `${OUT}/${name}.png`, ...opts });
const hideNotices = (page) => page.addStyleTag({ content: '#notices,#toast{display:none!important}' });

async function login(page, phone) {
  await page.goto(BASE + '/');
  await page.click('#auth-slot [data-do=login]');
  await page.fill('#a-phone', phone);
  await page.fill('#a-pass', 'demo1234');
  await page.click('#auth-form button[type=submit]');
  await page.waitForSelector('#auth-slot .ahdr__user');
  await hideNotices(page);
}
async function fill(page, { revenue, obligations } = {}) {
  await page.waitForSelector('#rev');
  if (revenue) await page.fill('#rev', String(revenue)); else await page.click('[data-act=use-fns]');
  if (obligations) await page.fill('#obl', String(obligations));
  await page.check('#consent');
  await page.waitForFunction(() => document.querySelector('#s-saved')?.textContent === 'Сохранено');
}
const submit = async (page) => { await page.click('#submit'); await page.waitForSelector('.tracker'); };
const top = async (page) => { await page.evaluate(() => window.scrollTo(0, 0)); await sleep(300); };

// гость: главная, калькулятор, вход
{
  const { ctx, page } = await session();
  await page.goto(BASE + '/#/products'); await page.waitForSelector('.a-hero h1'); await sleep(600);
  await shot(page, 'landing');
  await page.locator('#calc').scrollIntoViewIfNeeded(); await sleep(400);
  await shot(page, 'calc', { clip: await page.locator('#calc').boundingBox() });
  await top(page);
  await page.click('#auth-slot [data-do=login]'); await page.waitForSelector('#a-phone'); await sleep(300);
  await shot(page, 'login');
  await ctx.close();
}

// одобрение: анкета → прогресс → решение → кредит
{
  const { ctx, page } = await session();
  await login(page, '79000000001');
  await page.click('.mx-new [data-do=apply]');
  await fill(page); await top(page);
  await shot(page, 'apply');
  await submit(page); await top(page); await sleep(1200);
  await shot(page, 'progress');
  const appUrl = page.url();
  await page.goto(BASE + '/#/bank'); await page.waitForSelector('table.bank tbody tr'); await sleep(1500);
  await shot(page, 'bank-live', { clip: { x: 0, y: 0, width: 1440, height: 420 } });
  await page.goto(appUrl);
  await page.waitForSelector('.decision--green', { timeout: 30000 }); await top(page);
  await shot(page, 'approved');
  await page.click('.todo-banner [data-act=accept]'); await page.check('#sign-ok'); await page.click('#sign-btn');
  await page.waitForSelector('.schedule');
  await page.click('.todo-banner [data-act=pay]'); await page.click('#pay-ok');
  await page.waitForFunction(() => /Оплачено 1 из/.test(document.querySelector('.pay-row')?.textContent || ''));
  await page.goto(BASE + '/#/credits'); await page.waitForSelector('.mx-title'); await sleep(500);
  await shot(page, 'cab-credits');
  await page.goto(BASE + '/#/'); await page.waitForSelector('.mx-title'); await sleep(500);
  await shot(page, 'cab-apps');
  await page.click('#auth-slot .ahdr__user'); await page.waitForSelector('.dd[data-dd="me"].open'); await sleep(300);
  await shot(page, 'profile-menu');
  await page.goto(BASE + '/#/products'); await page.waitForSelector('.a-hero'); await sleep(500);
  await page.click('.ahdr__nav [data-do=menu]'); await page.waitForSelector('#megamenu.open .mm__cols'); await sleep(400);
  await shot(page, 'menu');
  await ctx.close();
}

// контрпредложение и отказ
{
  const { ctx, page } = await session();
  await login(page, '79000000002');
  await page.goto(BASE + '/#/products');
  await page.click('.a-prod [data-act=start][data-product="business-dev"]');
  await page.waitForSelector('[data-calc=amount-text]');
  await page.click('[data-calc=amount-text]'); await page.keyboard.type('20000000'); await page.keyboard.press('Enter');
  await fill(page, { obligations: 200000 }); await submit(page);
  await page.waitForSelector('.decision--amber', { timeout: 30000 });
  await page.locator('.decision').scrollIntoViewIfNeeded(); await sleep(400);
  await shot(page, 'counter', { clip: await page.locator('.decision').boundingBox() });
  await ctx.close();
}
{
  const { ctx, page } = await session();
  await login(page, '79000000003');
  await page.click('.mx-new [data-do=apply]');
  await fill(page); await submit(page);
  await page.waitForSelector('.decision--red', { timeout: 30000 });
  await page.locator('.decision').scrollIntoViewIfNeeded(); await sleep(400);
  await shot(page, 'reject', { clip: await page.locator('.decision').boundingBox() });
  await page.goto(BASE + '/#/bank'); await page.waitForSelector('table.bank tbody tr'); await sleep(2500);
  await shot(page, 'bank');
  await shot(page, 'bank-platform', { clip: { x: 0, y: 70, width: 1440, height: 330 } });
  await ctx.close();
}

// разбор балла
{
  const { ctx, page } = await session();
  await login(page, '79000000002');
  await page.click('.mx-new [data-do=apply]');
  await fill(page); await submit(page);
  await page.waitForSelector('.decision--green', { timeout: 30000 });
  await page.locator('.acc--score').scrollIntoViewIfNeeded(); await sleep(500);
  await shot(page, 'score', { clip: await page.locator('.acc--score').boundingBox() });
  await ctx.close();
}

// телефон
{
  const { ctx, page } = await session(PHONE);
  await page.goto(BASE + '/#/products'); await page.waitForSelector('.a-hero h1'); await sleep(500);
  await shot(page, 'mobile-landing');
  await login(page, '79000000001');
  await page.goto(BASE + '/#/'); await page.waitForSelector('.mx-title'); await sleep(500);
  await shot(page, 'mobile-cabinet');
  await ctx.close();
}

await browser.close();
console.log('Скриншоты сохранены в', OUT);
