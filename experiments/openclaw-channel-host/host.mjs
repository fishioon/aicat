#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import readline from 'node:readline';
import {format} from 'node:util';
import {Store} from './store.mjs';
import {createRuntime} from './runtime.mjs';
import {configure, profiles, packageEntry} from './profiles.mjs';
import {restrictNetwork} from './network.mjs';
import {loadPlugin} from './loader.mjs';

const wire = process.stdout.write.bind(process.stdout);
// Redirect even direct plugin stdout writes before importing any official plugin.
process.stdout.write = process.stderr.write.bind(process.stderr);
for (const name of ['log', 'info', 'warn', 'error', 'debug']) console[name] = (...args) => process.stderr.write(format(...args) + '\n');
const emit = value => wire(JSON.stringify(value) + '\n');
const stateDir = path.resolve(process.argv[2] ?? '');
if (!process.argv[2] || process.argv[3] !== '--synthetic') throw Error('Usage: node host.mjs <isolated-state-dir> --synthetic');
fs.mkdirSync(stateDir, {recursive: true, mode: 0o700});
fs.mkdirSync(path.join(stateDir, 'tmp'), {recursive: true});
process.env.OPENCLAW_STATE_DIR = stateDir;
process.env.OPENCLAW_CONFIG = path.join(stateDir, 'openclaw.json');
process.env.HOME = stateDir;
process.env.TMPDIR = path.join(stateDir, 'tmp');
const lock = path.join(stateDir, 'host.lock');
if (fs.existsSync(lock)) {
  const pid = Number(fs.readFileSync(lock, 'utf8'));
  if (!Number.isInteger(pid) || pid < 1) throw Error('INVALID_STATE_LOCK');
  try { process.kill(pid, 0); throw Error('STATE_IN_USE'); }
  catch (error) { if (error.code !== 'ESRCH') throw error; fs.unlinkSync(lock); }
}
const lockFd = fs.openSync(lock, 'wx', 0o600);
fs.writeFileSync(lockFd, String(process.pid));
const store = new Store(stateDir);
let channel, controller, task, runtimeInfo, network, profile, accountId, config;
let phase = 'stopped', sendEnabled = false, taskError;
const registrations = [];
const status = {running: false};
const lines = readline.createInterface({input: process.stdin, crlfDelay: Infinity});
let queue = Promise.resolve();

async function open(params) {
  if (phase !== 'stopped') throw Error('ALREADY_OPEN');
  profile = profiles[params.kind];
  if (!profile) throw Error('UNKNOWN_PLUGIN');
  network = restrictNetwork(params.endpoint);
  const bound = {kind: params.kind, endpoint: params.endpoint};
  const bindingFile = path.join(stateDir, 'binding.json');
  if (fs.existsSync(bindingFile) && JSON.stringify(JSON.parse(fs.readFileSync(bindingFile))) !== JSON.stringify(bound)) throw Error('STATE_BINDING_MISMATCH');
  fs.writeFileSync(bindingFile, JSON.stringify(bound), {mode: 0o600});
  ({accountId, config} = configure(params.kind, stateDir, params.endpoint));
  fs.writeFileSync(process.env.OPENCLAW_CONFIG, JSON.stringify(config), {mode: 0o600});
  sendEnabled = params.send === true;
  runtimeInfo = createRuntime(store, config, stateDir, status);
  const {plugin, mode: loader} = await loadPlugin(packageEntry(profile), stateDir);
  const api = {
    runtime: runtimeInfo.runtime, config, logger: console, registrationMode: 'full',
    registerChannel({plugin}) { if (channel) throw Error('MULTIPLE_CHANNELS'); channel = plugin; },
    // Intentionally record these registrations; never execute or expose them.
    registerCli: () => registrations.push({kind: 'cli'}),
    registerCommand: value => registrations.push({kind: 'command', name: value.name}),
    registerTool: (...args) => registrations.push({kind: 'tool', name: args[1]?.name}),
    registerHttpRoute: value => registrations.push({kind: 'http', path: value.path}),
    on: name => registrations.push({kind: 'hook', name}),
  };
  await plugin.register(api);
  if (channel?.id !== profile.channel) throw Error('CHANNEL_REGISTRATION_MISMATCH');
  const account = channel.config.resolveAccount(config, accountId);
  if (!account.configured && channel.config.isConfigured?.(account) !== true) throw Error('ACCOUNT_NOT_CONFIGURED');
  controller = new AbortController();
  phase = 'running';
  task = Promise.resolve(channel.gateway.startAccount({
    account, accountId, cfg: config, runtime: {log: console.info, error: console.error},
    channelRuntime: runtimeInfo.runtime.channel, abortSignal: controller.signal,
    setStatus: value => Object.assign(status, value), log: console,
  })).then(() => { if (phase === 'running') {phase = 'error'; taskError = 'PLUGIN_STOPPED_UNEXPECTEDLY';} }, error => {
    taskError = String(error.stack ?? error); phase = 'error'; console.error(taskError);
  });
  return {plugin: profile, loader, accountId, registrations, protocol: 'prototype-ndjson-v1', runtime: {name: process.versions.bun ? 'bun' : 'node', version: process.versions.bun ?? process.version, nodeCompatibility: process.versions.node}};
}
async function stop() {
  sendEnabled = false;
  if (!controller) return {stopped: true};
  phase = 'stopping'; controller.abort();
  await channel.gateway.stopAccount?.({account: channel.config.resolveAccount(config, accountId), accountId, cfg: config});
  let timer;
  try {
    await Promise.race([task, new Promise((_, reject) => {timer = setTimeout(() => reject(Error('PLUGIN_STOP_TIMEOUT')), 5000);})]);
  } finally {clearTimeout(timer);}
  phase = 'stopped'; controller = undefined;
  return {stopped: true, network: network.snapshot(), calls: runtimeInfo.calls};
}
async function send(params) {
  if (params.operation === 'lookup') return store.data.receipts[params.requestId] ?? {status: 'not_found'};
  if (phase !== 'running' || !sendEnabled) throw Error('SEND_DISABLED');
  if (!params.requestId || !params.to || typeof params.text !== 'string' || !params.text || params.text.length > 1000) throw Error('INVALID_SEND');
  const digest = crypto.createHash('sha256').update(JSON.stringify([params.to, params.text])).digest('hex');
  if (store.data.receipts[params.requestId]) {
    const receipt = store.data.receipts[params.requestId];
    if (receipt.digest !== digest) throw Error('REQUEST_ID_CONFLICT');
    return receipt;
  }
  store.data.receipts[params.requestId] = {requestId: params.requestId, digest, status: 'pending'};
  store.save();
  try {
    let timer;
    const result = await Promise.race([
      channel.outbound.sendText({cfg: config, accountId, to: params.to, text: params.text}),
      new Promise((_, reject) => {timer = setTimeout(() => reject(Error('SEND_TIMEOUT')), 3000);}),
    ]).finally(() => clearTimeout(timer));
    Object.assign(store.data.receipts[params.requestId], {status: 'sent', result});
  } catch (error) {
    Object.assign(store.data.receipts[params.requestId], {status: 'unknown', error: String(error.message)});
  }
  store.save();
  return store.data.receipts[params.requestId];
}
async function handle(method, params) {
  if (method === 'open') return open(params);
  if (method === 'status') return {phase, status, taskError, registrations, calls: runtimeInfo?.calls, network: network?.snapshot()};
  if (method === 'read') {
    const cursor = params.cursor ?? 0;
    if (!Number.isInteger(cursor) || cursor < 0 || cursor > store.data.events.length) throw Error('INVALID_CURSOR');
    return {events: store.data.events.slice(cursor), nextCursor: store.data.events.length};
  }
  if (method === 'send') return send(params);
  if (method === 'setSend') {sendEnabled = params.enabled === true; return {sendEnabled};}
  if (method === 'stop') return stop();
  throw Error('UNSUPPORTED_METHOD');
}
lines.on('line', line => {
  queue = queue.then(async () => {
    let request;
    try {
      if (Buffer.byteLength(line) > 65536) throw Error('FRAME_TOO_LARGE');
      request = JSON.parse(line);
      const result = await handle(request.method, request.params ?? {});
      emit({id: request.id, result});
      if (request.method === 'stop') {lines.close(); process.stdin.destroy();}
    } catch (error) {emit({id: request?.id ?? null, error: String(error.message)});}
  });
});
let ending;
function shutdown() {
  return ending ??= queue.then(() => stop()).catch(console.error).finally(() => {
    fs.closeSync(lockFd); fs.unlinkSync(lock);
  });
}
lines.on('close', shutdown);
process.on('SIGTERM', () => {lines.close(); process.stdin.destroy(); shutdown();});
