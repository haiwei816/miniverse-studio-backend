import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { api, login } from './client.mjs';
import { gm } from './gm-client.mjs';

// Disposable sandbox fixtures only. Use COMPOSE_PROJECT_NAME and API_URL to
// target the same isolated Compose project. Never run against production.
const project = process.env.COMPOSE_PROJECT_NAME || 'miniverse-mvp';
function sql(text) {
  return execFileSync('docker', [
    'compose', '-p', project, '-f', 'deploy/compose.yaml', 'exec', '-T',
    'mysql', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot miniverse -N',
  ], { input: text, encoding: 'utf8' }).trim();
}
function seedMail(player, count, amount = 10, expires = 4102444800) {
  const prefix = randomUUID().replaceAll('-', '').slice(0, 16);
  const ids = Array.from({ length: count }, (_, i) => `${prefix}-${i}`);
  sql(`INSERT INTO grants VALUES ${ids.map(id =>
    `('${id}','sky-island','${player}','fixture:${id}','wood',${amount},UNIX_TIMESTAMP())`
  ).join(',')};`);
  sql(`INSERT INTO mail VALUES ${ids.map(id =>
    `('${id}','sky-island','${player}','${id}','test reward',${expires},FALSE)`
  ).join(',')};`);
  return ids;
}

const a = await login();
const b = await login();
const token = a.access_token;
const base = '/v1/games/sky-island';
await api(`${base}/profile`, 'GET', undefined, token);
const ids = seedMail(a.player_id, 3);
let mail = await api(`${base}/mail?filter=unread`, 'GET', undefined, token);
assert.equal(mail.length, 3);
await api(`${base}/mail/${ids[0]}/read`, 'POST', {}, b.access_token, 404);
await api(`/v1/games/card-game/mail/${ids[0]}/read`, 'POST', {}, token, 404);
await api(`${base}/mail/${ids[0]}`, 'DELETE', undefined, token, 409);
await api(`${base}/mail/${ids[0]}/read`, 'POST', {}, token);
await api(`${base}/mail/${ids[0]}/read`, 'POST', {}, token);
assert.equal((await api(`${base}/mail?filter=unread`, 'GET', undefined, token)).length, 2);

await Promise.all([
  ...Array.from({ length: 6 }, () => api(`${base}/mail/claim-all`, 'POST', {}, token)),
  api(`${base}/mail/${ids[0]}/claim`, 'POST', {}, token),
]);
assert.equal((await api(`${base}/inventory`, 'GET', undefined, token)).wood, 30);
mail = await api(`${base}/mail`, 'GET', undefined, token);
assert.ok(mail.every(v => v.read && v.claimed));
assert.equal((await api(`${base}/mail/claim-all`, 'POST', {}, token)).count, 0);
await api(`${base}/mail/${ids[0]}`, 'DELETE', undefined, token);
await api(`${base}/mail/${ids[0]}`, 'DELETE', undefined, token);
assert.equal((await api(`${base}/mail`, 'GET', undefined, token)).length, 2);
await api(`${base}/mail/${ids[0]}/claim`, 'POST', {}, token, 404);
await api(`${base}/mail/${ids[0]}/read`, 'POST', {}, token, 404);
const [expired] = seedMail(a.player_id, 1, 999, 1);
assert.equal((await api(`${base}/mail/claim-all`, 'POST', {}, token)).count, 0);
await api(`${base}/mail/${expired}/claim`, 'POST', {}, token, 410);
await api(`${base}/mail/${expired}`, 'DELETE', undefined, token);

// More than one page with equal timestamps, including deletion of a cursor row.
const pager = await login();
const pageIDs = seedMail(pager.player_id, 205, 1, 1);
const seen = [];
let mailCursor = '';
let firstPage = true;
do {
  const page = await api(`${base}/mail/page?limit=50${mailCursor ? `&cursor=${encodeURIComponent(mailCursor)}` : ''}`, 'GET', undefined, pager.access_token);
  assert.ok(page.items.length <= 50);
  seen.push(...page.items.map(v => v.id));
  mailCursor = page.next_cursor;
  if (firstPage) {
    await api(`${base}/mail/${page.items.at(-1).id}`, 'DELETE', undefined, pager.access_token);
    firstPage = false;
  }
} while (mailCursor);
assert.equal(seen.length, 205);
assert.deepEqual(new Set(seen), new Set(pageIDs));
await api(`${base}/mail/page?cursor=invalid!`, 'GET', undefined, pager.access_token, 400);
assert.equal((await api(`${base}/mail/page`, 'GET', undefined, b.access_token)).items.length, 0);

const consume = { request_id: randomUUID(), item: 'wood', amount: 7 };
const replays = await Promise.all(Array.from({ length: 8 }, () =>
  api(`${base}/inventory/consume`, 'POST', consume, token)));
assert.ok(replays.every(v => v.balance === 23));
await api(`${base}/inventory/consume`, 'POST', { ...consume, amount: 8 }, token, 409);
await api(`${base}/inventory/consume`, 'POST', { ...consume, item: 'gem' }, token, 409);
await api(`${base}/inventory/consume`, 'POST', { ...consume, player_id: b.player_id }, token, 400);
await api(`${base}/inventory/consume`, 'POST', { ...consume, amount: -1 }, token, 400);
await api(`${base}/inventory/consume`, 'POST', { ...consume, amount: 0 }, token, 400);
await api(`${base}/inventory/consume`, 'POST', { ...consume, request_id: randomUUID(), item: 'unknown' }, token, 400);
const requests = Array.from({ length: 8 }, () => ({ ...consume, request_id: randomUUID() }));
const concurrent = await Promise.allSettled(requests.map(body =>
  api(`${base}/inventory/consume`, 'POST', body, token)));
assert.equal(concurrent.filter(v => v.status === 'fulfilled').length, 3);
for (const result of concurrent.filter(v => v.status === 'rejected')) {
  assert.match(String(result.reason), /insufficient_items/);
}
assert.equal((await api(`${base}/inventory`, 'GET', undefined, token)).wood, 2);
const retry = { ...consume, request_id: randomUUID(), amount: 1000 };
await api(`${base}/inventory/consume`, 'POST', retry, token, 409);
await api(`${base}/inventory/consume`, 'POST', { ...retry, amount: 1 }, token);
assert.equal((await api(`${base}/inventory`, 'GET', undefined, token)).wood, 1);
// A successful idempotent replay returns its original result, not today's balance.
assert.equal((await api(`${base}/inventory/consume`, 'POST', consume, token)).balance, 23);

let cursor = '';
const entries = [];
do {
  const page = await api(`${base}/inventory/ledger?limit=2${cursor ? `&before=${cursor}` : ''}`, 'GET', undefined, token);
  assert.ok(page.entries.length <= 2);
  entries.push(...page.entries);
  cursor = page.next_cursor;
} while (cursor);
assert.equal(entries.length, 8);
assert.equal(new Set(entries.map(v => v.id)).size, entries.length);
assert.equal(entries.reduce((sum, v) => sum + v.delta, 0), 1);
assert.equal(entries.filter(v => v.reason === 'mail_claim').length, 3);
assert.ok(entries.every(v => v.balance >= 0));
assert.equal((await api(`${base}/inventory/ledger`, 'GET', undefined, b.access_token)).entries.length, 0);
assert.equal((await api('/v1/games/card-game/inventory/ledger', 'GET', undefined, token)).entries.length, 0);
assert.equal(Object.keys(await api(`${base}/inventory`, 'GET', undefined, b.access_token)).length, 0);
await api(`${base}/inventory/consume`, 'POST', { ...consume, request_id: randomUUID(), amount: 1 }, b.access_token, 409);
await api(`${base}/inventory/ledger?before=invalid`, 'GET', undefined, token, 400);
await api(`${base}/mail?limit=0`, 'GET', undefined, token, 400);
await api(`${base}/mail?filter=invalid`, 'GET', undefined, token, 400);

sql("INSERT IGNORE INTO items VALUES('sky-island','apple-fixture','Apple'),('sky-island','zebra-fixture','Zebra');");
const catalog = [];
cursor = '';
do {
  const page = await api(`${base}/items?limit=1${cursor ? `&after=${encodeURIComponent(cursor)}` : ''}`, 'GET', undefined, token);
  catalog.push(...page.items);
  cursor = page.next_cursor;
} while (cursor);
assert.equal(new Set(catalog.map(v => v.id)).size, catalog.length);
assert.equal(catalog.find(v => v.id === 'wood').amount, 1);
assert.equal(catalog.find(v => v.id === 'apple-fixture').amount, 0);

const admin = await gm();
const detail = await api(`/gm/games/sky-island/players/${a.player_id}`, 'GET', undefined, admin);
assert.equal(Number(detail.inventory.find(v => v.item === 'wood').amount), 1);
assert.equal(detail.inventory_ledger.length, 8);
assert.ok(detail.mail.some(v => v.id === ids[0] && Number(v.deleted) === 1));

const bulk = await login();
seedMail(bulk.player_id, 101, 1);
const first = await api(`${base}/mail/claim-all`, 'POST', {}, bulk.access_token);
assert.equal(first.count, 100);
assert.equal(first.has_more, true);
const second = await api(`${base}/mail/claim-all`, 'POST', {}, bulk.access_token);
assert.equal(second.count, 1);
assert.equal(second.has_more, false);
assert.equal((await api(`${base}/inventory`, 'GET', undefined, bulk.access_token)).wood, 101);

// A failure on the second credit must roll back the first mail and its ledger.
const overflow = await login();
seedMail(overflow.player_id, 2, 10);
sql(`INSERT INTO inventory VALUES('sky-island','${overflow.player_id}','wood',9223372036854775792);`);
await api(`${base}/mail/claim-all`, 'POST', {}, overflow.access_token, 409);
assert.ok((await api(`${base}/mail`, 'GET', undefined, overflow.access_token)).every(v => !v.claimed));
assert.equal((await api(`${base}/inventory/ledger`, 'GET', undefined, overflow.access_token)).entries.length, 0);
assert.equal(sql(`SELECT amount FROM inventory WHERE game_id='sky-island' AND player_id='${overflow.player_id}' AND item='wood';`), '9223372036854775792');
console.log('Mail/inventory: read/delete/expiry, atomic claim-all, concurrent dedupe/consume, nonnegative balances, ledger, pagination and isolation: PASS');
