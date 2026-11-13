const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
let fixture = path.resolve(process.argv[3]);
const expectedMessages =
  JSON.parse(fs.readFileSync(path.join(fixture, "report.json")))
    .fixtureMessages ?? 9;
const root = path.resolve(
  process.argv[4] || "build/wecom-local-ui-" + Date.now(),
);
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const home = path.join(root, "home");
fs.mkdirSync(home, { mode: 0o700 });
if (process.argv[5] === "send-review") {
  const copy = path.join(root, "fixture");
  fs.cpSync(fixture, copy, { recursive: true });
  fixture = copy;
}
const osState = path.join(root, "os-state.json");
fs.writeFileSync(
  osState,
  JSON.stringify({ trusted: false, available: false, taken: false }),
);
const report = {
  binarySHA256: require("node:crypto")
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  passed: false,
  checks: [],
  coverage:
    "Headless Chromium with hidden e2e native app; real TS reader and Room service; no WKWebView pointer or native-window acceptance.",
};
const sendReview = process.argv[5] === "send-review";
const review = process.argv[5] === "review" || sendReview;
let proc, browser, preview, token;
const errors = [];
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
async function wait(fn) {
  const deadline = Date.now() + 25000;
  let last;
  while (Date.now() < deadline) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (e) {
      last = e;
    }
    await pause(100);
  }
  throw Error("timeout " + (last?.message || fn.toString()));
}
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
async function main() {
  try {
    const net = require("node:net");
    const listener = net.createServer();
    await new Promise((r) => listener.listen(0, "127.0.0.1", r));
    const port = listener.address().port;
    await new Promise((r) => listener.close(r));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `gateway:\n  addr: 127.0.0.1:${port}\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n`,
    );
    const sendScript = path.join(root, "native-send-stub.py");
    if (sendReview)
      fs.writeFileSync(
        sendScript,
        `#!/usr/bin/python3
import sys,time,json,sqlite3,importlib.util,os
from pathlib import Path
root=Path(${JSON.stringify(root)})
fixture=Path(${JSON.stringify(fixture)})
request=json.load(sys.stdin)
with (root/'script-started').open('a') as f:f.write('started\\n')
if (root/'script-success').exists():
 os.environ['PATH']='/usr/bin:/bin'
 spec=importlib.util.spec_from_file_location('fixture',${JSON.stringify(path.resolve("e2e/wecom-local.py"))})
 module=importlib.util.module_from_spec(spec)
 spec.loader.exec_module(module)
 db=sqlite3.connect(fixture/'plain-fixture.db')
 db.execute('PRAGMA journal_mode=WAL')
 db.execute('PRAGMA wal_autocheckpoint=0')
 number=db.execute('SELECT MAX(message_id) FROM message_table').fetchone()[0]+1
 db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)',(number,number,number,1,'group-fixture',2,2,2,module.proto(request['text'])))
 db.commit()
 module.publish(fixture/'plain-fixture.db',fixture/'database')
 db.close()
 sys.exit(0)
limit=time.monotonic()+10
while not (root/'release-script').exists() and time.monotonic()<limit:time.sleep(.04)
sys.exit(23)
`,
        { mode: 0o700 },
      );
    const previewFile = path.join(root, "preview-url");
    const log = fs.openSync(path.join(root, "app.log"), "w");
    proc = spawn(binary, ["--home", home], {
      stdio: ["ignore", log, log],
      env: {
        ...process.env,
        AICAT_E2E_BACKGROUND: "1",
        ...(sendReview ? { AICAT_E2E_WECOM_SEND_SCRIPT: sendScript } : {}),
        AICAT_E2E_PREVIEW: previewFile,
        AICAT_E2E_OS_STATE: osState,
      },
    });
    fs.closeSync(log);
    await wait(() => {
      if (proc.exitCode !== null) throw Error("app exited");
      return fs.existsSync(previewFile);
    });
    const url = new URL(fs.readFileSync(previewFile, "utf8"));
    preview = url.origin;
    token = url.searchParams.get("token");
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    const page = await browser.newPage({
      viewport: { width: 1180, height: 900 },
    });
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(url.href);
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom-local").click();
    assert.equal(await page.locator("#extension-wecom").count(), 1);
    await page.screenshot({ path: path.join(root, "separate-extensions.png") });
    report.checks.push(
      "client extension and BOT appear separately; client needs no BOT installation",
    );
    await page.locator("#extension-configure").click();
    await page.locator("#wecom-local-account").fill("synthetic-account");
    await page.locator("#wecom-local-dir").fill(path.join(fixture, "database"));
    const key = JSON.parse(
      fs.readFileSync(path.join(fixture, "fixture-key.json")),
    ).key;
    assert.equal(
      await page.locator("#wecom-local-key").getAttribute("type"),
      "password",
    );
    await page.locator("#wecom-local-key").fill(key);
    await page.locator("#wecom-local-save").click();
    await wait(
      async () =>
        (await rpc("State")).messageChannels["wecom-local"].configured,
    );
    await wait(
      async () => (await page.locator("#wecom-local-key").inputValue()) === "",
    );
    const credentialFile = path.join(home, "credentials/wecom-local.json");
    const saved = fs.readFileSync(credentialFile, "utf8");
    assert.equal(fs.statSync(credentialFile).mode & 0o777, 0o600);
    assert(!JSON.stringify(await rpc("State")).includes(key));
    assert(
      !fs.readFileSync(path.join(home, "aicat.yaml"), "utf8").includes(key),
    );
    await assert.rejects(
      rpc("ConfigureMessageChannel", [
        "wecom-local",
        {
          local: {
            accountId: "synthetic-account",
            dataDir: path.join(fixture, "database"),
          },
          secret: "/////////////////////w==",
        },
      ]),
    );
    assert.equal(fs.readFileSync(credentialFile, "utf8"), saved);
    await rpc("ConfigureMessageChannel", [
      "wecom-local",
      {
        local: {
          accountId: "synthetic-account",
          dataDir: path.join(fixture, "database"),
        },
        secret: "",
      },
    ]);
    assert.equal(fs.readFileSync(credentialFile, "utf8"), saved);
    await assert.rejects(
      rpc("ConfigureMessageChannel", [
        "wecom-local",
        {
          local: {
            accountId: "another-account",
            dataDir: path.join(fixture, "database"),
          },
          secret: "",
        },
      ]),
    );
    assert.equal(fs.readFileSync(credentialFile, "utf8"), saved);
    await page.screenshot({ path: path.join(root, "local-settings.png") });
    if (review) {
      const account = page.locator("#wecom-local-account");
      const directory = page.locator("#wecom-local-dir");
      await account.fill("next-account-draft");
      await directory.fill("/next/database/draft");
      if (
        (await page.locator("#wecom-local-key").getAttribute("type")) ===
        "password"
      ) {
        assert(await page.locator("#wecom-local-save").isDisabled());
        assert.equal(
          await page.locator("#wecom-local-key").getAttribute("required"),
          "",
        );
      }
      await page.locator("#theme").click();
      await page.waitForFunction(
        () => !document.body.classList.contains("busy"),
      );
      assert.equal(
        await account.inputValue(),
        "next-account-draft",
        "unrelated preference refresh must preserve the account draft",
      );
      assert.equal(
        await directory.inputValue(),
        "/next/database/draft",
        "unrelated preference refresh must preserve the directory draft",
      );
      await page.screenshot({
        path: path.join(root, "local-draft-preserved.png"),
      });
      await account.fill("synthetic-account");
      await directory.fill(path.join(fixture, "database"));
      if (
        (await page.locator("#wecom-local-key").getAttribute("type")) ===
        "password"
      ) {
        assert(await page.locator("#wecom-local-save").isEnabled());
        await directory.fill(path.join(fixture, "database") + "/.");
        assert(await page.locator("#wecom-local-save").isEnabled());
        await directory.fill(path.join(fixture, "database"));
      }
      report.checks.push(
        "unrelated preference refresh preserves unsaved local account and database drafts",
      );
    }
    report.checks.push(
      "client settings save Base64 key privately and validate stock SQLite3MC encrypted reads",
      "password input clears after save; public state and YAML omit the key; invalid replacement preserves credentials; blank retains only the matching account and directory",
    );
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom-local").click();
    await page.locator("#feature-wecom-local").click();
    await wait(
      async () =>
        (await rpc("State")).messageChannels["wecom-local"].records.length ===
        expectedMessages,
    );
    await page.locator("#extension-diagnostics").click();
    await page.locator("#wecom-local-messages tr").first().waitFor();
    if (review) {
      await rpc("SetPreferences", [{ language: "zh-Hans", theme: "light" }]);
      await page.waitForFunction(
        () => document.documentElement.lang === "zh-Hans",
      );
      const fixtureData = JSON.parse(
        fs.readFileSync(path.join(fixture, "report.json")),
      );
      if (fixtureData.longMessage) {
        const outgoing = page.locator("#wecom-local-messages tr", {
          hasText: "自己发出的消息",
        });
        assert.equal(
          (await outgoing.locator(".tag").innerText()).trim(),
          "发出",
        );
        assert(
          (
            await page
              .locator("#wecom-local-messages tr", { hasText: "图片元数据" })
              .innerText()
          ).includes("图片（仅元数据）"),
        );
        const event = page.locator("#wecom-local-messages details").filter({
          has: page.locator("pre", { hasText: fixtureData.longMessage }),
        });
        assert.equal(await event.getAttribute("open"), null);
        assert((await event.locator("summary").innerText()).length <= 81);
        await event.locator("summary").click();
        assert.equal(
          await event.locator("pre").textContent(),
          fixtureData.longMessage,
        );
        await event.locator("summary").click();
        report.checks.push(
          "incoming/outgoing and media metadata labels are explicit; full Unicode/multiline text survives collapsed previews",
        );
      }
    }
    assert.equal(await page.locator("#wecom-send-form").count(), 0);
    assert.equal(await page.locator("#wecom-agent-form").count(), 0);
    if (sendReview) {
      report.coverage +=
        " MCP uses real message services and Bun; native send script is replaced by a controlled stub and synthetic encrypted local echo, with no native UI or real messages.";
      const event = {
        specversion: "1.0",
        id: "diagnostics-send",
        source: "aicat-agent://mcp",
        type: "org.aicat.message.send.requested.v1",
        datacontenttype: "application/json",
        data: {
          action: "send",
          roomid: "group-fixture",
          search_name: "Synthetic room",
          msgtype: "text",
          text: { content: "合成发送请求，不操作真实客户端" },
        },
      };
      const send = async () => {
        const response = await fetch(`http://127.0.0.1:${port}/mcp`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json, text/event-stream",
            Authorization:
              "Bearer " +
              fs.readFileSync(path.join(home, "mcp-token"), "utf8").trim(),
          },
          body: JSON.stringify({
            jsonrpc: "2.0",
            id: 1,
            method: "tools/call",
            params: {
              name: "messages_send",
              arguments: { channel: "wecom-local", event },
            },
          }),
        });
        assert.equal(response.status, 200);
        const result = (await response.json()).result;
        assert(!result.isError, JSON.stringify(result));
        return result.structuredContent ?? JSON.parse(result.content[0].text);
      };
      const pending = send();
      pending.catch(() => {});
      await wait(() => fs.existsSync(path.join(root, "script-started")));
      const records = (await rpc("State")).messageChannels["wecom-local"]
        .records;
      fs.writeFileSync(
        path.join(root, "send-state-proof.json"),
        JSON.stringify({
          pendingVisible: records.some(
            (row) => row.event.id === event.id && row.status === "pending",
          ),
        }),
      );
      assert(
        records.some(
          (row) => row.event.id === event.id && row.status === "pending",
        ),
        "pending MCP submission must appear in diagnostics state",
      );
      const row = page.locator("#wecom-local-messages tr", {
        hasText: event.data.text.content,
      });
      await wait(
        async () =>
          (await row.locator(".tag").innerText()).trim() === "开始发送",
      );
      await row.locator("summary").click();
      await page.screenshot({ path: path.join(root, "send-pending.png") });
      fs.writeFileSync(path.join(root, "release-script"), "release");
      assert.equal((await pending).status, "unknown");
      await wait(
        async () =>
          (await row.locator(".tag").innerText()).trim() === "结果不明",
      );
      assert.equal(await row.count(), 1);
      assert.equal(
        await row.locator("details").getAttribute("open"),
        "",
        "status updates preserve the expanded message",
      );
      assert.equal((await send()).status, "unknown");
      assert.equal(
        fs
          .readFileSync(path.join(root, "script-started"), "utf8")
          .trim()
          .split("\n").length,
        1,
      );
      assert.equal(
        (await rpc("State")).messageChannels["wecom-local"].records.length,
        expectedMessages + 1,
      );
      await page.screenshot({ path: path.join(root, "send-unknown.png") });
      fs.writeFileSync(path.join(root, "script-success"), "success");
      event.id = "diagnostics-confirmed";
      event.data.text.content = "合成已确认消息，只写入测试数据库";
      assert.equal((await send()).status, "sent");
      const confirmed = page.locator("#wecom-local-messages tr", {
        hasText: event.data.text.content,
      });
      await wait(async () =>
        (await confirmed.locator(".tag").allTextContents()).some(
          (value) => value.trim() === "已发送",
        ),
      );
      await wait(async () =>
        (await confirmed.locator(".tag").allTextContents()).some(
          (value) => value.trim() === "发出",
        ),
      );
      const sentState = (await rpc("State")).messageChannels["wecom-local"]
        .records;
      assert.equal(
        sentState.filter((item) => item.event.id === event.id).length,
        1,
      );
      assert.equal(
        sentState.find((item) => item.event.id === event.id).status,
        "sent",
      );
      assert.equal(sentState.length, expectedMessages + 3);
      await page.screenshot({ path: path.join(root, "send-confirmed.png") });
      report.checks.push(
        "real MCP submission appears pending, updates the same row to unknown without collapsing text; idempotent retry does not execute again; encrypted local echo confirms sent and appears separately as an outgoing observation",
      );
    }
    await page.locator("#wecom-local-open-logs").click();
    await wait(
      () =>
        JSON.parse(fs.readFileSync(osState)).messageLogsOpened ===
        path.join(home, "rooms"),
    );
    for (const theme of ["light", "dark"]) {
      await rpc("SetPreferences", [{ language: "zh-Hans", theme }]);
      await wait(() =>
        page.evaluate(
          (value) => document.documentElement.dataset.theme === value,
          theme,
        ),
      );
      await pause(200);
      await page.screenshot({
        path: path.join(root, "diagnostics-" + theme + ".png"),
      });
    }
    if (review) {
      await rpc("SetPreferences", [{ language: "en", theme: "dark" }]);
      await page.waitForFunction(() => document.documentElement.lang === "en");
      assert(
        (
          await page.locator("#wecom-local-messages .tag").allTextContents()
        ).some((value) => value.trim() === "Received"),
      );
      await page.screenshot({
        path: path.join(root, "diagnostics-en-dark.png"),
      });
    }
    report.checks.push(
      "enabled client imports local messages to Rooms; diagnostics offers logs without send or Agent controls",
    );
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom-local").click();
    const pid = (await rpc("State")).messageChannels["wecom-local"].pid;
    assert(pid > 0);
    await page.locator("#feature-wecom-local").click();
    await wait(
      async () =>
        (await rpc("State")).messageChannels["wecom-local"].phase === "stopped",
    );
    assert.throws(() => process.kill(pid, 0));
    assert.equal((await rpc("State")).messageChannels.wecom.phase, "stopped");
    assert(!fs.existsSync(path.join(home, "credentials/wecom.json")));
    assert(!fs.readFileSync(path.join(root, "app.log"), "utf8").includes(key));
    assert.deepEqual(errors, []);
    report.checks.push(
      "disabling client reclaims its Bun host and leaves BOT stopped",
    );
    report.passed = true;
  } finally {
    if (sendReview)
      fs.writeFileSync(path.join(root, "release-script"), "release");
    await browser?.close();
    if (proc && proc.exitCode === null) {
      const done = new Promise((r) => proc.once("exit", r));
      proc.kill("SIGTERM");
      await Promise.race([
        done,
        pause(10000).then(() => {
          if (proc.exitCode === null) proc.kill("SIGKILL");
        }),
      ]);
    }
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2) + "\n",
    );
    console.log(root);
  }
}
main().catch((e) => {
  report.error = e.stack;
  fs.writeFileSync(
    path.join(root, "report.json"),
    JSON.stringify(report, null, 2) + "\n",
  );
  console.error(e);
  process.exitCode = 1;
});
