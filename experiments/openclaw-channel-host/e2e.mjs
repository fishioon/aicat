import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import readline from 'node:readline';
import {spawn} from 'node:child_process';
import {setTimeout as sleep} from 'node:timers/promises';
import {createPlatform} from './fixtures.mjs';
import {profiles} from './profiles.mjs';

const selected = process.argv[2] ?? 'all';
if (!['all', 'weixin', 'wecom'].includes(selected)) throw Error('Usage: npm run e2e -- [all|weixin|wecom]');
const runDir = path.join(import.meta.dirname, 'artifacts', new Date().toISOString().replace(/[:.]/g, '-'));
fs.mkdirSync(runDir, {recursive: true});
const dependencyLock = JSON.parse(fs.readFileSync(path.join(import.meta.dirname, 'package-lock.json')));
const hostDigest = crypto.createHash('sha256').update(fs.readFileSync(path.join(import.meta.dirname, 'host.mjs'))).update(fs.readFileSync(path.join(import.meta.dirname, 'runtime.mjs'))).digest('hex');
const report = {hostDigest, startedAt: new Date().toISOString(), node: process.version, coverage: 'synthetic HTTP/WebSocket; real official plugin packages; no aicat, browser, native desktop or real accounts', scenarios: []};
function fingerprint(profile) {
  const dir = path.join(import.meta.dirname, 'node_modules', profile.package), hash = crypto.createHash('sha256');
  function visit(relative) {
    for (const entry of fs.readdirSync(path.join(dir, relative), {withFileTypes: true}).sort((a,b) => a.name.localeCompare(b.name))) {
      if (entry.name === 'node_modules') continue;
      const file = path.join(relative, entry.name);
      if (entry.isDirectory()) visit(file); else if (entry.isFile()) hash.update(file).update(fs.readFileSync(path.join(dir, file)));
    }
  }
  visit(''); return hash.digest('hex');
}
async function eventually(fn, description, timeout = 10000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {const value = await fn(); if (value) return value; await sleep(50);}
  throw Error(`TIMEOUT: ${description}`);
}
function startHost(directory, kind, number) {
  const log = fs.createWriteStream(path.join(runDir, `${kind}-${number}.stderr.log`));
  const transcript = fs.createWriteStream(path.join(runDir, `${kind}-${number}.ndjson`));
  const child = spawn(process.execPath, [path.join(import.meta.dirname, 'host.mjs'), directory, '--synthetic'], {
    cwd: import.meta.dirname, stdio: ['pipe', 'pipe', 'pipe'],
    env: {HOME: directory, TMPDIR: directory, LANG: 'en_US.UTF-8'},
  });
  child.stderr.pipe(log);
  const pending = new Map(); let next = 0;
  const exit = new Promise(resolve => child.once('exit', (code, signal) => {
    for (const {reject, timer} of pending.values()) {clearTimeout(timer); reject(Error(`HOST_EXIT: ${code}/${signal}`));}
    pending.clear(); transcript.end(); resolve({code, signal});
  }));
  const output = readline.createInterface({input: child.stdout});
  output.on('line', line => {
    transcript.write(`OUT ${line}\n`);
    try {
      const value = JSON.parse(line), call = pending.get(value.id);
      if (!call) throw Error(`UNEXPECTED_RESPONSE: ${line}`);
      clearTimeout(call.timer); pending.delete(value.id);
      if (value.error) call.reject(Error(value.error)); else call.resolve(value.result);
    } catch (error) {for (const call of pending.values()) call.reject(error);}
  });
  return {child, exit, async call(method, params = {}) {
    const id = ++next;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {pending.delete(id); reject(Error(`RPC_TIMEOUT: ${method}`));}, 12000);
      pending.set(id, {resolve, reject, timer});
      const line = JSON.stringify({id, method, params}); transcript.write(`IN ${line}\n`);
      child.stdin.write(line + '\n');
    });
  }};
}
async function scenario(kind) {
  const profile = profiles[kind], hashBefore = fingerprint(profile);
  const result = {kind, plugin: profile, hostDigest, npmIntegrity: dependencyLock.packages[`node_modules/${profile.package}`].integrity, hashBefore, assertions: [], processes: [], policyChecks: []};
  report.scenarios.push(result);
  const check = (description, condition) => {assert.ok(condition, description); result.assertions.push(description);};
  const platform = await createPlatform(kind);
  const explicitDeliveries = () => platform.deliveries.filter(value => {
    const text = value.msg?.item_list?.[0]?.text_item?.text ?? value.body?.markdown?.content ?? '';
    return text.startsWith('synthetic ');
  });
  const state = path.join(runDir, kind, 'state');
  fs.mkdirSync(state, {recursive: true});
  let host, count = 0;
  async function launch() {
    host = startHost(state, kind, ++count); result.processes.push(host.child.pid);
    await host.call('open', {kind, endpoint: platform.endpoint, send: true});
    await eventually(() => platform.connected, 'platform connection');
    await eventually(async () => (await host.call('status')).phase === 'running', 'host running');
  }
  async function graceful() {
    const stopped = await host.call('stop');
    check('plugin task settles after abort', stopped.stopped);
    check('no outbound network attempts outside fixture', stopped.network.blocked.length === 0);
    let timer;
    const exit = await Promise.race([host.exit, new Promise((_, reject) => {timer = setTimeout(() => reject(Error('HOST_NOT_EXITED')), 6500);})]).finally(() => clearTimeout(timer));
    check('host exits naturally with code 0', exit.code === 0 && exit.signal === null);
    const requests = platform.requests.length;
    await sleep(200);
    check('no platform requests after exit', platform.requests.length === requests);
  }
  try {
    host = startHost(state, kind, ++count); result.processes.push(host.child.pid);
    await sleep(150);
    check('no platform access before open', platform.requests.length === 0);
    await assert.rejects(host.call('open', {kind, endpoint: 'https://example.invalid', send: true}), /SYNTHETIC_LOOPBACK_ENDPOINT_REQUIRED/);
    check('non-loopback configuration rejected before plugin import', platform.requests.length === 0);
    const opened = await host.call('open', {kind, endpoint: platform.endpoint, send: true});
    check('official plugin registered requested channel', opened.plugin.channel === profile.channel);
    await eventually(() => platform.connected, 'official SDK connected');
    platform.publish();
    const read = await eventually(async () => {const value = await host.call('read'); return value.events.length ? value : null;}, 'official inbound reaches durable host buffer');
    const event = read.events[0];
    check('plugin-normalized text arrives through shared dispatch', event.context.Body.includes('synthetic hello'));
    check('message is already persisted before read returns', JSON.parse(fs.readFileSync(path.join(state, 'host.json'))).events.length === 1);
    await sleep(200);
    result.policyChecks.push({name: 'no unsolicited platform output after intake', passed: platform.deliveries.length === 0, observed: structuredClone(platform.deliveries)});
    await host.call('setSend', {enabled: false});
    const beforeDisabledSend = platform.deliveries.length;
    await assert.rejects(host.call('send', {requestId: 'disabled', to: event.context.To, text: 'blocked'}), /SEND_DISABLED/);
    check('send switch rejects explicit send without platform delivery', platform.deliveries.length === beforeDisabledSend);
    const beforeDisabledInbound = platform.deliveries.length;
    platform.publish('incoming-disabled');
    await eventually(async () => (await host.call('read')).events.length === 2, 'inbound while send disabled');
    await sleep(250);
    result.policyChecks.push({name: 'send disabled also prevents plugin-originated output', passed: platform.deliveries.length === beforeDisabledInbound, observed: platform.deliveries.slice(beforeDisabledInbound)});
    await host.call('setSend', {enabled: true});
    const send = {requestId: 'reply-1', to: event.context.To, text: 'synthetic delayed reply'};
    const receipt = await host.call('send', send);
    check('official outbound sends reply', receipt.status === 'sent' && explicitDeliveries().length === 1);
    if (kind === 'weixin') check('original context_token reaches Weixin endpoint', explicitDeliveries()[0].msg.context_token === 'synthetic-context-token');
    if (kind === 'wecom') check('official SDK sends aibot_send_msg', explicitDeliveries()[0].cmd === 'aibot_send_msg');
    await host.call('send', send);
    check('same requestId does not send twice', explicitDeliveries().length === 1);
    await assert.rejects(host.call('send', {...send, text: 'conflict'}), /REQUEST_ID_CONFLICT/);
    check('conflicting payload rejected', explicitDeliveries().length === 1);
    const observed = await host.call('status');
    result.calls = observed.calls; result.registrations = observed.registrations;
    check('same runtime records routing, session and dispatch', observed.calls['inbound.dispatch'] === 2 && observed.calls['session.recordInboundSession'] === 2);
    await graceful();
    await launch();
    const restored = await host.call('read');
    check('durable inbound survives host restart', restored.events.length === 2);
    await host.call('send', send);
    check('sent receipt survives restart without resend', explicitDeliveries().length === 1);
    await host.call('send', {...send, requestId: 'reply-2', text: 'synthetic after restart'});
    check('official outbound works after restart', explicitDeliveries().length === 2);
    if (kind === 'weixin') check('plugin restores persisted context_token', explicitDeliveries()[1].msg.context_token === 'synthetic-context-token');
    platform.publish();
    await sleep(500);
    check('same platform message ID is not archived twice', (await host.call('read')).events.length === 2);
    // Kill after the platform receives a send, before the host can persist a final receipt.
    platform.hold(true);
    const unresolved = host.call('send', {...send, requestId: 'crash-pending', text: 'synthetic unacknowledged'}).catch(() => undefined);
    await eventually(() => explicitDeliveries().length === 3, 'unacknowledged send reaches platform');
    host.child.kill('SIGKILL'); await host.exit; await unresolved;
    platform.hold(false);
    await launch();
    const unknown = await host.call('send', {operation: 'lookup', requestId: 'crash-pending'});
    check('in-flight send becomes unknown after crash', unknown.status === 'unknown');
    await host.call('send', {...send, requestId: 'crash-pending', text: 'synthetic unacknowledged'});
    check('unknown requestId is never automatically resent', explicitDeliveries().length === 3);
    await graceful();
    result.hashAfter = fingerprint(profile);
    check('installed official plugin files are unchanged', result.hashAfter === hashBefore);
    result.textTransportPassed = true; result.policyCompatible = result.policyChecks.every(check => check.passed); result.passed = true;
  } catch (error) {
    result.passed = false; result.error = String(error.stack); throw error;
  } finally {
    if (host && host.child.exitCode === null && host.child.signalCode === null) {host.child.kill('SIGKILL'); await host.exit;}
    result.platformRequests = platform.requests.length;
    fs.writeFileSync(path.join(runDir, `${kind}.platform.json`), JSON.stringify({requests: platform.requests, deliveries: platform.deliveries}, null, 2));
    await platform.close();
  }
}
try {
  for (const kind of selected === 'all' ? ['weixin', 'wecom'] : [selected]) await scenario(kind);
  report.harnessPassed = true; report.textTransportPassed = true; report.policyCompatible = report.scenarios.every(s => s.policyCompatible);
} catch (error) {report.harnessPassed = false; report.textTransportPassed = false; console.error(error); process.exitCode = 1;}
finally {
  report.finishedAt = new Date().toISOString();
  report.status = !report.harnessPassed ? 'execution-failed' : report.policyCompatible ? 'text-prototype-verified' : 'text-transport-verified-with-policy-failures';
  fs.writeFileSync(path.join(runDir, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({harnessPassed: report.harnessPassed, textTransportPassed: report.textTransportPassed, policyCompatible: report.policyCompatible, report: path.join(runDir, 'report.json'), assertions: report.scenarios.map(s => ({kind: s.kind, count: s.assertions.length, error: s.error}))}, null, 2));
}
