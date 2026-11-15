import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';

// CI and local verification use the same list. Mutates only the sandbox selected
// by COMPOSE_PROJECT_NAME; API_URL must point at that same sandbox.
const targets = [
  ['api', process.env.API_URL || 'http://127.0.0.1:18080', '/ready'],
  ['gm', process.env.GM_URL || 'http://127.0.0.1:18081', '/'],
  ['player', process.env.PLAYER_URL || 'http://127.0.0.1:18082', '/'],
];
// The suites below deliberately stop dependencies and change sandbox data.
// Check every URL against this project's published ports before any suite runs.
for (const [service, base] of targets) {
  const url = new URL(base);
  assert.ok(url.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)
    && !url.username && !url.password && url.pathname === '/' && !url.search && !url.hash,
  `Expected a local sandbox origin for ${service}`);
  const binding = execFileSync('docker', ['compose', '-f', 'deploy/compose.yaml',
    '--profile', 'rules', '--profile', 'gm', '--profile', 'player', 'port', service, '8080'],
  {encoding: 'utf8', timeout: 15000}).trim();
  assert.equal(url.port || '80', binding.slice(binding.lastIndexOf(':') + 1),
    `${service} URL does not match this Compose project's published port`);
}
for (const [, base, path] of targets) {
  let ready = false;
  for (let attempt = 0; attempt < 60; attempt++) {
    try {
      const response = await fetch(base + path, {signal: AbortSignal.timeout(3000)});
      if (response.ok) { ready = true; break; }
    } catch {}
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  assert.ok(ready, `Sandbox endpoint not ready: ${base}${path}`);
}
for (const suite of [
  'e2e', 'mail-inventory', 'mail-inventory-upgrade', 'player-platform',
  'companion', 'witch-cauldron', 'migrations', 'production-boundary', 'recovery',
]) {
  console.log(`Running ${suite}`);
  execFileSync(process.execPath, [`tests/${suite}.mjs`], {stdio: 'inherit', timeout: 300000});
}
console.log('Sandbox HTTP, migrations, player platform, companion and recovery checks: PASS');
