import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {createReadStream, mkdirSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

// Export exactly the images tested by the integration job. No registry login,
// push, remote connection or production deployment occurs here.
const git = (...args) => execFileSync('git', args, {encoding: 'utf8'}).trim();
if (git('status', '--porcelain').length) {
  throw new Error('Image delivery requires a clean checkout so its commit label is accurate.');
}
const commit = git('rev-parse', 'HEAD');
if (!/^[a-f0-9]{40,64}$/.test(commit)) throw new Error('Invalid source commit');
const directory = resolve('work/delivery');
mkdirSync(directory, {recursive: true});
const images = [];
const override = {services: {}};
for (const service of ['api', 'gm', 'player', 'rulecore']) {
  const selected = execFileSync('docker', ['compose', '-f', 'deploy/compose.yaml',
    '--profile', 'rules', '--profile', 'gm', '--profile', 'player', 'images', '-q', service],
  {encoding: 'utf8'}).trim();
  if (!/^(?:sha256:)?[a-f0-9]{64}$/.test(selected)) throw new Error(`Expected one built image for ${service}`);
  const id = execFileSync('docker', ['image', 'inspect', '--format', '{{.Id}}', selected], {encoding: 'utf8'}).trim();
  if (!/^sha256:[a-f0-9]{64}$/.test(id)) throw new Error(`Invalid image ID for ${service}`);
  const tag = `miniverse-${service}:${commit}`;
  execFileSync('docker', ['tag', id, tag], {stdio: 'inherit'});
  override.services[service] = {image: tag};
  images.push({service, tag, id});
}
override.services.migrate = {image: override.services.api.image};
const archive = resolve(directory, 'images.tar');
execFileSync('docker', ['save', '--output', archive, ...images.map(image => image.tag)], {stdio: 'inherit'});
const sha = createHash('sha256');
for await (const chunk of createReadStream(archive)) sha.update(chunk);
writeFileSync(resolve(directory, 'SHA256SUMS'), `${sha.digest('hex')}  images.tar\n`);
writeFileSync(resolve(directory, 'manifest.json'), JSON.stringify({
  commit, created_at: new Date().toISOString(), platform: 'linux/amd64',
  environment: 'sandbox', images,
}, null, 2) + '\n');
writeFileSync(resolve(directory, 'images.override.json'), JSON.stringify(override, null, 2) + '\n');
console.log(`Exported ${images.length} tested sandbox images for ${commit} to ${directory}`);
