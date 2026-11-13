const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const fixture = path.resolve(process.argv[3]);
const expectedMessages =
  JSON.parse(fs.readFileSync(path.join(fixture, "report.json")))
    .fixtureMessages ?? 9;
const root = path.resolve(
  process.argv[4] || "build/wecom-local-ui-" + Date.now(),
);
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const home = path.join(root, "home");
fs.mkdirSync(home, { mode: 0o700 });
const osState = path.join(root, "os-state.json");
fs.writeFileSync(
  osState,
  JSON.stringify({ trusted: false, available: false, taken: false }),
);
const report = {
  passed: false,
  checks: [],
  coverage:
    "Headless Chromium with hidden e2e native app; real TS reader and Room service; no WKWebView pointer or native-window acceptance.",
};
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
    const previewFile = path.join(root, "preview-url");
    const log = fs.openSync(path.join(root, "app.log"), "w");
    proc = spawn(binary, ["--home", home], {
      stdio: ["ignore", log, log],
      env: {
        ...process.env,
        AICAT_E2E_BACKGROUND: "1",
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
    await page
      .locator("#wecom-local-key")
      .fill(path.join(fixture, "keys.json"));
    await page.locator("#wecom-local-save").click();
    await wait(
      async () =>
        (await rpc("State")).messageChannels["wecom-local"].configured,
    );
    await page.screenshot({ path: path.join(root, "local-settings.png") });
    report.checks.push(
      "client settings save paths in aicat.yaml and validate real encrypted snapshot",
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
    assert.equal(await page.locator("#wecom-send-form").count(), 0);
    assert.equal(await page.locator("#wecom-agent-form").count(), 0);
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
    assert(!fs.existsSync(path.join(home, "credentials")));
    assert.deepEqual(errors, []);
    report.checks.push(
      "disabling client reclaims its Bun host and leaves BOT stopped",
    );
    report.passed = true;
  } finally {
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
  console.error(e);
  process.exitCode = 1;
});
