const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const net = require("node:net");
const crypto = require("node:crypto");
const { spawn, execFileSync } = require("node:child_process");
const assert = require("node:assert/strict");
const { WebSocketServer } = require("ws");

const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
async function wait(fn, ms = 20000) {
  const deadline = Date.now() + ms;
  let last;
  while (Date.now() < deadline) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (error) {
      last = error;
    }
    await pause(50);
  }
  throw Error("timeout: " + (last?.stack || fn.toString()));
}
const alive = (pid) => {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};
const agentScenario = process.argv[4] === "agent";
const sourceBinary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] ||
    "build/wecom-e2e-" + new Date().toISOString().replace(/[:.]/g, "-"),
);
fs.mkdirSync(root, { recursive: true });
const bundle = path.join(root, "aicat.app", "Contents");
fs.mkdirSync(path.join(bundle, "MacOS"), { recursive: true });
fs.mkdirSync(path.join(bundle, "Resources"), { recursive: true });
const binary = path.join(bundle, "MacOS", "aicat");
fs.copyFileSync(sourceBinary, binary);
fs.chmodSync(binary, 0o755);
fs.writeFileSync(
  path.join(bundle, "Info.plist"),
  fs
    .readFileSync("build/darwin/Info.plist", "utf8")
    .replaceAll("@VERSION@", "0.1.0")
    .replace("ai.netcat.aicat", "ai.netcat.aicat.e2e.wecom"),
);
fs.cpSync(
  path.join(path.dirname(sourceBinary), "channel-host"),
  path.join(bundle, "Resources", "channel-host"),
  { recursive: true, dereference: true },
);
const home = path.join(root, "home");
fs.mkdirSync(home, { recursive: true, mode: 0o700 });
const secret = "synthetic-secret-private-e2e";
const report = {
  passed: false,
  checks: [],
  pids: [],
  binarySHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  command: process.argv,
};
function treeDigest(dir) {
  const hash = crypto.createHash("sha256");
  function visit(relative) {
    for (const name of fs.readdirSync(path.join(dir, relative)).sort()) {
      const entry = path.join(relative, name),
        full = path.join(dir, entry);
      if (fs.statSync(full).isDirectory()) visit(entry);
      else {
        hash.update(entry);
        hash.update(fs.readFileSync(full));
      }
    }
  }
  visit("");
  return hash.digest("hex");
}
const packagedRuntime = path.join(bundle, "Resources/channel-host");
const pluginPath = path.join(
  home,
  "extensions/wecom/2026.10.30",
  "node_modules/@wecom/wecom-openclaw-plugin",
);
const sdkPath = path.join(
  home,
  "extensions/wecom/2026.10.30",
  "node_modules/@wecom/aibot-node-sdk",
);
report.runtime = {
  manifest: JSON.parse(
    fs.readFileSync(path.join(packagedRuntime, "manifest.json")),
  ),
  bunSHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(path.join(packagedRuntime, "bun")))
    .digest("hex"),
};
const frames = [],
  deliveries = [],
  sockets = new Set(),
  errors = [];
let rejectAuthentication = false;
let hold = false,
  holdAt = Infinity,
  proc,
  browser,
  page,
  preview,
  token;
const check = (name) => report.checks.push(name);
const server = http.createServer((req, res) => {
  res.writeHead(404);
  res.end();
});
const wss = new WebSocketServer({ server });
wss.on("connection", (ws) => {
  sockets.add(ws);
  ws.on("close", () => sockets.delete(ws));
  ws.on("message", (raw) => {
    const frame = JSON.parse(raw);
    frames.push(
      JSON.parse(JSON.stringify(frame).replaceAll(secret, "[redacted]")),
    );
    if (frame.cmd === "aibot_subscribe") {
      assert.equal(frame.body.bot_id, "synthetic-bot");
      assert.equal(frame.body.secret, secret);
      if (rejectAuthentication) {
        ws.send(
          JSON.stringify({
            headers: frame.headers,
            errcode: 40001,
            errmsg: "invalid credentials",
          }),
        );
        return;
      }
    }
    if (["aibot_send_msg", "aibot_respond_msg"].includes(frame.cmd)) {
      deliveries.push(frame);
      if (hold || deliveries.length === holdAt) return;
    }
    ws.send(
      JSON.stringify({ headers: frame.headers, errcode: 0, errmsg: "ok" }),
    );
  });
});
async function rpc(method, args = []) {
  const response = await fetch(preview + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
  });
  const value = await response.json();
  if (!response.ok) throw Error(value.error);
  return value;
}
async function state() {
  return (await rpc("State")).messageChannels.wecom;
}
async function start() {
  const previewFile = path.join(root, "preview-url");
  fs.rmSync(previewFile, { force: true });
  const fd = fs.openSync(path.join(root, "app.log"), "a");
  proc = spawn(binary, ["--home", home], {
    stdio: ["ignore", fd, fd],
    env: {
      ...process.env,
      PATH: "/usr/bin:/bin",
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_PREVIEW: previewFile,
      AICAT_E2E_OS_STATE: path.join(root, "os-state.json"),
      AICAT_E2E_WECOM_URL: "ws://127.0.0.1:" + server.address().port,
    },
  });
  fs.closeSync(fd);
  report.pids.push(proc.pid);
  await wait(() => {
    if (proc.exitCode !== null || proc.signalCode !== null)
      throw Error(
        "App exited before preview: " + (proc.exitCode ?? proc.signalCode),
      );
    return fs.existsSync(previewFile);
  });
  const url = new URL(fs.readFileSync(previewFile, "utf8"));
  preview = url.origin;
  token = url.searchParams.get("token");
  await wait(() => rpc("State"));
  if (!page) {
    page = await browser.newPage({ viewport: { width: 1180, height: 1000 } });
    page.on("pageerror", (e) => errors.push(e.message));
  }
  await page.goto(url.href);
  await page.waitForFunction(() => !!window.desktop?.state());
  await page.locator("#nav-extensions").click();
  await page.locator("#extension-wecom").click();
}
async function manage() {
  if (await page.locator("#extension-diagnostics-configure").count())
    await page.locator("#extension-diagnostics-configure").click();
  else if (!(await page.locator("#feature-wecom").count())) {
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom").click();
  }
}
async function configure() {
  await manage();
  await page.locator("#wecom-settings").waitFor();
}
async function diagnostics() {
  await manage();
  await page.locator("#extension-diagnostics").click();
  await page.locator("#wecom-diagnostics").waitFor();
}
async function toggle() {
  await manage();
  await page.locator("#feature-wecom").click();
}
async function stop() {
  if (!proc || proc.exitCode !== null || proc.signalCode !== null) return;
  const exited = new Promise((resolve) => proc.once("exit", resolve));
  proc.kill("SIGTERM");
  const graceful = await Promise.race([
    exited.then(() => true),
    pause(15000).then(() => false),
  ]);
  if (!graceful) {
    proc.kill("SIGKILL");
    await exited;
    throw Error("Application did not stop within 15 seconds");
  }
}
function publish(id, user = "alice", type = "text", timestamp) {
  const body = {
    msgid: id,
    create_time:
      timestamp === undefined ? undefined : Math.floor(timestamp / 1000),
    chattype: "single",
    from: { userid: user },
    msgtype: type,
    text: { content: "hello " + id },
    image: { url: "https://must-not-fetch.invalid/image" },
  };
  for (const socket of sockets)
    socket.send(
      JSON.stringify({
        cmd: "aibot_msg_callback",
        headers: { req_id: "req-" + id },
        body,
      }),
    );
}
function event(id, text = "MCP outgoing") {
  return {
    specversion: "1.0",
    id,
    source: "aicat-agent://mcp",
    type: "org.aicat.message.send.requested.v1",
    datacontenttype: "application/json",
    data: {
      action: "send",
      roomid: "alice",
      msgtype: "text",
      text: { content: text },
    },
  };
}
async function mcp(outgoing, auth = true) {
  const address = JSON.parse(JSON.stringify(await rpc("State"))).gateway.addr;
  const bearer = fs.readFileSync(path.join(home, "mcp-token"), "utf8").trim();
  const response = await fetch("http://" + address + "/mcp", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      "MCP-Protocol-Version": "2025-06-18",
      ...(auth ? { Authorization: "Bearer " + bearer } : {}),
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: crypto.randomUUID(),
      method: "tools/call",
      params: {
        name: "messages_send",
        arguments: { channel: "wecom", event: outgoing },
      },
    }),
  });
  if (response.status !== 200) return { httpStatus: response.status };
  const body = await response.json();
  assert(!body.error, JSON.stringify(body));
  return body.result;
}
const receipt = (result) =>
  result.structuredContent || JSON.parse(result.content[0].text);
const logs = () =>
  fs.existsSync(path.join(home, "rooms"))
    ? fs.readdirSync(path.join(home, "rooms")).map((dir) => ({
        dir,
        records: fs
          .readFileSync(path.join(home, "rooms", dir, "messages.jsonl"), "utf8")
          .trim()
          .split("\n")
          .filter(Boolean)
          .map(JSON.parse),
      }))
    : [];
async function runAgent() {
  const fixture = path.join(root, "codex-fixture");
  fs.writeFileSync(
    fixture,
    "#!" +
      process.execPath +
      "\n" +
      fs.readFileSync("e2e/fixtures/codex-agent.cjs", "utf8"),
    { mode: 0o700 },
  );
  const trace = () =>
    fs.existsSync(path.join(root, "codex-trace.jsonl"))
      ? fs
          .readFileSync(path.join(root, "codex-trace.jsonl"), "utf8")
          .trim()
          .split("\n")
          .map(JSON.parse)
      : [];
  const completed = async (count) =>
    new Set(
      (await state()).tasks
        .filter((t) => t.status === "completed")
        .map((t) => t.trigger.msgid),
    ).size === count;
  await page.locator("#wecom-agent-enabled").check();
  await page.locator("#wecom-agent-binary").fill(fixture);
  await page.locator("#wecom-agent-save").click();
  await wait(async () => (await state()).agent.enabled);
  await toggle();
  await wait(async () => (await state()).phase === "connected");
  publish("expired", "alice", "text", Date.now() - 11 * 60 * 1000);
  publish("unsupported", "alice", "image", Date.now());
  await wait(() => logs().flatMap((l) => l.records).length >= 2);
  await pause(500);
  assert.equal(trace().length, 0);
  publish("agent-a1", "alice", "text", Date.now());
  await wait(() => trace().some((x) => x.method === "turn/start"));
  publish("agent-a2", "alice", "text", Date.now());
  publish("agent-b1", "bob", "text", Date.now());
  await wait(() => completed(3));
  assert.equal(trace().filter((x) => x.method === "turn/start").length, 2);
  assert(trace().some((x) => x.method === "turn/steer"));
  assert.equal(deliveries.length, 3);
  assert.equal(trace().filter((x) => x.method === "spawn").length, 1);
  assert.equal(trace().filter((x) => x.method === "thread/start").length, 2);
  check(
    "full legacy messages and requesters reach Codex directly; active Room uses turn/steer while another Room starts independently",
  );
  const taskDirectories = logs()
    .map((log) => path.join(home, "rooms", log.dir, "tasks"))
    .filter((dir) => fs.existsSync(dir));
  const cachedTasks = (await state()).tasks;
  try {
    for (const dir of taskDirectories) fs.renameSync(dir, dir + "-held");
    assert.deepEqual((await state()).tasks, cachedTasks);
  } finally {
    for (const dir of taskDirectories)
      if (fs.existsSync(dir + "-held")) fs.renameSync(dir + "-held", dir);
  }
  check(
    "task snapshots remain available without reading historical task files",
  );
  for (const log of logs()) {
    const history = fs
      .readFileSync(
        path.join(home, "rooms", log.dir, "context/messages.log"),
        "utf8",
      )
      .trim()
      .split("\n")
      .map(JSON.parse);
    assert(
      history.every(
        (m) =>
          m.msgtype &&
          m.action === "send" &&
          !m.event &&
          !m.status &&
          !m.account &&
          !m.message,
      ),
    );
  }
  check(
    "Room context stores message bodies only; CloudEvent data has no account or message wrapper, sends have separate receipts",
  );
  await diagnostics();
  await page.screenshot({ path: path.join(root, "wecom-agent-connected.png") });
  report.pids.push(
    ...trace()
      .filter((x) => x.method === "spawn")
      .map((x) => x.pid),
  );
  const recoveryTemplate = (await state()).tasks.find(
    (t) => t.room === "alice",
  );
  await stop();
  const taskDir = path.join(
    path.dirname(path.dirname(recoveryTemplate.log)),
    "tasks",
  );
  const pending = structuredClone(recoveryTemplate);
  pending.id = "pending-recovery";
  pending.status = "pending";
  pending.trigger.msgid = "pending-recovery";
  pending.trigger.text.content = "recover pending";
  fs.appendFileSync(pending.log, JSON.stringify(pending.trigger) + "\n");
  fs.writeFileSync(
    path.join(taskDir, "pending-recovery.json"),
    JSON.stringify(pending),
  );
  const interrupted = structuredClone(pending);
  interrupted.id = "running-recovery";
  interrupted.trigger.msgid = "running-recovery";
  interrupted.status = "running";
  fs.writeFileSync(
    path.join(taskDir, "running-recovery.json"),
    JSON.stringify(interrupted),
  );
  await start();
  await wait(() => completed(4));
  assert.equal(
    new Set(
      (await state()).tasks
        .filter((t) => t.status === "unknown")
        .map((t) => t.trigger.msgid),
    ).size,
    1,
  );
  assert.equal(deliveries.length, 4);
  check(
    "persisted pending delivery resumes after startup; abandoned running delivery becomes unknown without replay",
  );
  await wait(async () => (await state()).phase === "connected");
  publish("after-app-restart", "alice", "text", Date.now());
  await wait(() => completed(5));
  assert.equal(trace().filter((x) => x.method === "thread/start").length, 2);
  assert.equal(trace().filter((x) => x.method === "thread/resume").length, 1);
  check(
    "restart resumes the persisted Room thread without replaying completed messages",
  );
  publish("hold-agent", "alice", "text", Date.now());
  await wait(
    () => trace().filter((x) => x.method === "turn/start").length === 5,
  );
  const pid = trace()
    .filter((x) => x.method === "spawn")
    .at(-1).pid;
  report.pids.push(pid);
  process.kill(pid, "SIGKILL");
  await wait(
    async () =>
      new Set(
        (await state()).tasks
          .filter((t) => t.status === "unknown")
          .map((t) => t.trigger.msgid),
      ).size === 2,
  );
  publish("tool-check", "alice", "text", Date.now());
  await wait(() => completed(6));
  assert.equal(deliveries.length, 6);
  assert(!JSON.stringify(deliveries).includes("unauthorized-room"));
  check(
    "Codex crash marks running work unknown; a fresh turn resumes history and dynamic sends cannot escape the current Room",
  );
  publish("missing-platform-time");
  await wait(() => completed(7));
  assert.equal(deliveries.length, 7);
  assert.equal(
    logs()
      .flatMap((l) => l.records)
      .find((r) => r.event.data.msgid === "missing-platform-time").event.data
      .msgtime,
    undefined,
  );
  publish("long-response", "alice", "text", Date.now());
  await wait(() => completed(8));
  const chunks = deliveries
    .slice(7)
    .map((frame) => frame.body.markdown.content);
  assert.equal(chunks.join(""), "长🙂\n".repeat(1200));
  assert(
    chunks.every(
      (text) => Buffer.byteLength(text) <= 4000 && !text.includes("�"),
    ),
  );
  assert.equal(chunks.length, 3);
  check(
    "long Codex output splits at UTF-8 boundaries and reaches the platform intact",
  );
  publish("oversized-agent", "alice", "text", Date.now());
  await wait(async () =>
    (await state()).tasks.some(
      (task) =>
        task.trigger.msgid === "oversized-agent" && task.status === "unknown",
    ),
  );
  const oversizedPID = trace()
    .filter((row) => row.method === "spawn")
    .at(-1).pid;
  assert(!alive(oversizedPID));
  publish("after-oversized", "alice", "text", Date.now());
  await wait(() => completed(9));
  assert.notEqual(
    trace()
      .filter((row) => row.method === "spawn")
      .at(-1).pid,
    oversizedPID,
  );
  check(
    "oversized Codex frame reaps the blocked process; next message resumes the Room with a fresh client",
  );
  publish("hold-agent-disable", "alice", "text", Date.now());
  await wait(
    () => trace().filter((x) => x.method === "turn/start").length === 11,
  );
  await toggle();
  await wait(async () => (await state()).phase === "stopped");
  await wait(
    async () =>
      new Set(
        (await state()).tasks
          .filter((t) => t.status === "unknown")
          .map((t) => t.trigger.msgid),
      ).size === 4,
  );
  report.pids.push(
    ...trace()
      .filter((x) => x.method === "spawn")
      .map((x) => x.pid),
  );
  await wait(() =>
    report.pids.filter((pid) => pid !== proc.pid).every((pid) => !alive(pid)),
  );
  assert(!trace().some((x) => x.fixtureError));
  check(
    "missing platform time preserves original message; disabling interrupts work and reaps shared app-server",
  );
}
async function runRecovery() {
  await rpc("SetExtensionEnabled", ["wecom", true]);
  await wait(async () => (await state()).phase === "connected");
  const connectionPID = (await state()).pid;
  const subscribedBefore = frames.filter(
    (frame) => frame.cmd === "aibot_subscribe",
  ).length;
  const guardResult = await new Promise((resolve, reject) => {
    const child = spawn(
      process.execPath,
      ["e2e/wecom-live.cjs", path.join(root, "live-guard")],
      {
        env: {
          ...process.env,
          AICAT_LIVE_SOURCE_HOME: home,
          AICAT_LIVE_BINARY: binary,
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let output = "";
    child.stdout.on("data", (data) => (output += data));
    child.stderr.on("data", (data) => (output += data));
    child.on("error", reject);
    child.on("exit", (code) => resolve({ code, output }));
  });
  assert.notEqual(guardResult.code, 0);
  assert(
    guardResult.output.includes("原机器人连接正在运行"),
    guardResult.output,
  );
  assert.equal((await state()).pid, connectionPID);
  assert.equal((await state()).phase, "connected");
  assert.equal(
    frames.filter((frame) => frame.cmd === "aibot_subscribe").length,
    subscribedBefore,
  );
  check(
    "live acceptance refuses an occupied account before authentication and leaves the original connection untouched",
  );
  const longText = "甲🙂".repeat(1800);
  const longEvent = event("long-mcp", longText);
  const before = deliveries.length;
  const longReceipt = receipt(await mcp(longEvent));
  assert.equal(longReceipt.status, "sent");
  assert.equal(longReceipt.parts.length, 4);
  assert.equal(
    deliveries
      .slice(before)
      .map((frame) => frame.body.markdown.content)
      .join(""),
    longText,
  );
  assert(longReceipt.parts.every((part) => part.status === "sent"));
  assert.equal(receipt(await mcp(longEvent)).status, "sent");
  assert.equal(deliveries.length, before + 4);
  check(
    "long explicit send persists each segment receipt; repeated request does not send again",
  );
  const partialEvent = event("partial-mcp", "乙🙂".repeat(2200));
  const partialBefore = deliveries.length;
  holdAt = partialBefore + 2;
  const partial = mcp(partialEvent);
  await wait(() => deliveries.length === holdAt);
  const failedPID = (await state()).pid;
  report.pids.push(failedPID);
  process.kill(failedPID, "SIGKILL");
  const partialReceipt = receipt(await partial);
  assert.equal(partialReceipt.status, "unknown");
  assert.deepEqual(
    partialReceipt.parts.map((part) => part.status),
    ["sent", "unknown"],
  );
  holdAt = Infinity;
  await wait(async () => (await state()).phase === "connected");
  report.pids.push((await state()).pid);
  assert.equal(receipt(await mcp(partialEvent)).status, "unknown");
  assert.equal(deliveries.length, partialBefore + 2);
  check(
    "uncertain middle segment stops later segments and retries return stored partial receipts without resending",
  );
  await rpc("SetExtensionEnabled", ["wecom", false]);
  const hostFile = path.join(packagedRuntime, "host.mjs");
  const originalHost = fs.readFileSync(hostFile, "utf8");
  const registration =
    "if (channel?.id !== 'wecom') throw Error('CHANNEL_REGISTRATION_MISMATCH');";
  const fault = `
 const startAccount=channel.gateway.startAccount;
 channel.gateway.startAccount=ctx=>{
  const marker=path.join(root,'fixture-failed-once');
  if(!fs.existsSync(marker)) {fs.writeFileSync(marker,String(process.pid));return Promise.reject(Error('SYNTHETIC_PLUGIN_FAILURE'));}
  return startAccount(ctx);
 };
 `;
  try {
    fs.writeFileSync(
      hostFile,
      originalHost.replace(registration, registration + fault),
    );
    await rpc("SetExtensionEnabled", ["wecom", true]);
    await wait(async () => (await state()).phase === "connected");
    const failed = Number(
      fs.readFileSync(
        path.join(home, "state/channel-host/fixture-failed-once"),
      ),
    );
    const recovered = (await state()).pid;
    report.pids.push(failed, recovered);
    assert.notEqual(recovered, failed);
    assert(!alive(failed));
    report.pluginFailureInjection = {
      scope:
        "isolated packaged host startAccount boundary; official package and SDK unchanged",
      failed,
      recovered,
    };
    check(
      "plugin task rejection restarts the account even when the original Bun process had remained alive",
    );
  } finally {
    await rpc("SetExtensionEnabled", ["wecom", false]);
    fs.writeFileSync(hostFile, originalHost);
  }
  await rpc("SetExtensionEnabled", ["wecom", true]);
  await wait(async () => (await state()).phase === "connected");
  for (const socket of sockets)
    socket.send(
      JSON.stringify({
        cmd: "aibot_event_callback",
        headers: { req_id: "kicked" },
        body: { msgtype: "event", event: { eventtype: "disconnected_event" } },
      }),
    );
  await wait(
    async () =>
      (await state()).pid === 0 &&
      (await state()).error === "ACCOUNT_CONNECTED_ELSEWHERE",
  );
  const subscribed = frames.filter(
    (frame) => frame.cmd === "aibot_subscribe",
  ).length;
  await pause(2000);
  assert.equal(
    frames.filter((frame) => frame.cmd === "aibot_subscribe").length,
    subscribed,
  );
  check(
    "server kick stops the account without reconnecting and competing with another instance",
  );
  await rpc("SetExtensionEnabled", ["wecom", false]);
  rejectAuthentication = true;
  await rpc("SetExtensionEnabled", ["wecom", true]);
  await wait(
    async () =>
      (await state()).pid === 0 &&
      (await state()).error === "AUTHENTICATION_FAILED",
    65000,
  );
  const refused = frames.filter(
    (frame) => frame.cmd === "aibot_subscribe",
  ).length;
  await pause(2000);
  assert.equal(
    frames.filter((frame) => frame.cmd === "aibot_subscribe").length,
    refused,
  );
  rejectAuthentication = false;
  await rpc("SetExtensionEnabled", ["wecom", false]);
  check(
    "exhausted SDK authentication retries block host-level reconnect until explicitly enabled again",
  );
}

(async () => {
  try {
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const portServer = net.createServer();
    await new Promise((resolve) => portServer.listen(0, "127.0.0.1", resolve));
    const port = portServer.address().port;
    await new Promise((resolve) => portServer.close(resolve));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      "ui:\n  language: zh-Hans\n  theme: light\ngateway:\n  addr: 127.0.0.1:" +
        port +
        "\nextensions:\n  traffic:\n    enabled: false\n",
      { mode: 0o600 },
    );
    fs.writeFileSync(
      path.join(root, "os-state.json"),
      JSON.stringify({
        trusted: false,
        proxy: { http: { enabled: false }, https: { enabled: false } },
      }),
    );
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    await start();
    assert.equal((await state()).pid, 0);
    assert.equal(sockets.size, 0);
    assert.equal(frames.length, 0);
    assert(!fs.existsSync(path.join(home, "state/channel-host")));
    await page.screenshot({ path: path.join(root, "wecom-off.png") });
    check(
      "default off: extension visible, no runtime process or platform activity",
    );
    assert(!fs.existsSync(path.join(packagedRuntime, "node_modules")));
    assert.equal(
      (await rpc("State")).extensions.find((x) => x.id === "wecom").installed,
      false,
    );
    await assert.rejects(() => rpc("SetExtensionEnabled", ["wecom", true]));
    await page.locator("#extension-install").click();
    await wait(
      async () =>
        (await rpc("State")).extensions.find((x) => x.id === "wecom").installed,
      180000,
    );
    assert.equal(sockets.size, 0);
    report.runtime.pluginBefore = treeDigest(pluginPath);
    report.runtime.sdkBefore = treeDigest(sdkPath);
    await page.locator("#feature-wecom").waitFor();
    assert.equal(await page.locator("#feature-wecom").isDisabled(), true);
    check(
      "runtime contains no vendor plugin; UI installs original package and dependencies without connecting, enable requires validated account",
    );
    await page.locator("#wecom-config").waitFor();
    assert.equal(await page.locator("#wecom-send-form").count(), 0);
    await page.locator("#extension-codex").click();
    await page.locator("#history-back").click();
    await page.locator('.extension-detail[data-extension="wecom"]').waitFor();
    await configure();
    assert.equal(
      await page.locator("#nav-extensions").getAttribute("aria-current"),
      "page",
    );
    assert.equal(
      await page.locator("#extension-wecom").getAttribute("aria-current"),
      "true",
    );
    await diagnostics();
    assert.equal(await page.locator("#wecom-config").count(), 0);
    await page.locator("#history-back").click();
    await page.locator("#wecom-settings").waitFor();
    await page.locator("#history-forward").click();
    await page.locator("#wecom-diagnostics").waitFor();
    await configure();
    await page.locator("#nav-settings").click();
    await page.locator("#settings-general").waitFor();
    assert.equal(await page.locator("#settings-extensions").count(), 0);
    await page.locator("#history-back").click();
    await page.locator("#wecom-settings").waitFor();
    await page.screenshot({ path: path.join(root, "wecom-settings.png") });
    check(
      "installed extension details contain configuration; diagnostics is separate; history restores extension and module",
    );
    rejectAuthentication = true;
    await assert.rejects(() =>
      rpc("ConfigureMessageChannel", [
        "wecom",
        { accountId: "synthetic-bot", secret },
      ]),
    );
    assert.equal((await state()).validated, false);
    await assert.rejects(() => rpc("SetExtensionEnabled", ["wecom", true]));
    await wait(() => sockets.size === 0);
    assert.equal(deliveries.length, 0);
    rejectAuthentication = false;
    check(
      "failed platform authentication keeps enable blocked and closes the temporary connection without messages",
    );
    await page.locator("#wecom-bot-id").fill("synthetic-bot");
    await page.locator("#wecom-secret").fill(secret);
    await page.locator("#wecom-save").click();
    await wait(async () => (await state()).validated);
    await wait(
      async () => (await page.locator("#wecom-secret").inputValue()) === "",
    );
    assert.equal(
      fs.statSync(path.join(home, "credentials/wecom.json")).mode & 0o777,
      0o600,
    );
    assert(!JSON.stringify(await rpc("State")).includes(secret));
    assert(
      !fs.readFileSync(path.join(home, "aicat.yaml"), "utf8").includes(secret),
    );
    await wait(() => sockets.size === 0);
    check(
      "configure while off: real SDK authentication succeeds, validation connection closes, credentials remain private",
    );
    await page.locator("#wecom-agent-enabled").check();
    await page.locator("#wecom-agent-binary").fill("/draft/codex");
    await rpc("SetPreferences", [{ language: "zh-Hans", theme: "dark" }]);
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "dark",
    );
    assert(await page.locator("#wecom-agent-enabled").isChecked());
    assert.equal(
      await page.locator("#wecom-agent-binary").inputValue(),
      "/draft/codex",
    );
    await page.locator("#wecom-agent-enabled").uncheck();
    await page.locator("#wecom-agent-binary").fill("");
    await rpc("SetPreferences", [{ language: "zh-Hans", theme: "light" }]);
    check("unrelated state refresh preserves unsaved Agent fields");
    if (agentScenario) {
      await runAgent();
    } else {
      await toggle();
      await wait(async () => (await state()).phase === "connected");
      const pid = (await state()).pid;
      report.pids.push(pid);
      assert(pid > 0);
      assert.equal(sockets.size, 1);
      const command = execFileSync(
        "/bin/ps",
        ["-p", String(pid), "-o", "command="],
        { encoding: "utf8" },
      );
      assert(command.includes(path.join(bundle, "Resources/channel-host/bun")));
      assert(!command.includes(secret));
      check("bundled Bun starts and authenticates without system Bun in PATH");
      const savedConfig = fs.readFileSync(
        path.join(home, "aicat.yaml"),
        "utf8",
      );
      const installedRecord = fs.readFileSync(
        path.join(home, "extensions/wecom/current.json"),
        "utf8",
      );
      await assert.rejects(
        rpc("UpdateExtensionConfig", ["wecom", { agent: { enabled: true } }]),
      );
      await assert.rejects(rpc("UpdateExtension", ["wecom"]));
      await assert.rejects(rpc("UninstallExtension", ["wecom"]));
      assert.equal(
        fs.readFileSync(path.join(home, "aicat.yaml"), "utf8"),
        savedConfig,
      );
      assert.equal(
        fs.readFileSync(
          path.join(home, "extensions/wecom/current.json"),
          "utf8",
        ),
        installedRecord,
      );
      assert.equal((await state()).pid, pid);
      assert(
        !JSON.stringify(await rpc("ReadExtensionConfig", ["wecom"])).includes(
          secret,
        ),
      );
      check(
        "enabled extension rejects config writes, update and uninstall without changing config, package record or running host; config reads omit credentials",
      );
      publish("incoming-a1");
      publish("incoming-a2");
      publish("incoming-b", "bob");
      await wait(
        () => logs().reduce((n, log) => n + log.records.length, 0) >= 3,
      );
      await pause(1200);
      assert.equal(deliveries.length, 0);
      const incoming = logs();
      assert.equal(incoming.length, 2);
      for (const log of incoming)
        for (const record of log.records) {
          assert.equal(record.event.type, "org.aicat.message.received.v1");
          assert.equal(record.event.data.account, undefined);
          assert.equal(record.event.data.message, undefined);
          assert.equal(record.event.data.msgtime, undefined);
        }
      await wait(
        () =>
          Object.keys(
            JSON.parse(
              fs.readFileSync(
                path.join(home, "state/channel-host/unconfirmed.json"),
              ),
            ),
          ).length === 0,
      );
      check(
        "official SDK receives private text into distinct Room logs, ACK clears pending; implicit plugin replies blocked",
      );
      fs.renameSync(path.join(home, "rooms"), path.join(home, "rooms-kept"));
      fs.writeFileSync(path.join(home, "rooms"), "blocked");
      publish("retry-after-log-failure");
      await wait(async () => (await state()).error.includes("Room"));
      const pendingFile = path.join(
        home,
        "state/channel-host/unconfirmed.json",
      );
      await wait(
        () =>
          Object.keys(JSON.parse(fs.readFileSync(pendingFile))).length === 1,
      );
      process.kill(pid, "SIGKILL");
      await wait(
        async () =>
          (await state()).phase === "connected" &&
          (await state()).pid > 0 &&
          (await state()).pid !== pid,
      );
      report.pids.push((await state()).pid);
      assert.equal(
        Object.keys(JSON.parse(fs.readFileSync(pendingFile))).length,
        1,
      );
      fs.unlinkSync(path.join(home, "rooms"));
      fs.renameSync(path.join(home, "rooms-kept"), path.join(home, "rooms"));
      await wait(() =>
        logs().some((log) =>
          log.records.some(
            (record) => record.event.data.msgid === "retry-after-log-failure",
          ),
        ),
      );
      await wait(
        () =>
          Object.keys(JSON.parse(fs.readFileSync(pendingFile))).length === 0,
      );
      check(
        "Room write failure withholds ACK; host crash retains pending event and retry recovers after storage repair",
      );
      publish("incoming-image", "alice", "image");
      await wait(() =>
        logs().some((log) =>
          log.records.some((r) => r.event.data.msgtype === "unsupported"),
        ),
      );
      assert.equal(deliveries.length, 0);
      check(
        "unsupported media archived without downloading or implicit replies",
      );
      await diagnostics();
      assert.equal(await page.locator("#wecom-config").count(), 0);
      await page.locator("#wecom-open-logs").click();
      await wait(
        () =>
          JSON.parse(fs.readFileSync(path.join(root, "os-state.json")))
            .messageLogsOpened === path.join(home, "rooms"),
      );
      check(
        "diagnostics opens the server-selected Room directory through an isolated native boundary",
      );
      await page.locator("#wecom-recipient").fill("alice");
      await page.locator("#wecom-text").fill("explicit UI outgoing");
      await page.locator("#wecom-send").click();
      await wait(() => deliveries.length === 1);
      await page
        .locator("#wecom-send-result")
        .filter({ hasText: "平台已确认发送" })
        .waitFor();
      assert.equal(deliveries[0].body.chatid, "alice");
      assert(JSON.stringify(deliveries[0]).includes("explicit UI outgoing"));
      await page.screenshot({
        path: path.join(root, "wecom-connected-light.png"),
      });
      check("UI explicit send reaches platform and persists sent result");
      assert.equal((await mcp(event("unauth"), false)).httpStatus, 401);
      const sendEvent = event("mcp-1");
      assert.equal(receipt(await mcp(sendEvent)).status, "sent");
      assert.equal(receipt(await mcp(sendEvent)).status, "sent");
      assert.equal(deliveries.length, 2);
      assert.equal((await mcp(event("mcp-1", "different"))).isError, true);
      assert.equal(deliveries.length, 2);
      check(
        "authenticated messages_send reaches platform; identical ID returns receipt, conflicts rejected",
      );
      const uncertainPid = (await state()).pid;
      hold = true;
      const uncertainEvent = event("mcp-unknown", "uncertain outgoing");
      const uncertain = mcp(uncertainEvent);
      await wait(() => deliveries.length === 3);
      process.kill(uncertainPid, "SIGKILL");
      assert.equal(receipt(await uncertain).status, "unknown");
      hold = false;
      await wait(
        async () =>
          (await state()).phase === "connected" &&
          (await state()).pid > 0 &&
          (await state()).pid !== uncertainPid,
      );
      report.pids.push((await state()).pid);
      assert.equal(receipt(await mcp(uncertainEvent)).status, "unknown");
      assert.equal(deliveries.length, 3);
      check(
        "host crash recovers with a new PID, uncertain send stays unknown and is not resent",
      );
      const recoveryPid = (await state()).pid;
      await stop();
      await wait(() => !alive(recoveryPid));
      const compatibility = path.join(
        home,
        "extensions/wecom/2026.10.30/node_modules/openclaw",
      );
      fs.rmSync(compatibility, { recursive: true, force: true });
      fs.cpSync(path.join(packagedRuntime, "shim"), compatibility, {
        recursive: true,
      });
      fs.appendFileSync(
        path.join(compatibility, "sdk.mjs"),
        "\nthrow Error('STALE_SHIM_MUST_NOT_LOAD');\n",
      );
      await start();
      assert.equal(
        fs.realpathSync(compatibility),
        fs.realpathSync(path.join(packagedRuntime, "shim")),
      );
      check(
        "startup migrates copied SDK shim to the app's current shared compatibility layer without downloading packages",
      );
      await wait(async () => (await state()).phase === "connected");
      report.pids.push((await state()).pid);
      assert.equal(receipt(await mcp(sendEvent)).status, "sent");
      assert.equal(receipt(await mcp(uncertainEvent)).status, "unknown");
      assert.equal(deliveries.length, 3);
      publish("after-app-restart");
      await wait(() =>
        logs().some((log) =>
          log.records.some((r) => r.event.data.msgid === "after-app-restart"),
        ),
      );
      check(
        "app restart restores enabled account and credentials, retains logs and receipts without resending",
      );
      const finalPid = (await state()).pid;
      await toggle();
      await wait(
        async () =>
          (await state()).phase === "stopped" && (await state()).pid === 0,
      );
      await wait(() => !alive(finalPid) && sockets.size === 0);
      const before = frames.length;
      await pause(2200);
      assert.equal(frames.length, before);
      assert.equal((await mcp(event("disabled"))).isError, true);
      assert(
        !fs.readFileSync(path.join(root, "app.log"), "utf8").includes(secret),
      );
      assert(!JSON.stringify(logs()).includes(secret));
      assert.deepEqual(errors, []);
      check(
        "disable reaps Bun and stops all platform activity; disabled send rejected and credentials stay private",
      );
      await stop();
      await start();
      assert.equal((await state()).pid, 0);
      assert.equal((await state()).configured, true);
      assert.equal(sockets.size, 0);
      check(
        "app restart preserves disabled intent and credentials without starting Bun",
      );
      await runRecovery();
    }
    report.runtime.pluginAfter = treeDigest(pluginPath);
    report.runtime.sdkAfter = treeDigest(sdkPath);
    assert.equal(report.runtime.pluginBefore, report.runtime.pluginAfter);
    assert.equal(report.runtime.sdkBefore, report.runtime.sdkAfter);
    check("official plugin and SDK files remain unchanged");
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
    if (page && !report.passed)
      await page
        .screenshot({ path: path.join(root, "failure.png") })
        .catch(() => {});
    await browser?.close();
    await stop();
    for (const ws of wss.clients) ws.terminate();
    await Promise.race([
      new Promise((resolve) => wss.close(resolve)),
      pause(2000),
    ]);
    server.closeAllConnections();
    await Promise.race([
      new Promise((resolve) => server.close(resolve)),
      pause(2000),
    ]);
    await pause(300);
    report.livePids = report.pids.filter(alive);
    if (report.livePids.length) report.passed = false;
    report.errors = errors;
    fs.writeFileSync(
      path.join(root, "platform-frames.json"),
      JSON.stringify(frames, null, 2),
    );
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify({ ...report, artifacts: root }, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
