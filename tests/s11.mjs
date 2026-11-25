import assert from 'node:assert/strict';
import { writeFileSync, mkdirSync } from 'node:fs';
import { api } from './client.mjs';
import { gm } from './gm-client.mjs';

const admin = await gm();
const reviewer = await gm('reviewer');
const base = '/gm/games/sky-island';
const results = [];

for (const brief of [
  '每日升级建筑3次奖励5木材',
  '完成对局2次奖励8木材',
  '每日签到奖励3木材',
  '访问3次奖励4木材 [simulate-invalid]',
]) {
  const v = await api(base + '/planner/generate', 'POST', { brief }, admin);
  assert.equal(v.provider, 'deterministic_mock');
  assert.equal(v.review_status, 'draft');
  assert.equal(v.valid, true);
  results.push(v);
}

const v = results.at(-1);
assert.equal(v.attempts, 2);
await api(base + '/planner/validate', 'POST', { config: { ...v.config, script: 'injection' }, events: [] }, admin, 400);
await api(base + '/planner/validate', 'POST', { config: { ...v.config, item: 'unknown' }, events: [] }, admin, 400);

const guest = await api('/v1/auth/guest', 'POST', {});
assert.ok(!(await api('/v1/games/sky-island/activities', 'GET', undefined, guest.access_token)).some((x) => x.config.id === v.config.id));

const id = 'planner-' + v.run_id;
await api(base + '/requests', 'POST', { id, kind: 'activity', reason: 'review generated plan', payload: v.config }, admin);
await api(base + `/requests/${id}/approve`, 'POST', { reason: 'independent plan review' }, reviewer);

const report = {
  provider: 'deterministic_mock',
  real_ai: 'UNVERIFIED',
  samples: results.length,
  first_pass_rate: results.filter((x) => x.first_pass).length / results.length,
  final_pass_rate: 1,
  repaired: results.filter((x) => x.attempts > 1).length,
  cost_usd: 0,
  latencies_ms: results.map((x) => x.latency_ms),
};
mkdirSync('work/ci', { recursive: true });
writeFileSync('work/ci/planner-evaluation.json', JSON.stringify(report, null, 2));

console.log('S11 schema/business validation, mock generation/repair/simulation, independent publication approval: PASS; REAL AI UNVERIFIED');
