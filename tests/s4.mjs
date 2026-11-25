import assert from 'node:assert/strict';
import { api, login } from './client.mjs';

const a = await login();
const t = a.access_token;
const base = '/v1/games/sky-island';
const id = 'build-' + Date.now();
const config = {
  id,
  version: 1,
  kind: 'cumulative',
  event_type: id,
  target: 3,
  starts_at: 1,
  ends_at: 4102444800,
  client_allowed: true,
  item: 'wood',
  reward_amount: 10,
};

await api(base + '/activities', 'PUT', config, t);
await api(base + '/events', 'POST', { event_id: 'e1', event_type: id, amount: 2 }, t);
assert.equal((await api(base + '/events', 'POST', { event_id: 'e1', event_type: id, amount: 2 }, t)).duplicate, true);

let row = (await api(base + '/activities', 'GET', undefined, t)).find((x) => x.config.id === id);
assert.equal(row.progress, 2);

await api(base + '/events', 'POST', { event_id: 'e2', event_type: id, amount: 100 }, t);
row = (await api(base + '/activities', 'GET', undefined, t)).find((x) => x.config.id === id);
assert.equal(row.progress, 3);
assert.equal(row.complete, true);

await api(base + '/events', 'POST', { event_id: 'e3', event_type: id, amount: 1, source: 'server' }, t, 400);
await api(base + '/activities', 'PUT', { ...config, id: id + '-trusted', client_allowed: false }, t);
await api(base + '/events', 'POST', { event_id: 'e4', event_type: id, amount: 1 }, t);
assert.equal((await api(base + '/activities', 'GET', undefined, t)).find((x) => x.config.id === id + '-trusted').progress, 0);

for (const kind of ['daily', 'signin', 'milestone']) {
  await api(base + '/activities', 'PUT', {
    ...config,
    id: id + '-' + kind,
    kind,
    target: kind === 'signin' ? 1 : 3,
  }, t);
}
await api(base + '/events', 'POST', { event_id: 'e5', event_type: id, amount: 3 }, t);
for (const kind of ['daily', 'signin', 'milestone']) {
  assert.equal((await api(base + '/activities', 'GET', undefined, t)).find((x) => x.config.id === id + '-' + kind).complete, true);
}

console.log('S4 event dedupe/trust, bounded progress, cumulative/daily/signin/milestone: PASS');
