import assert from'node:assert/strict';import{execFileSync}from'node:child_process';import{mkdtempSync,mkdirSync,cpSync,appendFileSync,readdirSync}from'node:fs';import{resolve}from'node:path';
const compose=(...args)=>execFileSync('docker',['compose','-f','deploy/compose.yaml',...args],{encoding:'utf8'});
compose('run','--rm','migrate');
const expected=readdirSync('backend/migrations').filter(name=>/^\d+_.+\.sql$/.test(name)).sort();
const rows=compose('exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot miniverse -N -e "SELECT name FROM migration_files ORDER BY name"');assert.deepEqual(rows.trim().split(/\s+/),expected);
mkdirSync('work',{recursive:true});const dir=mkdtempSync(resolve('work/migration-fixture-'));cpSync('backend/migrations',dir,{recursive:true});appendFileSync(resolve(dir,'001_bootstrap.sql'),'\n-- tamper-check\n');
let rejected=false;try{compose('run','--rm','-v',dir+':/app/migrations:ro','migrate')}catch(e){rejected=String(e.stderr).includes('checksum changed')};assert.equal(rejected,true);console.log(`Migrations: ${expected.length} tracked versions, repeat no-op, modified applied file rejected: PASS`);
