/* Classic browser script and side-effect Node import. No source evaluation is
 * exposed to the game UI; all actions enter a fixed Lua adapter whitelist. */
(function (scope) {
  'use strict';
  const PROFILE = 'cauldron-transmutation-v1';
  const LISTS = ['bag', 'board', 'visualBoard', 'choices', 'events', 'values', 'drawn', 'fresh', 'pendingMainline', 'upgradeChoices'];
  const MAPS = ['relics', 'levels', 'rites', 'familyBoost', 'seenGroups', 'mainline', 'roundScores', 'additiveUses', 'buffSeen', 'additiveRemaining'];
  const PHASES = new Set(['ready', 'arrange', 'review', 'brewed', 'exploded', 'draft', 'tax', 'reward', 'upgrade', 'relic', 'victory', 'lost']);
  const ACTIONS = new Set(['fuel', 'additive', 'brew', 'collect', 'choose', 'skip', 'reroll', 'pay', 'reward', 'upgrade', 'skipUpgrade', 'remove', 'continue', 'finishExplosion', 'bottle', 'spin', 'resolve', 'openUpgrade']);
  const DERIVED = ['rent', 'estimate', 'nextTemperature', 'brewPreview', 'bossPlan', 'additiveRemaining'];
  const isObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);
  function fail(message, code = 'invalid_save') { const error = new Error(message); error.code = code; throw error; }
  function assert(ok, message, code) { if (!ok) fail(message, code); }
  function plain(value) {
    let count = 0;
    const seen = new Set();
    function visit(item, depth) {
      assert(++count <= 100000 && depth <= 70, '存档内容过大或嵌套过深');
      assert(item !== null, '存档包含不支持的空值');
      if (typeof item === 'string' || typeof item === 'boolean') return;
      if (typeof item === 'number') { assert(Number.isFinite(item), '存档包含无效数值'); return; }
      assert(typeof item === 'object' && (Array.isArray(item) || Object.getPrototypeOf(item) === Object.prototype || Object.getPrototypeOf(item) === null), '存档必须为纯 JSON 数据');
      assert(!seen.has(item), '存档包含循环引用'); seen.add(item);
      for (const key of Object.keys(item)) {
        assert(!['__proto__', 'prototype', 'constructor'].includes(key), '存档包含不支持的字段');
        visit(item[key], depth + 1);
      }
      seen.delete(item);
    }
    visit(value, 0);
    const text = JSON.stringify(value);
    assert(new TextEncoder().encode(text).length <= 1048576, '存档超过 1 MiB 上限');
    return JSON.parse(text);
  }
  function list(value) {
    if (Array.isArray(value)) return value;
    if (!isObject(value) || !Object.keys(value).length) return [];
    const out = [];
    for (const [key, item] of Object.entries(value)) {
      if (/^[1-9]\d*$/.test(key) && Number(key) <= 100000) out[Number(key) - 1] = item;
    }
    return out;
  }
  function map(value) {
    return Array.isArray(value) ? Object.fromEntries(value.map((item, index) => [String(index + 1), item])) : value || {};
  }
  function normalize(game) {
    for (const key of LISTS) game[key] = list(game[key]);
    for (const key of MAPS) game[key] = map(game[key]);
    game.board = Array.from({ length: 21 }, (_, i) => game.board[i] || false);
    if (game.incomeDetails) {
      game.incomeDetails.byUid = map(game.incomeDetails.byUid);
      game.incomeDetails.bySlot = map(game.incomeDetails.bySlot);
    }
    return game;
  }
  function validate(save, catalog) {
    assert(isObject(save) && save.version === 1, '存档版本不受支持，请保留原存档', 'unsupported_save');
    assert(save.profile === PROFILE && save.game?.profile === PROFILE, '此存档使用其他游戏规则，不能覆盖或强制转换', 'unsupported_save');
    assert(Object.keys(save).every(key => ['version', 'profile', 'game'].includes(key)), '存档包含不支持的顶层字段');
    const g = save.game;
    assert(isObject(g) && PHASES.has(g.phase), '存档游戏阶段无效');
    const integer = (value, min = 0, max = Number.MAX_SAFE_INTEGER) => Number.isSafeInteger(value) && value >= min && value <= max;
    for (const key of ['uid', 'round', 'paid', 'left', 'turn', 'removal', 'reroll', 'temperature', 'upgradeTokens', 'brewTally']) {
      assert(integer(g[key]) && (key !== 'round' || g[key] >= 1), `存档字段 ${key} 无效`);
    }
    assert(integer(g.rng, 1, 2147483646), '存档随机状态无效');
    assert(g.paid < g.round && g.round <= 1000 && g.left <= 1000, '存档回合状态无效');
    for (const key of ['coins', 'total', 'last', 'basePayout', 'brewBoost', 'brewRefund', 'blastChance', 'roundEarned', 'pivotBonus', 'brewRiskModifier', 'brewBoostModifier']) {
      assert(typeof g[key] === 'number' && Number.isFinite(g[key]) && g[key] >= 0 || ['brewRiskModifier'].includes(key) && typeof g[key] === 'number' && Number.isFinite(g[key]), `存档字段 ${key} 无效`);
    }
    assert(g.selectedFuel === 0 || g.selectedFuel === 1 || g.selectedFuel === 2, '存档燃料无效');
    assert(g.spinFuel === 0 || g.spinFuel === 1 || g.spinFuel === 2, '存档燃料无效');
    assert(typeof g.won === 'boolean' && typeof g.spinRaised === 'boolean' && typeof g.discoveryActive === 'boolean', '存档标记无效');
    assert(Array.isArray(g.bag) && g.bag.length <= 10000, '存档材料袋无效');
    const ids = new Map();
    for (const item of g.bag) {
      assert(isObject(item) && catalog.symbols[item.id] && integer(item.uid, 1, g.uid) && !ids.has(item.uid), '存档包含无效或重复的材料');
      for (const field of ['growth', 'age', 'bank']) assert(typeof item[field] === 'number' && Number.isFinite(item[field]) && item[field] >= 0, '存档材料属性无效');
      ids.set(item.uid, item.id);
    }
    assert(Array.isArray(g.board) && g.board.length === 21, '存档必须包含 21 个格子');
    const occupied = new Set();
    for (const item of g.board) {
      assert(item === false || isObject(item) && ids.get(item.uid) === item.id && !occupied.has(item.uid), '存档盘面引用无效');
      if (item) occupied.add(item.uid);
    }
    for (const key of ['choices', 'drawn', 'fresh', 'pendingMainline', 'upgradeChoices']) assert(Array.isArray(g[key]), `存档字段 ${key} 无效`);
    assert(g.choices.length <= 4 && g.upgradeChoices.length <= 3, '存档候选数量无效');
    const choicePool = g.phase === 'relic' ? catalog.relics : catalog.symbols;
    assert(g.choices.every(id => typeof id === 'string' && choicePool[id]), '存档候选无效');
    assert(g.upgradeChoices.every(id => typeof id === 'string' && catalog.symbols[id]), '存档升级候选无效');
    if (g.phase === 'upgrade') assert(g.upgradeTokens > 0 && g.upgradeChoices.length > 0, '存档升级状态无效');
    if (g.phase === 'draft' || g.phase === 'relic') assert(g.choices.length > 0, '存档缺少选择候选');
    for (const key of MAPS.filter(key => key !== 'additiveRemaining')) assert(isObject(g[key]), `存档字段 ${key} 必须是映射`);
    for (const [id, count] of Object.entries(g.relics)) assert(catalog.relics[id] && integer(count, 1, 1000000), '存档遗物无效');
    for (const [id, level] of Object.entries(g.levels)) assert(catalog.symbols[id] && integer(level, 1, catalog.balance.upgrade.maxLevel), '存档等级无效');
    const familyIDs = new Set(Object.values(catalog.symbols).filter(item => Number.isInteger(item.group)).map(item => String(item.group)));
    for (const key of ['rites', 'familyBoost']) for (const [id, value] of Object.entries(g[key])) assert(familyIDs.has(id) && typeof value === 'number' && Number.isFinite(value) && value >= 0, '存档流派数据无效');
    // Lua treats zero as truthy: universal cards legitimately record group 0.
    for (const [id, seen] of Object.entries(g.seenGroups)) assert(familyIDs.has(id) && typeof seen === 'boolean', '存档流派标记无效');
    for (const [round, score] of Object.entries(g.roundScores)) assert(/^[1-9]\d*$/.test(round) && typeof score === 'number' && Number.isFinite(score) && score >= 0, '存档夜间成绩无效');
    const mainlineIDs = new Set(catalog.balance.mainline.slots.map(slot => slot.id));
    for (const [id, round] of Object.entries(g.mainline)) assert(mainlineIDs.has(id) && integer(round, 1, g.round), '存档主线进度无效');
    assert(g.pendingMainline.every(id => mainlineIDs.has(id)), '存档待结算主线无效');
    assert(['spin', 'rent'].includes(g.draftContext) && ['rent', 'brew'].includes(g.relicContext), '存档选择阶段来源无效');
    for (const [id, count] of Object.entries(g.additiveUses)) assert(catalog.balance.brew.additives[id] && integer(count), '存档添加剂使用次数无效');
    assert(typeof g.selectedAdditive === 'string' && (g.selectedAdditive === '' || catalog.balance.brew.additives[g.selectedAdditive]), '存档添加剂无效');
    return save;
  }
  function freeze(value) { if (value && typeof value === 'object') { Object.freeze(value); for (const item of Object.values(value)) freeze(item); } return value; }
  async function create({ wasmURL, sources = scope.CauldronSources } = {}) {
    assert(scope.wasmoon?.LuaFactory, 'Lua 运行时尚未载入', 'runtime_missing');
    assert(isObject(sources), 'Lua 游戏源码尚未载入', 'sources_missing');
    const factory = new scope.wasmoon.LuaFactory(wasmURL);
    const lua = await factory.createEngine({ injectObjects: false, enableProxy: false, functionTimeout: 5000 });
    let closed = false;
    try {
      const version = lua.doStringSync('return _VERSION');
      const fullIntegers = lua.doStringSync('return math.maxinteger > 2147483647 and 2147483646 * 48271 % 2147483647 == 2147435376');
      assert(version === 'Lua 5.4' && fullIntegers, '此运行时无法保持原游戏的 Lua 5.4 数值规则', 'runtime_incompatible');
      for (const name of ['AlchemyBalance', 'Data', 'Game', 'BrowserBridge']) {
        const source = sources[name] ?? sources[name + '.lua'];
        assert(typeof source === 'string' && source.length > 0, `缺少 ${name}.lua`, 'sources_missing');
        lua.global.set('__module_name', name); lua.global.set('__module_source', source);
        lua.doStringSync('package.preload[__module_name] = assert(load(__module_source, "@" .. __module_name .. ".lua"))');
      }
      lua.doStringSync('local bridge = require "BrowserBridge"; function __cauldron_call(op, arg) return bridge[op](arg) end; __module_name=nil; __module_source=nil');
      const bridge = lua.global.get('__cauldron_call');
      // dispatch accepts a separate action and argument; no dynamic method name
      // or code from the iframe is evaluated by this entry point.
      lua.doStringSync('local bridge = require "BrowserBridge"; function __cauldron_dispatch(action, arg) return bridge.dispatch(action, arg) end');
      const dispatchLua = lua.global.get('__cauldron_dispatch');
      function run(fn) {
        assert(!closed, '游戏引擎已关闭', 'closed');
        try { return fn(); }
        catch (error) {
          const message = String(error.message || error).match(/CAULDRON:([^\r\n]*)/);
          if (message) fail(message[1], 'action_unavailable');
          throw error;
        }
      }
      const catalog = JSON.parse(JSON.stringify(bridge('catalog')));
      catalog.order = list(catalog.order); catalog.relicOrder = list(catalog.relicOrder); catalog.rarity = list(catalog.rarity);
      if (catalog.balance.rite?.familyGrowthPower) catalog.balance.rite.familyGrowthPower = map(catalog.balance.rite.familyGrowthPower);
      freeze(catalog);
      const takeSnapshot = value => normalize(JSON.parse(JSON.stringify(value)));
      return {
        catalog,
        newGame(seed) {
          if (seed === undefined) { const values = new Uint32Array(1); scope.crypto.getRandomValues(values); seed = values[0] % 2147483646 + 1; }
          assert(Number.isSafeInteger(seed) && seed >= 1 && seed <= 2147483646, '随机种子必须是有效正整数', 'invalid_seed');
          return run(() => takeSnapshot(bridge('newGame', seed)));
        },
        load(input) {
          let save = input;
          if (typeof save === 'string') { try { save = JSON.parse(save); } catch { fail('存档 JSON 已损坏，请保留原存档'); } }
          save = validate(plain(save), catalog);
          return run(() => takeSnapshot(bridge('load', save)));
        },
        snapshot() { return run(() => takeSnapshot(bridge('snapshot'))); },
        exportSave() {
          return run(() => {
            const save = JSON.parse(JSON.stringify(bridge('exportSave')));
            normalize(save.game);
            for (const key of DERIVED) delete save.game[key];
            delete save.game.incomeDetails;
            // No presentation queue, duplicated board art or per-card income
            // ledger enters the cloud slot; required gameplay state remains.
            delete save.game.visualBoard; delete save.game.events; delete save.game.values;
            return validate(plain(save), catalog);
          });
        },
        dispatch(action, arg) {
          assert(ACTIONS.has(action), '不支持此游戏操作', 'action_unavailable');
          if (['fuel', 'choose', 'remove'].includes(action)) assert(Number.isSafeInteger(arg), '操作参数必须为整数', 'invalid_action');
          if (action === 'fuel') assert([0, 1, 2].includes(arg), '请选择 0、1 或 2 根柴', 'invalid_action');
          if (['additive', 'upgrade'].includes(action)) assert(typeof arg === 'string' && arg.length <= 64, '操作参数无效', 'invalid_action');
          if (action === 'brew' || action === 'collect') assert(arg === undefined || isObject(arg) && Object.keys(arg).every(key => key === 'finishExplosion') && (arg.finishExplosion === undefined || typeof arg.finishExplosion === 'boolean'), '熬制参数无效', 'invalid_action');
          return run(() => takeSnapshot(dispatchLua(action, arg)));
        },
        close() { if (!closed) { closed = true; lua.global.close(); } },
      };
    } catch (error) { lua.global.close(); throw error; }
  }
  scope.CauldronEngine = Object.freeze({ create, profile: PROFILE, saveVersion: 1 });
})(globalThis);
