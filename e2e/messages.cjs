const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const http = require("node:http");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(process.argv[3] || "build/messages-" + Date.now());
if (fs.existsSync(root)) throw Error("Use a fresh output directory");
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const home = path.join(root, "home");
fs.mkdirSync(home, { mode: 0o700 });
const hash = (value) => crypto.createHash("sha256").update(value).digest("hex");
const report = {
  passed: false,
  command: process.argv,
  coverage:
    "Headless Chromium, hidden e2e desktop, installed NPM channel, real Room/dispatch/MCP/storage services. Codex and platform are protocol fixtures; no native-window or live-platform coverage.",
  hashes: Object.fromEntries(
    [
      binary,
      "ui/src/components/MessagesPage.vue",
      "ui/src/components/RoomSettings.vue",
      "ui/src/app.css",
      "internal/messages/issues.go",
      "e2e/messages.cjs",
    ].map((file) => [file, hash(fs.readFileSync(file))]),
  ),
  checks: [],
  pageErrors: [],
};
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function wait(fn, timeout = 30000) {
  let last;
  for (const end = Date.now() + timeout; Date.now() < end;) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (e) {
      last = e;
    }
    await pause(70);
  }
  throw Error("Timed out: " + (last?.stack || fn));
}
const check = (name, condition = true) => {
  assert(condition, name);
  report.checks.push(name);
  console.log("PASS", name);
};
let proc, browser, page, server, preview, token, base;
const log = fs.openSync(path.join(root, "app.log"), "a");
async function rpc(method, ...args) {
  const response = await fetch(new URL("/rpc", preview), {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
  });
  const value = await response.json();
  if (!response.ok) throw Error(JSON.stringify(value));
  return value;
}
async function start() {
  const file = path.join(root, "preview-url");
  fs.rmSync(file, { force: true });
  proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_PREVIEW: file,
      AICAT_E2E_OS_STATE: path.join(root, "os-state.json"),
    },
    stdio: ["ignore", log, log],
  });
  await wait(() => {
    if (proc.exitCode !== null) throw Error("App exited: " + proc.exitCode);
    return fs.existsSync(file);
  });
  preview = fs.readFileSync(file, "utf8").trim();
  token = new URL(preview).searchParams.get("token");
  await wait(async () => (await rpc("State")).gateway.state === "ready");
}
async function stop() {
  if (proc?.exitCode === null) {
    proc.kill("SIGTERM");
    await wait(() => proc.exitCode !== null);
    assert.equal(proc.exitCode, 0);
  }
}
const channel = "fixture.messages";
const list = () => rpc("ListRooms");
async function room(conversation) {
  const row = (await list()).find((r) => r.conversation === conversation);
  assert(row, "Missing Room " + conversation);
  return rpc("ReadRoomByID", row.id);
}
async function configure(conversation, patch) {
  const value = await room(conversation);
  await rpc("UpdateRoomSettings", value.id, { ...value.settings, ...patch });
}
async function emit(conversation, id, text, extra = {}) {
  await rpc("ReadExtensionData", channel, {
    message: {
      msgid: id,
      roomid: conversation,
      from: "业务成员",
      msgtype: "text",
      msgtime: Date.now(),
      text: { content: text },
      ...extra,
    },
  });
}
const sent = () => {
  const dir = path.join(home, "state", channel);
  return fs
    .readdirSync(dir)
    .filter((name) => /^[a-f0-9]{64}$/.test(name))
    .flatMap((name) => {
      const file = path.join(dir, name, "sent.jsonl");
      return fs.existsSync(file)
        ? fs
            .readFileSync(file, "utf8")
            .trim()
            .split("\n")
            .filter(Boolean)
            .map(JSON.parse)
        : [];
    });
};
async function openRoom(id) {
  await page.locator('[data-room-id="' + id + '"]').click();
  await wait(
    async () =>
      (await page
        .locator('[data-room-id="' + id + '"]')
        .getAttribute("aria-current")) === "true" &&
      (await page.locator("#room-settings-toggle").isVisible()),
  );
}
async function screenshot(name) {
  await page.screenshot({
    path: path.join(root, name + ".png"),
    fullPage: true,
  });
}

(async () => {
  try {
    const listener = net.createServer();
    await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
    const port = listener.address().port;
    await new Promise((resolve) => listener.close(resolve));
    base = "http://127.0.0.1:" + port;
    const quote = (s) => "'" + s.replaceAll("'", "'\\''") + "'";
    const codex = path.join(root, "codex");
    fs.copyFileSync(
      "e2e/fixtures/codex-room-agent.cjs",
      path.join(root, "codex-room-agent.cjs"),
    );
    fs.writeFileSync(
      codex,
      "#!/bin/sh\nexec " +
        quote(process.execPath) +
        " " +
        quote(path.join(root, "codex-room-agent.cjs")) +
        ' "$@"\n',
      { mode: 0o700 },
    );
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      JSON.stringify({
        gateway: { addr: "127.0.0.1:" + port },
        ui: { language: "zh-Hans", theme: "light" },
        systemProxy: { enabled: false },
        extensions: { traffic: { enabled: false } },
        messageExecution: { codexBinary: codex },
      }),
    );
    fs.writeFileSync(
      path.join(root, "os-state.json"),
      JSON.stringify({ services: [], trusted: true, recordProxyWrites: true }),
    );
    const pkg = path.join(root, "package");
    fs.mkdirSync(pkg);
    fs.writeFileSync(
      path.join(pkg, "package.json"),
      JSON.stringify({
        name: "@fixture/messages",
        version: "1.0.0",
        type: "module",
        aicat: {
          api: 1,
          id: channel,
          entry: "index.mjs",
          identityFields: ["account"],
          fields: [
            { key: "account", type: "string", required: true, label: "账号" },
          ],
          capabilities: { send: true },
          room: { mode: "conversation", trigger: { all: true } },
        },
      }),
    );
    fs.writeFileSync(
      path.join(pkg, "index.mjs"),
      `import fs from 'node:fs';import path from 'node:path';export function createChannel(api){return {open(){return {}},close(){},async observe(event){for(const message of event.query.messages || (event.query.message?[event.query.message]:[]))await api.publish(message);return {}},send(event){fs.appendFileSync(path.join(api.stateDirectory,'sent.jsonl'),JSON.stringify(event)+'\\n');if(event.data.text.content.includes('uncertain'))throw Error('PLATFORM_UNCERTAIN');return {confirmation:'fixture_platform',chatId:event.data.roomid}}}}`,
    );
    const tgz = path.join(root, "channel.tgz");
    execFileSync("tar", ["-czf", tgz, "-C", root, "package"]);
    const archive = fs.readFileSync(tgz);
    server = http.createServer((req, res) => {
      if (req.url.endsWith(".tgz")) return res.end(archive);
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          name: "@fixture/messages",
          "dist-tags": { latest: "1.0.0" },
          versions: {
            "1.0.0": {
              name: "@fixture/messages",
              version: "1.0.0",
              dist: {
                tarball:
                  "http://127.0.0.1:" + server.address().port + "/channel.tgz",
                shasum: crypto.createHash("sha1").update(archive).digest("hex"),
              },
            },
          },
        }),
      );
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    await start();
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.on("pageerror", (e) => report.pageErrors.push(e.message));
    const calls = [];
    page.on("request", (request) => {
      if (request.url().endsWith("/rpc"))
        calls.push(JSON.parse(request.postData() || "{}").method);
    });
    await page.goto(preview);
    await page.waitForFunction(() => !!window.desktop?.state());
    await page.locator("#nav-messages").click();
    await wait(async () =>
      (await page.locator("#messages-page").innerText()).includes(
        "渠道收到消息后",
      ),
    );
    await screenshot("empty");
    check("empty message page offers records entry without reading states");
    await page.locator("#room-empty-add-channel").click();
    await page.locator("#extension-search").waitFor();
    assert.equal(
      await page.locator("#extension-search").inputValue(),
      "tag:channel",
    );
    const channelIds = (await rpc("State")).extensions
      .filter((item) => item.categories.includes("消息渠道"))
      .map((item) => item.id)
      .sort();
    const visibleChannels = async () =>
      (
        await page
          .locator("#module-sidebar .xitem")
          .evaluateAll((items) =>
            items.map((item) => item.id.replace(/^extension-/, "")),
          )
      ).sort();
    assert.deepEqual(await visibleChannels(), channelIds);
    await page.locator("#extension-search").fill("tag:channel 企业微信");
    assert.deepEqual(await visibleChannels(), ["wecom", "wecom-local"]);
    await page.locator("#extension-search").fill("tag:channel");
    await screenshot("add-channel");
    await page.locator("#history-back").click();
    await page.locator("#messages-page").waitFor();
    await page.locator("#history-forward").click();
    assert.equal(
      await page.locator("#extension-search").inputValue(),
      "tag:channel",
    );
    await page.locator("#history-back").click();
    await page.locator("#messages-page").waitFor();
    check(
      "add-channel entry opens actual channel-tag results, combines text search and restores through navigation history",
    );
    await rpc("InstallChannelPackage", {
      id: channel,
      name: "验收渠道",
      package: "@fixture/messages",
      version: "1.0.0",
      format: "aicat",
      registry: "http://127.0.0.1:" + server.address().port,
      publisher: "fixture",
    });
    await rpc("ConfigureMessageChannel", channel, {
      accountId: "",
      secret: "",
      values: { account: "fixture-account" },
    });
    await rpc("SetExtensionEnabled", channel, true);
    await wait(
      async () =>
        (await rpc("State")).messageChannels[channel].phase === "connected",
    );
    for (const id of ["orders", "notice", "table", "failed"])
      await emit(id, "seed-" + id, "会话已接入", { historical: true });
    await configure("orders", {
      name: "订单协作群",
      automatic: true,
      allowOutbound: true,
      model: "room-a",
    });
    await emit("orders", "paid", "订单已支付，请核对。");
    await wait(async () =>
      (await room("orders")).messages.some((m) => m.reply_to === "paid"),
    );
    await emit("orders", "send-confirmed", "send:请补充订单号或支付截图。");
    await wait(async () =>
      (await room("orders")).messages.some(
        (m) => m.delivery?.status === "sent",
      ),
    );
    await configure("notice", {
      name: "项目通知",
      automatic: true,
      allowOutbound: true,
      model: "room-a",
    });
    await emit("notice", "uncertain", "send:uncertain platform");
    await wait(async () =>
      (await room("notice")).messages.some(
        (m) => m.delivery?.status === "unknown",
      ),
    );
    await configure("table", {
      name: "牌桌 10291",
      automatic: true,
      model: "room-b",
      mode: "snapshot",
    });
    await emit(
      "table",
      "state-1",
      JSON.stringify({ kind: "table_snapshot", state: 1 }),
      {
        summary: "牌桌状态 1",
        context_key: "hand-one",
        agent_snapshot: { content: "state 1" },
      },
    );
    await wait(async () =>
      (await room("table")).messages.some((m) => m.reply_to === "state-1"),
    );
    await emit(
      "table",
      "state-2",
      JSON.stringify({ kind: "table_snapshot", state: 2 }),
      {
        summary: "牌桌状态 2",
        context_key: "hand-one",
        agent_snapshot: { content: "state 2" },
      },
    );
    await wait(async () =>
      (await room("table")).messages.some((m) => m.reply_to === "state-2"),
    );
    await configure("failed", {
      name: "模型接入验收",
      automatic: true,
      model: "bad-model",
    });
    await emit("failed", "failed-task", "请检查当前模型。");
    await wait(async () =>
      (await room("failed")).tasks.some((task) => task.status === "failed"),
    );
    const a = await room("orders"),
      b = await room("notice"),
      c = await room("table"),
      d = await room("failed");
    check(
      "real ingress, Agent dispatch, authenticated MCP publication and authorized platform sending remain intact",
      a.messages.some((m) => m.reply_to === "paid") && sent().length === 2,
    );
    const oldRead = path.join(home, "rooms", b.id, "read.json");
    fs.writeFileSync(oldRead, "obsolete and malformed reading marker");
    const oldTime = fs.statSync(oldRead).mtimeMs;
    const summaries = await list();
    assert(
      summaries.every((r) => !("unread" in r) && !("lastMessageKey" in r)),
    );
    assert(
      (await room("notice")).issues.some(
        (issue) => issue.kind === "delivery" && issue.status === "unknown",
      ),
    );
    assert(
      (await room("failed")).issues.some(
        (issue) => issue.kind === "task" && issue.status === "failed",
      ),
    );
    const httpRooms = await (await fetch(base + "/api/rooms")).json();
    check(
      "Desktop and HTTP summaries share actual delivery/task issues and ignore obsolete reading files",
      httpRooms.filter((r) => r.issues.length).length === 2,
    );
    await wait(
      async () => (await page.locator("[data-room-id]").count()) === 4,
    );
    await openRoom(a.id);
    await page.locator("#room-add-channel").click();
    assert.equal(
      await page.locator("#extension-search").inputValue(),
      "tag:channel",
    );
    assert((await visibleChannels()).includes(channel));
    await page.locator("#history-back").click();
    await page.locator("#room-settings-toggle").waitFor();
    await wait(
      async () =>
        (await page.locator("#room-messages article").count()) ===
        a.messages.length,
    );
    assert.equal(await page.locator("#navigation img").count(), 0);
    assert.equal(await page.locator(".room-message.outgoing img").count(), 2);
    assert.equal(
      await page
        .locator("#messages-page")
        .getByText("未读", { exact: true })
        .count(),
      0,
    );
    assert.equal(await page.locator("#room-issue-banner").count(), 0);
    await screenshot("messages-light");
    const addBounds = await page.locator("#room-add-channel").boundingBox();
    const sidebarBounds = await page.locator("#module-sidebar").boundingBox();
    const searchBounds = await page.locator("#room-search").boundingBox();
    assert(
      addBounds.x + addBounds.width <=
        sidebarBounds.x + sidebarBounds.width - 8,
    );
    assert(searchBounds.x + searchBounds.width <= addBounds.x);
    await page.locator("#room-search").fill("订单");
    assert.equal(await page.locator("[data-room-id]").count(), 1);
    await page.locator("#room-search").fill("missing-room");
    assert((await page.locator(".room-list").innerText()).includes("没有匹配"));
    await page.locator("#room-search").fill("");
    check(
      "conversation search, native shell and canonical reply avatars match the approved design",
    );
    await page.locator("#room-filter-issues").click();
    await wait(
      async () => (await page.locator("[data-room-id]").count()) === 2,
    );
    await openRoom(b.id);
    await page.locator("#room-issue-details").click();
    await page.locator("#room-tasks").waitFor();
    assert(
      (await page.locator("#room-tasks").innerText()).includes("发送结果不明"),
    );
    await screenshot("delivery-issue");
    await page.keyboard.press("Escape");
    await page.locator("#room-filter-all").click();
    await openRoom(a.id);
    await page.locator("#room-filter-issues").click();
    await wait(
      async () => (await page.locator("[data-room-id]").count()) === 2,
    );
    assert.equal(sent().length, 2);
    assert.equal(fs.statSync(oldRead).mtimeMs, oldTime);
    assert.equal(
      fs.existsSync(path.join(home, "rooms", a.id, "read.json")),
      false,
    );
    assert(!calls.some((name) => name?.includes("MarkRoomRead")));
    check(
      "viewing and switching issues neither clears them nor writes reading markers or repeats sending",
    );
    await openRoom(d.id);
    await page.locator("#room-issue-details").click();
    assert(
      (await page.locator("#room-tasks").innerText()).includes(
        "MODEL_UNAVAILABLE",
      ),
    );
    await screenshot("task-issue");
    await page.keyboard.press("Escape");
    await page.locator("#room-filter-all").click();
    await openRoom(c.id);
    assert(
      (await page.locator("#room-messages").innerText()).includes(
        "基于较早局面",
      ),
    );
    assert.equal(
      await page.locator(".room-business pre").first().isVisible(),
      false,
    );
    await page.locator(".room-business summary").first().click();
    assert(
      (await page.locator(".room-business pre").first().innerText()).includes(
        "table_snapshot",
      ),
    );
    check(
      "structured originals stay collapsed and earlier business-state replies retain their association",
    );
    await openRoom(a.id);
    const traces = () =>
      fs
        .readFileSync(path.join(root, "codex-trace.jsonl"), "utf8")
        .trim()
        .split("\n")
        .map(JSON.parse);
    const modelQueries = traces().filter(
      (event) => event.method === "model/list",
    ).length;
    await page.locator("#room-settings-toggle").click();
    await page.locator("#room-settings").waitFor();
    assert.equal(await page.locator("#room-model").isVisible(), false);
    assert.equal(await page.locator("#room-prompt").isVisible(), false);
    assert.equal(
      traces().filter((event) => event.method === "model/list").length,
      modelQueries,
    );
    await screenshot("settings-collapsed");
    await page.locator("#room-advanced > summary").click();
    await wait(
      async () => (await page.locator("#room-model option").count()) === 3,
    );
    await page.locator("#room-prompt").fill("UNSAVED_MESSAGE_PROMPT");
    await rpc("SetPreferences", { language: "zh-Hans", theme: "dark" });
    assert.equal(
      await page.locator("#room-prompt").inputValue(),
      "UNSAVED_MESSAGE_PROMPT",
    );
    await page.locator('[data-room-id="' + b.id + '"]').click();
    await page.locator("#dialog[open]").waitFor();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    assert.equal(
      await page.locator("#room-prompt").inputValue(),
      "UNSAVED_MESSAGE_PROMPT",
    );
    await page.locator(".room-quote").first().focus();
    await page.keyboard.press("Enter");
    await page.locator("#dialog[open]").waitFor();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    assert.equal(
      await page.locator("#room-prompt").inputValue(),
      "UNSAVED_MESSAGE_PROMPT",
    );
    await page.locator("#room-context").fill("[]");
    await page.locator("#room-settings-save").click();
    await wait(async () =>
      (await page.locator("#room-settings [role=alert]").innerText()).includes(
        "上下文必须",
      ),
    );
    assert.notEqual(
      (await room("orders")).settings.prompt,
      "UNSAVED_MESSAGE_PROMPT",
    );
    await page.locator("#room-context").fill('{"scope":"message-e2e"}');
    await page.locator("#room-model").selectOption("room-b");
    await page.locator("#room-name").fill("订单协作群");
    await page.locator("#room-settings-save").click();
    await wait(
      async () =>
        (await room("orders")).settings.prompt === "UNSAVED_MESSAGE_PROMPT",
    );
    await page.locator("#room-panel").waitFor({ state: "detached" });
    assert.deepEqual((await room("orders")).settings.context, {
      scope: "message-e2e",
    });
    await page.locator("#room-settings-toggle").click();
    assert.equal(await page.locator("#room-model").isVisible(), false);
    await page.keyboard.press("Escape");
    check(
      "advanced settings load models on demand, preserve drafts across refresh, guard switching, validate and persist updates",
    );
    await page.locator("#room-activity-toggle").click();
    await page.locator("#room-tasks").waitFor();
    assert(
      (await page.locator("#room-tasks").innerText()).includes("处理完成"),
    );
    await page.keyboard.press("Escape");
    await screenshot("messages-dark");
    for (const width of [1180, 860]) {
      await page.setViewportSize({ width, height: 900 });
      const overflow = await page
        .locator("#messages-page")
        .evaluate((node) => node.scrollWidth > node.clientWidth);
      assert.equal(overflow, false);
      await page.locator("#room-settings-toggle").click();
      await page.locator("#room-advanced > summary").click();
      await page.locator("#room-context").scrollIntoViewIfNeeded();
      const footer = await page.locator(".room-panel-footer").boundingBox();
      assert(footer.y + footer.height <= 901);
      await screenshot("settings-dark-" + width);
      await page.keyboard.press("Escape");
    }
    await rpc("SetPreferences", { language: "en", theme: "light" });
    await wait(
      async () =>
        (await page.locator("#room-filter-issues").innerText()) === "Issues",
    );
    await screenshot("messages-english");
    await page.locator("#room-add-channel").click();
    assert.equal(
      await page.locator("#extension-search").inputValue(),
      "tag:channel",
    );
    assert.equal(
      await page.locator("#module-sidebar #extension-codex").count(),
      0,
    );
    assert.equal(
      await page.locator("#module-sidebar #extension-traffic").count(),
      0,
    );
    await page.locator("#history-back").click();
    await page.locator("#messages-page").waitFor();
    check(
      "light/dark, English and narrower desktop widths keep panels and controls usable",
    );
    await rpc("SetPreferences", { language: "zh-Hans", theme: "light" });
    const historical = Array.from({ length: 205 }, (_, index) => ({
      msgid: "archive-" + index,
      roomid: "notice",
      from: "历史成员",
      msgtype: "text",
      historical: true,
      msgtime: Date.now(),
      text: { content: "后续记录 " + index },
    }));
    for (let offset = 0; offset < historical.length; offset += 10)
      await rpc("ReadExtensionData", channel, {
        messages: historical.slice(offset, offset + 10),
      });
    const retained = await room("notice");
    check(
      "persisted send issues remain visible after their message leaves the latest 200 records",
      retained.messages.length === 200 &&
        !retained.messages.some((m) => m.delivery) &&
        retained.issues.some((issue) => issue.status === "unknown"),
    );
    await stop();
    await start();
    await wait(
      async () =>
        (await rpc("State")).messageChannels[channel].phase === "connected",
    );
    const restarted = await room("notice");
    assert(restarted.issues.some((issue) => issue.status === "unknown"));
    assert.equal((await room("failed")).tasks.length, 0);
    assert.equal((await room("orders")).settings.model, "room-b");
    assert.equal(sent().length, 2);
    await page.goto(preview);
    await page.waitForFunction(() => !!window.desktop?.state());
    await page.locator("#nav-messages").click();
    await page.locator("#room-filter-issues").click();
    await wait(
      async () => (await page.locator("[data-room-id]").count()) === 1,
    );
    await page.locator("#room-issue-details").click();
    await screenshot("issue-after-restart");
    check(
      "restart preserves send issues and settings, clears runtime tasks and performs no repeat send",
    );
    await rpc("SetExtensionEnabled", channel, false);
    await rpc("UninstallExtension", channel);
    await wait(async () =>
      (await page.locator(".room-header p").innerText()).includes("已停用"),
    );
    assert((await room("notice")).issues.length > 0);
    check(
      "uninstalled channel history and persisted issues remain inspectable",
    );
    assert.deepEqual(report.pageErrors, []);
    report.passed = true;
  } catch (e) {
    report.error = e.stack;
    if (page) {
      report.failureBody = await page
        .locator("body")
        .innerText()
        .catch(() => "");
      await screenshot("failure").catch(() => {});
    }
    throw e;
  } finally {
    if (browser) await browser.close();
    await stop();
    if (server) await new Promise((resolve) => server.close(resolve));
    fs.closeSync(log);
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
      { mode: 0o600 },
    );
    console.log(
      JSON.stringify({
        passed: report.passed,
        checks: report.checks.length,
        root,
      }),
    );
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
