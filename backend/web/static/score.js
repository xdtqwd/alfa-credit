'use strict';
/* Accordion по образцу Core Components Альфа-Банка (@alfalab/core-components-accordion):
   заголовок + контрол-шеврон справа, тело раскрывается анимацией высоты 400 мс с теми же кривыми,
   отступ контента 12 px. Без React — разметка + делегирование событий, доступность через aria-expanded.
   «Как банк оценил заявку»: шкала балла с зонами и факторы с плюсами и минусами. */

// ---------- Accordion ----------
const accState = new Map(); // состояние переживает перерисовку экрана (опрос статуса)
let accSeq = 0;
function acc({ header, body, open = false, key = '', cls = '' }) {
  const id = 'acc-' + (++accSeq);
  const isOpen = key && accState.has(key) ? accState.get(key) : open;
  return `<div class="acc ${cls} ${isOpen ? 'is-open' : ''}"${key ? ` data-acc-key="${esc(key)}"` : ''}>
    <button class="acc__head" type="button" aria-expanded="${isOpen}" aria-controls="${id}">
      <span class="acc__title">${header}</span><span class="acc__ctl" aria-hidden="true">${icon('chev', 20)}</span>
    </button>
    <div class="acc__body" id="${id}" role="region"><div class="acc__inner"><div class="acc__content">${body}</div></div></div>
  </div>`;
}
document.addEventListener('click', (e) => {
  const head = e.target.closest('.acc__head');
  if (!head) return;
  const box = head.parentElement;
  const open = !box.classList.contains('is-open');
  box.classList.toggle('is-open', open);
  head.setAttribute('aria-expanded', String(open));
  if (box.dataset.accKey) accState.set(box.dataset.accKey, open);
});

// ---------- шкала балла ----------
let SCALE = { base: 600, reject_below: 500, approve_from: 650, max_dti_percent: 50 };
api('GET', '/scoring/scale').then((s) => { SCALE = s; }).catch(() => {}); // пороги модели — с сервера

function scaleBar(score) {
  const MIN = 350, MAX = 850;
  const at = (v) => Math.max(0, Math.min(100, ((v - MIN) / (MAX - MIN)) * 100));
  const r = at(SCALE.reject_below), g = at(SCALE.approve_from);
  return `<div class="sbar" role="img" aria-label="Балл ${score ?? ''}: отказ ниже ${SCALE.reject_below}, одобрение от ${SCALE.approve_from}">
      <div class="sbar__track"><i class="z-red" style="width:${r}%"></i><i class="z-amber" style="width:${g - r}%"></i><i class="z-green" style="width:${100 - g}%"></i></div>
      ${score != null ? `<span class="sbar__mark" style="left:${at(score)}%"><b>${score}</b></span>` : ''}
      <div class="sbar__legend">
        <span style="width:${r}%"><i class="d-red"></i>Отказ<br><small>ниже ${SCALE.reject_below}</small></span>
        <span style="width:${g - r}%"><i class="d-amber"></i>Другие условия<br><small>${SCALE.reject_below}–${SCALE.approve_from - 1}</small></span>
        <span style="width:${100 - g}%"><i class="d-green"></i>Одобрение<br><small>от ${SCALE.approve_from}*</small></span>
      </div>
    </div>`;
}

/** Блок в карточке решения: как сложился балл. */
function scoreBlock(a) {
  if (a.score == null || !(a.factors || []).length) return '';
  const zone = a.score >= SCALE.approve_from ? ['зона одобрения', 'green'] : a.score >= SCALE.reject_below ? ['зона других условий', 'amber'] : ['зона отказа', 'red'];
  const fs = [...a.factors].sort((x, y) => y.impact - x.impact);
  const body = `${scaleBar(a.score)}
    <p class="sbar__note">* Для одобрения нужен ещё и посильный платёж: вместе с текущими — не больше ${SCALE.max_dti_percent}% месячной выручки.</p>
    <ul class="factors">
      <li class="f-base"><span class="f-pts">${SCALE.base}</span><div><b>Стартовый балл</b><small>С него начинается оценка любой заявки.</small></div></li>
      ${fs.map((f) => `<li class="${f.impact >= 0 ? 'f-plus' : 'f-minus'}"><span class="f-pts">${f.impact > 0 ? '+' : '−'}${Math.abs(f.impact)}</span><div><b>${esc(f.name)}</b><small>${esc(f.explain)}</small></div></li>`).join('')}
      <li class="f-total"><span class="f-pts">${a.score}</span><div><b>Итоговый балл</b><small>${zone[0]}</small></div></li>
    </ul>`;
  return acc({ key: 'score-' + a.id, open: true, cls: 'acc--score',
    header: `Как банк оценил заявку <span class="sc-chip sc-chip--${zone[1]}">${a.score} баллов · ${zone[0]}</span>`, body });
}
