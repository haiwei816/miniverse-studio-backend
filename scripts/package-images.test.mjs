import assert from 'node:assert/strict';
import test from 'node:test';
import childProcess from 'node:child_process';
import {syncBuiltinESMExports} from 'node:module';
import {mkdtempSync, readFileSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';

const commit = 'a'.repeat(40);
const services = ['api', 'gm', 'player', 'rulecore'];
let attempt = 0;

// Exercise the real exporter, real output files and checksum; replace only the
// Git/Docker boundary so this test never tags host images or exports secrets.
async function fixture({dirty = false, prefixed = false, missing} = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'miniverse-image-test-'));
  const previousCwd = process.cwd(), originalExec = childProcess.execFileSync;
  const calls = [];
  childProcess.execFileSync = (command, args) => {
    calls.push([command, ...args]);
    if (command === 'git') return args[0] === 'status' ? (dirty ? ' M source.go\n' : '') : commit;
    assert.equal(command, 'docker');
    if (args[0] === 'compose') {
      const service = args.at(-1);
      if (service === missing) return '';
      const hex = String(services.indexOf(service) + 1).repeat(64);
      return (prefixed ? 'sha256:' : '') + hex + '\n';
    }
    if (args[0] === 'image') return 'sha256:' + args.at(-1).replace(/^sha256:/, '') + '\n';
    if (args[0] === 'tag') return '';
    assert.equal(args[0], 'save');
    writeFileSync(args[2], 'docker archive fixture');
    return '';
  };
  syncBuiltinESMExports();
  try {
    process.chdir(directory);
    let error;
    try { await import(new URL(`./package-images.mjs?fixture=${++attempt}`, import.meta.url)); }
    catch (caught) { error = caught; }
    return {directory, calls, error};
  } finally {
    process.chdir(previousCwd);
    childProcess.execFileSync = originalExec;
    syncBuiltinESMExports();
  }
}

test('dirty source is rejected before any Docker operation', async () => {
  const result = await fixture({dirty: true});
  assert.match(result.error.message, /clean checkout/);
  assert.equal(result.calls.some(call => call[0] === 'docker'), false);
});

for (const prefixed of [false, true]) {
  test(`exports exactly the selected images with ${prefixed ? 'prefixed' : 'bare'} IDs`, async () => {
    const result = await fixture({prefixed});
    assert.ifError(result.error);
    const out = join(result.directory, 'work/delivery');
    const manifest = JSON.parse(readFileSync(join(out, 'manifest.json')));
    const override = JSON.parse(readFileSync(join(out, 'images.override.json')));
    assert.equal(manifest.commit, commit);
    assert.deepEqual(manifest.images.map(image => image.service), services);
    for (const [index, image] of manifest.images.entries()) {
      assert.equal(image.id, 'sha256:' + String(index + 1).repeat(64));
      assert.equal(override.services[image.service].image, image.tag);
    }
    assert.deepEqual(override.services.migrate, override.services.api);
    const saved = result.calls.find(call => call[1] === 'save');
    assert.deepEqual(saved.slice(4), manifest.images.map(image => image.tag));
    const checksum = createHash('sha256').update(readFileSync(join(out, 'images.tar'))).digest('hex');
    assert.equal(readFileSync(join(out, 'SHA256SUMS'), 'utf8'), `${checksum}  images.tar\n`);
  });
}

test('missing service image cannot produce a partial delivery archive', async () => {
  const result = await fixture({missing: 'player'});
  assert.match(result.error.message, /Expected one built image for player/);
  assert.equal(result.calls.some(call => call[1] === 'save'), false);
});
