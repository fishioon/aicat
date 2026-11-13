import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../..', import.meta.url));
const source = path.join(root, 'runtime/channel-host');
const output = path.join(root, 'bin/channel-host');
const bun = process.env.AICAT_BUILD_BUN || execFileSync('/bin/sh', ['-c', 'command -v bun'], {encoding: 'utf8'}).trim();
if (execFileSync(bun, ['--version'], {encoding: 'utf8'}).trim() !== '1.4.4') throw Error('Channel runtime requires Bun 1.4.4; set AICAT_BUILD_BUN to the build-time binary');
fs.mkdirSync(output, {recursive: true});
fs.rmSync(path.join(output,'node_modules'),{recursive:true,force:true});
for (const name of ['host.mjs', 'validate.mjs', 'shim', 'wecom-local']) {
  fs.rmSync(path.join(output, name), {recursive: true, force: true});
  fs.cpSync(path.join(source, name), path.join(output, name), {recursive: true, dereference: true});
}
fs.copyFileSync(bun, path.join(output, 'bun.next'));
fs.renameSync(path.join(output, 'bun.next'), path.join(output, 'bun'));
fs.chmodSync(path.join(output, 'bun'), 0o755);
fs.writeFileSync(path.join(output, 'manifest.json'), JSON.stringify({bun: '1.4.4', platform: process.platform, arch: process.arch,
  hostApi: '2026.5.12-aicat-prototype'}) + '\n');
fs.writeFileSync(path.join(output, '.ready'), '');
