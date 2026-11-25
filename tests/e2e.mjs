import { execFileSync } from 'node:child_process';
import { base } from './client.mjs';

let ready = false;
for (let i = 0; i < 40; i++) {
  try {
    if ((await fetch(base + '/ready')).status === 200) {
      ready = true;
      break;
    }
  } catch {}
  await new Promise((r) => setTimeout(r, 500));
}
if (!ready) throw Error('API readiness timeout');

for (const stage of [
  's0', 's1', 's1-isolation', 's1-expiry', 's3', 's4', 's5',
  's6', 's7', 's8', 's9', 's10', 's11', 's12',
]) {
  console.log('\nRunning ' + stage);
  execFileSync(process.execPath, [`tests/${stage}.mjs`], { stdio: 'inherit' });
}

console.log('\nAll automated HTTP milestone checks passed. Godot/C++ runtime checks are separate.');
