import assert from 'node:assert/strict';
import { api, login } from './client.mjs';

const a = await login();
const t = a.access_token;
const p = '/v1/games/card-game/replays/validate';
const replay = {
  version: 'card-v1',
  seed: 123,
  actions: [2, 2, 2, 2, 2, 2],
  claimed_score: 36,
  request_id: 'golden',
  board: 'verified',
  season: 'sandbox',
};

const v = await api(p, 'POST', replay, t);
assert.equal(v.score, 36);
assert.equal(v.state_hash, '8660768014306689972');
assert.equal(v.victory, true);
assert.equal((await api(p, 'POST', replay, t)).state_hash, v.state_hash);

await api(p, 'POST', { ...replay, request_id: 'bad-score', claimed_score: 999 }, t, 422);
await api(p, 'POST', { ...replay, request_id: 'illegal', actions: [9] }, t, 422);
await api(p, 'POST', { ...replay, request_id: 'unfinished', actions: [2], claimed_score: 4 }, t, 422);
await api('/v1/games/card-game/boards/verified/sandbox/scores', 'POST', { request_id: 'cheat', score: 999 }, t, 403);

const ranks = await api('/v1/games/card-game/boards/verified/sandbox', 'GET', undefined, t);
assert.ok(ranks.top.some((x) => x.player_id === a.player_id && x.score === 36));
assert.equal(ranks.trust, 'deterministic_replay');

console.log('S12 C++20 -> gRPC -> Go: golden replay/hash, illegal operation/forged score rejection, verified leaderboard: PASS');
