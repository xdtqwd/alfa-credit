// Кабинет → заявка: пока её можно редактировать — анкета, после отправки — статус и решение.
import { api } from '../core/api.js';
import { EDITABLE } from '../domain.js';
import applicationForm from './application-form.js';
import applicationStatus from './application-status.js';

export default async function application(root, { params, signal }) {
  root.className = 'page';
  const a = await api('GET', `/applications/${params.id}`);
  if (signal.aborted) return;
  if (EDITABLE.includes(a.status)) applicationForm(root, a, { signal });
  else applicationStatus(root, a, { signal });
}
