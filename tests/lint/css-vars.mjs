// Проверка стилей: каждая CSS-переменная, которую читают через var(--x), где-то объявлена
// (в CSS или inline-стилем из JS), и в именах классов нет слова default (ломает сборки).
// Запуск: node tests/lint/css-vars.mjs
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../../backend/web/static/', import.meta.url).pathname;
const walk = (dir) =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(join(dir, e.name)) : [join(dir, e.name)]));
const files = walk(root).filter((f) => /\.(css|js|html)$/.test(f));
const read = (f) => readFileSync(f, 'utf8');

const declared = new Set();
const used = new Map(); // переменная → файл, где встретилась
const errors = [];

for (const f of files) {
  const text = read(f);
  // объявление: «--name:» в начале правила или после «{ ; "»
  for (const m of text.matchAll(/(?:^|[\s{;"'`])(--[\w-]+)\s*:/g)) declared.add(m[1]);
  for (const m of text.matchAll(/setProperty\(\s*['"](--[\w-]+)/g)) declared.add(m[1]);
  for (const m of text.matchAll(/var\(\s*(--[\w-]+)/g)) if (!used.has(m[1])) used.set(m[1], f.replace(root, ''));
  if (f.endsWith('.css'))
    for (const m of text.matchAll(/\.[\w-]*default[\w-]*/g))
      errors.push(`${f.replace(root, '')}: класс ${m[0]} — слово default в имени класса запрещено`);
}
for (const [name, file] of used) if (!declared.has(name)) errors.push(`${file}: переменная ${name} нигде не объявлена`);

if (errors.length) {
  console.error(errors.join('\n'));
  process.exit(1);
}
console.log(`CSS-переменные: ${used.size} используются, все объявлены`);
