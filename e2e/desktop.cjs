/* Full desktop migration acceptance. Failure modes: dropped/oversized batches,
 * stale selection, focus loss, executable payloads, broken persistence/history,
 * config write failure, incorrect CA identity/trust/deletion, proxy rollback,
 * listener disruption, and closing the window stopping the gateway.
 * Same embedded frontend: real Chromium pointer/keyboard plus actual WKWebView.
 */
const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  net = require("node:net"),
  os = require("node:os"),
  crypto = require("node:crypto");
const { spawn, execFileSync, execFile } = require("node:child_process");
const execAsync = require("node:util").promisify(execFile);
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const assert = require("node:assert/strict");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function wait(fn, ms = 15000) {
  const until = Date.now() + ms;
  let last;
  while (Date.now() < until) {
    try {
      const v = await fn();
      if (v) return v;
    } catch (e) {
      last = e;
    }
    await sleep(40);
  }
  throw new Error("timeout " + (last?.stack || ""));
}
const root = path.resolve(
    process.argv[3] || fs.mkdtempSync("build/desktop-e2e-"),
  ),
  binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
const home = path.join(root, "home"),
  stateFile = path.join(root, "os-state.json"),
  previewFile = path.join(root, "preview-url"),
  sockFile = path.join(os.tmpdir(), "aicat-desktop-" + process.pid + ".sock");
fs.mkdirSync(home, { mode: 0o700 });
const report = {
  passed: false,
  checks: [],
  binary,
  binarySHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  artifacts: root,
};
let proc,
  browser,
  page,
  auto,
  upstream,
  proxyPort,
  fixturePort,
  errors = [],
  log,
  stateCalls = 0;
const fixture = (update) => {
  let s = JSON.parse(fs.readFileSync(stateFile));
  Object.assign(s, update);
  fs.writeFileSync(stateFile + ".next", JSON.stringify(s), { mode: 0o600 });
  fs.renameSync(stateFile + ".next", stateFile);
  return s;
};
function request(port, url, method = "GET", body = "", headers = {}) {
  return new Promise((resolve, reject) => {
    const r = http.request(
      {
        host: "127.0.0.1",
        port,
        path: url,
        method,
        headers: { ...headers, "Content-Length": Buffer.byteLength(body) },
      },
      (res) => {
        let bytes = [];
        res.on("data", (b) => bytes.push(b));
        res.on("end", () =>
          resolve({
            status: res.statusCode,
            data: Buffer.concat(bytes).toString(),
          }),
        );
      },
    );
    r.on("error", reject);
    r.end(body);
  });
}
const api = async () =>
  JSON.parse((await request(proxyPort, "/api/traffic?limit=2000")).data).items;
async function send(name, body = "") {
  const r = await request(
    proxyPort,
    `http://127.0.0.1:${fixturePort}${name}`,
    body ? "POST" : "GET",
    body,
    { "X-Multi": "one" },
  );
  assert.equal(r.status, 200);
  return r;
}
function connectAuto() {
  return new Promise((resolve, reject) => {
    const socket = net.connect(sockFile);
    socket.once("error", reject);
    socket.once("connect", () => {
      let buffer = "",
        pending;
      socket.on("data", (b) => {
        buffer += b;
        let n;
        while ((n = buffer.indexOf("\n")) >= 0) {
          const message = JSON.parse(buffer.slice(0, n));
          buffer = buffer.slice(n + 1);
          const p = pending;
          pending = null;
          if (message.error) p.reject(new Error(JSON.stringify(message.error)));
          else p.resolve(message.result);
        }
      });
      resolve({
        socket,
        call: (method, params = {}) =>
          new Promise((resolve, reject) => {
            assert(!pending);
            pending = { resolve, reject };
            socket.write(JSON.stringify({ id: 1, method, params }) + "\n");
          }),
      });
    });
  });
}
const evaluate = async (script) => (await auto.call("eval", { script })).value;
const check = (name) => {
  report.checks.push(name);
  console.log(JSON.stringify({ check: name }));
};
const click = async (id) => page.locator("#" + id).click();
const current = () => page.evaluate(() => window.desktop.state());
async function settled() {
  await wait(
    async () =>
      !((await page.locator("body").getAttribute("class")) || "").includes(
        "busy",
      ),
  );
}
async function navigate(route) {
  await page.evaluate((route) => window.desktop.navigate(route), route);
}
async function stop() {
  if (auto) {
    auto.socket.destroy();
    auto = null;
  }
  if (proc) {
    proc.kill("SIGTERM");
    await wait(() => proc.exitCode !== null, 15000);
    assert.equal(proc.exitCode, 0);
    proc = null;
  }
}
async function launch() {
  fs.rmSync(previewFile, { force: true });
  fs.rmSync(sockFile, { force: true });
  proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      MYGO_AUTOMATION: sockFile,
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
      MYGO_ENV: "development",
      MYGO_DEV_URL: "http://127.0.0.1:1/must-not-load",
    },
    stdio: ["ignore", log, log],
  });
  await wait(() => fs.existsSync(previewFile) && fs.existsSync(sockFile));
  auto = await connectAuto();
  await wait(async () => await evaluate("!!window.desktop?.state()"));
  await page.goto(fs.readFileSync(previewFile, "utf8"));
  await page.waitForFunction(() => !!window.desktop?.state());
}
// Focused regression: ambiguous Apply/App translation, unstable group order,
// and a positional domain counter pointing at App after a reorder.
async function verifySidebar() {
  const labels = () =>
    page.locator("#domains .ac-pin-group-title > span").allTextContents();
  assert.deepEqual(await labels(), ["固定", "域名", "应用"]);
  assert.deepEqual(
    await page
      .locator("#domains .sidebar-group")
      .evaluateAll((groups) => groups.map((group) => group.dataset.group)),
    ["pins", "domains", "apps"],
  );
  await page.screenshot({ path: path.join(root, "sidebar-zh.png") });
  await send("/sidebar/group-order");
  await page.waitForFunction(
    () => document.querySelector("#domain-count").textContent === "1",
  );
  await navigate("general");
  await page.selectOption("#preference-language", "en");
  await settled();
  await navigate("traffic");
  assert.deepEqual(await labels(), ["Pin", "Domain", "App"]);
  await wait(
    async () =>
      await evaluate(
        "Array.from(document.querySelectorAll('#domains .ac-pin-group-title > span'), e => e.textContent).join('|') === 'Pin|Domain|App'",
      ),
  );
  await page.screenshot({ path: path.join(root, "sidebar-en.png") });
  await auto
    .call("capture")
    .then((result) =>
      fs.writeFileSync(
        path.join(root, "sidebar-wkwebview.png"),
        Buffer.from(result.png, "base64"),
      ),
    );
  await navigate("proxy");
  assert.equal(
    (await page.locator("#listener-apply").textContent()).trim(),
    "Apply",
  );
  await navigate("general");
  await page.selectOption("#preference-language", "zh-Hans");
  await settled();
  await navigate("traffic");
  assert.deepEqual(await labels(), ["固定", "域名", "应用"]);
  await click("clear");
  await page.waitForFunction(() => window.trafficView.state().total === 0);
  assert.deepEqual(errors, []);
  check(
    "Pin/Domain/App order, contextual App versus Apply, Chinese labels and keyed domain count in browser and WKWebView",
  );
}
(async () => {
  try {
    if (
      execFileSync("ioreg", ["-n", "Root", "-d1"], {
        encoding: "utf8",
      }).includes('"CGSSessionScreenIsLocked"=Yes')
    )
      throw new Error("macOS locked; unlock before desktop E2E");
    upstream = http.createServer((req, res) => {
      let bytes = [];
      req.on("data", (b) => bytes.push(b));
      req.on("end", () => {
        const payload = req.url.startsWith("/body")
          ? "body-fixture\n<script>window.payloadExecuted=true</script>\n" +
            "response line\n".repeat(20000)
          : JSON.stringify({
              path: req.url,
              body: Buffer.concat(bytes).toString(),
            });
        res.writeHead(200, {
          "Content-Type": "text/plain; charset=utf-8",
          "Set-Cookie": ["one=1", "two=2"],
          "Content-Length": Buffer.byteLength(payload),
        });
        res.end(payload);
      });
    });
    await new Promise((r) => upstream.listen(0, "127.0.0.1", r));
    fixturePort = upstream.address().port;
    const temp = net.createServer();
    await new Promise((r) => temp.listen(0, "127.0.0.1", r));
    proxyPort = temp.address().port;
    await new Promise((r) => temp.close(r));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `# keep-comment\nfutureSetting: keep\ngateway:\n  addr: 127.0.0.1:${proxyPort}\nui:\n  language: zh-Hans\n  theme: light\ntraffic:\n  maxRecords: 2000\n  maxBodyBytes: 65536\n  maxTotalBodyBytes: 67108864\n`,
    );
    const original = [
      {
        id: "wifi",
        name: "Wi-Fi",
        proxies: {
          HTTPEnable: 0,
          HTTPProxy: "",
          HTTPPort: 0,
          HTTPSEnable: 0,
          HTTPSProxy: "",
          HTTPSPort: 0,
          ExceptionsList: ["localhost"],
          CustomValue: "keep",
        },
      },
    ];
    fs.writeFileSync(
      stateFile,
      JSON.stringify({
        services: original,
        trusted: false,
        recordProxyWrites: true,
      }),
      { mode: 0o600 },
    );
    log = fs.openSync(path.join(root, "app.log"), "a");
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    page = await browser.newPage({ viewport: { width: 1180, height: 900 } });
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("request", (request) => {
      if (request.url().endsWith("/rpc") && request.method() === "POST") {
        if (request.postDataJSON()?.method === "Desktop.State") stateCalls++;
      }
    });
    await launch();
    report.windows = await auto.call("windows");
    assert.equal(report.windows.windows[0].kind, "page");
    assert.equal(await page.evaluate(() => window.__aicatPreview), true);
    assert.equal((await current()).production, true);
    assert.equal(await evaluate("window.desktop.state().production"), true);
    assert.equal(await evaluate("location.origin"), "mygo://localhost");
    assert.equal(await evaluate("!!window.__aicatPreview"), false);
    const url = new URL(fs.readFileSync(previewFile, "utf8"));
    assert.equal((await request(Number(url.port), "/rpc")).status, 403);
    const denied = await request(
      Number(url.port),
      "/rpc",
      "POST",
      '{"method":"Desktop.SetTrafficEnabled","args":[false]}',
      {
        "X-Aicat-Preview": url.searchParams.get("token"),
        Origin: "https://evil.invalid",
      },
    );
    assert.equal(denied.status, 403);
    check(
      "real MyGo WebView, portable browser frontend, authenticated preview and cross-origin denial",
    );
    await verifySidebar();
    if (process.argv[4] === "sidebar") {
      report.scenario = "sidebar";
      report.passed = true;
      return;
    }
    await page.screenshot({ path: path.join(root, "empty.png") });
    let index = 0;
    await Promise.all(
      Array.from({ length: 12 }, async () => {
        while (index < 2200) {
          const n = index++;
          await send("/traffic/" + String(n).padStart(6, "0"));
        }
      }),
    );
    await wait(
      async () =>
        JSON.stringify(
          await page.evaluate(() => window.trafficView.state().ids),
        ) ===
        JSON.stringify((await api()).map((r) => r.id).sort((a, b) => a - b)),
    );
    assert.equal((await api()).length, 2000);
    assert((await page.locator(".traffic-row").count()) < 80);
    assert.equal(
      await page.evaluate(() => window.trafficView.state().total),
      2000,
    );
    await auto.call("menu", { path: ["E2E", "打开窗口"] });
    await wait(
      async () => await evaluate("window.trafficView.state().total===2000"),
    );
    check(
      "2200 real proxy requests, 2000 retained matching IDs, bounded DOM in browser and WKWebView",
    );
    await page.screenshot({ path: path.join(root, "loaded.png") });
    await settled();
    await sleep(100);
    const idleStateCalls = stateCalls;
    await sleep(1800);
    assert.equal(stateCalls, idleStateCalls, "idle UI must not poll State");
    for (let attempt = 0; attempt < 5; attempt++) {
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await auto.call("menu", { path: ["E2E", "关闭窗口"] });
      await auto.call("menu", { path: ["E2E", "打开窗口"] });
    }
    await wait(async () => {
      const diagnostic = await request(
        Number(url.port),
        "/diagnostics",
        "GET",
        "",
        { "X-Aicat-Preview": url.searchParams.get("token") },
      );
      return JSON.parse(diagnostic.data).trafficSubscriptions === 2;
    });
    assert.equal(
      (await page.evaluate(() => window.trafficView.status()))
        .activeSubscriptions,
      1,
    );
    check(
      "typed events replace idle polling; repeated focus cancels old browser and native Channels",
    );
    const search = page.locator("#search");
    await search.click();
    await search.pressSequentially("/traffic/002199");
    await page.waitForFunction(() => window.trafficView.state().visible === 1);
    assert.equal(
      await search.evaluate((e) => e === document.activeElement),
      true,
    );
    await search.fill("/TRAFFIC/002199 127");
    await page.waitForFunction(() => window.trafficView.state().visible === 1);
    await send("/traffic/live");
    assert.equal(await search.inputValue(), "/TRAFFIC/002199 127");
    assert.equal(
      await search.evaluate((e) => e === document.activeElement),
      true,
    );
    check(
      "case-insensitive fuzzy AND search and focus preserved during updates",
    );
    await send("/body");
    await search.fill("/body");
    await page.locator(".traffic-row").filter({ hasText: "/body" }).click();
    await page.waitForFunction(
      () => window.trafficView.state().detailRevision > 0,
    );
    assert.equal(
      await page.locator("[data-side=response] .headers").isVisible(),
      true,
    );
    assert.equal(
      await page
        .locator("[data-side=request] .headers")
        .textContent()
        .then((x) => x.includes("Host")),
      true,
    );
    await page.locator("[data-side=response] [data-view=raw]").click();
    await page.waitForFunction(() =>
      document
        .querySelector("[data-side=response] .raw")
        .textContent.includes("body-fixture"),
    );
    assert.equal(await page.evaluate(() => !!window.payloadExecuted), false);
    assert(
      (
        await page.locator("[data-side=response] .body-notice").textContent()
      ).includes("65536"),
    );
    await page.locator("[data-side-copy=response]").click();
    await settled();
    await wait(() =>
      execFileSync("pbpaste", { encoding: "utf8" }).includes("body-fixture"),
    );
    assert(
      execFileSync("pbpaste", { encoding: "utf8" }).startsWith("HTTP 200\n"),
    );
    await page.screenshot({ path: path.join(root, "raw.png") });
    report.design = await require("./design.cjs")(page, browser, root);
    check(
      "HTML design geometry, light/dark brand colors and captured reference/production screenshots",
    );
    check(
      "Header/Raw, duplicate headers, on-demand bounded body, HTML stays text, actual OS clipboard and adjacent feedback",
    );
    const split = page.locator("#list-split");
    const beforeHeight = (await page.locator(".table-area").boundingBox())
      .height;
    await split.focus();
    await page.keyboard.press("ArrowDown");
    assert(
      (await page.locator(".table-area").boundingBox()).height > beforeHeight,
    );
    await page.keyboard.press("ArrowUp");
    await page.locator("[data-side=request] [data-view=raw]").click();
    await page.waitForFunction(() =>
      document
        .querySelector("[data-side=request] .raw")
        .dataset.text?.startsWith("GET /body"),
    );
    await page.locator("[data-side=request] [data-view=headers]").click();
    assert(await page.locator("[data-side=response] .raw").isVisible());
    for (const target of [
      "/tree/v1/a",
      "/tree/v1/ab",
      "/tree/v10/sibling",
      "/tree/v1/a/child",
    ])
      await send(target);
    await navigate("traffic");
    const domain = "127.0.0.1:" + fixturePort;
    const branch = (pathValue) =>
      page
        .locator(
          '#domains button[data-host="' +
            domain +
            '"][data-path="' +
            pathValue +
            '"]',
        )
        .first();
    const expandBranch = async (pathValue) =>
      branch(pathValue).locator("..").locator(".ac-tree-toggle").click();
    await expandBranch("");
    await expandBranch("/tree");
    await expandBranch("/tree/v1");
    await branch("/tree/v1/a").click();
    await page.waitForFunction(() => window.trafficView.state().visible === 2);
    const treeRows = await page.locator(".traffic-row").all();
    await treeRows[0].click();
    const parentSelection = await page.evaluate(
      () => window.trafficView.state().selected,
    );
    await treeRows[1].click();
    const childSelection = await page.evaluate(
      () => window.trafficView.state().selected,
    );
    assert.notEqual(parentSelection, childSelection);
    await click("history-back");
    assert.equal(
      await page.evaluate(() => window.trafficView.state().selected),
      parentSelection,
    );
    await click("history-forward");
    assert.equal(
      await page.evaluate(() => window.trafficView.state().selected),
      childSelection,
    );
    await expandBranch("/tree");
    await page.waitForFunction(() => window.trafficView.state().visible === 4);
    await page.locator("#domain-search").fill("no-such-domain");
    assert.equal(await page.locator("#domains button[data-host]").count(), 0);
    await page.locator("#domain-search").fill("");
    await page
      .locator('#domains button[data-app]:not([data-app=""])')
      .first()
      .click();
    assert((await page.evaluate(() => window.trafficView.state().visible)) > 0);
    await navigate("traffic");
    check(
      "domain/path segment boundaries, ancestor collapse, sidebar search, application filter and request history",
    );
    await search.fill("");
    await page.locator("#rows-viewport").hover();
    await page.mouse.wheel(0, 1500);
    await page.waitForFunction(
      () => window.trafficView.state().scrollTop > 100,
    );
    assert((await page.locator(".traffic-row").count()) < 80);
    const special = "line\nquote' $HOME `echo bad`";
    await send("/curl-check", special);
    await search.fill("/curl-check");
    await page
      .locator(".traffic-row")
      .filter({ hasText: "/curl-check" })
      .click({ button: "right" });
    await page
      .locator("#popup button")
      .filter({ hasText: "复制 cURL" })
      .click();
    await settled();
    const curl = await wait(() => {
      const value = execFileSync("pbpaste", { encoding: "utf8" });
      return value.startsWith("curl ") ? value : false;
    });
    const echo = JSON.parse(
      (
        await execAsync("/bin/sh", ["-c", curl], {
          encoding: "utf8",
          timeout: 10000,
        })
      ).stdout,
    );
    assert.equal(echo.body, special);
    await page.locator(".traffic-row").click({ button: "right" });
    await page.locator("#popup button").filter({ hasText: "固定" }).click();
    await settled();
    assert(
      (await current()).pinnedDomains.includes("127.0.0.1:" + fixturePort),
    );
    check(
      "virtual scrolling, context-menu target, executable safely quoted cURL, persistent domain pin",
    );
    await click("export");
    await settled();
    const exported = JSON.parse(
      fs.readFileSync(path.join(root, "aicat-traffic.json")),
    );
    assert.equal(exported.length, 1);
    assert(exported[0].url.endsWith("/curl-check"));
    await click("recording");
    await settled();
    const count = (await api()).at(0).id;
    const maxBefore = Math.max(...(await api()).map((r) => r.id));
    await send("/paused");
    assert.equal(Math.max(...(await api()).map((r) => r.id)), maxBefore);
    await click("recording");
    await settled();
    await send("/resumed");
    await wait(async () =>
      (await api()).some((r) => r.url.endsWith("/resumed")),
    );
    check("filtered JSON export and pause/resume preserve proxy forwarding");
    await navigate("general");
    await page.selectOption("#preference-language", "en");
    await settled();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    await click("preference-theme-dark");
    await settled();
    assert(
      !(await page.locator("#module-page .ac-rule-desc").allInnerTexts())
        .join(" ")
        .match(/[\u4e00-\u9fff]/),
    );
    assert.equal(
      await page.locator("#connection").textContent(),
      "Proxy listening",
    );
    assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
    await page.screenshot({ path: path.join(root, "general-dark-en.png") });
    await page.selectOption("#preference-language", "zh-Hans");
    await settled();
    await click("preference-theme-light");
    await settled();
    await navigate("extensions");
    await page.screenshot({ path: path.join(root, "extensions-light.png") });
    await click("feature-traffic");
    await settled();
    assert.equal((await current()).trafficEnabled, false);
    assert.equal(await page.locator("#nav-traffic").count(), 0);
    await send("/feature-disabled");
    assert(!(await api()).some((r) => r.url.endsWith("/feature-disabled")));
    await click("feature-traffic");
    await settled();
    await click("extension-open-traffic");
    await page.locator("#nav-more").click();
    await page.locator("[data-action=pin-navigation]").click();
    await settled();
    assert.equal(await page.locator("#nav-traffic").count(), 0);
    await click("nav-more");
    await page.locator("#popup button").filter({ hasText: "流量" }).click();
    await click("nav-more");
    await page.locator("[data-action=pin-navigation]").click();
    await settled();
    await navigate("community");
    assert(
      (await page.locator("#module-page").textContent()).includes("即将推出"),
    );
    await navigate("extensions");
    await page.locator("#extension-search").fill("no-match");
    assert.equal(await page.locator("#extension-traffic").count(), 0);
    check(
      "extensions, community placeholder, search, traffic feature toggle, More and navigation pin",
    );
    const cfgPath = path.join(home, "aicat.yaml");
    fs.renameSync(cfgPath, cfgPath + ".saved");
    fs.mkdirSync(cfgPath);
    await click("feature-traffic");
    await page.waitForFunction(() => !document.querySelector("#notice").hidden);
    fs.rmdirSync(cfgPath);
    fs.renameSync(cfgPath + ".saved", cfgPath);
    await settled();
    assert.equal((await current()).trafficEnabled, true);
    check("config write failure keeps live feature state");
    await navigate("proxy");
    await page.waitForSelector("#certificate-install");
    fixture({ cancelInstall: true });
    await click("certificate-install");
    await settled();
    assert.equal(fixture({}).trusted, false);
    assert(fs.existsSync(path.join(home, "ca", "mitmproxy-ca-cert.pem")));
    fixture({ cancelInstall: false, failInstall: true });
    await click("certificate-install");
    await settled();
    assert.equal(fixture({}).trusted, false);
    fixture({ failInstall: false });
    await click("certificate-install");
    await settled();
    assert.equal((await current()).certificate.trusted, true);
    const firstFingerprint = (await current()).certificate.fingerprint;
    await click("certificate-details");
    assert(
      (await page.locator("#dialog").textContent()).includes(firstFingerprint),
    );
    await page.locator("#dialog button").filter({ hasText: "关闭" }).click();
    await click("certificate-export");
    await settled();
    assert(
      !fs
        .readFileSync(path.join(root, "aicat-root-ca.pem"), "utf8")
        .includes("PRIVATE"),
    );
    await click("certificate-keychain");
    await settled();
    assert.equal(fixture({}).keychainOpened, true);
    check(
      "certificate install cancel/failure/retry, identity preserved, trust verified, details/public export/keychain",
    );
    await click("add-decryption-domain");
    await page.locator("#decryption-domain-input").fill("https://invalid/path");
    await page.locator("#dialog button").filter({ hasText: "添加" }).click();
    await settled();
    assert.equal(await page.locator("#dialog").isVisible(), true);
    await page.locator("#decryption-domain-input").fill("EXAMPLE.COM");
    await page.locator("#dialog button").filter({ hasText: "添加" }).click();
    await settled();
    assert((await current()).domains.includes("example.com"));
    await page.locator('[id="remove-decryption-example.com"]').click();
    await settled();
    assert(!(await current()).domains.includes("example.com"));
    await click("system-proxy-toggle");
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await settled();
    assert.equal(fixture({}).services[0].proxies.HTTPEnable, 1);
    await click("system-proxy-toggle");
    await settled();
    assert.equal(fixture({}).services[0].proxies.HTTPEnable, 0);
    check(
      "decryption-domain validation/add/remove and system-proxy enable/restore",
    );
    await page.locator("#listener-port").fill("0");
    assert.equal(await page.locator("#listener-apply").isDisabled(), true);
    await page.locator("#listener-port").fill(String(proxyPort));
    const occupied = net.createServer();
    await new Promise((r) => occupied.listen(0, "127.0.0.1", r));
    const occupiedPort = occupied.address().port;
    await page.locator("#listener-port").fill(String(occupiedPort));
    await click("listener-apply");
    await settled();
    assert.equal((await current()).gateway.addr, "127.0.0.1:" + proxyPort);
    await send("/occupied-listener-still-live");
    await new Promise((r) => occupied.close(r));
    await page.locator("#listener-port").fill(String(proxyPort));
    await page.selectOption("#listener-address", "0.0.0.0");
    await click("listener-apply");
    await settled();
    assert.equal((await current()).gateway.addr, "0.0.0.0:" + proxyPort);
    await send("/after-rebind");
    await page.selectOption("#listener-address", "127.0.0.1");
    await click("listener-apply");
    await settled();
    check(
      "listener IP/port form validation and live rebind retain proxy and traffic",
    );
    fixture({ cancelRemove: true });
    await click("certificate-delete");
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await settled();
    assert.equal((await current()).certificate.pendingDeletion, true);
    fixture({ cancelRemove: false, failRemove: true });
    await click("certificate-delete");
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await settled();
    assert.equal((await current()).certificate.pendingDeletion, true);
    fixture({ failRemove: false });
    await click("certificate-delete");
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await settled();
    assert.equal((await current()).certificate.exists, false);
    await click("certificate-install");
    await settled();
    assert.equal((await current()).certificate.trusted, true);
    assert.notEqual(
      (await current()).certificate.fingerprint,
      firstFingerprint,
    );
    check(
      "root deletion disables old identity, reinstall creates and trusts new identity",
    );
    await navigate("traffic");
    await click("clear");
    await page.waitForFunction(() => window.trafficView.state().total === 0);
    assert.equal((await api()).length, 0);
    assert.equal(
      await page.evaluate(() => window.trafficView.state().selected),
      0,
    );
    await auto.call("menu", { path: ["E2E", "关闭窗口"] });
    assert.equal((await auto.call("windows")).windows[0].visible, false);
    await send("/while-hidden");
    await auto.call("menu", { path: ["E2E", "打开窗口"] });
    await wait(async () => (await auto.call("windows")).windows[0].visible);
    await wait(
      async () => await evaluate("window.trafficView.state().total>0"),
    );
    await evaluate(
      '(() => { document.querySelector("#search").value="while-hidden";document.querySelector("#search").dispatchEvent(new Event("input"));return true; })()',
    );
    await auto
      .call("capture")
      .then((r) =>
        fs.writeFileSync(
          path.join(root, "wkwebview.png"),
          Buffer.from(r.png, "base64"),
        ),
      );
    assert.deepEqual(await evaluate("window.trafficView.metrics.errors"), []);
    check(
      "clear cancels selection, native close hides to tray, gateway keeps running and WebView resumes",
    );
    await auto.call("menu", { path: ["E2E", "暂停或继续"] });
    await wait(
      async () => await evaluate("window.desktop.state().recording===false"),
    );
    const nativePauseMax = Math.max(...(await api()).map((r) => r.id));
    await send("/native-tray-paused");
    assert.equal(Math.max(...(await api()).map((r) => r.id)), nativePauseMax);
    await auto.call("menu", { path: ["E2E", "暂停或继续"] });
    await wait(
      async () => await evaluate("window.desktop.state().recording===true"),
    );
    await evaluate("window.desktop.navigate('general');true");
    await wait(
      async () =>
        await evaluate("!!document.querySelector('#preference-theme-dark')"),
    );
    await evaluate(
      "document.querySelector('#preference-theme-dark').click();true",
    );
    await wait(
      async () =>
        await evaluate("window.desktop.state().preferences.theme==='dark'"),
    );
    await navigate("general");
    await click("preference-theme-dark");
    await settled();
    await stop();
    await launch();
    assert.equal((await current()).preferences.theme, "dark");
    assert(
      (await current()).pinnedDomains.includes("127.0.0.1:" + fixturePort),
    );
    assert(fs.readFileSync(cfgPath, "utf8").includes("futureSetting: keep"));
    assert(fs.readFileSync(cfgPath, "utf8").includes("# keep-comment"));
    check(
      "process restart preserves language/theme, domain/navigation pins, certificate identity and unknown config",
    );
    await navigate("proxy");
    await click("system-proxy-toggle");
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await settled();
    fixture({ failApply: true });
    await auto.call("menu", { path: ["E2E", "退出应用"] });
    await wait(
      async () =>
        !!(await evaluate('document.querySelector("#notice").textContent')),
    );
    assert.equal(proc.exitCode, null);
    await send("/quit-restore-failed-still-live");
    fixture({ failApply: false });
    await stop();
    assert.deepEqual(fixture({}).services, original);
    check("explicit quit restores original OS proxy settings");
    assert.deepEqual(errors, []);
    report.browserVersion = browser.version();
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    report.browserErrors = errors;
    if (auto)
      try {
        report.webviewFailure = await evaluate(
          '({state:window.trafficView?.state(),metrics:window.trafficView?.metrics,visibility:document.visibilityState,connection:document.querySelector("#connection").textContent})',
        );
      } catch (e) {
        report.webviewError = String(e);
      }
    if (page)
      try {
        await page.screenshot({ path: path.join(root, "failure.png") });
        fs.writeFileSync(path.join(root, "failure.html"), await page.content());
      } catch {}
    process.exitCode = 1;
  } finally {
    if (proc)
      try {
        await stop();
      } catch {
        proc?.kill("SIGKILL");
      }
    if (browser) await browser.close();
    if (upstream) await new Promise((r) => upstream.close(r));
    if (log) fs.closeSync(log);
    report.sources = {};
    for (const dir of ["internal/app", "ui", "e2e"])
      for (const f of fs
        .readdirSync(dir, { recursive: true })
        .filter(
          (f) =>
            !f
              .split(path.sep)
              .some((part) =>
                ["node_modules", "dist", "dist-e2e"].includes(part),
              ) && /\.(go|js|ts|vue|cjs|html|css|py|json)$/.test(f),
        )) {
        const p = path.join(dir, f);
        report.sources[p] = crypto
          .createHash("sha256")
          .update(fs.readFileSync(p))
          .digest("hex");
      }
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
  }
})();
