import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { api, login } from './client.mjs';

const a = await login();
const h = createHash('sha256').update(a.access_token).digest('hex');

execFileSync('docker', [
  'compose', '-f', 'deploy/compose.yaml', 'exec', '-T', 'mysql',
  'mysql', '-uroot', '-plocal-root-only', 'miniverse',
  '-e', `UPDATE sessions SET expires_at=0 WHERE token_hash='${h}'`,
]);

await api('/v1/games/sky-island/profile', 'GET', undefined, a.access_token, 401);
console.log('S1 expired token: PASS');
