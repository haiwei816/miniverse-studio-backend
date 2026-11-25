import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { api, login } from './client.mjs';
import { gm } from './gm-client.mjs';

const a = await login();
const t = a.access_token;

for (const [game, event, item, board] of [
  ['sky-island', 'building_upgraded', 'wood', 'prosperity'],
  ['card-game', 'match_completed', 'gem', 'weekly'],
]) {
  const p = '/v1/games/' + game;
  await api(p + '/profile', 'GET', undefined, t);
  const data = JSON.stringify({ game, level: 3 });
  await api(p + '/saves/main', 'PUT', {
    revision: 0,
    schema_version: 1,
    data,
    checksum: createHash('sha256').update(data).digest('hex'),
  }, t);
  await api(p + '/events', 'POST', { event_id: 'gameplay', event_type: event, amount: 3 }, t);
  const mail = await api(p + '/mail', 'GET', undefined, t);
  assert.equal(mail.length, 1);
  await api(p + `/mail/${mail[0].id}/claim`, 'POST', {}, t);
  const inv = await api(p + '/inventory', 'GET', undefined, t);
  assert.ok(inv[item] > 0);
  assert.equal(Object.keys(inv).length, 1);
  await api(p + `/boards/${board}/sandbox/scores`, 'POST', { request_id: 'result', score: 3 }, t);
  assert.ok((await api(p + `/boards/${board}/sandbox`, 'GET', undefined, t)).top.some((x) => x.player_id === a.player_id));
}

await api('/gm/games/card-game/players', 'GET', undefined, await gm('support'), 403);
assert.ok((await api('/gm/games/card-game/players', 'GET', undefined, await gm())).some((x) => x.player_id === a.player_id));

console.log('S9 two games share login/save/event/mail/inventory/leaderboard modules, data and GM isolation: PASS');
