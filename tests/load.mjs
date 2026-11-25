import { writeFileSync } from 'node:fs';
import { api, base, login } from './client.mjs';
import { gm } from './gm-client.mjs';

async function runScenario({ name, count, concurrency, request }) {
  const samples = [];
  const errors = [];
  let next = 0;
  const start = performance.now();

  await Promise.all(
    Array.from({ length: concurrency }, async () => {
      while (next++ < count) {
        const t = performance.now();
        try {
          const r = await request();
          if (r.status !== 200) {
            errors.push(r.status);
          }
          await r.arrayBuffer();
        } catch (e) {
          errors.push(e.message);
        }
        samples.push(performance.now() - t);
      }
    }),
  );

  const elapsed = performance.now() - start;
  samples.sort((a, b) => a - b);
  const pick = (q) => samples[Math.min(samples.length - 1, Math.floor(samples.length * q))] || 0;
  return {
    name,
    requests: count,
    concurrency,
    errors: errors.length,
    error_details: errors.slice(0, 10),
    elapsed_ms: Number(elapsed.toFixed(2)),
    requests_per_second: Number((count / (elapsed / 1000)).toFixed(2)),
    p50_ms: Number(pick(0.5).toFixed(2)),
    p95_ms: Number(pick(0.95).toFixed(2)),
    p99_ms: Number(pick(0.99).toFixed(2)),
  };
}

const player = await login();
const admin = await gm('admin');
await api('/v1/games/sky-island/profile', 'GET', undefined, player.access_token);
await api('/v1/games/sky-island/boards/prosperity/sandbox/scores', 'POST', { request_id: 'load-' + Date.now(), score: 10 }, player.access_token);

const scenarios = [
  await runScenario({
    name: 'profile_read_c10',
    count: 100,
    concurrency: 10,
    request: () => fetch(base + '/v1/games/sky-island/profile', {
      headers: { Authorization: 'Bearer ' + player.access_token },
    }),
  }),
  await runScenario({
    name: 'profile_read_c50',
    count: 200,
    concurrency: 50,
    request: () => fetch(base + '/v1/games/sky-island/profile', {
      headers: { Authorization: 'Bearer ' + player.access_token },
    }),
  }),
  await runScenario({
    name: 'leaderboard_read_c25',
    count: 150,
    concurrency: 25,
    request: () => fetch(base + '/v1/games/sky-island/boards/prosperity/sandbox', {
      headers: { Authorization: 'Bearer ' + player.access_token },
    }),
  }),
  await runScenario({
    name: 'gm_audit_trace_c20',
    count: 100,
    concurrency: 20,
    request: () => fetch(base + '/gm/games/sky-island/ops-events?limit=20', {
      headers: { Authorization: 'Bearer ' + admin },
    }),
  }),
];

const report = {
  date: new Date().toISOString(),
  scope: 'local Docker sandbox multi-scenario benchmark, no production capacity claim',
  scenarios,
};
writeFileSync('docs/verification/load-results.json', JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
if (scenarios.some((s) => s.errors > 0)) {
  process.exitCode = 1;
}
