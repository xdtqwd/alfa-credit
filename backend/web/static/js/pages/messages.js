// Кабинет → «Сообщения»: решения банка и изменения по заявкам.
import { esc, dateTime } from '../core/format.js';
import { session, lastSeen, markMessagesSeen } from '../core/session.js';
import { STATUS, appPath, bankEvents } from '../domain.js';
import { pageHead, empty } from './parts.js';

export default async function messages(root) {
  root.className = 'page';
  const seen = lastSeen();
  const items = bankEvents(session.apps);
  root.innerHTML = `<div class="container">
    ${pageHead('Сообщения', 'Решения банка и изменения по вашим заявкам')}
    <section class="card">
      ${
        items.length
          ? items
              .map(
                (e) => `
        <a class="message ${new Date(e.at).getTime() > seen ? 'is-unread' : ''}" href="#${appPath(e.app)}">
          <span class="message__dot"></span>
          <span class="message__body"><b>${STATUS[e.status][0]} · ${esc(e.app.number)}</b><span>${esc(e.comment)}</span></span>
          <time>${dateTime(e.at)}</time>
        </a>`,
              )
              .join('')
          : empty('Сообщений пока нет', 'Здесь появятся решения банка и изменения по заявкам.')
      }
    </section>
  </div>`;
  markMessagesSeen();
}
