import {copyFileSync, existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

const web = fileURLToPath(new URL('../player/web/', import.meta.url));
const game = resolve(web, 'public/games/witch-cauldron');
for (const path of ['index.html', 'game.js', 'style.css', 'engine.js', 'assets/manifest.js']) {
  if (!existsSync(resolve(game, path))) throw new Error(`Missing Cauldron entry asset ${path}`);
}
const core = resolve(game, 'core');
const sources = {};
for (const name of readdirSync(core).filter(name => name.endsWith('.lua')).sort()) {
  sources[name.slice(0, -4)] = readFileSync(resolve(core, name), 'utf8');
}
for (const name of ['Game', 'Data', 'AlchemyBalance', 'BrowserBridge']) {
  if (!sources[name]) throw new Error(`Missing Cauldron module ${name}`);
}
writeFileSync(resolve(game, 'core-sources.js'), 'globalThis.CauldronSources = ' + JSON.stringify(sources) + ';\n');
const runtime = resolve(game, 'runtime');
mkdirSync(runtime, {recursive: true});
const packageRoot = resolve(web, 'node_modules/wasmoon');
copyFileSync(resolve(packageRoot, 'dist/index.js'), resolve(runtime, 'wasmoon.js'));
copyFileSync(resolve(packageRoot, 'dist/glue.wasm'), resolve(runtime, 'glue.wasm'));
copyFileSync(resolve(packageRoot, 'LICENSE'), resolve(runtime, 'LICENSE.txt'));
console.log(`Cauldron: bundled ${Object.keys(sources).length} Lua modules and local Wasmoon runtime.`);
