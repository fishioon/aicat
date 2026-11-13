import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
import crypto from 'node:crypto';
import {AsyncLocalStorage} from 'node:async_hooks';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
const pluginEntry=process.argv[3];
const {WSClient,WSAuthFailureError}=createRequire(pluginEntry)('@wecom/aibot-node-sdk');

const write = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const silent = () => {};
for (const key of ['log', 'info', 'warn', 'error', 'debug']) console[key] = silent;
const emit = value => write(JSON.stringify({jsonrpc: '2.0', ...value}) + '\n');
const root = path.resolve(process.argv[2]);
fs.mkdirSync(root, {recursive: true, mode: 0o700});
const lock = path.join(root, 'host.lock');
if (fs.existsSync(lock)) {
  const pid = Number(fs.readFileSync(lock, 'utf8'));
  if (!Number.isInteger(pid) || pid < 1) throw Error('INVALID_HOST_LOCK');
  try {process.kill(pid, 0); throw Error('HOST_ALREADY_RUNNING');}
  catch (error) {if (error.code !== 'ESRCH') throw error; fs.unlinkSync(lock);}
}
const lockFd = fs.openSync(lock, 'wx', 0o600);
fs.writeFileSync(lockFd, String(process.pid));
process.env.OPENCLAW_STATE_DIR = root;
process.env.OPENCLAW_CONFIG = path.join(root, 'openclaw.json');
process.env.TMPDIR = path.join(root, 'tmp');
fs.mkdirSync(process.env.TMPDIR, {recursive: true, mode: 0o700});
const file = path.join(root, 'unconfirmed.json');
const pending = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : {};
const accounts = new Map();
const config = {tools: {}, channels: {wecom: {enabled: true, accounts: {}}}};
const permittedSend = new AsyncLocalStorage();
const rawTimes = new Map();
const logger = {info: silent, debug: silent, warn: silent, error: silent, log: silent};
let closing = false;
function save() {
  const fd = fs.openSync(file + '.tmp', 'w', 0o600);
  try {fs.writeFileSync(fd, JSON.stringify(pending)); fs.fsyncSync(fd);} finally {fs.closeSync(fd);}
  fs.renameSync(file + '.tmp', file);
  const dir = fs.openSync(root, 'r');
  try {fs.fsyncSync(dir);} finally {fs.closeSync(dir);}
}
function state(account, phase, error = '') {
  account.phase = phase;
  account.error = error;
  emit({method: 'channel_status', params: {accountId: account.id, session: account.session, phase, error}});
}
function push(entry) {
  const account = accounts.get(entry.accountId);
  if (!account || account.source !== entry.event.source || account.closing) return;
  emit({id: entry.event.id, method: 'message_push', params: {accountId: account.id, session: account.session, event: entry.event}});
}
function ingest(account, room, message, msgtime = null, kind = 'text') {
  if (!account || account.closing || !room || !message.msgid) throw Error('INVALID_INBOUND');
  const event = {specversion: '1.0', id: crypto.randomUUID(), source: account.source,
    type: 'org.aicat.message.received.v1', datacontenttype: 'application/json', time: new Date().toISOString(),
    data: {msgid:message.msgid,action:'send',from:message.from,tolist:[account.botId],roomid:room,msgtype:kind,text:{content:message.text ?? ''},...(msgtime ? {msgtime} : {})}};
  const entry = {event,accountId:account.id};
  if (Buffer.byteLength(JSON.stringify(pending)) + Buffer.byteLength(JSON.stringify(entry)) > 256 * 1024 * 1024) {
    state(account, 'error', 'UNCONFIRMED_QUEUE_FULL');
    account.controller.abort();
    throw Error('UNCONFIRMED_QUEUE_FULL');
  }
  pending[event.id] = entry;
  save();
  push(entry);
}
for (const method of ['reply', 'sendMessage', 'uploadMedia']) {
  const original = WSClient.prototype[method];
  if (typeof original !== 'function') throw Error('SDK_SEND_INTERFACE_CHANGED');
  WSClient.prototype[method] = function (...args) {
    const permission = permittedSend.getStore();
    const account = accounts.get(permission?.accountId);
    if (!permission || !account || account.closing || account.botId !== this.options.botId) {
      return Promise.reject(Error('HOST_AUTOMATIC_SEND_DISABLED'));
    }
    return original.apply(this, args);
  };
}
const originalEmit = WSClient.prototype.emit;
WSClient.prototype.emit = function (name, ...args) {
  const account = [...accounts.values()].find(value => value.botId === this.options.botId);
  if (account && !account.closing && !account.blocked) {
    if (name === 'authenticated') state(account, 'connected');
    if (name === 'reconnecting' || name === 'disconnected') state(account, 'connecting');
    if (name === 'error') {
      account.blocked=args[0] instanceof WSAuthFailureError;
      state(account, account.blocked ? 'blocked' : 'error', account.blocked ? 'AUTHENTICATION_FAILED' : 'PLATFORM_CONNECTION_ERROR');
    }
    if (name === 'event.disconnected_event') {account.blocked=true;state(account, 'blocked', 'ACCOUNT_CONNECTED_ELSEWHERE');}
    if (name === 'message') {
      const body = args[0]?.body;
      if (body?.chattype !== 'single' || body?.msgtype !== 'text') {
        if (body?.msgid && (body.chatid || body.from?.userid)) ingest(account, body.chatid || body.from.userid,
          {msgid: body.msgid, from: body.from?.userid, text: 'Unsupported message type'}, null, 'unsupported');
        return false;
      }
      const rawTime = Number(body.create_time);
      const timestamp = rawTime < 1e12 ? rawTime * 1000 : rawTime;
      rawTimes.set(JSON.stringify([account.id, body.msgid]), Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null);
      if (rawTimes.size > 1000) rawTimes.delete(rawTimes.keys().next().value);
    }
  }
  return originalEmit.call(this, name, ...args);
};
const reply = {
  finalizeInboundContext: value => value,
  resolveEnvelopeFormatOptions: () => ({}), formatAgentEnvelope: ({body}) => body,
  resolveHumanDelayConfig: () => ({mode: 'off'}),
  dispatchReplyWithBufferedBlockDispatcher: async ({ctx}) => {
    const key = JSON.stringify([ctx.AccountId, ctx.MessageSid]);
    ingest(accounts.get(ctx.AccountId), ctx.To.replace(/^wecom:/, ''),
      {msgid: ctx.MessageSid, from: ctx.SenderId, text: ctx.RawBody ?? ctx.Body}, rawTimes.get(key) ?? null);
    rawTimes.delete(key);
    return {queuedFinal: false, counts: {tool: 0, block: 0, final: 0}};
  },
};
const runtime = {
  version: '2026.5.12', config: {loadConfig: () => config, current: () => config},
  logging: {getChildLogger: () => logger},
  channel: {reply, routing: {resolveAgentRoute: ({accountId, peer}) => ({agentId: 'aicat', accountId,
    sessionKey: JSON.stringify([accountId, peer.id]), mainSessionKey: JSON.stringify([accountId, peer.id])})},
    session: {resolveStorePath: () => path.join(root, 'sessions'), recordInboundSession: async () => {}},
    commands: {shouldComputeCommandAuthorized: () => false, isControlCommandMessage: () => false,
      shouldHandleTextCommands: () => false, resolveCommandAuthorizedFromAuthorizers: () => false},
    text: {chunkMarkdownText: (text, limit = 4000) => Array.from({length: Math.ceil(text.length / limit)}, (_, i) => text.slice(i * limit, (i + 1) * limit))},
    pairing: {readAllowFromStore: async () => []}},
};
const {default: plugin} = await import(pathToFileURL(pluginEntry).href);
let channel;
await plugin.register({runtime, config, logger, registrationMode: 'full',
  registerChannel: value => {channel = value.plugin;}, registerTool: silent, registerCli: silent,
  registerCommand: silent, registerHttpRoute: silent, on: silent});
if (channel?.id !== 'wecom') throw Error('CHANNEL_REGISTRATION_MISMATCH');
function closeAccount(account) {
 return account.stopping ??= (async()=>{
  account.closing = true;
  account.controller.abort();
  await channel.gateway.stopAccount?.({account: channel.config.resolveAccount(config, account.id), accountId: account.id, cfg: config});
  await account.task;
  accounts.delete(account.id);
  delete config.channels.wecom.accounts[account.id];
 })();
}
async function handle(method, params) {
  if (closing) throw Error('HOST_CLOSING');
  if (method === 'channel_open') {
    if (!params.accountId || !params.session || !params.botId || !params.secret || !params.source) throw Error('INVALID_ACCOUNT');
    if (accounts.has(params.accountId) || [...accounts.values()].some(a => a.botId === params.botId)) throw Error('ACCOUNT_ALREADY_OPEN');
    const endpoint = new URL(params.endpoint || 'wss://openws.work.weixin.qq.com');
    if (endpoint.href !== 'wss://openws.work.weixin.qq.com/' &&
      !(endpoint.protocol === 'ws:' && endpoint.hostname === '127.0.0.1')) throw Error('INVALID_PLATFORM_ENDPOINT');
    const account = {id: params.accountId, session: params.session, botId: params.botId, source: params.source,
      controller: new AbortController(), closing: false};
    config.channels.wecom.accounts[account.id] = {enabled: true, botId: params.botId, secret: params.secret,
      websocketUrl: endpoint.href, dmPolicy: 'open', allowFrom: ['*'], groupPolicy: 'disabled',
      sendThinkingMessage: false, dynamicAgents: {enabled: false}};
    accounts.set(account.id, account);
    const resolved = channel.config.resolveAccount(config, account.id);
    account.task = Promise.resolve().then(()=>channel.gateway.startAccount({account: resolved, accountId: account.id, cfg: config,
      runtime: logger, channelRuntime: runtime.channel, abortSignal: account.controller.signal,
      setStatus: value => {if (value.lastError && !account.closing && !account.blocked) state(account, 'error', 'PLATFORM_ACCOUNT_ERROR');}, log: logger
    })).then(() => {if (!account.closing && !account.blocked) state(account, 'failed', 'PLUGIN_STOPPED');}, () => {if (!account.closing && !account.blocked) state(account, 'failed', 'PLUGIN_FAILED');});
    return {opened: true};
  }
  const account = accounts.get(params.accountId);
  if (!account || account.session !== params.session || account.closing) throw Error('ACCOUNT_SESSION_CLOSED');
  if (method === 'channel_close') {await closeAccount(account); return {closed:true};}
  if (method === 'channel_send') {
    const event = params.event;
    if (account.phase !== 'connected') throw Error('ACCOUNT_NOT_CONNECTED');
    if (event?.type !== 'org.aicat.message.send.requested.v1' ||
      event.data?.msgtype !== 'text' || typeof event.data.text.content !== 'string' || !event.data.roomid) throw Error('INVALID_SEND');
    return permittedSend.run({accountId: account.id}, () => channel.outbound.sendText({cfg: config, accountId: account.id,
      to: event.data.roomid, text: event.data.text.content}));
  }
  throw Error('UNSUPPORTED_METHOD');
}
const lines = readline.createInterface({input: process.stdin, crlfDelay: Infinity});
lines.on('line', line => {
  let request;
  try {
    if (Buffer.byteLength(line) > 2 * 1024 * 1024) throw Error('FRAME_TOO_LARGE');
    request = JSON.parse(line);
    if (!request.method) {
      const entry = pending[request.id];
      const ack = request.result;
      if (entry && ack?.status === 'accepted' && ack.source === entry.event.source && ack.id === entry.event.id) {
        delete pending[request.id]; save();
      }
      return;
    }
    Promise.resolve(handle(request.method, request.params ?? {})).then(result => emit({id: request.id, result}),
      error => emit({id: request.id, error: {code: -32000, message: /^[A-Z_]+$/.test(error.message) ? error.message : 'CHANNEL_OPERATION_FAILED'}}));
  } catch {emit({id: request?.id ?? null, error: {code: -32700, message: 'INVALID_FRAME'}});}
});
let migrated=false;
for (const entry of Object.values(pending)) {
  const data=entry.event?.data;
  if (!entry.accountId && data?.account && data?.message) {
    entry.accountId=data.account;
    const message=data.message;
    entry.event.data={msgid:message.msgid,action:'send',from:message.from,roomid:data.room,
      msgtype:message.msgtype,text:{content:message.text ?? ''},...(message.msgtime ? {msgtime:message.msgtime} : {})};
    migrated=true;
  }
}
if(migrated) save();
const retry = setInterval(() => {for (const entry of Object.values(pending)) push(entry);}, 2000);
let ending;
function shutdown() {
  return ending ??= (async () => {
    closing = true; clearInterval(retry);
    await Promise.allSettled([...accounts.values()].map(closeAccount));
    fs.closeSync(lockFd); fs.unlinkSync(lock);
    process.exit(0);
  })();
}
lines.on('close', shutdown);
process.on('SIGTERM', shutdown);
