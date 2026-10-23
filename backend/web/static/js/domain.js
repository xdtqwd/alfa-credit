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
  full_form: ['Полная анкета', 'warn'],
  verification: ['Окончательная проверка', 'info'],
  rejected: ['Отказ', 'bad'],
  signing: ['Подписание', 'info'],
  issued: ['Кредит выдан', 'ok'],
  cancelled: ['Отменена', ''],
  closed: ['Погашен', ''],
};

/** Решение по быстрой анкете — предварительное: дальше полная анкета. */
export const isPreliminary = (a) => ['approved', 'counter_offer'].includes(a?.status) && a.decision && !a.decision.final;

/** Подпись статуса с учётом уровня: «Предварительно одобрено» вместо «Одобрено». */
export function statusLabel(s, a) {
  if (a && isPreliminary(a)) return s === 'approved' ? 'Предварительно одобрено' : 'Предварительно: другие условия';
  if (s === 'approved' && a?.preliminary) return 'Одобрено окончательно';
  if (s === 'need_info' && a?.level === 2) return 'Уточните полную анкету';
  return STATUS[s][0];
}

export const statusBadge = (s, a) => `<span class="status ${STATUS[s][1] ? `status--${STATUS[s][1]}` : ''}">${statusLabel(s, a)}</span>`;

/** Уровень анкеты, которую клиент сейчас правит: 1 — быстрая, 2 — полная, 0 — править нечего. */
export function editLevel(a) {
  if (a.status === 'draft' || (a.status === 'need_info' && a.level !== 2)) return 1;
  if (a.status === 'full_form' || (a.status === 'need_info' && a.level === 2)) return 2;
  return 0;
}

// Этапы одноуровневой заявки (овердрафт).
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

// Этапы двухуровневой заявки: быстрая анкета → предварительное решение → полная анкета → окончательное.
export const STAGES_2 = [
  'Быстрая анкета',
  'Проверка и оценка',
  'Предварительное решение',
  'Полная анкета',
  'Окончательное решение',
  'Деньги на счёте',
];

/** Этапы и текущий шаг для заявки. twoLevel — продукт двухуровневый. */
export function stagesFor(a, twoLevel) {
  if (!twoLevel) return { stages: STAGES, idx: STAGE_OF[a.status] ?? 0 };
  const final = a.decision?.final;
  const idx = {
    draft: 0,
    cancelled: 0,
    submitted: 1,
    data_check: 1,
    scoring: 1,
    rejected: 2,
    approved: final ? 4 : 2,
    counter_offer: 2,
    full_form: 3,
    need_info: a.level === 2 ? 3 : 0,
    verification: 4,
    signing: 4,
    issued: 5,
    closed: 5,
  }[a.status];
  return { stages: STAGES_2, idx: idx ?? 0 };
}

export const IN_PIPELINE = ['submitted', 'data_check', 'scoring', 'verification'];
export const DECIDED = ['approved', 'counter_offer', 'rejected', 'need_info'];
export const FINAL = ['issued', 'closed', 'rejected', 'cancelled'];
export const LOANS = ['issued', 'closed'];

export const DOING = {
  submitted: 'Заявка принята, ставим в очередь на проверку…',
  data_check: 'Проверяем компанию в ЕГРЮЛ/ЕГРИП и кредитную историю в БКИ…',
  scoring: 'Считаем долговую нагрузку и принимаем решение…',
  verification: 'Проверяем полную анкету, обеспечение и поручителя…',
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
    case 'verification':
      return t(
        false,
        'От вас ничего не требуется',
        `Банк сам проверяет ${a.status === 'verification' ? 'полную анкету' : 'данные'} и принимает решение — обычно 10–20 секунд. Страницу можно закрыть: статус сохранится.`,
      );
    case 'full_form':
      return t(
        true,
        'Заполните полную анкету',
        'О бизнесе, обеспечении и согласие на выписки — 3–5 минут. После неё — окончательное решение.',
        ['fix', 'Открыть полную анкету'],
      );
    case 'approved':
      if (isPreliminary(a))
        return t(
          true,
          'Примите условия и заполните полную анкету',
          `Банк предварительно одобрил ${rub(d.amount)}. Осталось рассказать о бизнесе и обеспечении — 3–5 минут, затем окончательное решение.`,
          ['accept', 'Принять и продолжить'],
        );
      return t(true, 'Примите условия и подпишите договор', `Банк одобрил ${rub(d.amount)}. Проверьте платёж и переплату ниже.`, [
        'accept',
        'Принять и подписать',
      ]);
    case 'counter_offer':
      return t(
        true,
        'Сравните условия и решите',
        `Банк готов выдать кредит на других условиях — сравнение ниже.${isPreliminary(a) ? ' Если подходят — дальше полная анкета.' : ''}`,
        ['accept', 'Принять новые условия'],
      );
    case 'need_info':
      return a.level === 2
        ? t(true, 'Уточните полную анкету', d?.reasons?.[0] || 'Банку нужно уточнение по обеспечению.', ['fix', 'Исправить полную анкету'])
        : t(true, 'Исправьте данные и отправьте снова', d?.reasons?.[0] || 'Банку нужно уточнение.', ['fix', 'Исправить анкету']);
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
