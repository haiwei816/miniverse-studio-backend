(() => {
  'use strict';
  const sdk = window.MiniVerseGame;
  const assets = window.CauldronAssets || { symbols: {}, relics: {}, theme: {} };
  const $ = id => document.getElementById(id);
  const asList = value => Array.isArray(value) ? value : Object.values(value || {});
  const number = value => Math.floor(Number(value) || 0).toLocaleString('zh-CN');
  const modal = $('modal');
  let engine, catalog, state, busy = false, cloudReady = false, started = false, hasSave = false;
  let dirty = false, conflict = false, modalRequired = false, soundOn = true, music;
  let previousBoard = [];
  let coinAnimation = 0;
  let loadGeneration = 0;

  function node(tag, text, className) {
    const element = document.createElement(tag);
    if (text !== undefined) element.textContent = text;
    if (className) element.className = className;
    return element;
  }
  function button(text, action, className = '') {
    const element = node('button', text, className);
    element.type = 'button'; element.addEventListener('click', action);
    return element;
  }
  function closeModal() { if (modal.open) modal.close(); }
  function openModal(title, content, actions = [], required = false) {
    modalRequired = required;
    $('modal-title').textContent = title;
    $('modal-close').hidden = required;
    $('modal-body').replaceChildren(...content);
    $('modal-actions').replaceChildren(...actions);
    if (!modal.open) modal.showModal();
    modal.scrollTop = 0;
  }
  function message(text, error = false) {
    sdk.status(text, error ? 'error' : 'ok');
  }
  function showFailure(error) {
    $('save-warning').hidden = false;
    $('save-warning').textContent = error?.message || String(error);
  }
  function clearFailure() { $('save-warning').hidden = true; $('save-warning').textContent = ''; }
  function art(id, relic = false, className = 'item-art') {
    const definition = (relic ? catalog.relics : catalog.symbols)?.[id] || {};
    const path = (relic ? assets.relics : assets.symbols)?.[id];
    const fallback = () => node('span', definition.icon || (relic ? '✦' : '◇'), `${className} emoji`);
    if (!path) return fallback();
    const image = node('img', undefined, className);
    image.src = path; image.alt = ''; image.loading = 'lazy';
    image.addEventListener('error', () => image.replaceWith(fallback()), { once: true });
    return image;
  }
  function isPlayable() { return started && cloudReady && !busy && !dirty; }
  function shortName(id) { return catalog.symbols[id]?.name || id; }
  function rarity(definition) { return asList(catalog.rarity)[(definition.rarity || 1) - 1] || ''; }
  function stats(entries) {
    const grid = node('div', undefined, 'stat-grid');
    for (const [label, value] of entries) {
      const card = node('div', undefined, 'stat-card');
      card.append(node('small', label), node('strong', value)); grid.append(card);
    }
    return grid;
  }
  function playSound(name) {
    if (!soundOn) return;
    const audio = audioFor(name).cloneNode();
    audio.volume = name === 'burst' ? 0.45 : name === 'coin' ? 0.32 : 0.24;
    audio.play().catch(() => {});
  }

  // ---- audio: preloaded clips, music starts on the first real gesture ----
  const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const audioCache = new Map();
  let audioUnlocked = false;
  function audioFor(name) {
    let audio = audioCache.get(name);
    if (!audio) { audio = new Audio(`assets/audio/${name}.mp3`); audio.preload = 'auto'; audioCache.set(name, audio); }
    return audio;
  }
  function startMusic() {
    if (!soundOn) return;
    if (!music) { music = audioFor('bgm'); music.loop = true; music.volume = 0.16; }
    music.play().catch(() => {});
  }
  function unlockAudio() {
    if (audioUnlocked) return;
    audioUnlocked = true;
    startMusic();
  }
  function reflectSound() {
    const toggle = $('sound');
    if (!toggle) return;
    toggle.textContent = soundOn ? '🔊' : '🔇';
    toggle.setAttribute('aria-pressed', String(soundOn));
    toggle.title = soundOn ? '关闭声音' : '开启声音';
  }
  function toggleSound() {
    soundOn = !soundOn;
    reflectSound();
    if (soundOn) { unlockAudio(); startMusic(); playSound('coin'); } else music?.pause();
  }

  // ---- effects: perform the brew instead of just swapping numbers ----
  const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
  function spawnFx(className, styles, text) {
    const layer = $('fx');
    if (!layer) return null;
    const element = document.createElement('i');
    element.className = className;
    if (text) element.textContent = text;
    Object.assign(element.style, styles);
    layer.append(element);
    element.addEventListener('animationend', () => element.remove(), { once: true });
    setTimeout(() => element.remove(), 2000);
    return element;
  }
  function raiseSteam(count = 12) {
    if (reduceMotion) return;
    for (let i = 0; i < count; i++) {
      spawnFx('steam', {
        left: `${24 + Math.random() * 52}%`,
        bottom: `${28 + Math.random() * 14}%`,
        animationDelay: `${(Math.random() * 0.55).toFixed(2)}s`,
        '--drift': `${(Math.random() * 46 - 23).toFixed(0)}px`,
      });
    }
    for (let i = 0; i < 9; i++) {
      spawnFx('ember', {
        left: `${30 + Math.random() * 40}%`,
        bottom: `${30 + Math.random() * 10}%`,
        animationDelay: `${(Math.random() * 0.6).toFixed(2)}s`,
        '--dx': `${(Math.random() * 60 - 30).toFixed(0)}px`,
      });
    }
  }
  function raiseSmoke(count = 14) {
    for (let i = 0; i < count; i++) {
      spawnFx('smoke', {
        left: `${18 + Math.random() * 64}%`,
        bottom: `${26 + Math.random() * 18}%`,
        animationDelay: `${(Math.random() * 0.25).toFixed(2)}s`,
        '--drift': `${(Math.random() * 80 - 40).toFixed(0)}px`,
      });
    }
  }
  function flashStage() {
    const flash = $('flash');
    if (!flash || reduceMotion) return;
    flash.classList.remove('on');
    void flash.offsetWidth;
    flash.classList.add('on');
    setTimeout(() => flash.classList.remove('on'), 620);
  }
  function flyCoins(stage, purse, count = 6) {
    if (reduceMotion) return;
    const fromX = stage.left + stage.width / 2;
    const fromY = stage.top + stage.height * 0.46;
    for (let i = 0; i < count; i++) {
      const coin = document.createElement('i');
      coin.className = 'coin';
      coin.style.left = `${fromX + (Math.random() * 44 - 22)}px`;
      coin.style.top = `${fromY + (Math.random() * 20 - 10)}px`;
      coin.style.setProperty('--dx', `${(purse.left + purse.width / 2 - fromX).toFixed(0)}px`);
      coin.style.setProperty('--dy', `${(purse.top + purse.height / 2 - fromY).toFixed(0)}px`);
      coin.style.animationDelay = `${i * 55}ms`;
      document.body.append(coin);
      coin.addEventListener('animationend', () => coin.remove(), { once: true });
      setTimeout(() => coin.remove(), 2200);
    }
  }
  function floatLabel(text) {
    const stage = $('stage').getBoundingClientRect();
    const label = document.createElement('div');
    label.className = 'float-label';
    label.textContent = text;
    label.style.left = `${stage.left + stage.width / 2}px`;
    label.style.top = `${stage.top + stage.height * 0.38}px`;
    document.body.append(label);
    label.addEventListener('animationend', () => label.remove(), { once: true });
    setTimeout(() => label.remove(), 1800);
  }
  function animatePurse(from, to) {
    const purse = $('coins');
    if (!purse) return;
    if (reduceMotion || from === to) { purse.textContent = number(to); return; }
    const token = ++coinAnimation;
    const startedAt = performance.now();
    const step = now => {
      if (token !== coinAnimation) return;
      const progress = Math.min(1, (now - startedAt) / 720);
      const eased = 1 - Math.pow(1 - progress, 3);
      purse.textContent = number(Math.round(from + (to - from) * eased));
      if (progress < 1) requestAnimationFrame(step); else coinAnimation = 0;
    };
    requestAnimationFrame(step);
  }
  function payOut(fromCoins) {
    const gained = state.coins - fromCoins;
    if (gained <= 0) return;
    const stage = $('stage').getBoundingClientRect();
    const purseElement = document.querySelector('.coin-purse');
    if (!purseElement) return;
    floatLabel(`+${number(gained)} 币`);
    flyCoins(stage, purseElement.getBoundingClientRect(), Math.max(4, Math.min(9, Math.round(gained / 30))));
    purseElement.classList.add('pulse');
    setTimeout(() => purseElement.classList.remove('pulse'), 760);
    animatePurse(fromCoins, state.coins);
    const payout = $('payout');
    payout.classList.add('pop');
    setTimeout(() => payout.classList.remove('pop'), 650);
  }
  function boardIds() {
    if (!state) return [];
    return (Array.isArray(state.board) ? state.board : Array.from({ length: 21 }, (_, i) => state.board?.[i + 1])).map(item => item?.id || '');
  }
  function updateButtons() {
    $('save').disabled = busy || !started || !cloudReady || conflict;
    $('reload').disabled = busy || !engine;
    $('retry-load').disabled = busy || !engine;
    $('start').disabled = busy || !cloudReady;
    $('brew').disabled = !isPlayable() || state?.phase !== 'ready';
    $('hint').disabled = !started || !cloudReady || busy;
    $('upgrade-token').disabled = !isPlayable() || state?.phase !== 'ready';
    for (const control of document.querySelectorAll('[data-fuel]')) control.disabled = !isPlayable() || state?.phase !== 'ready';
    for (const control of document.querySelectorAll('#additives button')) control.disabled = !isPlayable() || state?.phase !== 'ready' || control.dataset.unavailable === 'true';
  }
  function renderBoard() {
    const board = Array.isArray(state.board) ? state.board : Array.from({ length: 21 }, (_, i) => state.board?.[i + 1]);
    const positions = [1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 21, 22, 23];
    const changed = board.map((item, index) => previousBoard.length > 0 && (item?.id || '') !== (previousBoard[index] || ''));
    $('board').replaceChildren();
    positions.forEach((position, index) => {
      const instance = board[index];
      const cell = button('', () => { if (instance) showMaterial(instance.id, instance, index); }, `board-cell${instance ? '' : ' empty'}${changed[index] && instance ? ' pop' : ''}`);
      cell.style.gridColumn = String(position % 5 + 1);
      cell.style.gridRow = String(Math.floor(position / 5) + 1);
      if (instance) {
        const definition = catalog.symbols[instance.id] || {};
        cell.append(art(instance.id, false, 'material-image'));
        const values = Array.isArray(state.values) ? state.values[index] : state.values?.[index + 1];
        const value = typeof values === 'number' ? values : (Number(definition.value) || 0) + (Number(instance.growth) || 0);
        cell.append(node('small', `${number(value)}币`));
        cell.setAttribute('aria-label', `第${index + 1}格，${definition.name || instance.id}，查看材料效果`);
      } else { cell.disabled = true; cell.setAttribute('aria-label', `第${index + 1}格，空位`); }
      $('board').append(cell);
    });
    previousBoard = board.map(item => item?.id || '');
  }
  function render() {
    if (!state) { updateButtons(); return; }
    $('night').textContent = `第 ${state.round} 夜`;
    $('rent').textContent = number(state.rent);
    $('coins').textContent = coinAnimation ? $('coins').textContent : number(state.coins);
    $('rent-gap').textContent = state.coins >= state.rent ? '本夜房租已经筹齐' : `离房租还差 ${number(state.rent - state.coins)} 币`;
    $('rent-progress').value = Math.min(100, state.rent ? state.coins / state.rent * 100 : 0);
    $('spins').textContent = `本夜剩余 ${state.left} 次熬制`;
    $('total').textContent = `累计收入 ${number(state.total)} 币`;
    $('temperature').textContent = `${state.temperature || 0} → ${state.nextTemperature ?? state.temperature ?? 0}`;
    $('brew-left').textContent = `本夜剩余 ${state.left} 次`;
    $('upgrade-token').hidden = !(state.upgradeTokens > 0);
    $('upgrade-token').textContent = `强化令牌 ×${state.upgradeTokens || 0}`;
    for (const control of document.querySelectorAll('[data-fuel]')) control.setAttribute('aria-pressed', String(Number(control.dataset.fuel) === state.selectedFuel));
    const preview = state.brewPreview || {};
    $('risk').textContent = `预估炸锅 ${Number(preview.risk || 0).toFixed(1).replace(/\.0$/, '')}% · 收益加成 +${number(preview.boost || 0)}%（盘面反应会影响结果）`;
    $('bag-count').textContent = asList(state.bag).length;
    $('relic-count').textContent = Object.values(state.relics || {}).reduce((n, count) => n + (count === true ? 1 : Number(count) || 0), 0);
    const names = { ready: '选好柴火和助剂，开始下一锅。', draft: '新材料正在等你选择。', exploded: '本次炸锅，已存金币与材料成长保留。', tax: '熬制次数用完了，死神来收房租。', reward: '交租成功，领取本夜奖励。', upgrade: '选择一张材料强化，或保留令牌稍后使用。', relic: '选择一件遗物，继续下一夜。', lost: '这段冒险结束了，你可以重新出发。', victory: '死神倒下了！也可以继续无尽之夜。' };
    $('phase-note').textContent = busy ? '正在处理并同步进度…' : dirty ? '进度尚未同步，先重试保存或读取云端。' : names[state.phase] || '盘面反应中…';
    $('payout').textContent = state.turn ? state.phase === 'exploded' ? '炸锅了 · 已存金币保留' : `上一锅 +${number(state.last)} 币` : '材料会在锅里相遇';
    $('last-result').textContent = state.turn ? `${state.lastOutcome === 'exploded' ? '上次炸锅收回' : '上次收入'} ${number(state.last)} 币。点击盘面材料，查看规则与成长。` : '点击盘面材料，查看它的效果。相邻也包括斜角。';
    $('reactions').replaceChildren(...asList(state.events).filter(event => event.text).slice(-6).map(event => node('li', event.text)));
    renderBoard(); renderPotions(); renderAdditives(); updateButtons();
  }
  function renderPotions() {
    const slots = asList(catalog.balance.mainline?.slots);
    let completed = 0;
    const items = slots.map(slot => {
      const done = !!state.mainline?.[slot.id]; if (done) completed++;
      const item = node('div', undefined, `potion-item${done ? ' done' : ''}`);
      item.append(node('span', done ? '✦' : '♧', 'potion-icon'));
      const text = node('div'); text.append(node('h3', slot.name), node('small', done ? `第 ${state.mainline[slot.id]} 夜炼成` : slot.hint)); item.append(text);
      return item;
    });
    $('potions').replaceChildren(...items); $('potion-count').textContent = `${completed} / ${slots.length}`;
  }
  function renderAdditives() {
    const owned = asList(catalog.balance.brew?.additiveOrder).filter(id => Number(state.relics?.[id]) > 0 || state.relics?.[id] === true);
    $('additives').hidden = !owned.length;
    $('additives').replaceChildren();
    if (!owned.length) return;
    for (const id of ['', ...owned]) {
      const definition = catalog.balance.brew.additives[id];
      const remaining = id ? Number(state.additiveRemaining?.[id]) || 0 : 1;
      const control = button(id ? `${definition.name} ×${remaining}` : '不用助剂', () => act('additive', id));
      control.setAttribute('aria-pressed', String((state.selectedAdditive || '') === id));
      control.dataset.unavailable = String(remaining < 1);
      if (definition) control.title = `收益${definition.boost >= 0 ? '+' : ''}${definition.boost}%，风险${definition.risk >= 0 ? '+' : ''}${definition.risk}个百分点`;
      $('additives').append(control);
    }
  }
  async function persist() {
    dirty = true; updateButtons();
    try {
      await sdk.save(engine.exportSave());
      dirty = false; conflict = false; clearFailure();
      return true;
    } catch (error) {
      conflict = error.status === 409 || /云端冲突|revision_conflict/.test(error.message);
      showFailure({ message: `${error.message} ${conflict ? '当前局面仍保留在本页；读取云端前会再次确认，请勿关闭页面。' : '当前局面仍保留在本页，可点击“保存”重试，请勿关闭页面。'}` });
      return false;
    } finally { updateButtons(); }
  }
  function syncContext() {
    const completed = Object.keys(state.mainline || {}).length;
    sdk.context('witch-workshop', `第${state.round}夜，剩余${state.left}锅，金币${number(state.coins)}，房租${number(state.rent)}，秘药${completed}/4，当前${state.phase}。`).catch(() => {});
  }
  async function act(action, argument) {
    if (!isPlayable()) return;
    busy = true; closeModal(); render();
    try {
      let beforeCoins = state.coins;
      if (action === 'brew') {
        $('stage').classList.add('stir', 'brewing');
        playSound('spin');
        raiseSteam(12);
        await sleep(reduceMotion ? 150 : 900);
      }
      const result = engine.dispatch(action, argument);
      if (result === false) throw new Error('当前阶段不能执行这个操作，请先完成眼前的选择。');
      state = engine.snapshot();
      $('stage').classList.remove('stir', 'brewing');
      const exploded = action === 'brew' && state.phase === 'exploded';
      if (action === 'brew') {
        playSound(exploded ? 'burst' : 'coin');
        if (exploded) {
          $('stage').classList.add('burst');
          setTimeout(() => $('stage').classList.remove('burst'), 900);
          raiseSmoke(14);
          flashStage();
        }
        sdk.event('brew_completed').catch(() => {});
      }
      if (action === 'pay') sdk.event('rent_paid').catch(() => {});
      render();
      if (action === 'brew' && !exploded) payOut(beforeCoins);
      await persist(); syncContext();
    } catch (error) { $('stage').classList.remove('stir', 'brewing'); showFailure(error); message(error.message, true); }
    finally { busy = false; render(); if (!dirty) presentPhase(); }
  }
  function choiceCard(id, index, relic, upgrade = false) {
    const definition = (relic ? catalog.relics : catalog.symbols)[id];
    if (!definition) return node('p', `无法读取候选：${id}`, 'error');
    const card = button('', () => act(upgrade ? 'upgrade' : 'choose', upgrade ? id : index), 'choice-card');
    card.append(art(id, relic));
    const copy = node('div', undefined, 'choice-copy');
    const level = Number(state.levels?.[id]) || 1;
    const owned = relic ? Number(state.relics?.[id]) || 0 : asList(state.bag).filter(item => item.id === id).length;
    copy.append(node('small', `${rarity(definition)} · 已有 ${owned} 份${upgrade ? ` · 等级 ${level} → ${level + 1}` : ''}`), node('h3', definition.name), node('p', definition.desc));
    card.append(copy, node('span', upgrade ? '强化 ↗' : '选它 ↗', 'choice-pick'));
    return card;
  }
  function presentPhase() {
    if (!state || busy || dirty || !started) return;
    if (state.phase === 'arrange' || state.phase === 'review' || state.phase === 'brewed') {
      openModal('继续这一锅', [node('p', '云端保留了尚未完成的熬制阶段。材料盘面和随机状态已恢复，继续后完成结算，不会重抽材料。')], [button('继续完成当前熬制', () => act(state.phase === 'brewed' ? 'bottle' : 'brew'), 'primary')], true);
    } else if (state.phase === 'draft' || state.phase === 'relic') {
      const relic = state.phase === 'relic';
      const choices = asList(state.choices);
      const content = [node('p', relic ? '房租交齐了。选一件魔女遗物，永久改变接下来的每一锅。' : `${state.lastOutcome === 'exploded' ? '炸锅后收回' : '这一锅收获'} ${number(state.last)} 币。${choices.length > 3 ? '升温领悟，多一张候选！' : ''}选一份材料加入袋中，下次随机入锅。`)];
      content.push(...choices.map((id, index) => choiceCard(id, index + 1, relic)));
      const actions = relic ? [] : [button(`骨骰刷新 ×${state.reroll || 0}`, () => act('reroll')), button('跳过这次材料', () => act('skip'), 'secondary')];
      if (!relic) actions[0].disabled = !state.reroll;
      openModal(relic ? '魔女遗物 · 三选一' : '新发现 · 选择材料', content, actions, true);
    } else if (state.phase === 'exploded') {
      openModal('哎呀，坩埚炸锅了', [node('p', '这一锅的加成收益化成了烟，不过你已经存下的金币、材料反应和成长都还在。'), stats([['本锅炸锅概率', `${state.brewRiskAtOpen}%`], ['保温收回', `${number(state.brewRefund)} 币`], ['已存金币', `${number(state.coins)} 币`], ['基础收入', `${number(state.basePayout)} 币`]])], [button('结束这一锅，选择新材料', () => act('finishExplosion'), 'primary')], true);
    } else if (state.phase === 'tax') {
      const content = [node('p', '死神敲了敲工坊的门。交齐这一夜的房租，你就能保住坩埚，迎接新的材料和遗物。'), stats([['本夜房租', `${number(state.rent)} 币`], ['现有金币', `${number(state.coins)} 币`]])];
      const image = node('img', undefined, 'result-art'); image.src = assets.theme?.reaper || 'assets/reaper.webp'; image.alt = '前来收租的死神'; content.unshift(image);
      if (state.round === catalog.balance.winRound && state.bossPlan) content.push(node('p', `终局决战：已完成 ${state.bossPlan.broken} 阶秘药，预计伤害 ${number(state.bossPlan.damage)} / ${number(state.bossPlan.hp)}。交租后将按本夜收益和秘药进度结算。`, 'inline-note'));
      openModal('死神的账单', content, [button(`交房租 ${number(state.rent)} 币`, () => act('pay'), 'primary')], true);
    } else if (state.phase === 'reward') {
      const reward = catalog.balance.rentReward || {};
      openModal('房租交齐了', [node('p', '从死神留下的边角料里，找到一点继续冒险的帮助。'), stats([['驱魔符', `+${reward.removal ?? 1}`], ['骨骰', `+${reward.reroll || 0}`]]), node('p', '驱魔符可以在准备阶段永久移除一份材料；骨骰用于刷新新材料候选。', 'inline-note')], [button('领取奖励', () => act('reward'), 'primary')], true);
    } else if (state.phase === 'upgrade') {
      openModal('材料强化', [node('p', `剩余 ${state.upgradeTokens} 枚升级令牌。强化同名材料的产出；也可以保留令牌，下次再选。`), ...asList(state.upgradeChoices).map(id => choiceCard(id, 0, false, true))], [button('稍后再升', () => act('skipUpgrade'), 'secondary')], true);
    } else if (state.phase === 'lost' || state.phase === 'victory') {
      const victory = state.phase === 'victory';
      const content = [node('p', victory ? '你的秘药让契约失去了力量。工坊还亮着灯，这段冒险也可以继续。' : state.boss ? '房租交上了，但最后的秘药还不足以击退死神。下一次，试着更早筹备药架。' : '最后一锅熬完了，金币仍不足以交租。试着让材料彼此配合，也留意炉温和牌袋稀释。'), stats([['累计收入', `${number(state.total)} 币`], ['成功交租', `${state.paid} 次`]])];
      if (state.boss) content.push(node('p', `决战伤害 ${number(state.boss.damage)} / ${number(state.boss.hp)} · 秘药 ${state.boss.broken} / ${state.boss.shields}`, 'inline-note'));
      const actions = [button('重新开始冒险', confirmNewGame, 'secondary')];
      if (victory) actions.unshift(button('继续无尽之夜', () => act('continue'), 'primary'));
      openModal(victory ? '契约之外，是新的夜晚' : '工坊暂时熄灯了', content, actions, true);
    }
  }
  function showMaterial(id, instance, index) {
    const definition = catalog.symbols[id]; if (!definition) return;
    const content = [art(id), node('p', `${rarity(definition)} · 基础 ${number(definition.value)} 币`), node('p', definition.desc)];
    if (instance) content.push(node('p', `材料等级 ${state.levels?.[id] || 1} · 永久成长 +${number(instance.growth)} · 已上场 ${instance.age || 0} 次${index === undefined ? '' : ` · 盘面第 ${index + 1} 格`}`, 'inline-note'));
    if (definition.lore) content.push(node('p', definition.lore, 'fine'));
    openModal(definition.name, content);
  }
  function showBag() {
    if (!state) return;
    const items = asList(state.bag);
    const content = [node('p', `袋中 ${items.length} 份材料。每锅随机抽取并排入二十一格；相同材料可以有多份。驱魔符 ${state.removal || 0}，骨骰 ${state.reroll || 0}。`), node('p', '驱魔移除只在准备阶段可用，至少保留一份材料。牌袋过满会稀释收益，跳过新卡也是一种选择。', 'inline-note')];
    for (const instance of items) {
      const definition = catalog.symbols[instance.id] || {};
      const item = node('div', undefined, 'list-item'); item.append(art(instance.id));
      const text = node('div', undefined, 'item-info'); text.append(node('h3', definition.name || instance.id), node('p', `等级 ${state.levels?.[instance.id] || 1} · 成长 +${number(instance.growth)}`)); item.append(text);
      item.append(button('详情', () => showMaterial(instance.id, instance), 'secondary'));
      const remove = button('驱魔移除', () => {
        openModal('永久移除这份材料？', [node('p', `消耗一张驱魔符，将这份“${definition.name}”从当前材料袋移除。相同材料的其他份数不受影响。`)], [button('确认移除', () => act('remove', instance.uid)), button('返回材料袋', showBag, 'secondary')]);
      });
      remove.disabled = !isPlayable() || state.phase !== 'ready' || !(state.removal > 0) || items.length <= 1;
      item.append(remove); content.push(item);
    }
    openModal('我的坩埚袋', content);
  }
  function showRelics() {
    if (!state) return;
    const content = [node('p', '每次交租后选择一件遗物。相同遗物的叠加与助剂次数沿用原作规则，助剂次数在下次交租后恢复。')];
    const owned = Object.entries(state.relics || {}).filter(([, count]) => Number(count) > 0 || count === true);
    if (!owned.length) content.push(node('p', '还没有遗物。先熬好眼前几锅，交齐第一夜房租。', 'inline-note'));
    for (const [id, count] of owned) {
      const definition = catalog.relics[id]; if (!definition) continue;
      const item = node('div', undefined, 'list-item'); item.append(art(id, true));
      const text = node('div', undefined, 'item-info'); text.append(node('h3', `${definition.name} ×${count === true ? 1 : count}`), node('p', definition.desc)); item.append(text); content.push(item);
    }
    openModal('魔女遗物', content);
  }
  function showCodex(page = 0) {
    if (!catalog) return;
    const ids = Object.keys(catalog.symbols), size = 16, total = Math.ceil(ids.length / size);
    page = Math.max(0, Math.min(total - 1, page));
    const content = [node('p', `全部 ${ids.length} 种材料与产物 · 第 ${page + 1} / ${total} 页。产物由反应获得，不会进入普通候选池。`)];
    for (const id of ids.slice(page * size, (page + 1) * size)) {
      const definition = catalog.symbols[id];
      const item = button('', () => showMaterial(id), 'choice-card'); item.append(art(id));
      const text = node('div', undefined, 'choice-copy'); text.append(node('small', rarity(definition)), node('h3', definition.name), node('p', definition.desc)); item.append(text); content.push(item);
    }
    const previous = button('上一页', () => showCodex(page - 1), 'secondary'); previous.disabled = page === 0;
    const next = button('下一页', () => showCodex(page + 1), 'secondary'); next.disabled = page + 1 === total;
    openModal('材料图鉴', content, [previous, next]);
  }
  function showRecipes() {
    if (!catalog) return;
    const content = [node('p', '相邻包括横、竖、斜角。盘面先高温裂解，再合成与反应，最后计算收益。成功熬制才会点亮秘药药架。')];
    for (const slot of asList(catalog.balance.mainline?.slots)) {
      const item = node('div', undefined, 'list-item');
      if (slot.potion) item.append(art(slot.potion));
      const text = node('div', undefined, 'item-info'); text.append(node('h3', `${state?.mainline?.[slot.id] ? '✓ ' : ''}${slot.name}`), node('p', slot.hint)); item.append(text); content.push(item);
    }
    content.push(node('p', '高温 ≥3 还会使矿石、矿脉与封魂容器裂解。反应产物永久进入材料袋；即使炸锅，材料变化与成长也会保留。', 'inline-note'));
    openModal('秘药与炉温', content);
  }
  function showHelp() {
    const list = node('ol', undefined, 'help-steps');
    for (const text of ['每锅先选柴火：加2根升温、加1根维持、不加柴降温。高温收益高，也可能炸锅。', '点击“开始熬制”，材料随机入锅并自动反应。相邻包括斜角，留意材料之间的吞噬、成长与倍率。', '每锅结束选择一份新材料，也可以跳过。骨骰能刷新候选；袋子太满会稀释收益。', '本夜次数用完，金币够就交租；不够则结束冒险。交租后领驱魔符、强化材料，并选一件永久遗物。', '尝试合成四阶秘药。第十二夜交租后，会按本夜收益与秘药进度和死神决战。', '每次操作后自动同步云端。保存失败会保留当前界面并暂停继续操作；冲突时先确认读取云端，不会静默覆盖。']) list.append(node('li', text));
    openModal('第一次来工坊？', [node('p', '这是原游戏 Lua 规则在网页中的完整玩法接入，不是点击积累金币的演示。'), list]);
  }
  function showSettings() {
    reflectSound();
    const sounds = button(soundOn ? '关闭音乐与音效' : '开启音乐与音效', async () => {
      toggleSound();
      showSettings();
    });
    const content = [node('p', '声音默认开启（首次点击后开始播放），也可以点右上角随时静音。音乐与音效互不影响玩法，游戏进度通过平台账号保存。'), sounds];
    const newGame = button('开始新的冒险', confirmNewGame, 'secondary'); newGame.disabled = !cloudReady || busy || dirty;
    openModal('工坊设置', content, started ? [newGame] : []);
  }
  function confirmNewGame() {
    if (!cloudReady || busy || dirty) return;
    openModal('开始新的冒险？', [node('p', '当前这场冒险会被新游戏替代，并同步到本游戏的云端存档。其他游戏和平台账号不会受到影响。')], [button('确认新开局', () => startNewGame(), 'primary'), button('保留当前冒险', () => { closeModal(); presentPhase(); }, 'secondary')]);
  }
  async function startNewGame() {
    if (!cloudReady || busy || dirty) return;
    closeModal(); busy = true;
    try {
      const randomSeed = crypto.getRandomValues(new Uint32Array(1))[0] % 2147483646 + 1;
      engine.newGame(randomSeed); state = engine.snapshot(); hasSave = true; started = true;
      $('welcome').hidden = true; $('game').hidden = false;
      render(); await persist(); syncContext();
    } catch (error) { showFailure(error); }
    finally { busy = false; render(); }
  }
  async function readCloud(confirmDiscard = false) {
    if (!engine || busy) return;
    if (started && !confirmDiscard) {
      openModal('读取云端进度？', [node('p', dirty ? '当前页面还有未同步的进度。读取成功后会用云端版本替换当前冒险；失败或坏档不会被自动覆盖。' : '读取成功后会恢复云端版本，并替换当前页面的局面。')], [button('确认读取云端', () => { closeModal(); readCloud(true); }, 'primary'), button('保留当前页面', () => { closeModal(); presentPhase(); }, 'secondary')]);
      return;
    }
    const generation = ++loadGeneration;
    busy = true; cloudReady = false; clearFailure(); updateButtons();
    message('正在确认云端进度…');
    try {
      const saved = await sdk.load();
      if (generation !== loadGeneration) return;
      if (saved !== null && saved !== undefined) {
        engine.load(saved); state = engine.snapshot(); hasSave = true;
      } else {
        hasSave = false; state = undefined; started = false;
        $('welcome').hidden = false; $('game').hidden = true;
      }
      dirty = false; conflict = false; cloudReady = true;
      $('retry-load').hidden = true;
      $('start').textContent = hasSave ? `继续冒险 · 第 ${state.round} 夜` : '开始我的第一锅';
      $('welcome-note').textContent = hasSave ? '已找到并验证你的云端冒险。没有新开局，也没有覆盖进度。' : '云端没有现有冒险。点击开始后，才会创建并保存新游戏。';
      message(hasSave ? '云端进度已读取 · 可以继续冒险' : '云端没有现有进度 · 可以开始新冒险');
      if (started) { render(); syncContext(); }
    } catch (error) {
      cloudReady = false;
      $('start').textContent = '暂时无法开始';
      $('welcome-note').textContent = '未能确认云端进度。为保护已有冒险，本页不会自动新建或覆盖存档。';
      $('retry-load').hidden = false;
      showFailure({ message: `${error.message} 已保留原存档；请重试读取云端，或返回平台后重新进入。` });
      message('未能读取或验证存档 · 没有覆盖已有进度', true);
    } finally {
      busy = false; render(); if (started && cloudReady) presentPhase();
    }
  }
  $('modal-close').addEventListener('click', closeModal);
  modal.addEventListener('cancel', event => { if (modalRequired) event.preventDefault(); });
  $('start').addEventListener('click', () => {
    if (!cloudReady || busy) return;
    if (!hasSave) { startNewGame(); return; }
    started = true; $('welcome').hidden = true; $('game').hidden = false; render(); syncContext(); presentPhase();
  });
  $('brew').addEventListener('click', () => act('brew'));
  for (const control of document.querySelectorAll('[data-fuel]')) control.addEventListener('click', () => act('fuel', Number(control.dataset.fuel)));
  $('save').addEventListener('click', async () => {
    if (busy || !cloudReady || !started || conflict) return;
    busy = true; updateButtons(); await persist(); busy = false; render(); if (!dirty) presentPhase();
  });
  $('reload').addEventListener('click', () => readCloud());
  $('retry-load').addEventListener('click', () => readCloud());
  $('help').addEventListener('click', showHelp);
  $('bag').addEventListener('click', showBag);
  $('relics').addEventListener('click', showRelics);
  $('codex').addEventListener('click', () => showCodex());
  $('recipes').addEventListener('click', showRecipes);
  $('upgrade-token').addEventListener('click', () => act('openUpgrade'));
  $('settings').addEventListener('click', showSettings);
  $('sound')?.addEventListener('click', event => { event.stopPropagation(); toggleSound(); });
  reflectSound();
  document.addEventListener('pointerdown', unlockAudio, { once: true });
  document.addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') unlockAudio(); }, { once: true });
  document.addEventListener('visibilitychange', () => { if (document.hidden) music?.pause(); else if (soundOn) music?.play().catch(() => {}); });
  (async () => {
    try {
      if (!sdk || !window.CauldronEngine) throw new Error('游戏运行文件未完整加载，请返回平台重试。');
      message('正在准备原作炼药规则…');
      engine = await window.CauldronEngine.create({ wasmURL: new URL('runtime/glue.wasm', location.href).href, sources: window.CauldronSources });
      catalog = engine.catalog;
      if (!catalog?.symbols || !catalog?.balance) throw new Error('游戏材料配置无法读取。');
      $('reload').disabled = false;
      await sdk.ready;
      await readCloud();
      sdk.setupHint();
    } catch (error) {
      $('welcome-note').textContent = '没有创建新冒险，也没有覆盖云端存档。请返回平台后重试。';
      $('start').textContent = '游戏尚未准备好';
      showFailure(error); if (sdk) message(error.message, true);
    }
  })();
})();
