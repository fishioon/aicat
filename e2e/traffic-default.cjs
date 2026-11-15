// Failure modes: traffic entry hidden by default; recording starts before user
// action; toggles lose session state; restart starts recording unexpectedly;
// paused capture blocks forwarding. This scenario runs with a headless browser
// and a hidden native application; it does not drive WKWebView or native menus.
// Usage: node e2e/traffic-default.cjs bin/aicat-e2e build/traffic-default-e2e
// Capture labels must distinguish never started from paused, including clear
// and extension toggles that must not erase the session's started state.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

module.exports = async function verifyTrafficDefault({
  page,
  root,
  home,
  proxyPort,
  send,
  api,
  current,
  wait,
  launch,
  stop,
}) {
  const configFile = path.join(home, "aicat.yaml");
  const checks = [];
  const state = async (enabled, recording) => {
    await wait(async () => {
      const value = await current();
      return value.trafficEnabled === enabled && value.recording === recording;
    });
    assert.equal(await page.locator("#nav-traffic").count(), enabled ? 1 : 0);
  };
  const unrecorded = async (url) => {
    await send(url);
    assert(!(await api()).some((row) => row.url.endsWith(url)));
  };
  const recorded = async (url) => {
    await send(url);
    await wait(async () =>
      (await api()).some(
        (row) => row.url.endsWith(url) && row.state === "completed",
      ),
    );
  };
  const toggleExtension = async () => {
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-traffic").click();
    await page.locator("#feature-traffic").click();
  };
  const action = async (zh, en, started) => {
    const english = await page.evaluate(
      () => document.documentElement.lang === "en",
    );
    await wait(
      async () =>
        (await page.locator("#recording-label").innerText()) ===
        (english ? en : zh),
    );
    assert.equal((await current()).captureStarted, started);
  };
  await state(true, false);
  assert.match(fs.readFileSync(configFile, "utf8"), /traffic:\s+enabled: true/);
  assert.match(fs.readFileSync(configFile, "utf8"), /recording: false/);
  await unrecorded("/fresh-paused");
  assert.equal((await api()).length, 0);
  await page.locator("#nav-traffic").click();
  await page.locator("#rows-viewport").waitFor();
  await action("开始采集", "Start capture", false);
  assert.match(
    await page.locator("#list-empty .btn.primary").innerText(),
    /开始采集|Start capture/,
  );
  await page.screenshot({
    path: path.join(root, "default-visible-paused.png"),
  });
  checks.push(
    "fresh Home: traffic enabled and visible; capture paused; proxy forwards without recording",
  );

  await page.locator("#list-empty .btn.primary").click();
  await state(true, true);
  await action("暂停采集", "Pause capture", true);
  await page.locator("#recording").click();
  await state(true, false);
  await action("继续采集", "Resume capture", true);
  assert.match(
    await page.locator("#list-empty .btn.primary").innerText(),
    /继续采集|Resume capture/,
  );
  await page.screenshot({
    path: path.join(root, "paused-before-first-request.png"),
  });
  await page.locator("#list-empty .btn.primary").click();
  await state(true, true);
  await recorded("/manually-recorded");
  await page
    .locator(".traffic-row", { hasText: "/manually-recorded" })
    .waitFor();
  await page.screenshot({ path: path.join(root, "manual-recording.png") });
  await toggleExtension();
  await state(false, true);
  await unrecorded("/disabled-while-recording");
  await toggleExtension();
  await state(true, true);
  await recorded("/resumed-after-toggle");
  await page.locator("#nav-traffic").click();
  await page.locator("#recording").click();
  await state(true, false);
  await page.locator("#clear").click();
  await action("继续采集", "Resume capture", true);
  await page.locator("#recording").click();
  await state(true, true);
  await stop();
  await launch();
  await state(true, false);
  await unrecorded("/restart-paused");
  checks.push(
    "manual recording works; extension toggles preserve recording; restart returns to paused capture with traffic visible",
  );

  await page.locator("#nav-traffic").click();
  await action("开始采集", "Start capture", false);
  await toggleExtension();
  await state(false, false);
  await toggleExtension();
  await state(true, false);
  await unrecorded("/still-paused");
  await page.locator("#nav-traffic").click();
  await action("开始采集", "Start capture", false);
  await toggleExtension();
  await state(false, false);
  await stop();
  await launch();
  await state(false, false);
  await unrecorded("/disabled-after-restart");
  checks.push(
    "paused capture survives extension toggles; explicit extension disable persists across restart",
  );

  const base = `gateway:\n  addr: 127.0.0.1:${proxyPort}\nui:\n  language: zh-Hans\n  theme: light\n`;
  for (const [name, section, enabled, recording] of [
    ["omitted", "", true, false],
    ["empty", "extensions: {}\n", true, false],
    ["null", "extensions: null\n", true, false],
    [
      "explicit-on",
      "extensions:\n  traffic:\n    enabled: true\n",
      true,
      false,
    ],
    [
      "explicit-off",
      "extensions:\n  traffic:\n    enabled: false\n",
      false,
      false,
    ],
    ["explicit-recording", "traffic:\n  recording: true\n", true, true],
  ]) {
    await stop();
    fs.writeFileSync(configFile, base + section, { mode: 0o600 });
    await launch();
    await state(enabled, recording);
    if (enabled) {
      await page.locator("#nav-traffic").click();
      await action(
        recording ? "暂停采集" : "开始采集",
        recording ? "Pause capture" : "Start capture",
        recording,
      );
    }
    if (recording) await recorded("/config-" + name);
    else await unrecorded("/config-" + name);
    checks.push(`${name}: traffic=${enabled}, recording=${recording}`);
  }
  return { passed: true, checks };
};

async function run({
  verify = module.exports,
  scenario = "traffic-default",
  fixture = { services: [], trusted: false, recordProxyWrites: true },
} = {}) {
  const http = require("node:http");
  const crypto = require("node:crypto");
  const { spawn, execFileSync } = require("node:child_process");
  const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
  const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
  const root = path.resolve(
    process.argv[3] || fs.mkdtempSync(`build/${scenario}-headless-`),
  );
  fs.mkdirSync(root, { recursive: true, mode: 0o700 });
  const home = path.join(root, "home");
  fs.mkdirSync(home, { mode: 0o700 });
  const stateFile = path.join(root, "os-state.json");
  const previewFile = path.join(root, "preview-url");
  fs.writeFileSync(stateFile, JSON.stringify(fixture), { mode: 0o600 });
  const hash = (file) =>
    crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
  const report = {
    passed: false,
    scenario,
    mode: {
      browser: "headless Chromium",
      application: "AICAT_E2E_BACKGROUND=1",
      nativeAutomation: false,
    },
    coverage:
      "browser pointer input, embedded frontend and real desktop services; no native window/menu input",
    binary,
    binarySHA256: hash(binary),
    command: ["node", `e2e/${scenario}.cjs`, binary, root],
    sources: Object.fromEntries(
      [
        "e2e/traffic-default.cjs",
        "e2e/os-fixture.py",
        `e2e/${scenario}.cjs`,
        "internal/app/app.go",
        "internal/app/desktop.go",
        "internal/app/tray.go",
        "internal/app/runtime.go",
        "internal/app/ui_automation_e2e.go",
        "internal/config/config.go",
        "ui/src/components/TrafficPage.vue",
        "ui/src/translations.json",
      ].map((file) => [file, hash(file)]),
    ),
  };
  let proc, browser, page, upstream, log, proxyPort, fixturePort;
  const errors = [];
  const wait = async (fn, ms = 15000) => {
    const until = Date.now() + ms;
    let last;
    while (Date.now() < until) {
      try {
        if (await fn()) return;
      } catch (error) {
        last = error;
      }
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    throw new Error("timeout: " + (last?.stack || "condition not met"));
  };
  const request = (url) =>
    new Promise((resolve, reject) => {
      const req = http.get(
        { host: "127.0.0.1", port: proxyPort, path: url },
        (res) => {
          const chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () =>
            resolve({
              status: res.statusCode,
              data: Buffer.concat(chunks).toString(),
            }),
          );
          res.on("error", reject);
        },
      );
      req.setTimeout(5000, () =>
        req.destroy(new Error("fixture request timed out")),
      );
      req.on("error", reject);
    });
  const preview = (pathname, body) =>
    new Promise((resolve, reject) => {
      const address = new URL(fs.readFileSync(previewFile, "utf8"));
      const req = http.request(
        {
          host: address.hostname,
          port: address.port,
          path: pathname,
          method: body ? "POST" : "GET",
          headers: {
            "X-Aicat-Preview": address.searchParams.get("token"),
            "Content-Type": "application/json",
          },
        },
        (res) => {
          const chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () =>
            resolve({
              status: res.statusCode,
              data: Buffer.concat(chunks).toString(),
            }),
          );
        },
      );
      req.setTimeout(5000, () => req.destroy(new Error("preview timed out")));
      req.on("error", reject);
      req.end(body ? JSON.stringify(body) : undefined);
    });
  const stop = async () => {
    if (!proc) return;
    const child = proc;
    if (child.exitCode === null && child.signalCode === null)
      child.kill("SIGTERM");
    try {
      await wait(() => child.exitCode !== null || child.signalCode !== null);
      assert.equal(child.exitCode, 0, "background application exits cleanly");
    } finally {
      if (child.exitCode === null && child.signalCode === null)
        child.kill("SIGKILL");
      proc = undefined;
    }
  };
  const launch = async ({ configuredListener = false } = {}) => {
    fs.rmSync(previewFile, { force: true });
    const env = {
      ...process.env,
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
      AICAT_E2E_BACKGROUND: "1",
    };
    delete env.MYGO_AUTOMATION;
    proc = spawn(
      binary,
      [
        "--home",
        home,
        ...(configuredListener ? [] : ["--addr", `127.0.0.1:${proxyPort}`]),
      ],
      {
        env,
        stdio: ["ignore", log, log],
      },
    );
    let spawnError;
    proc.on("error", (error) => {
      spawnError = error;
    });
    await wait(() => {
      if (spawnError) throw spawnError;
      assert.equal(proc.exitCode, null, "background application stays running");
      return fs.existsSync(previewFile);
    });
    await wait(
      async () =>
        (await preview("/rpc", { method: "Desktop.State", args: [] }))
          .status === 200,
    );
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.waitForFunction(() => !!window.desktop?.state());
    await page.locator("#overview").waitFor();
  };
  try {
    upstream = http.createServer((req, res) => {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ path: req.url, ok: true }));
    });
    await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
    fixturePort = upstream.address().port;
    const reserve = require("node:net").createServer();
    await new Promise((resolve) => reserve.listen(0, "127.0.0.1", resolve));
    proxyPort = reserve.address().port;
    await new Promise((resolve) => reserve.close(resolve));
    log = fs.openSync(path.join(root, "app.log"), "a");
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    page = await browser.newPage({ viewport: { width: 1180, height: 900 } });
    page.on("pageerror", (error) => errors.push(error.message));
    await launch();
    report[scenario === "traffic-default" ? "trafficDefault" : scenario] =
      await verify({
        page,
        root,
        home,
        proxyPort,
        fixturePort,
        request: (_port, target) => request(target),
        navigate: (route) => page.locator("#nav-" + route).click(),
        settled: () => page.waitForFunction(() => !document.body.classList.contains("busy")),
        wait,
        launch,
        stop,
        preview,
        fixture: (update) =>
          JSON.parse(
            execFileSync(
              "python3",
              [path.join(__dirname, "os-fixture.py"), stateFile],
              {
                input: JSON.stringify(update),
                encoding: "utf8",
                timeout: 5000,
              },
            ),
          ),
        current: () => page.evaluate(() => window.desktop.state()),
        api: async () => {
          const response = await request("/api/traffic?limit=2000");
          assert.equal(response.status, 200);
          return JSON.parse(response.data).items;
        },
        send: async (name) => {
          const response = await request(
            `http://127.0.0.1:${fixturePort}${name}`,
          );
          assert.equal(response.status, 200);
          return response;
        },
      });
    assert.deepEqual(errors, []);
    report.browserVersion = browser.version();
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    if (page)
      await page
        .screenshot({ path: path.join(root, "failure.png") })
        .catch(() => {});
  } finally {
    try {
      await stop();
    } catch (error) {
      report.passed = false;
      report.cleanupError = error.stack;
    }
    if (browser) await browser.close();
    if (upstream) await new Promise((resolve) => upstream.close(resolve));
    if (log !== undefined) fs.closeSync(log);
    report.browserErrors = errors;
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(
      JSON.stringify({
        passed: report.passed,
        artifacts: root,
        error: report.error,
      }),
    );
    if (!report.passed) process.exitCode = 1;
  }
}

module.exports.run = run;
if (require.main === module)
  run().catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
