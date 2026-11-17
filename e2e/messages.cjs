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
    "Headless Chromium, hidden e2e desktop, installed NPM channel, real Room storage and settings services. Codex model list and platform are protocol fixtures; no native-window or live-platform coverage.",
  hashes: Object.fromEntries(
    [
      binary,
      "ui/src/components/MessagesPage.vue",
      "ui/src/components/RoomSettings.vue",
      "ui/index.html",
      "internal/messages/presentation.go",
      "internal/messages/catalog.go",
      "runtime/channel-host/native.mjs",
      "ui/src/app.css",
      "internal/messages/triggers.go",
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
      (await page.locator("#room-settings").isVisible()),
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
          room: {
            trigger: {
              chat: { enabled: true },
              regex: { enabled: true, pattern: ".*" },
            },
          },
        },
      }),
    );
    fs.writeFileSync(
      path.join(pkg, "index.mjs"),
      `import fs from 'node:fs';import path from 'node:path';export function createChannel(api){return {open(){return {}},close(){},async observe(event){if(event.query.presentation)await api.updateRoom(event.query.presentation);for(const message of event.query.messages || (event.query.message?[event.query.message]:[]))await api.publish(message);return {}},send(event){fs.appendFileSync(path.join(api.stateDirectory,'sent.jsonl'),JSON.stringify(event)+'\\n');if(event.data.text.content.includes('uncertain'))throw Error('PLATFORM_UNCERTAIN');return {confirmation:'fixture_platform',chatId:event.data.roomid}}}}`,
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
    check("empty Room management page offers the add-channel entry");
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
    await emit("orders", "order-1", "订单 1001 已创建", { room_type: "group" });
    await emit("notice", "notice-1", "系统公告");
    await rpc("ReadExtensionData", channel, {
      presentation: {
        roomid: "orders",
        name: "订单协作群",
        kind: "group",
        avatar: "",
        members: {},
      },
    });
    await wait(async () => (await list()).length === 2);
    const orders = (await list()).find((r) => r.conversation === "orders");
    const notice = (await list()).find((r) => r.conversation === "notice");
    await wait(async () => (await page.locator(".room-item").count()) === 2);
    const item = page.locator('[data-room-id="' + orders.id + '"]');
    await wait(async () => (await item.innerText()).includes("订单协作群"));
    const itemText = await item.innerText();
    assert(itemText.includes("验收渠道") && itemText.includes("未自动处理"));
    await page.locator("#room-search").fill("订单");
    assert.equal(await page.locator(".room-item").count(), 1);
    await page.locator("#room-search").fill(notice.id.slice(0, 12));
    assert.equal(await page.locator(".room-item").count(), 1);
    await page.locator("#room-search").fill("验收渠道");
    assert.equal(await page.locator(".room-item").count(), 2);
    await page.locator("#room-search").fill("missing-room");
    assert.equal(await page.locator(".room-item").count(), 0);
    assert(
      (await page.locator(".room-empty-list").innerText()).includes(
        "没有匹配的 Room",
      ),
    );
    await page.locator("#room-search").fill("");
    check(
      "Room list shows name, channel and automatic state and searches by name, channel and ID",
    );
    await openRoom(orders.id);
    assert.equal(
      await page.locator(".room-header h1").innerText(),
      "订单协作群",
    );
    assert.equal(
      await page
        .locator(
          "#room-messages, .room-message, #room-tasks, #room-filter-issues",
        )
        .count(),
      0,
    );
    check(
      "Room management page shows settings without chat history, tasks or send records",
    );
    assert.equal(await page.locator("#room-model").isVisible(), false);
    assert.equal(await page.locator("#room-prompt").isVisible(), false);
    assert.equal(await page.locator("#room-trigger-self").isVisible(), false);
    assert(!calls.includes("Desktop.ListRoomModels"));
    await page.locator("#room-advanced > summary").click();
    await wait(
      async () => (await page.locator("#room-model option").count()) === 3,
    );
    assert(calls.includes("Desktop.ListRoomModels"));
    check(
      "advanced settings stay collapsed and load the model list only when opened",
    );
    await page.locator("#room-prompt").fill("UNSAVED_ROOM_PROMPT");
    await rpc("SetPreferences", { language: "zh-Hans", theme: "dark" });
    await pause(300);
    assert.equal(
      await page.locator("#room-prompt").inputValue(),
      "UNSAVED_ROOM_PROMPT",
    );
    await page.locator('[data-room-id="' + notice.id + '"]').click();
    await page
      .locator("#dialog-title")
      .filter({ hasText: "放弃未保存的设置" })
      .waitFor();
    await page.locator("#dialog footer button").first().click();
    await page.locator("#dialog-title").waitFor({ state: "hidden" });
    assert.equal(await item.getAttribute("aria-current"), "true");
    assert.equal(
      await page.locator("#room-prompt").inputValue(),
      "UNSAVED_ROOM_PROMPT",
    );
    check(
      "drafts survive state refresh and switching Room asks before discarding them",
    );
    await page.locator("#room-context").fill("[]");
    await page.locator("#room-settings-save").click();
    await wait(async () =>
      (await page.locator("#room-settings [role=alert]").innerText()).includes(
        "上下文必须是键值均为文本的 JSON 对象",
      ),
    );
    await page.locator("#room-context").fill('{"scope":"message-e2e"}');
    await page.locator(".room-trigger > summary").click();
    await page.locator("#room-trigger-self").check();
    await page.locator("#room-settings-save").click();
    await wait(async () =>
      (await page.locator("#room-settings [role=alert]").innerText()).includes(
        "启用自己发送的消息时必须填写正则表达式",
      ),
    );
    assert.equal(
      (await rpc("ReadRoomByID", orders.id)).settings.trigger.self.enabled,
      false,
    );
    check(
      "invalid context and an empty self pattern are rejected without saving",
    );
    await page.locator("#room-self-pattern").fill("^#run");
    await page.locator("#room-regex-pattern").fill("订单");
    await page.locator("#room-automatic").check();
    await page.locator("#room-model").selectOption("room-b");
    await page.locator("#room-name").fill("订单处理");
    await page.locator("#room-settings-save").click();
    await wait(
      async () =>
        (await rpc("ReadRoomByID", orders.id)).settings.name === "订单处理",
    );
    const saved = (await rpc("ReadRoomByID", orders.id)).settings;
    assert.deepEqual(saved.trigger, {
      chat: { enabled: true },
      regex: { enabled: true, pattern: "订单" },
      self: { enabled: true, pattern: "^#run" },
    });
    assert(
      saved.automatic &&
        saved.model === "room-b" &&
        saved.prompt === "UNSAVED_ROOM_PROMPT" &&
        saved.context.scope === "message-e2e",
    );
    const stored = JSON.parse(
      fs.readFileSync(path.join(home, "rooms", orders.id, "room.json"), "utf8"),
    );
    assert.deepEqual(stored.settings.trigger, {
      self: { enabled: true, pattern: "^#run" },
      regex: { pattern: "订单" },
    });
    await wait(
      async () =>
        (await item.innerText()).includes("自动处理") &&
        !(await item.innerText()).includes("未自动处理"),
    );
    check(
      "settings save chat, regex and self triggers as room.json overrides and update the list",
    );
    await screenshot("rooms-dark");
    await rpc("SetPreferences", { language: "en", theme: "light" });
    await wait(async () =>
      (await page.locator(".room-trigger").innerText()).includes(
        "Messages sent by this account",
      ),
    );
    await screenshot("rooms-en-light");
    for (const width of [1180, 860]) {
      await page.setViewportSize({ width, height: 800 });
      await pause(200);
      assert(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      );
      await screenshot("rooms-" + width);
    }
    await rpc("SetPreferences", { language: "zh-Hans", theme: "light" });
    check(
      "English, light and dark themes and 1180/860 widths keep the Room page usable",
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
