import assert from 'node:assert/strict';
import { api, base, login } from './client.mjs';

const gmLogin = (username) => api('/gm/login', 'POST', { username, password: `sandbox-${username}` });

const admin = await gmLogin('admin');
const reviewer = await gmLogin('reviewer');
const support = await gmLogin('support');
const p1 = await login();
const p2 = await login();
await api('/v1/games/sky-island/profile', 'GET', undefined, p1.access_token);
await api('/v1/games/sky-island/profile', 'GET', undefined, p2.access_token);

// 1. W3C traceparent & X-Trace-ID propagation
{
  const traceId = '4bf92f3577b34da6a3ce929d0e0e4736';
  const res = await fetch(base + '/health', {
    headers: { traceparent: `00-${traceId}-00f067aa0ba902b7-01` },
  });
  assert.equal(res.status, 200);
  assert.equal(res.headers.get('x-trace-id'), traceId);
  assert.ok((res.headers.get('traceparent') || '').startsWith(`00-${traceId}-`));
}
console.log('1. W3C traceparent / X-Trace-ID propagation: PASS');

// 2. Redis ZSET leaderboard tie-break & cache hits (max + min)
{
  const boardMax = 'flagship-max-' + Date.now();
  await api('/v1/games/sky-island/boards', 'PUT', {
    id: boardMax, season: 's1', mode: 'max', starts_at: 1, ends_at: 4102444800, item: 'wood', reward_amount: 5,
  }, p1.access_token);
  await api(`/v1/games/sky-island/boards/${boardMax}/s1/scores`, 'POST', { request_id: 'r1', score: 500 }, p1.access_token);
  await new Promise((r) => setTimeout(r, 15));
  await api(`/v1/games/sky-island/boards/${boardMax}/s1/scores`, 'POST', { request_id: 'r2', score: 500 }, p2.access_token);

  const b1 = await api(`/v1/games/sky-island/boards/${boardMax}/s1`, 'GET', undefined, p1.access_token);
  const b2 = await api(`/v1/games/sky-island/boards/${boardMax}/s1`, 'GET', undefined, p2.access_token);
  assert.equal(b1.top[0].player_id, p1.player_id, 'earlier tie must rank first');
  assert.equal(b1.top[1].player_id, p2.player_id);
  assert.equal(b2.nearby[0].player_id, p1.player_id);

  const tel = await api('/gm/system/telemetry', 'GET', undefined, admin.access_token);
  const cacheHits = tel.telemetry?.cache_hits ?? tel.cache_hits ?? 0;
  assert.ok(cacheHits >= 1, `expected cache_hits >= 1, got ${cacheHits}`);
}
console.log('2. Redis ZSET tie-break ranking & L1/L2 cache hits: PASS');

// 3. Outbox + Redpanda/Kafka async reward_batch approval -> job completion -> mail delivery + MCP inspect_mq_and_jobs
{
  const reqId = 'batch-' + Date.now();
  await api('/gm/games/sky-island/requests', 'POST', {
    id: reqId,
    kind: 'reward_batch',
    reason: 'flagship async outbox kafka test',
    payload: {
      players: [p1.player_id, p2.player_id],
      item: 'wood',
      amount: 7,
    },
  }, admin.access_token);

  await api(`/gm/games/sky-island/requests/${reqId}/approve`, 'POST', {
    reason: 'approved by reviewer',
  }, reviewer.access_token);

  let jobDone = false;
  for (let i = 0; i < 40; i++) {
    const res = await api('/gm/games/sky-island/batch-jobs', 'GET', undefined, admin.access_token);
    const job = (res.jobs || []).find((j) => j.request_id === reqId);
    if (job && job.state === 'completed' && Number(job.succeeded_players) === 2) {
      jobDone = true;
      break;
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  assert.equal(jobDone, true, 'reward_batch job did not complete via Outbox+Kafka');

  const mails = await api('/v1/games/sky-island/mail', 'GET', undefined, p1.access_token);
  assert.ok(mails.some((m) => Number(m.amount) === 7 && m.item === 'wood'), 'player should receive async batch reward mail');

  const mcpResp = await api('/gm/mcp', 'POST', {
    jsonrpc: '2.0',
    id: 101,
    method: 'tools/call',
    params: { name: 'inspect_mq_and_jobs', arguments: { game_id: 'sky-island' } },
  }, admin.access_token);
  assert.equal(mcpResp.result.isError, false);
  assert.ok((mcpResp.result.structuredContent.batch_jobs || []).some((j) => j.request_id === reqId));
}
console.log('3. Transactional Outbox + Redpanda/Kafka reward_batch & inspect_mq_and_jobs: PASS');

// 4. Progressive MCP retrieval tools (search_runbook & inspect_player_summary)
{
  const r1 = await api('/gm/mcp', 'POST', {
    jsonrpc: '2.0', id: 102, method: 'tools/call',
    params: { name: 'search_runbook', arguments: { query: 'rulecore 503 熔断', top_k: 2 } },
  }, support.access_token);
  assert.equal(r1.result.isError, false);
  assert.equal(r1.result.structuredContent.chunks[0].chunk_id, 'rulecore_unavailable');

  const r2 = await api('/gm/mcp', 'POST', {
    jsonrpc: '2.0', id: 103, method: 'tools/call',
    params: { name: 'inspect_player_summary', arguments: { game_id: 'sky-island', player_id: p1.player_id } },
  }, support.access_token);
  assert.equal(r2.result.isError, false);
  assert.ok(Number(r2.result.structuredContent.unclaimed_mails) >= 1);
}
console.log('4. Progressive MCP retrieval (search_runbook & inspect_player_summary): PASS');

// 5. SSE streaming Ops Agent diagnosis
{
  const res = await fetch(base + '/gm/games/sky-island/ops-agent/diagnose/stream', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: 'Bearer ' + admin.access_token,
    },
    body: JSON.stringify({
      question: '检查玩家奖励未到账问题',
      player_id: p1.player_id,
      allow_propose: false,
    }),
  });
  assert.equal(res.status, 200);
  assert.ok((res.headers.get('content-type') || '').includes('text/event-stream'));
  const text = await res.text();
  assert.ok(text.includes('event: final'), 'SSE stream must emit final diagnosis event');
}
console.log('5. SSE streaming Ops Agent diagnosis: PASS');

// 6. Protected pprof authorization boundaries
{
  const unauth = await fetch(base + '/gm/system/pprof/heap?debug=1');
  assert.equal(unauth.status, 401);

  const supRes = await fetch(base + '/gm/system/pprof/heap?debug=1', {
    headers: { Authorization: 'Bearer ' + support.access_token },
  });
  assert.equal(supRes.status, 403);

  const adminRes = await fetch(base + '/gm/system/pprof/heap?debug=1', {
    headers: { Authorization: 'Bearer ' + admin.access_token },
  });
  assert.equal(adminRes.status, 200);
  const heapText = await adminRes.text();
  assert.ok(heapText.includes('heap profile:'), 'admin pprof heap profile should render');
}
console.log('6. Protected /gm/system/pprof RBAC boundaries: PASS');
