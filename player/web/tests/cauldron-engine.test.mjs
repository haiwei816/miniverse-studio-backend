import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import wasmoon from 'wasmoon';
import '../public/games/witch-cauldron/engine.js';

globalThis.wasmoon = wasmoon;
const core = new URL('../public/games/witch-cauldron/core/', import.meta.url);
const sources = Object.fromEntries(await Promise.all(['Game', 'Data', 'AlchemyBalance', 'BrowserBridge'].map(async name => [name, await readFile(new URL(name + '.lua', core), 'utf8')])));
async function create(t, seed = 12345) {
  const engine = await CauldronEngine.create({ sources });
  t.after(() => engine.close());
  engine.newGame(seed);
  return engine;
}

test('Lua 5.4 preserves the original seeded shuffle and full gameplay transition', async t => {
  const engine = await create(t);
  const initial = engine.snapshot();
  assert.equal(initial.phase, 'ready');
  assert.equal(initial.board.length, 21);
  assert.equal(initial.bag.length, 6);
  assert.equal(initial.rent, 100);
  assert.equal(engine.catalog.balance.id, 'cauldron-transmutation-v1');
  assert.ok(engine.catalog.symbols.rose.name);
  assert.ok(Array.isArray(engine.catalog.order));
  const first = engine.dispatch('brew', { finishExplosion: true });
  assert.equal(first.phase, 'draft');
  assert.equal(first.left, initial.left - 1);
  assert.equal(first.turn, initial.turn + 1);
  assert.ok(first.choices.length >= 3);
  const replay = await create(t);
  assert.deepEqual(replay.dispatch('brew', { finishExplosion: true }), first);
  const next = engine.dispatch('choose', 1);
  assert.equal(next.phase, 'ready');
  assert.equal(next.bag.length, first.bag.length + 1);
});

test('save reload preserves RNG, candidate choices, and following actions', async t => {
  const engine = await create(t);
  engine.dispatch('fuel', 2);
  engine.dispatch('brew', { finishExplosion: true });
  const save = engine.exportSave();
  assert.equal(save.version, 1);
  assert.equal(save.game.incomeDetails, undefined);
  assert.equal(save.game.events, undefined);
  assert.equal(save.game.visualBoard, undefined);
  assert.ok(save.game.bag.every(item => !Object.hasOwn(item, 'lastIncome')));
  const loaded = await create(t, 91);
  loaded.load(JSON.stringify(save));
  assert.deepEqual(loaded.exportSave(), save);
  engine.dispatch('choose', 1); loaded.dispatch('choose', 1);
  assert.deepEqual(loaded.exportSave(), engine.exportSave());
  engine.dispatch('brew', { finishExplosion: true }); loaded.dispatch('brew', { finishExplosion: true });
  assert.deepEqual(loaded.exportSave(), engine.exportSave());
});

test('bad or legacy saves and unavailable actions never replace a running game', async t => {
  const engine = await create(t);
  const before = engine.exportSave();
  for (const mutate of [
    save => { save.version = 4; },
    save => { save.profile = save.game.profile = 'cauldron-heat-brew-v3'; },
    save => { save.game.rng = 0; },
    save => { save.game.bag[0].id = 'missing'; },
    save => { save.game.board[0] = { id: 'rose', uid: 99999 }; },
    save => { save.game.round = '1'; },
    save => { save.game.choices = ['missing']; },
  ]) {
    const save = structuredClone(before); mutate(save);
    assert.throws(() => engine.load(save));
    assert.deepEqual(engine.exportSave(), before);
  }
  assert.throws(() => engine.load('{broken json'));
  assert.throws(() => engine.dispatch('RewardRemoval'));
  assert.throws(() => engine.dispatch('additive', 'frost_dew'));
  assert.throws(() => engine.dispatch('fuel', 3));
  assert.throws(() => engine.dispatch('choose', 1));
  assert.deepEqual(engine.exportSave(), before);
  const snapshot = engine.snapshot(); snapshot.coins = 999999;
  assert.equal(engine.snapshot().coins, before.game.coins);
});

test('the last draw reaches tax or loss and follows the original rent reward loop', async t => {
  const engine = await create(t);
  for (let i = 0; i < 5; i++) {
    const draw = engine.dispatch('collect');
    assert.equal(draw.phase, 'draft');
    engine.dispatch('choose', 1);
  }
  const final = engine.snapshot();
  assert.equal(final.left, 0);
  assert.ok(['lost', 'tax'].includes(final.phase));
  if (final.phase === 'tax') {
    assert.equal(engine.dispatch('pay').phase, 'reward');
    assert.equal(engine.dispatch('reward').phase, 'relic');
    const next = engine.dispatch('choose', 1);
    assert.equal(next.phase, 'ready'); assert.equal(next.round, 2); assert.equal(next.paid, 1);
  }
});

test('stored numeric family maps retain Lua numeric keys and integer RNG semantics', async t => {
  const engine = await create(t, 2147483646);
  const save = engine.exportSave();
  save.game.rites = { '1': 0.25, '3': 0.5 };
  save.game.familyBoost = { '2': 0.12 };
  save.game.seenGroups = { '1': true, '2': true, '4': true };
  const restored = engine.load(save);
  assert.deepEqual(restored.rites, save.game.rites);
  assert.deepEqual(restored.familyBoost, save.game.familyBoost);
  assert.deepEqual(restored.seenGroups, save.game.seenGroups);
  const future = engine.dispatch('spin');
  assert.ok(future.rng > 0 && future.rng < 2147483647);
});

test('closed engines reject further operations', async t => {
  const engine = await create(t);
  engine.close(); engine.close();
  assert.throws(() => engine.snapshot(), /已关闭/);
});

test('the shipped core matches recorded original-file SHA-256 hashes', async () => {
  const manifest = JSON.parse(await readFile(new URL('SOURCE-SHA256.json', core), 'utf8'));
  for (const [name, expected] of Object.entries(manifest.files)) {
    const bytes = await readFile(new URL(name, core));
    assert.equal(createHash('sha256').update(bytes).digest('hex'), expected, name + ' was changed');
  }
});

test('original transmutation, mainline, boss and income suites pass in the shipped Lua 5.4 VM', async () => {
  const lua = await new wasmoon.LuaFactory().createEngine({ injectObjects: false, enableProxy: false, functionTimeout: 10000 });
  const fixtures = new URL('fixtures/witch-cauldron/', import.meta.url);
  try {
    for (const name of ['Game', 'Data', 'AlchemyBalance', 'IncomeAnimation', 'legacy_heat_v3.Game', 'legacy_heat_v3.Data', 'legacy_heat_v3.AlchemyBalance']) {
      const source = sources[name] || await readFile(new URL(name.replaceAll('.', '/') + '.lua', fixtures), 'utf8');
      lua.global.set('__name', name); lua.global.set('__src', source);
      lua.doStringSync('package.preload[__name] = assert(load(__src, "@" .. __name))');
    }
    for (const spec of ['transmutation_spec', 'mainline_spec', 'boss_spec', 'income_details_spec']) {
      lua.doStringSync(await readFile(new URL(spec + '.lua', fixtures), 'utf8'));
    }
  } finally { lua.global.close(); }
});

test('upgrade reload retains its already-drawn candidates and RNG', async t => {
  const engine = await create(t);
  const save = engine.exportSave();
  save.game.phase = 'upgrade'; save.game.upgradeTokens = 2;
  save.game.upgradeChoices = ['rose', 'cat', 'amethyst'];
  engine.load(save);
  assert.equal(engine.snapshot().rng, save.game.rng);
  assert.deepEqual(engine.snapshot().upgradeChoices, save.game.upgradeChoices);
  const exported = engine.exportSave();
  engine.load(exported);
  assert.deepEqual(engine.exportSave(), exported);
  const level = engine.dispatch('upgrade', 'rose').levels.rose;
  assert.equal(level, 2);
});

test('compact cloud saves stay below the existing 64 KiB bridge limit after 60 pots', async t => {
  const engine = await create(t, 7919);
  const rich = engine.exportSave(); rich.game.coins = 1000000000000;
  engine.load(rich);
  const bytes = value => new TextEncoder().encode(JSON.stringify(value)).length;
  const start = bytes(engine.exportSave());
  let pots = 0, maxSave = start, maxRequest = 0, maxSnapshot = 0;
  for (let step = 0; step < 500 && pots < 60; step++) {
    let state = engine.snapshot();
    if (state.phase === 'ready') { state = engine.dispatch('brew', { finishExplosion: true }); pots++; }
    else if (state.phase === 'draft') engine.dispatch(state.bag.length >= 24 ? 'skip' : 'choose', 1);
    else if (state.phase === 'tax') engine.dispatch('pay');
    else if (state.phase === 'reward') engine.dispatch('reward');
    else if (state.phase === 'upgrade') engine.dispatch('upgrade', state.upgradeChoices[0]);
    else if (state.phase === 'relic') engine.dispatch('choose', 1);
    else if (state.phase === 'exploded') engine.dispatch('finishExplosion');
    else break;
    const save = engine.exportSave();
    maxSnapshot = Math.max(maxSnapshot, bytes(engine.snapshot()));
    maxSave = Math.max(maxSave, bytes(save));
    maxRequest = Math.max(maxRequest, bytes({ revision: 1, schema_version: 1, data: JSON.stringify(save), checksum: 'a'.repeat(64), encoding: 'identity' }));
    assert.ok(maxRequest < 65536, `save request exceeded bridge limit: ${maxRequest}`);
  }
  assert.equal(pots, 60);
  t.diagnostic(`UTF-8 bytes: initial save=${start}; 60-pot max save=${maxSave}, max PUT body=${maxRequest}, max full snapshot=${maxSnapshot}`);
});

test('natural progression including universal group zero remains saveable and reloadable', async t => {
  const engine = await create(t, 7919);
  const mirror = await create(t, 1);
  let encounteredZero = false;
  for (let step = 0; step < 150; step++) {
    const state = engine.snapshot();
    if (state.phase === 'ready') engine.dispatch('brew', { finishExplosion: true });
    else if (state.phase === 'draft' || state.phase === 'relic') engine.dispatch('choose', (1 + step) % state.choices.length + 1);
    else if (state.phase === 'tax') engine.dispatch('pay');
    else if (state.phase === 'reward') engine.dispatch('reward');
    else if (state.phase === 'upgrade') engine.dispatch('upgrade', state.upgradeChoices[(1 + step) % state.upgradeChoices.length]);
    else break;
    const save = engine.exportSave();
    mirror.load(save);
    assert.deepEqual(mirror.exportSave(), save);
    encounteredZero ||= save.game.seenGroups['0'] === true;
  }
  assert.equal(encounteredZero, true, 'fixture must obtain a real universal card');
});

test('explosion results survive reload and settle once without rerolling', async t => {
  const engine = await create(t);
  const hot = engine.exportSave(); hot.game.temperature = 100;
  engine.load(hot);
  const exploded = engine.dispatch('brew');
  assert.equal(exploded.phase, 'exploded');
  assert.equal(exploded.blastChance, 100);
  const save = engine.exportSave(), mirror = await create(t, 2);
  mirror.load(save);
  assert.deepEqual(mirror.exportSave(), save);
  engine.dispatch('finishExplosion'); mirror.dispatch('finishExplosion');
  assert.deepEqual(mirror.exportSave(), engine.exportSave());
  assert.equal(engine.snapshot().phase, 'draft');
  const settled = engine.exportSave();
  assert.throws(() => engine.dispatch('finishExplosion'));
  assert.deepEqual(engine.exportSave(), settled);
});
