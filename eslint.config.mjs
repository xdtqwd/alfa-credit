// ESLint для фронтенда (backend/web/static/js) и браузерных тестов (tests/e2e). Запуск: tests/lint.
const browser = Object.fromEntries(['window', 'document', 'location', 'history', 'navigator', 'fetch', 'localStorage', 'sessionStorage',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'requestAnimationFrame', 'getComputedStyle', 'HashChangeEvent',
  'AbortController', 'crypto', 'Intl', 'URL', 'console', 'performance', 'HTMLElement', 'Event'].map((g) => [g, 'readonly']));
const node = { process: 'readonly', console: 'readonly' };

const rules = {
  'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
  'no-undef': 'error',
  'no-shadow': 'error',
  'no-unreachable': 'error',
  'no-dupe-keys': 'error',
  'no-empty': ['error', { allowEmptyCatch: true }],
  eqeqeq: ['error', 'smart'],
  'prefer-const': 'error',
  'no-var': 'error',
};

export default [
  { files: ['backend/web/static/js/**/*.js'], languageOptions: { ecmaVersion: 2023, sourceType: 'module', globals: browser }, rules },
  { files: ['tests/**/*.mjs'], languageOptions: { ecmaVersion: 2023, sourceType: 'module', globals: { ...browser, ...node } }, rules },
];
