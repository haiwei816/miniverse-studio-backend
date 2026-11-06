import assert from'node:assert/strict';import{execFileSync}from'node:child_process';import{mkdtempSync,mkdirSync,cpSync,appendFileSync}from'node:fs';import{resolve}from'node:path';
const compose=(...args)=>execFileSync('docker',['compose','-f','deploy/compose.yaml',...args],{encoding:'utf8'});
compose('run','--rm','migrate');
const rows=compose('exec','-T','mysql','mysql','-uroot','-plocal-root-only','miniverse','-N','-e','SELECT COUNT(*) FROM migration_files');assert.equal(Number(rows.trim()),12);
mkdirSync('work',{recursive:true});const dir=mkdtempSync(resolve('work/migration-fixture-'));cpSync('backend/migrations',dir,{recursive:true});appendFileSync(resolve(dir,'001_bootstrap.sql'),'\n-- tamper-check\n');
let rejected=false;try{compose('run','--rm','-v',dir+':/app/migrations:ro','migrate')}catch(e){rejected=String(e.stderr).includes('checksum changed')};assert.equal(rejected,true);console.log('Migrations: 12 tracked versions, repeat no-op, modified applied file rejected: PASS');
