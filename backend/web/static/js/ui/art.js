// Иллюстрации главной (первый экран и плитки кредитов): свои SVG в духе сайта Альфа-Банка — объёмные мягкие
// формы на светло-сером, красный бренда плюс приглушённые фиолетовый, бирюзовый и сталь.
// Один набор градиентов на все картинки — так плитки выглядят единой серией.

const PALETTE = {
  red: ['#FF6A5C', '#E3271B'],
  violet: ['#B7A4FF', '#6E55E8'],
  teal: ['#9BEFE0', '#2EBFA8'],
  steel: ['#E9EEF7', '#9DABC6'],
  ink: ['#4A5254', '#1F2426'],
  gold: ['#FFE7A8', '#D9A33E'],
  paper: ['#FFFFFF', '#DDE3EE'],
  gloss: ['rgba(255,255,255,.75)', 'rgba(255,255,255,0)'],
};

let seq = 0;

/** Оборачивает рисунок: свои id градиентов у каждого экземпляра, чтобы картинки не конфликтовали на странице. */
function svg(draw, box = '0 0 200 150', cls = 'art') {
  const p = `a${++seq}`;
  const fill = (name) => `url(#${p}-${name})`;
  const defs = Object.entries(PALETTE)
    .map(
      ([name, [from, to]]) =>
        `<linearGradient id="${p}-${name}" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient>`,
    )
    .join('');
  return `<svg class="${cls}" viewBox="${box}" aria-hidden="true" focusable="false"><defs>${defs}</defs>${draw(fill)}</svg>`;
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

const sparkle = (x, y, s, color = '#EF3124') =>
  `<path d="M${x} ${y - s}C${x + s * 0.18} ${y - s * 0.18} ${x + s * 0.18} ${y - s * 0.18} ${x + s} ${y}C${x + s * 0.18} ${y + s * 0.18} ${x + s * 0.18} ${y + s * 0.18} ${x} ${y + s}C${x - s * 0.18} ${y + s * 0.18} ${x - s * 0.18} ${y + s * 0.18} ${x - s} ${y}C${x - s * 0.18} ${y - s * 0.18} ${x - s * 0.18} ${y - s * 0.18} ${x} ${y - s}Z" fill="${color}"/>`;

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

/** Купюра: бирюзовая с рамкой и знаком рубля. */
const bill = (f, x, y, rot) => `
  <g transform="rotate(${rot} ${x + 80} ${y + 40})">
    <rect x="${x}" y="${y}" width="160" height="80" rx="10" fill="${f('teal')}"/>
    <rect x="${x + 8}" y="${y + 8}" width="144" height="64" rx="6" fill="none" stroke="#fff" stroke-opacity=".55" stroke-width="2"/>
    <circle cx="${x + 80}" cy="${y + 40}" r="20" fill="#fff" fill-opacity=".3"/>
    <text x="${x + 80}" y="${y + 48}" text-anchor="middle" font-size="24" font-weight="700" fill="#fff">₽</text>
    <rect x="${x}" y="${y}" width="160" height="34" rx="10" fill="${f('gloss')}" opacity=".45"/>
  </g>`;

/** Первый экран: пачка денег, телефон с графиком роста в приложении и стрелка вверх. */
export const heroArt = () =>
  svg(
    (f) => `
  <ellipse cx="200" cy="304" rx="160" ry="11" fill="#1F2426" opacity=".10"/>
  <!-- деньги -->
  ${bill(f, 14, 196, -14)}
  ${bill(f, 26, 214, -4)}
  ${bill(f, 40, 228, 6)}
  <!-- стрелка вверх за телефоном -->
  <path d="M300 298 V128 H270 L326 46 L382 128 H352 V298 Z" fill="${f('gold')}"/>
  <path d="M326 46 L382 128 H352 V298 H330 V128 Z" fill="#000" opacity=".1"/>
  <!-- телефон -->
  <rect x="134" y="20" width="160" height="286" rx="30" fill="${f('ink')}"/>
  <rect x="144" y="30" width="140" height="266" rx="22" fill="${f('paper')}"/>
  <rect x="194" y="38" width="40" height="8" rx="4" fill="#1F2426"/>
  <!-- экран: шапка приложения, график, кнопка -->
  <rect x="158" y="62" width="58" height="8" rx="4" fill="#1F2426" opacity=".75"/>
  <rect x="158" y="78" width="96" height="14" rx="5" fill="#1F2426" opacity=".9"/>
  ${[0, 1, 2, 3].map((i) => `<line x1="158" x2="270" y1="${118 + i * 30}" y2="${118 + i * 30}" stroke="#9DABC6" stroke-opacity=".45" stroke-dasharray="2 6"/>`).join('')}
  <path d="M160 196 C180 170 196 206 214 180 S246 120 270 132" fill="none" stroke="${f('teal')}" stroke-width="7" stroke-linecap="round"/>
  <path d="M160 214 C182 196 200 226 220 200 S250 150 270 158" fill="none" stroke="${f('red')}" stroke-width="7" stroke-linecap="round"/>
  <rect x="158" y="246" width="112" height="30" rx="10" fill="${f('red')}"/>
  <rect x="186" y="257" width="56" height="8" rx="4" fill="#fff" opacity=".9"/>
  <rect x="144" y="30" width="140" height="110" rx="22" fill="${f('gloss')}" opacity=".35"/>
  <!-- монета -->
  ${coin(f, 330, 262, 40, 'red')}
`,
    '0 0 400 320',
    'art art--hero',
  );

export const productArt = (productId) => svg(DRAW[PRODUCT_ART[productId]] || DRAW.growth);
