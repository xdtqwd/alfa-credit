// Юнит-тесты чистых модулей фронтенда (без браузера): форматирование, правила заявки, маршруты.
// Запуск: node --test tests/unit
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  months,
  shortRub,
  shortTerm,
  rub,
  rate,
  phone,
  esc,
  digits,
  maskAccount,
  initials,
} from '../../backend/web/static/js/core/format.js';
import { STATUS, IN_PIPELINE, todoFor, appSummary, stagesFor, editable, statusLabel } from '../../backend/web/static/js/domain.js';
import { route, redirect, match } from '../../backend/web/static/js/core/router.js';

const nbsp = (s) => s.replace(/ | /g, ' ');

test('months склоняет «месяц»', () => {
  const cases = {
    1: 'месяц',
    2: 'месяца',
    4: 'месяца',
    5: 'месяцев',
    11: 'месяцев',
    12: 'месяцев',
    21: 'месяц',
    22: 'месяца',
    111: 'месяцев',
  };
  for (const [n, word] of Object.entries(cases)) assert.equal(months(+n), `${n} ${word}`);
});

test('шкала ползунка: суммы коротко, ровные годы — годами', () => {
  assert.equal(nbsp(shortRub(300000)), '300 тыс ₽');
  assert.equal(shortRub(15150000), '15,2 млн ₽');
  assert.equal(shortRub(30000000), '30 млн ₽');
  assert.equal(shortTerm(12), '1 год');
  assert.equal(shortTerm(24), '2 года');
  assert.equal(shortTerm(60), '5 лет');
  assert.equal(shortTerm(33), '33 месяца');
});

test('деньги, ставка, телефон, маски', () => {
  assert.equal(nbsp(rub(3000000)), '3 000 000 ₽');
  assert.equal(rate(19.9), '19,9%');
  assert.equal(phone('89000000001'), '+7 (900) 000-00-01');
  assert.equal(phone('900'), '+7 (900)');
  assert.equal(phone(''), '');
  assert.equal(digits('+7 (900) 1-2'), '790012');
  assert.equal(maskAccount('40702810970008037747'), '•• 7747');
  assert.equal(initials('Смирнов Алексей Викторович'), 'СА');
});

test('esc экранирует разметку из пользовательских данных', () => {
  assert.equal(esc('<img src=x onerror="a">&\''), '&lt;img src=x onerror=&quot;a&quot;&gt;&amp;&#39;');
  assert.equal(esc(null), '');
});

test('у каждого статуса есть подпись и этап прогресса', () => {
  for (const s of Object.keys(STATUS)) {
    assert.ok(STATUS[s][0], `нет подписи статуса ${s}`);
    const { stages, idx } = stagesFor({ status: s, decision: {} });
    assert.ok(Number.isInteger(idx) && idx >= 0 && idx < stages.length, `нет этапа для статуса ${s}`);
  }
  for (const s of IN_PIPELINE) assert.ok(STATUS[s], `неизвестный статус ${s}`);
});

test('двухуровневая анкета: когда правится 2-й уровень и на каком этапе заявка', () => {
  assert.equal(editable({ status: 'full_form', level: 2 }), true);
  assert.equal(editable({ status: 'need_info', level: 2 }), true);
  assert.equal(editable({ status: 'draft' }), false); // черновик прошлой версии — только подсказка начать заново
  assert.equal(editable({ status: 'submitted', level: 2 }), false);
  assert.equal(statusLabel('full_form'), 'Документы');
  const stage = (status) => stagesFor({ status }).stages[stagesFor({ status }).idx];
  assert.equal(stage('full_form'), 'Госуслуги и документы');
  assert.equal(stage('scoring'), 'Полный скоринг');
  assert.equal(stage('approved'), 'Решение');
  assert.equal(stage('issued'), 'Деньги на счёте');
});

test('todoFor: главная кнопка заявки — одна и по статусу', () => {
  const app = (status, extra = {}) => ({
    status,
    amount: 3000000,
    term_months: 24,
    decision: { amount: 3000000, reasons: ['Уточните выручку'] },
    next_payment: 151080,
    payments: [],
    ...extra,
  });
  assert.deepEqual(todoFor(app('approved', { decision: { amount: 3000000, final: true } })).action, ['accept', 'Принять и подписать']);
  assert.equal(todoFor(app('full_form')).action[0], 'fix');
  assert.equal(todoFor(app('draft')).action[0], 'restart');
  assert.equal(todoFor(app('counter_offer')).action[0], 'accept');
  assert.equal(todoFor(app('need_info')).action[0], 'fix');
  assert.equal(todoFor(app('need_info')).text, 'Уточните выручку');
  assert.equal(todoFor(app('signing')).action[0], 'sign');
  assert.equal(todoFor(app('issued')).action[0], 'pay');
  for (const s of ['submitted', 'data_check', 'scoring', 'verification', 'rejected', 'closed', 'cancelled']) {
    assert.equal(todoFor(app(s)).need, false, `от клиента ничего не нужно на статусе ${s}`);
    assert.equal(todoFor(app(s)).action, undefined);
  }
  assert.equal(nbsp(appSummary(app('approved'), { name: 'Кредит на развитие' })), 'Кредит на развитие · 3 000 000 ₽ на 24 месяца');
});

test('маршруты: параметры и старые адреса', () => {
  const page = () => {};
  route('/cabinet/applications/:id', page, { auth: true });
  redirect('/app/:id', '/cabinet/applications/$1');
  assert.deepEqual(match('/cabinet/applications/abc123').params, { id: 'abc123' });
  assert.equal(match('/app/abc123').path, '/cabinet/applications/abc123');
  assert.equal(match('/cabinet/applications/abc/extra'), null);
});
