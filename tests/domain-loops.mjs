import assert from 'node:assert/strict';
import { api, base, login } from './client.mjs';
import { gm } from './gm-client.mjs';

function solveCardSeed(seed) {
  function sim(rngInit, actions) {
    let hp = 20;
    let enemy = 30;
    let rng = rngInit >>> 0;
    let damage = 0;
    for (const action of actions) {
      if (enemy <= 0 || hp <= 0) return null;
      let dealt = 0;
      if (action !== 0) {
        rng ^= (rng << 13) >>> 0;
        rng ^= rng >>> 17;
        rng ^= (rng << 5) >>> 0;
        rng >>>= 0;
        dealt = (rng % 6) + action * 2;
      }
      const actual = Math.min(dealt, enemy);
      enemy -= actual;
      damage += actual;
      if (enemy > 0) {
        rng ^= (rng << 13) >>> 0;
        rng ^= rng >>> 17;
        rng ^= (rng << 5) >>> 0;
        rng >>>= 0;
        const attack = (rng % 4) + 1;
        hp -= action === 0 ? Math.floor(attack / 2) : attack;
      }
    }
    return { hp, enemy, score: damage + (enemy <= 0 && hp > 0 ? hp : 0), victory: enemy <= 0 && hp > 0 };
  }

  for (const fill of [[2], [2, 1], [1, 2]]) {
    const actions = [];
    for (let i = 0; i < 20; i++) {
      actions.push(fill[i % fill.length]);
      const st = sim(seed, actions);
      if (!st) break;
      if (st.victory) return { actions, score: st.score };
    }
  }
  const queue = [{ actions: [], rng: seed >>> 0, hp: 20, enemy: 30, damage: 0 }];
  const seen = new Set();
  while (queue.length > 0) {
    const cur = queue.shift();
    if (cur.actions.length >= 16) continue;
    for (const action of [2, 1, 0]) {
      let rng = cur.rng;
      let hp = cur.hp;
      let enemy = cur.enemy;
      let dealt = 0;
      if (action !== 0) {
        rng ^= (rng << 13) >>> 0;
        rng ^= rng >>> 17;
        rng ^= (rng << 5) >>> 0;
        rng >>>= 0;
        dealt = (rng % 6) + action * 2;
      }
      const actual = Math.min(dealt, enemy);
      enemy -= actual;
      const damage = cur.damage + actual;
      if (enemy > 0) {
        rng ^= (rng << 13) >>> 0;
        rng ^= rng >>> 17;
        rng ^= (rng << 5) >>> 0;
        rng >>>= 0;
        const attack = (rng % 4) + 1;
        hp -= action === 0 ? Math.floor(attack / 2) : attack;
      }
      const nextActions = [...cur.actions, action];
      if (enemy <= 0 && hp > 0) {
        return { actions: nextActions, score: damage + hp };
      }
      if (hp > 0 && enemy > 0) {
        const key = `${rng}:${hp}:${enemy}:${nextActions.length}`;
        if (!seen.has(key)) {
          seen.add(key);
          queue.push({ actions: nextActions, rng, hp, enemy, damage });
        }
      }
    }
  }
  throw new Error(`Unable to find winning card-v1 replay for seed ${seed}`);
}

async function mcpCall(token, name, args) {
  const res = await fetch(`${base}/gm/mcp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }),
  });
  const payload = await res.json();
  assert.equal(res.status, 200, `MCP HTTP ${res.status}: ${JSON.stringify(payload)}`);
  assert.ok(!payload.error, `MCP RPC error: ${JSON.stringify(payload.error)}`);
  return JSON.parse(payload.result.content[0].text);
}

const suffix = Date.now().toString(36);
const [admin, operator, reviewer] = await Promise.all([gm('admin'), gm('operator'), gm('reviewer')]);

// 1. Server-issued match ticket anti-seed-farming on card-game ranked board
const p1 = await login();
const p1Token = p1.access_token;
await api('/v1/games/card-game/profile', 'GET', undefined, p1Token);

const noTicket = await api(
  '/v1/games/card-game/replays/validate',
  'POST',
  { version: 'card-v1', seed: 123, actions: [2, 2, 2, 2, 2, 2], request_id: `nt-${suffix}`, board: 'ranked', season: 'sandbox' },
  p1Token,
  400,
);
assert.equal(noTicket.error, 'match_ticket_required');

const ticket = await api(
  '/v1/games/card-game/matches/start',
  'POST',
  { board: 'ranked', season: 'sandbox', rule_version: 'card-v1' },
  p1Token,
);
assert.ok(ticket.match_id && ticket.seed > 0);
const solution = solveCardSeed(ticket.seed);

const badSeed = ticket.seed === 123 ? 124 : 123;
const badSol = solveCardSeed(badSeed);
const wrongSeed = await api(
  '/v1/games/card-game/replays/validate',
  'POST',
  {
    version: 'card-v1',
    seed: badSeed,
    actions: badSol.actions,
    request_id: `ws-${suffix}`,
    board: 'ranked',
    season: 'sandbox',
    match_id: ticket.match_id,
  },
  p1Token,
  422,
);
assert.equal(wrongSeed.error, 'seed_mismatch');

const validated = await api(
  '/v1/games/card-game/replays/validate',
  'POST',
  {
    version: 'card-v1',
    seed: ticket.seed,
    actions: solution.actions,
    claimed_score: solution.score,
    request_id: `ok-${suffix}`,
    board: 'ranked',
    season: 'sandbox',
    match_id: ticket.match_id,
  },
  p1Token,
);
assert.equal(validated.trust, 'server_seeded_replay');
assert.equal(validated.victory, true);

const reused = await api(
  '/v1/games/card-game/replays/validate',
  'POST',
  {
    version: 'card-v1',
    seed: ticket.seed,
    actions: solution.actions,
    request_id: `reuse-${suffix}`,
    board: 'ranked',
    season: 'sandbox',
    match_id: ticket.match_id,
  },
  p1Token,
  409,
);
assert.equal(reused.error, 'match_ticket_consumed');

const rankedBoard = await api('/v1/games/card-game/boards/ranked/sandbox', 'GET', undefined, p1Token);
assert.equal(rankedBoard.trust, 'server_seeded_replay');
assert.ok(rankedBoard.top.some((x) => x.player_id === p1.player_id && x.score === solution.score));

// 2. GM board listing + dual-approval board_upsert & season_settle payout
const boards = await api('/gm/games/card-game/boards', 'GET', undefined, admin);
assert.ok(boards.some((b) => b.id === 'ranked' && b.season === 'sandbox' && b.require_ticket === true));

const beforeInv = await api('/v1/games/card-game/inventory', 'GET', undefined, p1Token);
const beforeGem = Number(beforeInv.gem || 0);

const cupBoard = `cup-${suffix}`;
const nowSec = Math.floor(Date.now() / 1000);
await api(
  '/gm/games/card-game/requests',
  'POST',
  {
    id: `bu-${suffix}`,
    kind: 'board_upsert',
    reason: 'open timed cup season',
    payload: {
      id: cupBoard,
      season: 's1',
      mode: 'max',
      starts_at: nowSec - 60,
      ends_at: nowSec + 3,
      frozen: false,
      item: 'gem',
      reward_amount: 5,
      rule_version: 'card-v1',
      require_ticket: true,
    },
  },
  operator,
);
await api(`/gm/games/card-game/requests/bu-${suffix}/approve`, 'POST', { reason: 'approve cup board' }, reviewer);

const cupTicket = await api('/v1/games/card-game/matches/start', 'POST', { board: cupBoard, season: 's1', rule_version: 'card-v1' }, p1Token);
const cupWin = solveCardSeed(cupTicket.seed);
await api(
  '/v1/games/card-game/replays/validate',
  'POST',
  { version: 'card-v1', seed: cupTicket.seed, actions: cupWin.actions, request_id: `cup-win-${suffix}`, board: cupBoard, season: 's1', match_id: cupTicket.match_id },
  p1Token,
);

await new Promise((r) => setTimeout(r, 3500));

await api(
  '/gm/games/card-game/requests',
  'POST',
  { id: `ss-${suffix}`, kind: 'season_settle', reason: 'settle closed cup season', payload: { board: cupBoard, season: 's1' } },
  operator,
);
await api(`/gm/games/card-game/requests/ss-${suffix}/approve`, 'POST', { reason: 'approve cup payout' }, reviewer);

await api('/v1/games/card-game/mail/claim-all', 'POST', {}, p1Token);
const afterInv = await api('/v1/games/card-game/inventory', 'GET', undefined, p1Token);
const afterGem = Number(afterInv.gem || 0);
assert.equal(afterGem, beforeGem + 5);

// 3. Player ban + atomic kick + unban + MCP propose_player_freeze dual-approval
const p2 = await login();
await api('/v1/games/card-game/profile', 'GET', undefined, p2.access_token);
await api(`/gm/games/card-game/players/${encodeURIComponent(p2.player_id)}/ban`, 'POST', { reason: 'abnormal replay exploit' }, operator, 403);
await api(`/gm/games/card-game/players/${encodeURIComponent(p2.player_id)}/ban`, 'POST', { reason: 'abnormal replay exploit' }, admin);

// Active session was deleted atomically; relogin is rejected with 403 account_suspended.
await api('/v1/games/card-game/profile', 'GET', undefined, p2.access_token, 401);
const reloginSuspended = await api('/v1/auth/guest', 'POST', { secret: p2.secret }, undefined, 403);
assert.equal(reloginSuspended.error, 'account_suspended');

const p2Detail = await api(`/gm/games/card-game/players/${encodeURIComponent(p2.player_id)}`, 'GET', undefined, admin);
assert.equal(p2Detail.ban.banned, true);

await api(`/gm/games/card-game/players/${encodeURIComponent(p2.player_id)}/unban`, 'POST', { reason: 'false positive cleared' }, reviewer);
const p2Restored = await api('/v1/auth/guest', 'POST', { secret: p2.secret });
assert.equal(p2Restored.player_id, p2.player_id);
await api('/v1/games/card-game/profile', 'GET', undefined, p2Restored.access_token);

const mcpFreeze = await mcpCall(operator, 'propose_player_freeze', {
  game_id: 'card-game',
  player_id: p2.player_id,
  reason: 'mcp detected spike',
  request_id: `mcp-fr-${suffix}`,
});
assert.equal(mcpFreeze.kind, 'player_ban');
assert.equal(mcpFreeze.requires_human_approval, true);
await api(`/gm/games/card-game/requests/${mcpFreeze.request_id}/approve`, 'POST', { reason: 'reviewer confirmed freeze' }, reviewer);
await api('/v1/games/card-game/profile', 'GET', undefined, p2Restored.access_token, 401);
await api(`/gm/games/card-game/players/${encodeURIComponent(p2.player_id)}/unban`, 'POST', { reason: 'cleanup domain loop test' }, admin);

console.log('Domain loops (server match tickets, GM board/season dual-approval, player ban/kick & MCP freeze): PASS');
