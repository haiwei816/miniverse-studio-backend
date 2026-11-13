import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, cpSync } from 'node:fs';
import { resolve } from 'node:path';
import { randomUUID } from 'node:crypto';

// Uses a newly generated database, not the live sandbox schema. No production use.
const project = process.env.COMPOSE_PROJECT_NAME || 'miniverse-mvp';
const database = `miniverse_upgrade_${randomUUID().replaceAll('-', '').slice(0, 12)}`;
const compose = (...args) => execFileSync('docker', [
  'compose', '-p', project, '-f', 'deploy/compose.yaml', ...args,
], { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });
function sql(text, db = 'miniverse') {
  return execFileSync('docker', [
    'compose', '-p', project, '-f', 'deploy/compose.yaml', 'exec', '-T', 'mysql',
    'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N "$1"', 'mysql', db,
  ], { input: text, encoding: 'utf8' }).trim();
}
mkdirSync('work', { recursive: true });
const fixture = mkdtempSync(resolve('work/mail-upgrade-'));
cpSync('backend/migrations', fixture, {
  recursive: true,
  filter: path => !path.endsWith('013_mail_inventory.sql'),
});
const dsn = `MYSQL_DSN=miniverse:local-development-only@tcp(mysql:3306)/${database}?parseTime=true`;
sql(`CREATE DATABASE ${database}; GRANT ALL ON ${database}.* TO 'miniverse'@'%';`);
let passed = false;
try {
  compose('run', '--rm', '-e', dsn, '-v', `${fixture}:/app/migrations:ro`, 'migrate');
  sql("INSERT INTO inventory VALUES('sky-island','legacy-player','wood',7); " +
    "INSERT INTO grants VALUES('legacy-mail','sky-island','legacy-player','legacy-key','wood',7,1); " +
    "INSERT INTO mail VALUES('legacy-mail','sky-island','legacy-player','legacy-mail','legacy',4102444800,TRUE);", database);
  compose('run', '--rm', '-e', dsn, 'migrate');
  assert.equal(sql('SELECT COUNT(*) FROM migration_files;', database), '13');
  assert.equal(sql("SELECT amount FROM inventory WHERE player_id='legacy-player';", database), '7');
  assert.equal(sql("SELECT delta,balance,reason FROM inventory_ledger WHERE player_id='legacy-player';", database), '7\t7\tmigration_opening');
  assert.equal(sql("SELECT claimed FROM mail WHERE id='legacy-mail';", database), '1');
  compose('run', '--rm', '-e', dsn, 'migrate');
  assert.equal(sql('SELECT COUNT(*) FROM inventory_ledger;', database), '1');
  passed = true;
  console.log('Schema 12 -> 13: legacy inventory/mail preserved, one opening entry, repeated migration does not duplicate: PASS');
} finally {
  // Delete only this test's explicitly generated schema, retain failed fixtures.
  if (passed) sql(`DROP DATABASE ${database};`);
  else console.error(`Upgrade fixture retained for diagnosis: ${database}`);
}
