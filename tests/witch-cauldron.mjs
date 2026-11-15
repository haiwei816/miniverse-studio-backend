import assert from 'node:assert/strict';
import {createHash, randomBytes} from 'node:crypto';
import {api, login} from './client.mjs';
import {gm} from './gm-client.mjs';

const game = 'witch-cauldron';
const root = `/v1/games/${game}`;
const slot = `${root}/saves/web-main`;
const stamp = randomBytes(8).toString('hex');
const username = `witch_${stamp}`;
const password = 'Witch-integration-password-7';
const account = await api('/v1/platform/auth/register', 'POST', {username, password});
const guest = await login();

// A sandbox guest remains a separate identity, not the platform username account.
assert.notEqual(guest.player_id, account.player_id);
assert.equal((await api('/v1/platform/me', 'GET', undefined, account.access_token)).username, username);
assert.equal((await api('/v1/platform/me', 'GET', undefined, guest.access_token)).username, 'sandbox-player');

// Traverse the entire catalog rather than assuming that it contains two games.
const catalog = [], cursors = new Set();
let cursor = '';
do {
  const page = await api(`/v1/platform/games?limit=1&cursor=${encodeURIComponent(cursor)}`);
  catalog.push(...page.items);
  cursor = page.next_cursor;
  if (cursor) {
    assert.ok(page.items.length > 0, 'a continuing catalog page must contain a game');
    assert.ok(!cursors.has(cursor), 'catalog cursor repeated');
    cursors.add(cursor);
    assert.ok(cursors.size < 1000, 'catalog pagination did not terminate');
  }
} while (cursor);
assert.equal(new Set(catalog.map(item => item.game_id)).size, catalog.length);
assert.ok(catalog.some(item => item.game_id === game));
const listing = await api(`/v1/platform/games/${game}`);
assert.equal(listing.name, '女巫的坩埚');
assert.equal(listing.entry_url, '/games/witch-cauldron/');
assert.equal(listing.accent, 'violet');
assert.equal(listing.icon, '⚗');
assert.equal(listing.companion_level, 'context');

function saveBody(revision, state) {
  const data = JSON.stringify(state);
  return {revision, schema_version: 1, data, checksum: createHash('sha256').update(data).digest('hex')};
}
async function launch(token) {
  const verifier = randomBytes(48).toString('base64url');
  const code_challenge = createHash('sha256').update(verifier).digest('base64url');
  const grant = await api(`/v1/platform/games/${game}/launch`, 'POST', {code_challenge}, token);
  const body = {launch_code: grant.launch_code, game_id: game, code_verifier: verifier};
  const session = await api('/v1/platform/launch/exchange', 'POST', body);
  assert.equal(session.game_id, game);
  assert.equal(session.epoch, grant.epoch);
  assert.ok(session.access_token.startsWith('g_'));
  await api('/v1/platform/launch/exchange', 'POST', body, undefined, 401);
  return session;
}

const guestState = {fixture: stamp, coins: 3, run: 'guest'};
await api(slot, 'GET', undefined, undefined, 401);
await api(slot, 'PUT', saveBody(0, guestState), guest.access_token);
await api(slot, 'GET', undefined, account.access_token, 404);
const otherGameState = {fixture: stamp, run: 'another-game'};
await api('/v1/games/card-game/saves/web-main', 'PUT', saveBody(0, otherGameState), account.access_token);

let session = await launch(account.access_token);
assert.equal(session.player_id, account.player_id);
await api(`${root}/profile`, 'GET', undefined, session.access_token);
await api('/v1/games/card-game/profile', 'GET', undefined, session.access_token, 403);
await api('/v1/games/card-game/saves/web-main', 'GET', undefined, session.access_token, 403);
await api('/v1/platform/me', 'GET', undefined, session.access_token, 401);
await api('/gm/games/witch-cauldron/players', 'GET', undefined, session.access_token, 401);

const state = {fixture: stamp, coins: 47, run: {stage: 3, deck: ['integration-fixture']}};
const saved = await api(slot, 'PUT', saveBody(0, state), session.access_token);
assert.equal(saved.revision, 1);
assert.deepEqual(JSON.parse((await api(slot, 'GET', undefined, session.access_token)).data), state);
await api(slot, 'PUT', saveBody(0, {...state, coins: 999}), session.access_token, 409);
assert.deepEqual(JSON.parse((await api(slot, 'GET', undefined, guest.access_token)).data), guestState);

// Client gameplay events can be recorded and deduplicated without inventing
// an economy: the game's coins remain exclusively inside its save document.
const event = {event_id: `witch-integration-${stamp}`, event_type: 'integration_witch_completed', amount: 1};
assert.equal((await api(`${root}/events`, 'POST', event, session.access_token)).duplicate, false);
assert.equal((await api(`${root}/events`, 'POST', event, session.access_token)).duplicate, true);
assert.ok(Array.isArray(await api(`${root}/activities`, 'GET', undefined, session.access_token)));
assert.deepEqual(await api(`${root}/inventory`, 'GET', undefined, session.access_token), {});

await api('/v1/platform/library/witch-cauldron', 'PUT', {favorite: true}, account.access_token);
assert.ok((await api('/v1/platform/library', 'GET', undefined, account.access_token)).items.some(item => item.game_id === game && item.favorite && item.last_played_at > 0));
await api(`${root}/session/logout`, 'POST', {}, session.access_token);
await api(slot, 'GET', undefined, session.access_token, 401);
await api('/v1/platform/companion/context', 'POST', {game_id: '', expected_epoch: session.epoch}, account.access_token);
await api('/v1/platform/auth/logout', 'POST', {}, account.access_token);

const returned = await api('/v1/platform/auth/login', 'POST', {username, password});
assert.equal(returned.player_id, account.player_id);
session = await launch(returned.access_token);
const restored = await api(slot, 'GET', undefined, session.access_token);
assert.equal(restored.revision, saved.revision);
assert.deepEqual(JSON.parse(restored.data), state);
assert.deepEqual(JSON.parse((await api('/v1/games/card-game/saves/web-main', 'GET', undefined, returned.access_token)).data), otherGameState);

for (const role of ['admin', 'operator', 'reviewer']) {
  const token = await gm(role);
  assert.ok((await api('/gm/games', 'GET', undefined, token)).some(item => item.game_id === game));
  assert.ok((await api(`/gm/games/${game}/players?q=${encodeURIComponent(account.player_id)}`, 'GET', undefined, token)).some(player => player.player_id === account.player_id));
}
const support = await gm('support');
assert.ok(!(await api('/gm/games', 'GET', undefined, support)).some(item => item.game_id === game));
await api('/gm/games/witch-cauldron/players', 'GET', undefined, support, 403);
await api('/v1/platform/auth/logout', 'POST', {}, returned.access_token);
await api('/v1/auth/logout', 'POST', {}, guest.access_token);

console.log('Witch Cauldron: full catalog pagination, PKCE, guest/account and game save isolation, return-session restoration, event deduplication and GM access: PASS');
