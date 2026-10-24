// Форматирование для интерфейса. Деньги здесь только показываются — считает их сервер (domain.Quote).

const nf = new Intl.NumberFormat('ru-RU');

export const fmt = (n) => nf.format(Math.round(n || 0));
export const rub = (n) => `${fmt(n)} ₽`;
export const rate = (r) => `${String(r).replace('.', ',')}%`;
export const digits = (s) => String(s ?? '').replace(/\D/g, '');

/** Склонение по числу: plural(5, 'год', 'года', 'лет') → «лет». */
const plural = (n, one, few, many) => {
  const m = n % 10;
  const h = n % 100;
  return m === 1 && h !== 11 ? one : m >= 2 && m <= 4 && (h < 10 || h >= 20) ? few : many;
};

export const months = (n) => `${n} ${plural(n, 'месяц', 'месяца', 'месяцев')}`;
const short1 = (x) => String(+x.toFixed(1)).replace('.', ',');

/** Подписи шкалы ползунка: «300 тыс ₽», «15,2 млн ₽». */
export const shortRub = (n) => (n >= 1e6 ? `${short1(n / 1e6)} млн ₽` : n >= 1e3 ? `${short1(n / 1e3)} тыс ₽` : rub(n));
/** Подписи шкалы срока: ровные годы — годами («5 лет»), иначе месяцами. */
export const shortTerm = (n) => (n % 12 === 0 ? `${n / 12} ${plural(n / 12, 'год', 'года', 'лет')}` : months(n));

export function phone(raw) {
  let d = digits(raw);
  if (d.startsWith('8')) d = `7${d.slice(1)}`;
  if (d && !d.startsWith('7')) d = `7${d}`;
  d = d.slice(0, 11);
  if (!d) return '';
  const [a, b, c, e] = [d.slice(1, 4), d.slice(4, 7), d.slice(7, 9), d.slice(9, 11)];
  return `+7${a ? ` (${a}` : ''}${a.length === 3 ? ')' : ''}${b ? ` ${b}` : ''}${c ? `-${c}` : ''}${e ? `-${e}` : ''}`;
}

export const initials = (name) =>
  (name || '')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0])
    .join('')
    .toUpperCase();
export const maskAccount = (acc) => `•• ${String(acc).slice(-4)}`;

export const date = (iso) => new Date(iso).toLocaleDateString('ru-RU');
export const dateTime = (iso) =>
  new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
export const time = (iso) => new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });

/** Экранирование для вставки пользовательских данных в разметку. */
export const esc = (s) =>
  String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
