// Кабинет → заявка: пока её можно править — 2-й уровень анкеты (документы), после отправки — статус и решение.
import { api } from '../core/api.js';
import { editable } from '../domain.js';
import applicationFull from './application-full.js';
import applicationStatus from './application-status.js';

export default async function application(root, { params, signal }) {
  root.className = 'page';
  const a = await api('GET', `/applications/${params.id}`);
  if (signal.aborted) return;
  if (editable(a)) applicationFull(root, a, { signal });
  else applicationStatus(root, a, { signal });
}
