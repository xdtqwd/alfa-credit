// Кабинет → заявка: пока её можно редактировать — анкета нужного уровня (быстрая или полная),
// после отправки — статус и решение.
import { api } from '../core/api.js';
import { editLevel } from '../domain.js';
import applicationForm from './application-form.js';
import applicationFull from './application-full.js';
import applicationStatus from './application-status.js';

export default async function application(root, { params, signal }) {
  root.className = 'page';
  const a = await api('GET', `/applications/${params.id}`);
  if (signal.aborted) return;
  const level = editLevel(a);
  if (level === 1) applicationForm(root, a, { signal });
  else if (level === 2) applicationFull(root, a, { signal });
  else applicationStatus(root, a, { signal });
}
