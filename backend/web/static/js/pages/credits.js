// Кабинет → «Кредиты»: выданные кредиты, остаток, оплата платежа.
import { on } from '../core/dom.js';
import { esc, rub, rate, date } from '../core/format.js';
import { session, product, subscribe } from '../core/session.js';
import { appPath, LOANS } from '../domain.js';
import { icon } from '../ui/icons.js';
import { payDialog, startApplication } from '../features/loan.js';
import { pageHead, empty } from './parts.js';

function loanHtml(a) {
  const paid = (a.payments || []).length;
  const rest = paid ? (a.schedule?.[paid - 1]?.rest ?? 0) : a.amount;
  const issuedAt = a.history.find((h) => h.status === 'issued')?.at || a.updated_at;
  const open = a.status === 'issued';
  return `
    <div class="loan" data-loan="${esc(a.number)}">
      <div class="row row--between row--wrap">
        <div><b>${esc(product(a.product_id)?.name)}</b> <span class="muted small">· договор ${esc(a.number)} от ${date(issuedAt)}</span></div>
        ${open ? '<span class="status status--ok">Действует</span>' : '<span class="status">Погашен</span>'}
      </div>
      <dl class="offer">
        <div><dt>Остаток долга</dt><dd>${rub(rest)}</dd></div>
        <div><dt>Платёж в месяц</dt><dd>${rub(a.decision.monthly_payment)}</dd></div>
        <div><dt>Ставка</dt><dd>${rate(a.decision.rate)}</dd></div>
      </dl>
      <div class="stack">
        <div class="row row--between small"><span class="muted">Оплачено ${paid} из ${a.term_months}</span><a class="btn--link" href="#${appPath(a)}">График и договор</a></div>
        <div class="bar bar--ok"><i style="width:${(paid / a.term_months) * 100}%"></i></div>
      </div>
      ${open ? `<div><button class="btn btn--primary btn--sm" data-pay="${a.id}">${icon('wallet', 20)} Оплатить ${rub(a.next_payment)}</button></div>` : ''}
    </div>`;
}

export default async function credits(root, { signal }) {
  root.className = 'page';
  const draw = () => {
    const loans = session.apps.filter((a) => LOANS.includes(a.status));
    root.innerHTML = `<div class="container">
      ${pageHead('Кредиты', 'Платёж списывается с расчётного счёта — оплатить можно в любой момент')}
      <section class="card">
        ${
          loans.length
            ? loans.map(loanHtml).join('')
            : empty(
                'Выданных кредитов пока нет',
                'Когда вы подпишете договор, кредит появится здесь вместе с графиком и кнопкой оплаты.',
                '<button class="btn btn--primary" data-new-loan>Оформить кредит</button>',
              )
        }
      </section>
    </div>`;
  };
  draw();
  const unsubscribe = subscribe(draw);
  signal.addEventListener('abort', unsubscribe, { once: true });
  on(
    root,
    'click',
    '[data-pay]',
    (_, el) => {
      const a = session.apps.find((x) => x.id === el.dataset.pay);
      if (a) payDialog(a);
    },
    signal,
  );
  on(root, 'click', '[data-new-loan]', () => startApplication(), signal);
}
