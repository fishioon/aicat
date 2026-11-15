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
for (const name of ['host.mjs', 'native.mjs', 'validate.mjs', 'shim']) {
  fs.rmSync(path.join(output, name), {recursive: true, force: true});
  fs.cpSync(path.join(source, name), path.join(output, name), {recursive: true, dereference: true});
}
fs.rmSync(path.join(output, 'wecom-local'), {recursive:true, force:true});
const sqliteBuild = path.join(root, 'build/sqlite3mc-stock', process.platform + '-' + process.arch);
execFileSync('python3', [path.join(source, 'sqlite3mc/build.py'), '--output', sqliteBuild], {stdio: 'inherit'});
const sqliteOutput = path.join(output, 'sqlite3mc');
fs.mkdirSync(sqliteOutput, {recursive: true});
for (const name of ['libsqlite3mc.dylib', 'LICENSE', 'manifest.json']) fs.copyFileSync(path.join(sqliteBuild, name), path.join(sqliteOutput, name));
fs.copyFileSync(bun, path.join(output, 'bun.next'));
fs.renameSync(path.join(output, 'bun.next'), path.join(output, 'bun'));
fs.chmodSync(path.join(output, 'bun'), 0o755);
fs.writeFileSync(path.join(output, 'manifest.json'), JSON.stringify({bun: '1.4.4', platform: process.platform, arch: process.arch,
  hostApi: '2026.5.12-aicat-prototype', sqlite3mc: '2.7.0'}) + '\n');
fs.rmSync(path.join(output, 'packages'), {recursive:true, force:true});
fs.rmSync(path.join(output, 'packages.json'), {force:true});
fs.writeFileSync(path.join(output, '.ready'), '');
