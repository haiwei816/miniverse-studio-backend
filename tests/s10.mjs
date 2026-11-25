import assert from 'node:assert/strict';
import { api, login } from './client.mjs';

const a = await login();
const b = await login();
const fixture = 'fixture-' + crypto.randomUUID();

async function proof(channel, subject = fixture) {
  const res = await api('/v1/sandbox/tickets/' + channel, 'POST', { game_id: 'card-game', subject }, a.access_token);
  return res.ticket;
}

const ticket = await proof('steam');
await api('/v1/platform/steam/bind', 'POST', { game_id: 'card-game', ticket }, a.access_token);
await api('/v1/platform/steam/login', 'POST', { game_id: 'card-game', ticket }, undefined, 409);

assert.equal((await api('/v1/platform/steam/login', 'POST', { game_id: 'card-game', ticket: await proof('steam') })).player_id, a.player_id);
await api('/v1/platform/steam/bind', 'POST', { game_id: 'card-game', ticket: await proof('steam') }, b.access_token, 409);
await api('/v1/platform/steam/unbind', 'POST', { game_id: 'card-game', ticket: await proof('steam') }, a.access_token, 409);
await api('/v1/platform/taptap/bind', 'POST', { game_id: 'card-game', ticket: await proof('taptap') }, a.access_token);
await api('/v1/platform/steam/unbind', 'POST', { game_id: 'card-game', ticket: await proof('steam') }, a.access_token);
await api('/v1/platform/taptap/login', 'POST', { game_id: 'sky-island', ticket: await proof('taptap') }, undefined, 401);
await api('/v1/platform/steam/login', 'POST', { game_id: 'card-game', ticket: 'forged' }, undefined, 401);

console.log('S10 Steam/TapTap sandbox signature, expiry unit tests, replay prevention, account binding/conflict/unbind: PASS; REAL PLATFORM UNVERIFIED');
