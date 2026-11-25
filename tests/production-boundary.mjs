import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { api, login } from './client.mjs';
import { gm } from './gm-client.mjs';

const a = await login();
const admin = await gm();
const name = 'miniverse-production-check-' + Date.now();
const network = process.env.COMPOSE_PROJECT_NAME ? process.env.COMPOSE_PROJECT_NAME + '_default' : 'miniverse-mvp_default';
const image = execFileSync('docker', ['compose', '-f', 'deploy/compose.yaml', 'images', '-q', 'api'], { encoding: 'utf8' }).trim();

try {
  execFileSync('docker', [
    'run', '-d', '--rm', '--name', name, '--network', network,
    '-p', '127.0.0.1:18084:8080',
    '-e', 'MYSQL_DSN=miniverse:local-development-only@tcp(mysql:3306)/miniverse?parseTime=true',
    '-e', 'APP_ENV=production',
    '-e', 'HTTP_HOST=0.0.0.0',
    image,
  ]);

  async function check(path, body, status, token) {
    const r = await fetch('http://127.0.0.1:18084' + path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    assert.equal(r.status, status, await r.text());
  }

  for (let i = 0; i < 40; i++) {
    try {
      if ((await fetch('http://127.0.0.1:18084/ready')).status === 200) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 250));
  }

  await check('/v1/auth/guest', {}, 403);
  await check('/v1/platform/steam/login', { game_id: 'card-game', ticket: 'mock' }, 503);
  await check('/gm/login', { username: 'admin', password: 'sandbox-admin' }, 403);
  await check('/gm/games/sky-island/players', undefined, 403, admin);
  await check('/v1/games/sky-island/profile', undefined, 401, a.access_token);

  console.log('Production boundary: sandbox login/credentials/sessions and mock provider disabled: PASS');
} finally {
  execFileSync('docker', ['stop', name]);
}
