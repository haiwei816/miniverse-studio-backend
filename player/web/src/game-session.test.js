import test from 'node:test';
import assert from 'node:assert/strict';
import { request, createChallenge, setToken } from './api.js';
import { createGameSessionController } from './game-session.js';

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

// Exercise the same controller and HTTP transport used by the page. The fake
// API can delay individual requests to reproduce real network ordering.
function harness(t) {
  const originalFetch = globalThis.fetch;
  const session = { value: { player_id: 'player-a' } };
  const play = { value: null }, frameState = { value: '' }, launchBusy = { value: false };
  const notices = [], calls = [], grants = new Map(), tokens = new Map();
  const server = { world: { game_id: '', epoch: 0 }, before: async () => {}, libraryFails: false };
  const json = (body, status = 200) => new Response(JSON.stringify(body), { status });
  globalThis.fetch = async (path, options = {}) => {
    const body = options.body ? JSON.parse(options.body) : undefined;
    calls.push({ path, body });
    await server.before(path, body);
    if (path === '/v1/platform/library') return server.libraryFails ? json({ error: 'internal_error' }, 503) : json({ items: [] });
    if (path === '/v1/platform/launch/exchange') {
      const grant = grants.get(body.launch_code);
      const token = `g_${body.launch_code}`;
      tokens.set(token, grant);
      return json({ ...grant, access_token: token, player_id: 'player-a' });
    }
    const launch = path.match(/^\/v1\/platform\/games\/([^/]+)\/launch$/);
    if (launch) {
      server.world = { game_id: launch[1], epoch: server.world.epoch + 1 };
      const code = `code-${server.world.epoch}`;
      grants.set(code, { ...server.world });
      return json({ launch_code: code, entry_url: `/games/${launch[1]}/`, epoch: server.world.epoch });
    }
    if (path.endsWith('/session/logout')) {
      tokens.delete(options.headers.Authorization.slice('Bearer '.length));
      return json({ ok: true });
    }
    if (path === '/v1/platform/companion/context') {
      if (body.expected_epoch !== server.world.epoch) return json({ error: 'companion_context_changed' }, 409);
      server.world = { game_id: body.game_id, epoch: server.world.epoch + 1 };
      return json(server.world);
    }
    if (path.endsWith('/profile')) {
      const grant = tokens.get(options.headers.Authorization.slice('Bearer '.length));
      return grant?.epoch === server.world.epoch && grant.game_id === server.world.game_id ? json({ player_id: 'player-a' }) : json({ error: 'unauthorized' }, 401);
    }
    throw new Error(`Unexpected request: ${path}`);
  };
  setToken('platform-a');
  t.after(() => { globalThis.fetch = originalFetch; setToken(''); });
  const controller = createGameSessionController({
    session, play, frameState, launchBusy, request, createChallenge,
    requireLogin: () => !!session.value,
    loadLibrary: () => request('/v1/platform/library'),
    notify: message => notices.push(message), origin: 'http://localhost',
  });
  const profile = () => globalThis.fetch(`/v1/games/${controller.current.game_id}/profile`, { headers: { Authorization: `Bearer ${controller.current.access_token}` } });
  return { controller, session, play, frameState, launchBusy, notices, calls, server, profile };
}

test('a library refresh failure preserves the launched frame and usable game credentials', async t => {
  const h = harness(t);
  h.server.libraryFails = true;
  await h.controller.launch({ game_id: 'sky-island' });
  assert.equal(h.play.value.game_id, 'sky-island');
  assert.equal(h.controller.current.game_id, 'sky-island');
  assert.equal((await h.profile()).status, 200);
  assert.equal(h.launchBusy.value, false);
  assert.equal(h.notices.length, 1);
});

for (const phase of ['logout', 'context']) {
  test(`a delayed old-game ${phase} cannot revoke a newly launched game`, async t => {
    const h = harness(t);
    await h.controller.launch({ game_id: 'sky-island' });
    const oldEpoch = h.controller.current.epoch;
    const reached = deferred(), release = deferred();
    h.server.before = async path => {
      const delayed = phase === 'logout' ? path.endsWith('/sky-island/session/logout') : path === '/v1/platform/companion/context';
      if (delayed) { reached.resolve(); await release.promise; }
    };
    const closing = h.controller.close();
    await reached.promise;
    assert.equal(h.play.value, null);
    await h.controller.launch({ game_id: 'card-game' });
    const newSession = h.controller.current;
    release.resolve();
    await closing;
    assert.equal(h.controller.current, newSession);
    assert.equal(h.play.value.game_id, 'card-game');
    assert.equal(h.server.world.game_id, 'card-game');
    assert.equal((await h.profile()).status, 200);
    assert.deepEqual(h.calls.find(call => call.path === '/v1/platform/companion/context').body, { game_id: '', expected_epoch: oldEpoch });
    assert.deepEqual(h.notices, []);
  });
}

test('closing the current game revokes it and clears only its own world', async t => {
  const h = harness(t);
  await h.controller.launch({ game_id: 'sky-island' });
  const oldEpoch = h.controller.current.epoch;
  await h.controller.close();
  assert.equal(h.controller.current, null);
  assert.equal(h.play.value, null);
  assert.equal(h.frameState.value, '');
  assert.deepEqual(h.server.world, { game_id: '', epoch: oldEpoch + 1 });
  assert.deepEqual(h.notices, []);
});

test('closing without a game does not clear a different world', async t => {
  const h = harness(t);
  h.server.world = { game_id: 'another-device-game', epoch: 8 };
  await h.controller.close();
  assert.equal(h.server.world.game_id, 'another-device-game');
  assert.deepEqual(h.calls, []);
});

test('an old launch response cannot reinstall a frame after the account changes', async t => {
  const h = harness(t);
  const reached = deferred(), release = deferred();
  h.server.before = async path => {
    if (path === '/v1/platform/launch/exchange') { reached.resolve(); await release.promise; }
  };
  const launching = h.controller.launch({ game_id: 'sky-island' });
  await reached.promise;
  h.session.value = { player_id: 'player-b' };
  h.controller.reset();
  release.resolve();
  await launching;
  assert.equal(h.controller.current, null);
  assert.equal(h.play.value, null);
  assert.equal(h.launchBusy.value, false);
});

test('a delayed refresh failure from a closed game leaves the next game intact', async t => {
  const h = harness(t);
  const reached = deferred(), release = deferred();
  let libraryRequests = 0;
  h.server.before = async path => {
    if (path === '/v1/platform/library' && ++libraryRequests === 1) { reached.resolve(); await release.promise; }
  };
  const firstLaunch = h.controller.launch({ game_id: 'sky-island' });
  await reached.promise;
  await h.controller.close();
  await h.controller.launch({ game_id: 'card-game' });
  const current = h.controller.current;
  h.server.libraryFails = true;
  release.resolve();
  await firstLaunch;
  assert.equal(h.controller.current, current);
  assert.equal(h.play.value.game_id, 'card-game');
  assert.equal((await h.profile()).status, 200);
  assert.equal(h.launchBusy.value, false);
  assert.deepEqual(h.notices, []);
});
