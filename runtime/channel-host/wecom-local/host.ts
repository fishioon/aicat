import fs from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { Sender } from "./sender";
import { Source, digest, type LocalConfig, type Position } from "./source";

const root = path.resolve(process.argv[2]);
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const emit = (frame: unknown) =>
  process.stdout.write(
    JSON.stringify({ jsonrpc: "2.0", ...(frame as object) }) + "\n",
  );
const lockFile = path.join(root, "host.lock");
if (fs.existsSync(lockFile)) {
  const pid = Number(fs.readFileSync(lockFile, "utf8"));
  if (!Number.isInteger(pid) || pid < 1) throw Error("INVALID_HOST_LOCK");
  try {
    process.kill(pid, 0);
    throw Error("HOST_ALREADY_RUNNING");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
    fs.unlinkSync(lockFile);
  }
}
const lock = fs.openSync(lockFile, "wx", 0o600);
fs.writeFileSync(lock, String(process.pid));
const pending = new Map<string, (accepted: boolean) => void>();
let account:
  | { accountId: string; session: string; source: string; local: LocalConfig }
  | undefined;
let source: Source | undefined;
let sender: Sender | undefined;
let sending = false;
let cursor: Position = { position: "0", anchor: "" };
let closed = false;
let timer: ReturnType<typeof setTimeout> | undefined;
let cursorFile = "";
let identity = "";
function status(phase: string, error = "") {
  if (account)
    emit({
      method: "channel_status",
      params: { ...accountIdentity(), phase, error },
    });
}
function accountIdentity() {
  return { accountId: account!.accountId, session: account!.session };
}
function store(fileName: string, value: unknown) {
  const file = fileName + ".tmp";
  const fd = fs.openSync(file, "w", 0o600);
  try {
    fs.writeFileSync(fd, JSON.stringify(value));
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  fs.renameSync(file, fileName);
  const dir = fs.openSync(root, "r");
  try {
    fs.fsyncSync(dir);
  } finally {
    fs.closeSync(dir);
  }
}
function save(next: Position) {
  store(cursorFile, { version: 1, identity, ...next });
}
async function poll() {
  if (closed || !source || !account) return;
  let delay = 1000;
  try {
    const rows = source.updates(cursor);
    status("connected");
    for (const row of rows) {
      if (closed) break;
      const event = {
        specversion: "1.0",
        id: digest(account.source + "\0" + row.message.msgid),
        source: account.source,
        type: "org.aicat.message.received.v1",
        datacontenttype: "application/json",
        data: row.message,
      };
      const accepted = await new Promise<boolean>((resolve) => {
        pending.set(event.id, resolve);
        emit({
          id: event.id,
          method: "message_push",
          params: { ...accountIdentity(), event },
        });
        const timeout = setTimeout(() => {
          pending.delete(event.id);
          resolve(false);
        }, 5000);
        pending.set(event.id, (value) => {
          clearTimeout(timeout);
          resolve(value);
        });
      });
      if (!accepted || closed) break;
      save(row.cursor);
      cursor = row.cursor;
    }
    if (rows.length) delay = 10;
  } catch (error) {
    const message = error instanceof Error ? error.message : "";
    status(
      "error",
      /^[A-Z][A-Z0-9_]+$/.test(message)
        ? message
        : "LOCAL_DATABASE_READ_FAILED",
    );
  }
  if (!closed) timer = setTimeout(poll, delay);
}
function close() {
  closed = true;
  if (timer) clearTimeout(timer);
  for (const resolve of pending.values()) resolve(false);
  pending.clear();
  sender?.close();
  sender = undefined;
  source?.close();
  source = undefined;
}
const lines = readline.createInterface({ input: process.stdin });
lines.on("line", (line) => {
  let frame: any;
  try {
    frame = JSON.parse(line);
    if (!frame.method) {
      const resolve = pending.get(frame.id);
      if (resolve) {
        pending.delete(frame.id);
        resolve(
          frame.result?.source === account?.source &&
            frame.result?.id === frame.id &&
            frame.result?.status === "accepted",
        );
      }
      return;
    }
    if (frame.method === "channel_open") {
      if (account || closed) throw Error("ACCOUNT_ALREADY_OPEN");
      const params = frame.params;
      const local: LocalConfig = { ...params.local, key: params.secret };
      const candidate = new Source(local);
      candidate.refresh();
      account = {
        accountId: params.accountId,
        session: params.session,
        source: params.source,
        local,
      };
      source = candidate;
      sender = new Sender(params.sendScript);
      identity = digest(local.accountId + "\0" + path.normalize(local.dataDir));
      cursorFile = path.join(root, "cursor-" + identity + ".json");
      const existingCursor = fs.existsSync(cursorFile);
      if (existingCursor) {
        const stored = JSON.parse(fs.readFileSync(cursorFile, "utf8"));
        if (
          stored.version !== 1 ||
          stored.identity !== identity ||
          typeof stored.position !== "string" ||
          typeof stored.anchor !== "string"
        )
          throw Error("INVALID_SAVED_CURSOR");
        cursor = { position: stored.position, anchor: stored.anchor };
      }
      const formatFile = path.join(
        root,
        "message-format-" + identity + ".json",
      );
      if (!fs.existsSync(formatFile)) {
        store(formatFile, {
          version: 1,
          metadataAfter: existingCursor ? candidate.latestPosition() : "0",
        });
      }
      const format = JSON.parse(fs.readFileSync(formatFile, "utf8"));
      if (format.version !== 1 || typeof format.metadataAfter !== "string")
        throw Error("INVALID_MESSAGE_FORMAT_STATE");
      candidate.useMessageMetadataAfter(format.metadataAfter);
      emit({
        id: frame.id,
        result: { opened: true, position: candidate.latestPosition() },
      });
      timer = setTimeout(poll, 0);
    } else if (frame.method === "channel_send") {
      if (
        !account ||
        closed ||
        sending ||
        frame.params?.accountId !== account.accountId ||
        frame.params?.session !== account.session
      )
        throw Error("LOCAL_SEND_NOT_READY");
      sending = true;
      sender!
        .send(frame.params.event)
        .then(
          (result) => emit({ id: frame.id, result }),
          (error) =>
            emit({
              id: frame.id,
              error: {
                message:
                  error instanceof Error
                    ? error.message.slice(0, 1200)
                    : "LOCAL_SEND_SCRIPT_FAILED",
              },
            }),
        )
        .finally(() => {
          sending = false;
        });
    } else if (frame.method === "channel_close") {
      close();
      emit({ id: frame.id, result: { closed: true } });
    } else throw Error("LOCAL_CHANNEL_READ_ONLY");
  } catch (error) {
    const message = error instanceof Error ? error.message : "";
    emit({
      id: frame?.id,
      error: {
        message: /^[A-Z][A-Z0-9_]+$/.test(message)
          ? message
          : "LOCAL_DATABASE_OPEN_FAILED",
      },
    });
  }
});
function exit() {
  close();
  fs.closeSync(lock);
  if (fs.readFileSync(lockFile, "utf8") === String(process.pid))
    fs.unlinkSync(lockFile);
  process.exit(0);
}
lines.on("close", exit);
process.on("SIGTERM", exit);
process.on("SIGINT", exit);
