import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import { resolve } from 'node:path';

const composeArgs = ['compose', '-f', 'deploy/compose.yaml'];
const verifyDB = 'miniverse_backup_verify';

function mysqlExec(sql, db = '') {
  const dbArg = db ? ` ${db}` : '';
  return execFileSync(
    'docker',
    [...composeArgs, 'exec', '-T', 'mysql', 'sh', '-c', `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --batch --raw --skip-column-names -uroot${dbArg}`],
    { input: sql, encoding: 'utf8', timeout: 60000 },
  ).trim();
}

const migrationFiles = readdirSync(resolve('backend/migrations')).filter((f) => /^\d+_.+\.sql$/.test(f));
assert.ok(migrationFiles.length >= 25, `Expected >=25 migration SQL files, found ${migrationFiles.length}`);

try {
  const dumpSQL = execFileSync(
    'docker',
    [...composeArgs, 'exec', '-T', 'mysql', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -uroot --single-transaction --routines --triggers miniverse'],
    { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, timeout: 120000 },
  );
  assert.ok(dumpSQL.length > 1000, 'mysqldump output unexpectedly small');

  mysqlExec(`DROP DATABASE IF EXISTS \`${verifyDB}\`; CREATE DATABASE \`${verifyDB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;`);
  execFileSync(
    'docker',
    [...composeArgs, 'exec', '-T', 'mysql', 'sh', '-c', `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot ${verifyDB}`],
    { input: dumpSQL, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, timeout: 120000 },
  );

  const schemaVersion = Number(mysqlExec('SELECT MAX(version) FROM schema_migrations;', verifyDB));
  assert.equal(schemaVersion, 25, `Expected restored schema_migrations version 25, got ${schemaVersion}`);

  const migCount = Number(mysqlExec('SELECT COUNT(*) FROM migration_files;', verifyDB));
  assert.equal(migCount, migrationFiles.length, `Expected ${migrationFiles.length} migration_files rows, got ${migCount}`);

  for (const table of ['games', 'admins', 'boards', 'rule_boards']) {
    const srcCount = Number(mysqlExec(`SELECT COUNT(*) FROM \`${table}\`;`, 'miniverse'));
    const dstCount = Number(mysqlExec(`SELECT COUNT(*) FROM \`${table}\`;`, verifyDB));
    assert.equal(dstCount, srcCount, `Row count mismatch on restored table ${table}: src=${srcCount}, dst=${dstCount}`);
    assert.ok(dstCount > 0, `Expected non-empty core table ${table} in restored backup`);
  }

  console.log(`Backup & restore verification: PASS (schema v${schemaVersion}, ${migCount} migrations, core tables verified)`);
} finally {
  try {
    mysqlExec(`DROP DATABASE IF EXISTS \`${verifyDB}\`;`);
  } catch (err) {
    console.error('Cleanup warning:', err.message);
  }
}
