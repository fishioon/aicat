// Registration-only compatibility probe. Does not start an account or an Agent.
import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {restrictNetwork} from './network.mjs';
import {createRuntime} from './runtime.mjs';
import {Store} from './store.mjs';
import {loadPlugin} from './loader.mjs';

const wire = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const directory = path.join(import.meta.dirname, 'artifacts', 'registration-probe', String(process.pid));
fs.mkdirSync(path.join(directory, 'tmp'), {recursive: true});
process.env.OPENCLAW_STATE_DIR = directory;
process.env.HOME = directory;
process.env.TMPDIR = path.join(directory, 'tmp');
const network = restrictNetwork('http://127.0.0.1:1');
const name = process.argv[2];
const root = path.join(import.meta.dirname, 'node_modules', name);
const metadata = JSON.parse(fs.readFileSync(path.join(root, 'package.json')));
const entry = metadata.openclaw?.runtimeExtensions?.[0] ?? metadata.openclaw?.extensions?.[0] ?? metadata.main;
const registrations = [];
const config = {channels: {}, tools: {}};
const {runtime} = createRuntime(new Store(directory), config, directory, {});
try {
  const {plugin, mode: loader} = await loadPlugin(pathToFileURL(path.join(root, entry)).href, directory);
  await plugin.register({runtime, config, logger: console, registrationMode: 'full',
    registerChannel: () => registrations.push('channel'), registerTool: () => registrations.push('tool'),
    registerHttpRoute: () => registrations.push('http'),
    registerCli: () => registrations.push('cli'), registerCommand: () => registrations.push('command'), on: () => registrations.push('hook'),
  });
  wire(JSON.stringify({registered: true, loader, name, version: metadata.version, entry, registrations, network: network.snapshot()})+'\n');
} catch (error) {
  wire(JSON.stringify({registered: false, name, version: metadata.version, entry, error: String(error.stack), network: network.snapshot()})+'\n');
  process.exitCode = 1;
}
