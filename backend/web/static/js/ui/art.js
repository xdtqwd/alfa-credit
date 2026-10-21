// Иллюстрации для плиток кредитов на главной: свои SVG в духе сайта Альфа-Банка — объёмные мягкие
// формы на светло-сером, красный бренда плюс приглушённые фиолетовый, бирюзовый и сталь.
// Один набор градиентов на все картинки — так плитки выглядят единой серией.

const PALETTE = {
  red: ['#FF6A5C', '#E3271B'],
  violet: ['#B7A4FF', '#6E55E8'],
  teal: ['#9BEFE0', '#2EBFA8'],
  steel: ['#E9EEF7', '#9DABC6'],
  ink: ['#4A5254', '#1F2426'],
  gloss: ['rgba(255,255,255,.75)', 'rgba(255,255,255,0)'],
};

let seq = 0;

/** Оборачивает рисунок: свои id градиентов у каждого экземпляра, чтобы картинки не конфликтовали на странице. */
function svg(draw) {
  const p = `a${++seq}`;
  const fill = (name) => `url(#${p}-${name})`;
  const defs = Object.entries(PALETTE).map(([name, [from, to]]) =>
    `<linearGradient id="${p}-${name}" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient>`).join('');
  return `<svg class="art" viewBox="0 0 200 150" aria-hidden="true" focusable="false"><defs>${defs}</defs>${draw(fill)}</svg>`;
}

// ---------- детали ----------
const coin = (f, x, y, r, color = 'violet') => `
  <ellipse cx="${x + r * 0.12}" cy="${y + r * 0.1}" rx="${r}" ry="${r}" fill="${f(color)}" opacity=".55"/>
  <circle cx="${x}" cy="${y}" r="${r}" fill="${f(color)}"/>
  <circle cx="${x}" cy="${y}" r="${r * 0.78}" fill="none" stroke="#fff" stroke-opacity=".35" stroke-width="2"/>
  <text x="${x}" y="${y + r * 0.36}" text-anchor="middle" font-size="${r * 1.05}" font-weight="700" fill="#fff" fill-opacity=".92">₽</text>
  <ellipse cx="${x - r * 0.35}" cy="${y - r * 0.45}" rx="${r * 0.35}" ry="${r * 0.18}" fill="${f('gloss')}" transform="rotate(-30 ${x - r * 0.35} ${y - r * 0.45})"/>`;

const card = (f, x, y, w, h, color, rot = 0) => `
  <g transform="rotate(${rot} ${x + w / 2} ${y + h / 2})">
    <rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${Math.min(w, h) * 0.12}" fill="${f(color)}"/>
    <rect x="${x}" y="${y}" width="${w}" height="${h * 0.45}" rx="${Math.min(w, h) * 0.12}" fill="${f('gloss')}" opacity=".5"/>
    <rect x="${x + w * 0.18}" y="${y + h * 0.7}" width="${w * 0.34}" height="${h * 0.07}" rx="${h * 0.035}" fill="#fff" fill-opacity=".9"/>
  </g>`;

const sparkle = (x, y, s, color = '#EF3124') => `<path d="M${x} ${y - s}C${x + s * 0.18} ${y - s * 0.18} ${x + s * 0.18} ${y - s * 0.18} ${x + s} ${y}C${x + s * 0.18} ${y + s * 0.18} ${x + s * 0.18} ${y + s * 0.18} ${x} ${y + s}C${x - s * 0.18} ${y + s * 0.18} ${x - s * 0.18} ${y + s * 0.18} ${x - s} ${y}C${x - s * 0.18} ${y - s * 0.18} ${x - s * 0.18} ${y - s * 0.18} ${x} ${y - s}Z" fill="${color}"/>`;

// ---------- рисунки ----------
const DRAW = {
  // кредит на развитие: карта и растущая стрелка
  growth: (f) => `
    ${card(f, 34, 34, 74, 100, 'red', -8)}
    <path d="M58 118 L96 86 L116 100 L160 52" fill="none" stroke="${f('teal')}" stroke-width="14" stroke-linecap="round" stroke-linejoin="round"/>
    <path d="M140 46 L168 42 L164 70 Z" fill="#2EBFA8" stroke="#2EBFA8" stroke-width="6" stroke-linejoin="round"/>
    ${coin(f, 150, 112, 20)}`,

  // оборотный кредит: деньги в обороте
  cycle: (f) => `
    <path d="M58 50 A48 48 0 0 1 150 62" fill="none" stroke="${f('red')}" stroke-width="12" stroke-linecap="round"/>
    <path d="M142 46 L160 66 L136 72 Z" fill="#E3271B" stroke="#E3271B" stroke-width="5" stroke-linejoin="round"/>
    <path d="M144 104 A48 48 0 0 1 52 92" fill="none" stroke="${f('teal')}" stroke-width="12" stroke-linecap="round"/>
    <path d="M60 108 L42 88 L66 82 Z" fill="#2EBFA8" stroke="#2EBFA8" stroke-width="5" stroke-linejoin="round"/>
    ${coin(f, 92, 82, 24, 'steel')}
    ${coin(f, 114, 72, 24)}`,

  // оборудование и транспорт: фургон
  van: (f) => `
    <ellipse cx="102" cy="128" rx="74" ry="8" fill="#1F2426" opacity=".08"/>
    <rect x="28" y="44" width="96" height="70" rx="12" fill="${f('violet')}"/>
    <rect x="28" y="44" width="96" height="30" rx="12" fill="${f('gloss')}" opacity=".45"/>
    <path d="M124 66 h22 a10 10 0 0 1 8 4 l16 20 a10 10 0 0 1 2 6 v10 a8 8 0 0 1 -8 8 h-40 Z" fill="${f('red')}"/>
    <path d="M132 74 h14 l12 16 h-26 Z" fill="#fff" fill-opacity=".85"/>
    <rect x="42" y="88" width="30" height="6" rx="3" fill="#fff" fill-opacity=".8"/>
    <circle cx="58" cy="116" r="15" fill="${f('ink')}"/><circle cx="58" cy="116" r="6" fill="${f('steel')}"/>
    <circle cx="146" cy="116" r="15" fill="${f('ink')}"/><circle cx="146" cy="116" r="6" fill="${f('steel')}"/>
    ${sparkle(170, 36, 13)}`,

  // овердрафт: переключатель «деньги есть всегда»
  toggle: (f) => `
    ${coin(f, 84, 56, 30)}
    <rect x="44" y="84" width="124" height="48" rx="24" fill="${f('teal')}"/>
    <rect x="44" y="84" width="124" height="22" rx="11" fill="${f('gloss')}" opacity=".5"/>
    <circle cx="144" cy="108" r="19" fill="${f('red')}"/>
    <circle cx="144" cy="108" r="11" fill="none" stroke="#fff" stroke-opacity=".35" stroke-width="2"/>
    <circle cx="144" cy="108" r="5" fill="none" stroke="#fff" stroke-opacity=".35" stroke-width="2"/>`,
};

/** Картинка кредита по id продукта. */
const PRODUCT_ART = { 'business-dev': 'growth', 'working-capital': 'cycle', equipment: 'van', overdraft: 'toggle' };

export const productArt = (productId) => svg(DRAW[PRODUCT_ART[productId]] || DRAW.growth);
