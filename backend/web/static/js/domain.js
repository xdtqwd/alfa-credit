// Словари и правила интерфейса для заявки: статусы, этапы, «что нужно от клиента».
import { rub, months } from './core/format.js';

/** Статус заявки → подпись и тон бейджа. */
export const STATUS = {
  draft: ['Черновик', ''],
  submitted: ['Отправлена', 'info'],
  data_check: ['Проверка данных', 'info'],
  scoring: ['Оценка риска', 'info'],
  approved: ['Одобрено', 'ok'],
  counter_offer: ['Другие условия', 'warn'],
  need_info: ['Нужно уточнение', 'warn'],
  rejected: ['Отказ', 'bad'],
  signing: ['Подписание', 'info'],
  issued: ['Кредит выдан', 'ok'],
  cancelled: ['Отменена', ''],
  closed: ['Погашен', ''],
};

export const statusBadge = (s) => `<span class="status ${STATUS[s][1] ? `status--${STATUS[s][1]}` : ''}">${STATUS[s][0]}</span>`;

export const STAGES = ['Анкета', 'Проверка данных', 'Оценка риска', 'Решение', 'Подписание', 'Деньги на счёте'];
export const STAGE_OF = {
  draft: 0,
  need_info: 0,
  cancelled: 0,
  submitted: 1,
  data_check: 1,
  scoring: 2,
  approved: 3,
  counter_offer: 3,
  rejected: 3,
  signing: 4,
  issued: 5,
  closed: 5,
};

export const IN_PIPELINE = ['submitted', 'data_check', 'scoring'];
export const DECIDED = ['approved', 'counter_offer', 'rejected', 'need_info'];
export const FINAL = ['issued', 'closed', 'rejected', 'cancelled'];
export const EDITABLE = ['draft', 'need_info'];
export const LOANS = ['issued', 'closed'];

export const DOING = {
  submitted: 'Заявка принята, ставим в очередь на проверку…',
  data_check: 'Проверяем компанию в ЕГРЮЛ/ЕГРИП и кредитную историю в БКИ…',
  scoring: 'Считаем долговую нагрузку и принимаем решение…',
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
      return t(true, 'Заполните и отправьте анкету', 'Всё сохраняется автоматически — можно продолжить в любой момент.');
    case 'submitted':
    case 'data_check':
    case 'scoring':
      return t(
        false,
        'От вас ничего не требуется',
        'Банк сам проверяет данные и принимает решение — обычно 10–20 секунд. Страницу можно закрыть: статус сохранится.',
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
      return t(true, 'Исправьте данные и отправьте снова', d?.reasons?.[0] || 'Банку нужно уточнение.', ['fix', 'Исправить анкету']);
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
      return t(false, 'Ничего делать не нужно', 'Заявка отменена. Новую можно оформить за пару минут.');
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
