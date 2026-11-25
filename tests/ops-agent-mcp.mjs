import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { api, base, login } from './client.mjs';
import { gm } from './gm-client.mjs';

async function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function mcpRPC(token, method, params, id) {
  const payload = { jsonrpc: '2.0', method, ...(params !== undefined ? { params } : {}) };
  if (id !== undefined) {
    payload.id = id;
  }
  const res = await fetch(`${base}/gm/mcp`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(payload),
  });
  return res;
}

// Log in once per GM role to respect auth rate limits across sequential suites.
const [admin, reviewer, support] = await Promise.all([
  gm('admin'),
  gm('reviewer'),
  gm('support'),
]);

// Ensure deterministic_playbook baseline even if a prior local session configured an external LLM.
await api('/gm/system/ops-agent/config', 'PUT', { url: '', clear_api_key: true }, admin);

// ---------------------------------------------------------------------------
// 1. Reward-missing complaint -> AI Ops Agent -> Controlled dual-approval
// ---------------------------------------------------------------------------
const p1 = await login();
await api('/v1/games/sky-island/profile', 'GET', undefined, p1.access_token);
await api(
  '/v1/games/sky-island/events',
  'POST',
  { event_id: `build-ops-${Date.now()}`, event_type: 'building_upgraded', amount: 3 },
  p1.access_token
);
const initialMails = await api('/v1/games/sky-island/mail', 'GET', undefined, p1.access_token);
assert.equal(initialMails.length, 1, 'expected 1 unclaimed reward mail before AI diagnosis');

const diag1 = await api(
  '/gm/games/sky-island/ops-agent/diagnose',
  'POST',
  {
    question: '玩家反馈完成升级建筑活动但背包没有道具，请排查奖励未到账原因并生成补偿提案',
    player_id: p1.player_id,
    allow_propose: true,
    proposal_item: 'wood',
    proposal_amount: 5,
  },
  admin
);

assert.equal(diag1.mode, 'deterministic_playbook');
assert.equal(diag1.real_ai, false);
assert.equal(diag1.status, 'completed');
assert.ok(diag1.steps.some((s) => s.tool_name === 'diagnose_player'), 'missing diagnose_player step');
assert.ok(diag1.steps.some((s) => s.tool_name === 'explain_reward_issue'), 'missing explain_reward_issue step');
assert.ok(
  diag1.steps.some((s) => s.tool_name === 'propose_reward_compensation'),
  'missing propose_reward_compensation step'
);
assert.ok(diag1.proposal_request_id, 'expected non-empty proposal_request_id');
assert.ok(diag1.root_cause.includes('未领取') || diag1.summary.includes('奖励'), 'unexpected diagnosis text');

const savedRun1 = await api(
  `/gm/games/sky-island/ops-agent/runs/${diag1.run_id}`,
  'GET',
  undefined,
  admin
);
assert.equal(savedRun1.run_id, diag1.run_id);
assert.equal(savedRun1.proposal_request_id, diag1.proposal_request_id);
assert.equal(savedRun1.steps.length, diag1.steps.length);

const runList = await api('/gm/games/sky-island/ops-agent/runs', 'GET', undefined, admin);
assert.ok(runList.some((r) => r.run_id === diag1.run_id), 'diagnose run missing from runs list');

// Dual-approval guardrail: admin creator cannot self-approve; reviewer approves cleanly.
await api(
  `/gm/games/sky-island/requests/${diag1.proposal_request_id}/approve`,
  'POST',
  { reason: 'self approval must fail' },
  admin,
  403
);
await api(
  `/gm/games/sky-island/requests/${diag1.proposal_request_id}/approve`,
  'POST',
  { reason: '核实 AI 排障建议后批准补偿' },
  reviewer
);

const updatedMails = await api('/v1/games/sky-island/mail', 'GET', undefined, p1.access_token);
assert.equal(updatedMails.length, 2, 'expected both activity and AI-proposed compensation mails');
for (const m of updatedMails) {
  await api(`/v1/games/sky-island/mail/${m.id}/claim`, 'POST', {}, p1.access_token);
}
const invAfter = await api('/v1/games/sky-island/inventory', 'GET', undefined, p1.access_token);
assert.ok(invAfter.wood > 5, `expected claimed activity reward + 5 compensation wood, got ${invAfter.wood}`);
console.log('1. AI Ops Agent reward diagnostics & dual-approval proposal: PASS');

// ---------------------------------------------------------------------------
// 2. RuleCore outage drill -> Telemetry/Prometheus + AI Ops Agent triage
// ---------------------------------------------------------------------------
const p2 = await login();
await api('/v1/games/card-game/profile', 'GET', undefined, p2.access_token);
const replayBody = {
  version: 'card-v1',
  seed: 123,
  actions: [2, 2, 2, 2, 2, 2],
  claimed_score: 36,
  request_id: `outage-drill-${Date.now()}`,
  board: 'verified',
  season: 'sandbox',
};

let stoppedRulecore = false;
try {
  execFileSync('docker', ['compose', '-f', 'deploy/compose.yaml', '--profile', 'rules', 'stop', 'rulecore'], {
    stdio: 'inherit',
  });
  stoppedRulecore = true;

  await api('/v1/games/card-game/replays/validate', 'POST', replayBody, p2.access_token, 503);

  const telDown = await api('/gm/system/telemetry', 'GET', undefined, admin);
  assert.equal(telDown.db_ready, true);
  assert.equal(telDown.rulecore_reachable, false);
  assert.ok(telDown.telemetry.rulecore_errors >= 1, 'expected rulecore_errors >= 1 during outage');

  const metricsRes = await fetch(`${base}/gm/system/metrics`, {
    headers: { Authorization: `Bearer ${admin}` },
  });
  assert.equal(metricsRes.status, 200);
  const metricsText = await metricsRes.text();
  assert.ok(metricsText.includes('miniverse_rulecore_errors_total '), 'missing miniverse_rulecore_errors_total');
  assert.ok(metricsText.includes('miniverse_http_latency_p95_ms '), 'missing miniverse_http_latency_p95_ms');
  assert.ok(metricsText.includes('miniverse_http_latency_p99_ms '), 'missing miniverse_http_latency_p99_ms');
  const schemaMatch = metricsText.match(/miniverse_schema_version (\d+)/);
  assert.ok(schemaMatch && Number(schemaMatch[1]) >= 25, `expected schema version >= 25 in Prometheus output, got ${metricsText}`);

  const diagRulecore = await api(
    '/gm/games/card-game/ops-agent/diagnose',
    'POST',
    { question: 'card-game 回放验证接口大量 503 报错，请检查服务健康度与 RuleCore 异常' },
    admin
  );
  assert.ok(
    diagRulecore.steps.some((s) => s.tool_name === 'inspect_service_health'),
    'expected inspect_service_health in RuleCore triage'
  );
  assert.ok(
    diagRulecore.steps.some((s) => s.tool_name === 'query_ops_logs'),
    'expected query_ops_logs in RuleCore triage'
  );
  assert.ok(
    diagRulecore.root_cause.includes('RuleCore') || diagRulecore.summary.includes('rulecore_reachable=false'),
    `expected RuleCore root cause explanation, got: ${diagRulecore.root_cause}`
  );
} finally {
  if (stoppedRulecore) {
    execFileSync('docker', ['compose', '-f', 'deploy/compose.yaml', '--profile', 'rules', 'start', 'rulecore'], {
      stdio: 'inherit',
    });
  }
}

let telRecovered = null;
for (let i = 0; i < 30; i += 1) {
  telRecovered = await api('/gm/system/telemetry', 'GET', undefined, admin);
  if (telRecovered.rulecore_reachable === true) {
    break;
  }
  await sleep(500);
}
assert.equal(telRecovered?.rulecore_reachable, true, 'rulecore did not recover within 15s');
let replayRecovered = false;
for (let i = 0; i < 20; i += 1) {
  const rRes = await fetch(`${base}/v1/games/card-game/replays/validate`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${p2.access_token}`,
    },
    body: JSON.stringify({ ...replayBody, request_id: `recovered-${Date.now()}-${i}` }),
  });
  if (rRes.status === 200) {
    replayRecovered = true;
    break;
  }
  assert.equal(rRes.status, 503, `unexpected replay status during recovery: ${rRes.status}`);
  await sleep(500);
}
assert.equal(replayRecovered, true, 'RuleCore gRPC replay validation did not recover within 10s');
console.log('2. RuleCore outage drill, telemetry/Prometheus metrics & recovery: PASS');

// ---------------------------------------------------------------------------
// 3. Save 409 revision_conflict -> X-Request-ID log correlation & Agent trace
// ---------------------------------------------------------------------------
const saveData = JSON.stringify({ level: 1 });
const saveHash = createHash('sha256').update(saveData).digest('hex');
await api(
  '/v1/games/sky-island/saves/main',
  'PUT',
  { revision: 0, schema_version: 1, data: saveData, checksum: saveHash },
  p1.access_token
);
const conflictRes = await fetch(`${base}/v1/games/sky-island/saves/main`, {
  method: 'PUT',
  headers: {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${p1.access_token}`,
  },
  body: JSON.stringify({ revision: 0, schema_version: 1, data: saveData, checksum: saveHash }),
});
assert.equal(conflictRes.status, 409, 'expected 409 revision_conflict on stale save revision');
const reqId = conflictRes.headers.get('x-request-id');
assert.ok(reqId && /^[0-9a-f]{24}$/.test(reqId), `expected 24-hex X-Request-ID, got ${reqId}`);

const opsEvents = await api(
  `/gm/games/sky-island/ops-events?request_id=${encodeURIComponent(reqId)}`,
  'GET',
  undefined,
  admin
);
assert.ok(
  opsEvents.some((ev) => ev.request_id === reqId && ev.error_code === 'revision_conflict'),
  'ops-events did not record 409 revision_conflict with X-Request-ID'
);

const diagTrace = await api(
  '/gm/games/sky-island/ops-agent/diagnose',
  'POST',
  { question: '请追踪玩家存档写入 409 revision_conflict 的请求链路', request_id: reqId },
  admin
);
assert.ok(diagTrace.steps.some((s) => s.tool_name === 'trace_request'), 'expected trace_request step');
assert.ok(diagTrace.steps.some((s) => s.tool_name === 'query_ops_logs'), 'expected query_ops_logs step');
assert.ok(
  diagTrace.root_cause.includes(reqId) || diagTrace.root_cause.includes('revision_conflict'),
  `expected trace root cause to cite request_id or revision_conflict, got: ${diagTrace.root_cause}`
);
console.log('3. Save conflict X-Request-ID ops-event correlation & trace diagnosis: PASS');

// ---------------------------------------------------------------------------
// 4. MCP HTTP (/gm/mcp) protocol, RBAC filtering, resources & propose_activity_stop
// ---------------------------------------------------------------------------
const initRes = await mcpRPC(admin, 'initialize', {}, 1);
assert.equal(initRes.status, 200);
const initJson = await initRes.json();
assert.equal(initJson.result?.protocolVersion, '2025-03-26');

const notifRes = await mcpRPC(admin, 'notifications/initialized', undefined, undefined);
assert.equal(notifRes.status, 202);

const adminToolsJson = await (await mcpRPC(admin, 'tools/list', {}, 2)).json();
const adminToolNames = adminToolsJson.result?.tools?.map((t) => t.name) ?? [];
for (const expectedTool of [
  'inspect_service_health',
  'diagnose_player',
  'query_ops_logs',
  'trace_request',
  'explain_reward_issue',
  'query_audit_trail',
  'propose_reward_compensation',
  'propose_activity_stop',
  'propose_player_freeze',
]) {
  assert.ok(adminToolNames.includes(expectedTool), `admin tools/list missing ${expectedTool}`);
}

const resourcesJson = await (await mcpRPC(admin, 'resources/list', {}, 3)).json();
const resourceURIs = resourcesJson.result?.resources?.map((r) => r.uri) ?? [];
for (const uri of ['miniverse://health/snapshot', 'miniverse://ops/runbook', 'miniverse://ops/games']) {
  assert.ok(resourceURIs.includes(uri), `resources/list missing ${uri}`);
}
const runbookJson = await (
  await mcpRPC(admin, 'resources/read', { uri: 'miniverse://ops/runbook' }, 4)
).json();
assert.ok(runbookJson.result?.contents?.[0]?.text?.length > 50, 'expected non-empty runbook text');

// RBAC: support cannot see write tools, cannot invoke them, and cannot cross games.
const supportToolsJson = await (await mcpRPC(support, 'tools/list', {}, 5)).json();
assert.ok(
  !supportToolsJson.result?.tools?.some((t) => t.name.startsWith('propose_')),
  'support role should not see propose_* write tools'
);
const supportWriteJson = await (
  await mcpRPC(
    support,
    'tools/call',
    {
      name: 'propose_reward_compensation',
      arguments: { game_id: 'sky-island', player_id: p1.player_id, item: 'wood', amount: 1, reason: 'should fail' },
    },
    6
  )
).json();
assert.equal(supportWriteJson.error?.code, -32602);

const supportCrossGameJson = await (
  await mcpRPC(
    support,
    'tools/call',
    { name: 'diagnose_player', arguments: { game_id: 'card-game', player_id: p2.player_id } },
    7
  )
).json();
assert.equal(supportCrossGameJson.result?.isError, true);
assert.ok(
  supportCrossGameJson.result?.content?.[0]?.text?.includes('game_forbidden'),
  'expected game_forbidden on cross-game MCP call by support'
);

// propose_activity_stop via MCP -> reviewer approve -> verify activity deactivated.
const actStopId = `mcp-stop-act-${Date.now()}`;
await api(
  '/v1/games/sky-island/activities',
  'PUT',
  {
    id: actStopId,
    version: 1,
    kind: 'milestone',
    event_type: actStopId,
    target: 1,
    starts_at: 1,
    ends_at: 4102444800,
    client_allowed: true,
    item: 'wood',
    reward_amount: 5,
  },
  p1.access_token
);
const activeBefore = await api('/v1/games/sky-island/activities', 'GET', undefined, p1.access_token);
assert.ok(activeBefore.some((a) => a.config.id === actStopId), 'expected sandbox activity before stop');

const stopCallJson = await (
  await mcpRPC(
    admin,
    'tools/call',
    {
      name: 'propose_activity_stop',
      arguments: { game_id: 'sky-island', activity_id: actStopId, reason: '异常发奖活动停用演练' },
    },
    8
  )
).json();
assert.equal(stopCallJson.result?.isError, false);
const stopPayload = stopCallJson.result?.structuredContent;
assert.equal(stopPayload?.kind, 'activity_stop');
assert.equal(stopPayload?.state, 'pending');
assert.equal(stopPayload?.requires_human_approval, true);

await api(
  `/gm/games/sky-island/requests/${stopPayload.request_id}/approve`,
  'POST',
  { reason: '核实异常活动，批准停用止损' },
  reviewer
);
const activeAfter = await api('/v1/games/sky-island/activities', 'GET', undefined, p1.access_token);
assert.ok(!activeAfter.some((a) => a.config.id === actStopId), 'expected activity deactivated after approval');
console.log('4. /gm/mcp protocol, RBAC enforcement, resources & propose_activity_stop: PASS');

// ---------------------------------------------------------------------------
// 5. Real /app/mcp stdio-to-/gm/mcp bridge inside Compose api container
// ---------------------------------------------------------------------------
const stdioInput = [
  JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: {} }),
  JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }),
  JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }),
  JSON.stringify({
    jsonrpc: '2.0',
    id: 3,
    method: 'tools/call',
    params: { name: 'inspect_service_health', arguments: { game_id: 'sky-island' } },
  }),
  '',
].join('\n');

const stdioRaw = execFileSync(
  'docker',
  [
    'compose',
    '-f',
    'deploy/compose.yaml',
    'exec',
    '-T',
    '-e',
    'MINIVERSE_API_URL=http://127.0.0.1:8080',
    '-e',
    `MINIVERSE_GM_TOKEN=${admin}`,
    'api',
    '/app/mcp',
  ],
  { input: stdioInput, encoding: 'utf8' }
);

const rpcLines = stdioRaw
  .split(/\r?\n/)
  .map((l) => l.trim())
  .filter(Boolean)
  .map((l) => JSON.parse(l));
assert.equal(rpcLines.length, 3, `expected 3 JSON-RPC replies from /app/mcp stdio, got ${rpcLines.length}`);
assert.equal(rpcLines[0].id, 1);
assert.equal(rpcLines[0].result?.protocolVersion, '2025-03-26');
assert.equal(rpcLines[1].id, 2);
assert.equal(rpcLines[1].result?.tools?.length, 9);
assert.equal(rpcLines[2].id, 3);
assert.equal(rpcLines[2].result?.isError, false);
assert.equal(rpcLines[2].result?.structuredContent?.db_ready, true);
console.log('5. /app/mcp stdio bridge inside api container: PASS');

console.log('OPS_AGENT_MCP_E2E: ALL 5 SCENARIOS PASSED (deterministic_playbook; external LLM unconfigured in sandbox)');
