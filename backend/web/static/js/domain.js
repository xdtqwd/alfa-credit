// Словари и правила интерфейса для заявки: статусы, этапы, «что нужно от клиента».
import { rub, months } from './core/format.js';

/** Статус заявки → подпись и тон бейджа. draft и verification — у заявок прошлой версии. */
export const STATUS = {
  draft: ['Черновик', ''],
  full_form: ['Документы', 'warn'],
  submitted: ['Отправлена', 'info'],
  data_check: ['Проверка данных', 'info'],
  scoring: ['Полный скоринг', 'info'],
  approved: ['Одобрено', 'ok'],
  counter_offer: ['Другие условия', 'warn'],
  need_info: ['Нужно уточнение', 'warn'],
  verification: ['Окончательная проверка', 'info'],
  rejected: ['Отказ', 'bad'],
  signing: ['Подписание', 'info'],
  issued: ['Кредит выдан', 'ok'],
  cancelled: ['Отменена', ''],
  closed: ['Погашен', ''],
};

export const statusLabel = (s) => STATUS[s]?.[0] || s;

export const statusBadge = (s) => `<span class="status ${STATUS[s]?.[1] ? `status--${STATUS[s][1]}` : ''}">${statusLabel(s)}</span>`;

/** Можно ли править 2-й уровень анкеты (документы, обеспечение, согласие). */
export const editable = (a) => a.status === 'full_form' || (a.status === 'need_info' && a.level === 2);

// Этапы заявки: 1-й уровень — расчёт без входа, 2-й — Госуслуги, документы и полный скоринг.
const STAGES = ['Предварительный расчёт', 'Госуслуги и документы', 'Проверка данных', 'Полный скоринг', 'Решение', 'Деньги на счёте'];
const STAGE_OF = {
  draft: 0,
  full_form: 1,
  need_info: 1,
  cancelled: 1,
  submitted: 2,
  data_check: 2,
  scoring: 3,
  verification: 3,
  approved: 4,
  counter_offer: 4,
  rejected: 4,
  signing: 4,
  issued: 5,
  closed: 5,
};

/** Этапы и текущий шаг заявки. */
export const stagesFor = (a) => ({ stages: STAGES, idx: STAGE_OF[a.status] ?? 0 });

export const IN_PIPELINE = ['submitted', 'data_check', 'scoring', 'verification'];
export const DECIDED = ['approved', 'counter_offer', 'rejected', 'need_info'];
export const FINAL = ['issued', 'closed', 'rejected', 'cancelled'];
export const LOANS = ['issued', 'closed'];

export const DOING = {
  submitted: 'Заявка принята, ставим в очередь на полный скоринг…',
  data_check: 'Проверяем компанию в ЕГРЮЛ, кредитную историю в БКИ, паспорт и документы…',
  scoring: 'Считаем полный скоринг: балл с кредитной историей и обеспечение…',
  verification: 'Проверяем документы и обеспечение…',
};

export const appPath = (a) => `/cabinet/applications/${a.id}`;

/**
 * Что требуется от предпринимателя на каждом статусе. Показывается крупно на странице заявки
 * и в списке заявок. action — кнопка на странице заявки.
 */
export function todoFor(a) {
  const d = a.decision;
  const t = (need, title, text, action) => ({ need, title, text, action });
  switch (a.status) {
    case 'draft':
      return t(true, 'Анкета из прошлой версии', 'Начните новый предварительный расчёт — это пара минут, вход через Госуслуги.', [
        'restart',
        'Рассчитать заново',
      ]);
    case 'full_form':
      return t(
        true,
        'Приложите документы',
        'Паспорт руководителя, обеспечение и согласие на запрос в БКИ — 2–3 минуты. После них — полный скоринг и окончательное решение.',
        ['fix', 'Открыть анкету'],
      );
    case 'submitted':
    case 'data_check':
    case 'scoring':
    case 'verification':
      return t(
        false,
        'От вас ничего не требуется',
        'Банк проверяет документы и считает полный скоринг с кредитной историей — обычно 10–20 секунд. Страницу можно закрыть: статус сохранится.',
      );
    case 'approved':
      return t(true, 'Примите условия и подпишите договор', `Банк одобрил ${rub(d.amount)}. Проверьте платёж и переплату ниже.`, [
        'accept',
        'Принять и подписать',
      ]);
    case 'counter_offer':
      return t(true, 'Сравните условия и решите', 'Банк готов выдать кредит на других условиях — сравнение ниже.', [
        'accept',
        'Принять новые условия',
      ]);
    case 'need_info':
      return t(true, 'Уточните данные', d?.reasons?.[0] || 'Банку нужно уточнение по документам или обеспечению.', [
        'fix',
        'Исправить анкету',
      ]);
    case 'signing':
      return t(true, 'Подпишите документы', 'Договор, график платежей и заявление на зачисление готовы. Подпись — прямо здесь, без SMS.', [
        'sign',
        'Подписать',
      ]);
    case 'issued':
      return t(
        true,
        `Вносите платёж ${rub(a.next_payment)} раз в месяц`,
        `Оплачено ${(a.payments || []).length} из ${a.term_months}. Платёж списывается с расчётного счёта.`,
        ['pay', 'Оплатить сейчас'],
      );
    case 'rejected':
      return t(false, 'Заявка закрыта', 'Ниже — почему и что можно сделать, чтобы получить кредит.');
    case 'closed':
      return t(false, 'Ничего делать не нужно', 'Кредит полностью погашен.');
    case 'cancelled':
      return t(false, 'Ничего делать не нужно', 'Заявка отменена. Новый расчёт — пара минут.');
    default:
      return t(false, '', '');
  }
}

/** Короткая строка о заявке для списков. */
export const appSummary = (a, product) =>
  `${product?.name || 'Кредит'} · ${rub(a.decision?.amount || a.amount)} на ${months(a.decision?.term_months || a.term_months)}`;

/** События банка по заявкам — лента «Сообщения». */
export const bankEvents = (apps) =>
  apps
    .flatMap((a) => a.history.filter((h) => h.actor !== 'client').map((h) => ({ app: a, ...h })))
    .sort((x, y) => new Date(y.at) - new Date(x.at));
