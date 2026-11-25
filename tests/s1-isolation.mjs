import assert from 'node:assert/strict';
import { api, login } from './client.mjs';

const a = await login();
const t = a.access_token;
const g = 'scope-' + Date.now();

await api('/v1/games', 'POST', { game_id: g, name: 'scope test' }, t);
await api('/v1/games/sky-island/profile', 'PATCH', { name: 'island' }, t);
assert.equal((await api(`/v1/games/${g}/profile`, 'GET', undefined, t)).name, '');

console.log('S1 multi-game profile isolation: PASS');
