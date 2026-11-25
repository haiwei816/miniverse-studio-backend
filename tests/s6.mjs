import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { api, login } from './client.mjs';

const a = await login();
const b = await login();
const t = a.access_token;
const id = 'board' + Date.now();
const root = '/v1/games/sky-island';

for (const mode of ['max', 'sum', 'min']) {
  await api(root + '/boards', 'PUT', {
    id: id + mode, season: 's1', mode, starts_at: 1, ends_at: 4102444800, item: 'wood', reward_amount: 5,
  }, t);
  const p = root + `/boards/${id + mode}/s1`;
  for (const [request_id, score] of [['a', 10], ['b', 20], ['b', 20]]) {
    await api(p + '/scores', 'POST', { request_id, score }, t);
  }
  assert.equal((await api(p, 'GET', undefined, t)).top[0].score, { max: 20, sum: 30, min: 10 }[mode]);
}

const p = root + `/boards/${id}max/s1`;
await api(p + '/scores', 'POST', { request_id: 'other', score: 30 }, b.access_token);
assert.equal((await api(p, 'GET', undefined, t)).nearby[1].player_id, a.player_id);
await api(p + '/settle', 'POST', {}, t, 409);

execFileSync('docker', [
  'compose', '-f', 'deploy/compose.yaml', 'exec', '-T', 'mysql',
  'mysql', '-uroot', '-plocal-root-only', 'miniverse',
  '-e', `UPDATE boards SET ends_at=1 WHERE id='${id}max'`,
]);

await api(p + '/scores', 'POST', { request_id: 'late', score: 40 }, t, 409);
await api(p + '/settle', 'POST', {}, t);
await api(p + '/settle', 'POST', {}, t);
assert.equal((await api(root + '/mail', 'GET', undefined, t)).length, 1);

console.log('S6 ranking max/sum/min, duplicate score, nearby rank, season cutoff/idempotent settlement: PASS');
