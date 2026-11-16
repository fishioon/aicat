import fs from "node:fs";
import path from "node:path";
import readline from "node:readline";
import crypto from "node:crypto";
import { pathToFileURL } from "node:url";

const root = path.resolve(process.argv[2]);
const entry = process.argv[3];
const output = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const emit = (value) => output(JSON.stringify({ jsonrpc: "2.0", ...value }) + "\n");
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const lockFile=path.join(root,"host.lock");
if(fs.existsSync(lockFile)) {
  const pid=Number(fs.readFileSync(lockFile,"utf8"));
  if(!Number.isInteger(pid) || pid<1) throw Error("INVALID_HOST_LOCK");
  try {process.kill(pid,0);throw Error("HOST_ALREADY_RUNNING");}
  catch(error) {if(error.code!=="ESRCH") throw error;fs.unlinkSync(lockFile);}
}
const lock=fs.openSync(lockFile,"wx",0o600);
fs.writeFileSync(lock,String(process.pid));
const pendingFile = path.join(root, "unconfirmed.json");
const pending = fs.existsSync(pendingFile)
  ? JSON.parse(fs.readFileSync(pendingFile, "utf8")) : {};
const accounts = new Map();
const waiters = new Map();
let closing = false;

function savePending() {
  const file = pendingFile + ".next";
  const fd = fs.openSync(file, "w", 0o600);
  try { fs.writeFileSync(fd, JSON.stringify(pending)); fs.fsyncSync(fd); }
  finally { fs.closeSync(fd); }
  fs.renameSync(file, pendingFile);
  const dir = fs.openSync(root, "r");
  try { fs.fsyncSync(dir); } finally { fs.closeSync(dir); }
}

function pushStored(record) {
  const account = accounts.get(record.accountId);
  if (!account || account.closing || account.binding.source !== record.event.source) return;
  emit({ id: record.event.id, method: "message_push", params: {
    accountId: record.accountId, session: account.binding.session, event: record.event,
  } });
}

async function open(binding) {
  if (!binding.accountId || !binding.session || !binding.source || accounts.has(binding.accountId))
    throw Error("INVALID_ACCOUNT_BINDING");
  const account = { binding, closing: false, controller: new AbortController() };
  accounts.set(binding.accountId, account);
  const directory = path.join(root, crypto.createHash("sha256").update(binding.accountId).digest("hex"));
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  const status = (phase, error = "") => {
    if (!account.closing) emit({ method: "channel_status", params: {
      accountId: binding.accountId, session: binding.session, phase, error,
    } });
  };
  function enqueue(event) {
    const wait = () => new Promise((resolve, reject) => {
      const list = waiters.get(event.id) ?? [];
      list.push({resolve, reject}); waiters.set(event.id, list);
    });
        const key = crypto.createHash("sha256").update(binding.source + "\0" + event.id).digest("hex");
        const record = { accountId: binding.accountId, event };
        if (Buffer.byteLength(JSON.stringify(event)) > 256 * 1024) throw Error("CHANNEL_MESSAGE_TOO_LARGE");
        if (pending[key]) {
          if (JSON.stringify(pending[key].event.data) !== JSON.stringify(event.data))
            throw Error("MESSAGE_ID_CONFLICT");
          const acknowledged = wait();
          pushStored(pending[key]);
          return acknowledged;
        }
        if (Buffer.byteLength(JSON.stringify(pending)) + Buffer.byteLength(JSON.stringify(record)) > 32 * 1024 * 1024)
          throw Error("UNCONFIRMED_QUEUE_FULL");
        pending[key] = record;
        savePending();
        const acknowledged = wait();
        pushStored(record);
        return acknowledged;
  }
  try {
    const module = await import(pathToFileURL(entry).href);
    if (typeof module.createChannel !== "function") throw Error("INVALID_CHANNEL_ENTRY");
    account.plugin = await module.createChannel({
      stateDirectory: directory, signal: account.controller.signal,
      configuration: binding.configuration ?? {}, native: binding.nativeContext ?? {}, status,
      updateRoom(presentation) {
        if (account.closing || closing || typeof presentation?.roomid !== "string" || !presentation.roomid)
          throw Error("INVALID_ROOM_PRESENTATION");
        const data = JSON.parse(JSON.stringify(presentation));
        const id = crypto.createHash("sha256").update(binding.source + "\0presentation\0" + JSON.stringify(data)).digest("hex");
        return enqueue({ specversion: "1.0", id, source: binding.source,
          type: "org.aicat.room.presentation.updated.v1", datacontenttype: "application/json", data });
      },
      publish(message) {
        if (account.closing || closing || !message?.msgid || !message?.roomid)
          throw Error("INVALID_CHANNEL_MESSAGE");
        if (typeof message.msgid !== "string" || typeof message.roomid !== "string" || message.roomid.length>256 || message.reply_id || message.delivery ||
            (message.msgtime != null && (!Number.isSafeInteger(message.msgtime) || message.msgtime<0 || message.msgtime>253402300799999)) ||
            (message.summary != null && (typeof message.summary!=="string" || Buffer.byteLength(message.summary)>8192)) ||
            (message.agent_snapshot != null && (message.outgoing || message.msgtype!=="text" || typeof message.context_key!=="string" || !message.context_key.trim() || typeof message.agent_snapshot.content!=="string" || !message.agent_snapshot.content.trim() || Buffer.byteLength(message.agent_snapshot.content)>65536)))
          throw Error("INVALID_CHANNEL_MESSAGE");
        const eventId = crypto.createHash("sha256").update(binding.source + "\0" + message.roomid + "\0" + message.msgid).digest("hex");
        const event = { specversion: "1.0", id: eventId, source: binding.source,
          type: "org.aicat.message.received.v1", datacontenttype: "application/json",
          time: message.msgtime != null ? new Date(message.msgtime).toISOString() : new Date().toISOString(),
          data: { ...message, action: "receive" },
        };
        return enqueue(event);
      },
    });
    if (!account.plugin || typeof account.plugin.open !== "function") throw Error("INVALID_CHANNEL_LIFECYCLE");
    const result = await account.plugin.open();
    status("connected");
    return result ?? { opened: true };
  } catch (error) {
    account.closing = true;
    account.controller.abort();
    await account.plugin?.close?.();
    accounts.delete(binding.accountId);
    throw error;
  }
}

async function close(account) {
  if (account.stopping) return account.stopping;
  account.closing = true;
  account.controller.abort();
  for(const record of Object.values(pending)) {
    if(record.accountId!==account.binding.accountId) continue;
    for(const waiter of waiters.get(record.event.id) ?? []) waiter.reject(Error("ACCOUNT_SESSION_CLOSED"));
    waiters.delete(record.event.id);
  }
  account.stopping = Promise.resolve().then(() => account.plugin?.close?.()).finally(() => {
    accounts.delete(account.binding.accountId);
  });
  return account.stopping;
}

async function handle(method, params) {
  if (closing) throw Error("HOST_CLOSING");
  if (method === "channel_open") return open(params);
  const account = accounts.get(params.accountId);
  if (!account || account.closing || account.binding.session !== params.session)
    throw Error("ACCOUNT_SESSION_CLOSED");
  if (method === "channel_close") { await close(account); return { closed: true }; }
  if (method === "channel_send") {
    if (typeof account.plugin.send !== "function") throw Error("CHANNEL_SEND_UNSUPPORTED");
    return account.plugin.send(params.event);
  }
  if (method === "channel_observe") {
    if (typeof account.plugin.observe !== "function") throw Error("CHANNEL_OBSERVE_UNSUPPORTED");
    return (await account.plugin.observe(params.event)) ?? {};
  }
  throw Error("UNSUPPORTED_METHOD");
}

const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
lines.on("line", (line) => {
  let request;
  try {
    if (Buffer.byteLength(line) > 4 * 1024 * 1024) throw Error("FRAME_TOO_LARGE");
    request = JSON.parse(line);
    if (!request.method) {
      const ack = request.result;
      if (request.error) {
        for (const waiter of waiters.get(request.id) ?? []) waiter.reject(Error("ROOM_RECEIVE_FAILED"));
        waiters.delete(request.id);
        return;
      }
      for (const [key, record] of Object.entries(pending)) {
        if (record.event.id === request.id && ack?.status === "accepted" &&
            ack.source === record.event.source && ack.id === record.event.id) {
          delete pending[key];
          try {savePending();} catch(error) {pending[key]=record;throw error;}
          for (const waiter of waiters.get(record.event.id) ?? []) waiter.resolve(ack);
          waiters.delete(record.event.id);
          break;
        }
      }
      return;
    }
    Promise.resolve(handle(request.method, request.params ?? {})).then(
      (result) => emit({ id: request.id, result }),
      (error) => emit({ id: request.id, error: { code: -32000, message:
        /^[A-Z_]+$/.test(error.message) ? error.message : "CHANNEL_OPERATION_FAILED" } }),
    );
  } catch { emit({ id: request?.id ?? null, error: { code: -32700, message: "INVALID_FRAME" } }); }
});
const retry = setInterval(() => Object.values(pending).forEach(pushStored), 2000);
let stopping;
function shutdown() {
  return stopping ??= (async () => {
    closing = true;
    clearInterval(retry);
    for (const list of waiters.values()) for (const waiter of list) waiter.reject(Error("HOST_CLOSING"));
    waiters.clear();
    await Promise.allSettled([...accounts.values()].map(close));
    fs.closeSync(lock);
    if(fs.existsSync(lockFile) && fs.readFileSync(lockFile,"utf8")===String(process.pid)) fs.unlinkSync(lockFile);
    process.exit(0);
  })();
}
lines.on("close", shutdown);
process.on("SIGTERM", shutdown);
