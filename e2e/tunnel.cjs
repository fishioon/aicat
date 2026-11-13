// Failure modes: CONNECT replaces both panes; headers/Raw disappear; encrypted
// response exposes misleading tabs; domain enable/cancel loses historical state;
// switching back to HTTP breaks the response inspector.
// Usage: node e2e/tunnel.cjs bin/aicat-e2e build/tunnel-<run>
// Headless preview + real services; no native window interaction or authorization.
const assert = require("node:assert/strict");
const net = require("node:net");
const fs = require("node:fs");
const path = require("node:path");
async function verifyTunnel({
  page,
  root,
  proxyPort,
  fixturePort,
  send,
  api,
  wait,
}) {
  const checks = [];
  await new Promise((resolve, reject) => {
    const socket = net.connect(proxyPort, "127.0.0.1", () =>
      socket.write(
        `CONNECT localhost:${fixturePort} HTTP/1.1\r\nHost: localhost:${fixturePort}\r\nProxy-Connection: keep-alive\r\n\r\n`,
      ),
    );
    socket.setTimeout(5000, () => socket.destroy(new Error("CONNECT timeout")));
    socket.once("data", (data) => {
      assert.match(data.toString(), /200/);
      socket.end();
      resolve();
    });
    socket.once("error", reject);
  });
  const record = await wait(async () => (await api()).find((r) => r.tunnel));
  const row = `.traffic-row[data-id="${record.id}"]`;
  await page.locator(row).click();
  const request = page.locator('[data-side="request"]');
  const response = page.locator('[data-side="response"]');
  await response.locator("#enable-decryption").waitFor();
  assert.match(await request.locator(".kv").innerText(), /Host\s+localhost:/);
  assert.match(
    await request.locator(".kv").innerText(),
    /Proxy-Connection\s+keep-alive/i,
  );
  assert.equal(await response.locator(".ptabs").count(), 0);
  assert.equal(await request.locator("#enable-decryption").count(), 0);
  const left = await request.boundingBox(),
    right = await response.boundingBox();
  assert(left.x + left.width <= right.x && Math.abs(left.y - right.y) <= 1);
  await page.screenshot({ path: path.join(root, "tunnel-light.png") });
  await request.locator('[data-view="raw"]').click();
  assert.match(await request.locator(".raw").innerText(), /CONNECT localhost:/);
  await page.locator("#enable-decryption").click();
  await page.locator("#dialog").getByText("取消", { exact: true }).click();
  await page.locator("#enable-decryption").waitFor();
  checks.push(
    "CONNECT keeps request headers/Raw and side-by-side response prompt; cancel preserves entry",
  );

  await page.locator("#theme").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.screenshot({ path: path.join(root, "tunnel-dark.png") });
  await page.locator("#nav-settings").click();
  await page.locator("#settings-general").click();
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#nav-traffic").click();
  await page.locator("#enable-decryption").waitFor();
  assert.match(
    await response.innerText(),
    /HTTPS Response\s+This connection is encrypted; response content is unavailable\./,
  );
  assert.equal(
    await page.locator("#enable-decryption").innerText(),
    "Enable only this domain",
  );
  await page.screenshot({ path: path.join(root, "tunnel-en-dark.png") });
  await page.locator("#enable-decryption").click();
  await page
    .locator("#dialog")
    .getByText("Client trusts the CA separately", { exact: true })
    .click();
  await response.locator("#decryption-covered").waitFor();
  assert.equal(await page.locator("#enable-decryption").count(), 0);
  assert((await api()).find((r) => r.id === record.id).tunnel);
  checks.push(
    "Concise English copy; enabling domain keeps old tunnel encrypted and replaces button with new-connection notice",
  );

  await send("/tunnel-http-regression");
  await page
    .locator(".traffic-row", { hasText: "/tunnel-http-regression" })
    .click();
  await response.locator('[data-view="headers"]').waitFor();
  assert.equal(await page.locator("#tunnel-detail").count(), 0);
  await page.locator(row).click();
  await response.locator("#decryption-covered").waitFor();
  await request.locator('[data-view="raw"][aria-selected="true"]').waitFor();
  checks.push(
    "HTTP response inspector restored; request Raw preference survives tunnel/HTTP switches",
  );
  return { passed: true, checks };
}

const http = require("node:http");
const crypto = require("node:crypto");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function wait(fn, ms = 15000) {
  const until = Date.now() + ms;
  let last;
  while (Date.now() < until) {
    try {
      const value = await fn();
      if (value) return value;
    } catch (error) {
      last = error;
    }
    await sleep(50);
  }
  throw new Error("timeout " + (last?.stack || ""));
}
function request(port, url) {
  return new Promise((resolve, reject) => {
    const req = http.get({ host: "127.0.0.1", port, path: url }, (res) => {
      const bytes = [];
      res.on("data", (chunk) => bytes.push(chunk));
      res.on("end", () =>
        resolve({
          status: res.statusCode,
          data: Buffer.concat(bytes).toString(),
        }),
      );
      res.on("error", reject);
    });
    req.setTimeout(5000, () => req.destroy(new Error("HTTP timeout")));
    req.on("error", reject);
  });
}
async function main() {
  const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
  const root = path.resolve(
    process.argv[3] || `build/tunnel-headless-${Date.now()}`,
  );
  // Never remove prior proof artifacts when a directory is reused.
  fs.mkdirSync(root, { mode: 0o700 });
  const home = path.join(root, "home");
  fs.mkdirSync(home, { mode: 0o700 });
  const previewFile = path.join(root, "preview-url");
  const stateFile = path.join(root, "os-state.json");
  const hash = (file) =>
    crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
  const report = {
    passed: false,
    scenario: "tunnel",
    command: [process.execPath, ...process.argv.slice(1)],
    binary,
    binarySHA256: hash(binary),
    sourceSHA256: hash(__filename),
    mode: {
      browser: "headless",
      AICAT_E2E_BACKGROUND: "1",
      coverage:
        "Chromium preview with real Desktop services and proxy; OS trust fixture",
      nativeWindow: "not exercised",
      systemAuthorization: "not exercised",
    },
    checks: [],
    errors: [],
  };
  let proc, browser, page, log;
  const upstream = http.createServer((req, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ path: req.url }));
  });
  try {
    await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
    const fixturePort = upstream.address().port;
    const temp = net.createServer();
    await new Promise((resolve) => temp.listen(0, "127.0.0.1", resolve));
    const proxyPort = temp.address().port;
    await new Promise((resolve) => temp.close(resolve));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `traffic:\n  recording: true\nextensions:\n  traffic:\n    enabled: true\ngateway:\n  addr: 127.0.0.1:${proxyPort}\nui:\n  language: zh-Hans\n  theme: light\n`,
    );
    fs.writeFileSync(
      stateFile,
      JSON.stringify({ services: [], trusted: false, recordProxyWrites: true }),
      { mode: 0o600 },
    );
    log = fs.openSync(path.join(root, "app.log"), "a");
    const env = {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
    };
    // This scenario never connects the native automation socket.
    delete env.MYGO_AUTOMATION;
    proc = spawn(binary, ["--home", home], {
      env,
      stdio: ["ignore", log, log],
    });
    let spawnError;
    proc.once("error", (error) => {
      spawnError = error;
    });
    await wait(() => {
      if (spawnError) throw spawnError;
      if (proc.exitCode !== null)
        throw new Error(`app exited: ${proc.exitCode}`);
      return fs.existsSync(previewFile);
    });
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    report.browserVersion = browser.version();
    page = await browser.newPage({ viewport: { width: 1180, height: 900 } });
    page.on("pageerror", (error) => report.errors.push(error.message));
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.waitForFunction(() => !!window.desktop?.state());
    await page.locator("#nav-traffic").click();
    const api = async () =>
      JSON.parse((await request(proxyPort, "/api/traffic?limit=2000")).data)
        .items;
    const send = async (pathname) => {
      assert.equal(
        (await request(proxyPort, `http://127.0.0.1:${fixturePort}${pathname}`))
          .status,
        200,
      );
    };
    const result = await verifyTunnel({
      page,
      root,
      proxyPort,
      fixturePort,
      send,
      api,
      wait,
    });
    report.checks = result.checks;
    assert.deepEqual(report.errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    if (page) {
      await page
        .screenshot({ path: path.join(root, "failure.png") })
        .catch(() => {});
      fs.writeFileSync(
        path.join(root, "failure.html"),
        await page.content().catch(() => ""),
      );
    }
  } finally {
    if (browser) await browser.close();
    if (proc?.pid && proc.exitCode === null && proc.signalCode === null) {
      const exited = new Promise((resolve) => proc.once("exit", resolve));
      proc.kill("SIGTERM");
      try {
        await wait(
          () => proc.exitCode !== null || proc.signalCode !== null,
          15000,
        );
      } catch {
        proc.kill("SIGKILL");
      }
      await exited;
    }
    if (proc) {
      report.exitCode = proc.exitCode;
      report.signalCode = proc.signalCode;
      if (proc.exitCode !== 0) report.passed = false;
    }
    upstream.closeAllConnections();
    await new Promise((resolve) => upstream.close(resolve));
    if (log !== undefined) fs.closeSync(log);
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
    process.exitCode = report.passed ? 0 : 1;
  }
}
main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
