import {copyFileSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve, dirname, relative} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';

// Explicit, read-only import from the user's Maker source. Builds never require
// that external project: all selected art is retained in this repository.
if (!process.argv[2]) throw new Error('Usage: node scripts/import-cauldron-assets.mjs <Maker project directory>');
const source = resolve(process.argv[2]);
const target = fileURLToPath(new URL('../player/web/public/games/witch-cauldron/assets/', import.meta.url));
const theme = readFileSync(resolve(source, 'scripts/UITheme.lua'), 'utf8');
const main = readFileSync(resolve(source, 'scripts/main.lua'), 'utf8');
const manifest = {symbols: {}, relics: {}, theme: {}, audio: {}};
const provenance = [];
function copy(group, key, input, output) {
  const inputPath = resolve(source, 'assets', input);
  const inside = relative(resolve(source, 'assets'), inputPath);
  if (inside.startsWith('..') || inside.includes(':')) throw new Error('Source asset escaped the project');
  const out = resolve(target, output);
  mkdirSync(dirname(out), {recursive: true});
  copyFileSync(inputPath, out);
  manifest[group][key] = `assets/${output.replaceAll('\\', '/')}`;
  provenance.push({source: `assets/${input}`, destination: manifest[group][key], sha256: createHash('sha256').update(readFileSync(out)).digest('hex')});
}
function parseMap(text, start) {
  const section = text.slice(text.indexOf(start)).match(/\{([\s\S]*?)\}/)?.[1];
  if (!section) throw new Error(`Missing art map ${start}`);
  return Object.fromEntries([...section.matchAll(/\b([a-z][a-z0-9_]*)\s*=\s*"([^"]+)"/g)].map(m => [m[1], m[2]]));
}
for (const [id, path] of Object.entries({...parseMap(theme, 'Theme.symbolImages'), ...parseMap(main, 'local newIcons')})) copy('symbols', id, path, `symbols/${id}.png`);
for (const [id, path] of Object.entries(parseMap(theme, 'Theme.relicImages'))) copy('relics', id, path, `relics/${id}.png`);
for (const [key, path] of Object.entries({
  background: 'image/handpaint_v5/workshop_bg_detailed.png', cauldron: 'image/handpaint_v5/cauldron_frame.png',
  liquid: 'image/handpaint_v5/potion_liquid.png', workbench: 'image/handpaint_v5/brewing_workbench.png',
  scroll: 'image/handpaint_v5/contract_scroll.png', 'brew-button': 'image/handpaint_v5/brew_button.png',
  cat: 'image/handpaint_v5/cat_familiar.png', title: 'image/title-screen.png',
  reaper: 'image/reaper_bill_v1.png', victory: 'image/victory_witch_v1.png',
})) copy('theme', key, path, `${key}.png`);
for (const [key, path] of Object.entries({bgm: 'audio/魔女的暴富坩埚_20261029072105.mp3', spin: 'audio/sfx/spin.mp3', coin: 'audio/sfx/coin.mp3', burst: 'audio/sfx/cartoon_cauldron_burst.mp3'})) copy('audio', key, path, `audio/${key}.mp3`);
writeFileSync(resolve(target, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
writeFileSync(resolve(target, 'manifest.js'), 'globalThis.CauldronAssets = ' + JSON.stringify(manifest) + ';\n');
writeFileSync(resolve(target, 'provenance.json'), JSON.stringify({source_project: '女巫的坩埚', source_paths_relative_to_project: true, assets: provenance}, null, 2) + '\n');
console.log(`Imported ${provenance.length} original art assets; run "python scripts/optimize-cauldron-assets.py" to compress to WebP.`);
