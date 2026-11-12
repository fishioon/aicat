/* Focused scenario for the redesigned shell (docs/design/aicat-redesign.html).
 * Failure modes: missing rail/breadcrumb state, traffic status/type columns
 * misreporting a response, inspector views (Query/Body/事件/Raw) not loading,
 * keyboard selection breaking, tunnel decryption entry missing, extension
 * toggle not hiding the rail entry, settings wildcard domains rejected.
 * Usage: node e2e/redesign.cjs bin/aicat-e2e build/redesign-e2e
 */
const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  net = require("node:net");
const { spawn } = require("node:child_process");
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
    await sleep(50);
  }
  throw new Error("timeout " + (last?.stack || ""));
}
const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(process.argv[3] || "build/redesign-e2e");
fs.rmSync(root, { recursive: true, force: true });
fs.mkdirSync(path.join(root, "home"), { recursive: true, mode: 0o700 });
const home = path.join(root, "home"),
  previewFile = path.join(root, "preview-url"),
  stateFile = path.join(root, "os-state.json");

function send(port, url, method = "GET", body = "", headers = {}) {
  return new Promise((resolve) => {
    const r = http.request(
      { host: "127.0.0.1", port, path: url, method, headers },
      (res) => {
        res.resume();
        res.on("end", () => resolve(res.statusCode));
      },
    );
    r.on("error", () => resolve(0));
    r.end(body);
  });
}
function tunnel(port, authority) {
  return new Promise((resolve) => {
    const socket = net.connect(port, "127.0.0.1", () =>
      socket.write(
        `CONNECT ${authority} HTTP/1.1\r\nHost: ${authority}\r\n\r\n`,
      ),
    );
    socket.once("data", () => {
      socket.end();
      resolve();
    });
    socket.on("error", resolve);
  });
}

(async () => {
  const report = { passed: false, checks: [] };
  const check = (name) => report.checks.push(name);
  const upstream = http.createServer((req, res) => {
    let chunks = [];
    req.on("data", (b) => chunks.push(b));
    req.on("end", () => {
      if (req.url.startsWith("/v1/messages")) {
        res.writeHead(200, { "Content-Type": "text/event-stream" });
        res.end(
          'event: message_start\ndata: {"type":"message_start"}\n\n' +
            'event: content_block_delta\ndata: {"delta":{"text":"hi"}}\n\n' +
            'event: message_stop\ndata: {"type":"message_stop"}\n\n',
        );
      } else if (req.url.startsWith("/denied")) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end('{"error":{"message":"bad key"}}');
      } else if (req.url.startsWith("/cached")) {
        res.writeHead(304);
        res.end();
      } else {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ ok: true, n: 12, list: ["a", null] }));
      }
    });
  });
  await new Promise((r) => upstream.listen(0, "127.0.0.1", r));
  const fixture = upstream.address().port;
  const temp = net.createServer();
  await new Promise((r) => temp.listen(0, "127.0.0.1", r));
  const proxyPort = temp.address().port;
  await new Promise((r) => temp.close(r));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `gateway:\n  addr: 127.0.0.1:${proxyPort}\nui:\n  language: zh-Hans\n  theme: light\n`,
  );
  fs.writeFileSync(
    stateFile,
    JSON.stringify({ services: [], trusted: false, recordProxyWrites: true }),
    { mode: 0o600 },
  );
  const log = fs.openSync(path.join(root, "app.log"), "a");
  const proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
      // Preview-only scenario: keep the native window out of the way.
      AICAT_E2E_BACKGROUND: "1",
    },
    stdio: ["ignore", log, log],
  });
  const browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AICAT_CHROMIUM ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  const errors = [];
  try {
    await wait(() => fs.existsSync(previewFile));
    const page = await browser.newPage({
      viewport: { width: 1180, height: 820 },
    });
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.waitForFunction(() => !!window.desktop?.state());
    const shot = (name) =>
      page.screenshot({ path: path.join(root, name + ".png") });

    // Shell: overview is home; rail, breadcrumb and listen address.
    await page.locator("#overview").waitFor();
    assert.equal(
      await page.locator("#nav-overview").getAttribute("aria-current"),
      "page",
    );
    assert.match(
      await page.locator("#gateway-address").innerText(),
      new RegExp(`127.0.0.1:${proxyPort}`),
    );
    for (const id of [
      "nav-overview",
      "nav-traffic",
      "nav-extensions",
      "nav-more",
      "nav-settings",
    ])
      assert(await page.locator("#" + id).isVisible(), id);
    check("shell");

    // Seed traffic through the proxy.
    const base = `http://127.0.0.1:${fixture}`;
    await send(proxyPort, base + "/v1/messages", "POST", '{"model":"m"}', {
      "Content-Type": "application/json",
    });
    await send(proxyPort, base + "/items?state=open&per_page=20");
    await send(proxyPort, base + "/denied", "POST", "{}", {
      "Content-Type": "application/json",
    });
    await send(proxyPort, base + "/cached");
    await send(proxyPort, "http://127.0.0.1:1/unreachable");
    await tunnel(proxyPort, `localhost:${fixture}`);
    await wait(
      async () =>
        (await page.evaluate(() => window.trafficView.state().total)) >= 6,
    );
    await page
      .locator("#overview-todos")
      .getByText("查看请求")
      .first()
      .waitFor();
    await shot("overview-light");
    check("overview");

    await page.locator("#nav-traffic").click();
    await page.locator(".traffic-row").first().waitFor();
    const rowText = async (path) =>
      page
        .locator(".traffic-row", { hasText: path })
        .first()
        .locator("span")
        .allInnerTexts();
    assert((await rowText("/v1/messages")).includes("SSE"));
    assert((await rowText("/items")).includes("JSON"));
    assert(
      await page
        .locator(".traffic-row", { hasText: "/denied" })
        .locator(".sd.bad")
        .count(),
    );
    assert(
      await page
        .locator(".traffic-row", { hasText: "/cached" })
        .locator(".sd.mute")
        .count(),
    );
    assert(
      await page
        .locator(".traffic-row", { hasText: "localhost:" })
        .locator(".sd.ring")
        .count(),
    );
    check("list status/type");

    // Inspector: SSE events, query, JSON body, raw.
    await page.locator(".traffic-row", { hasText: "/v1/messages" }).click();
    await page.locator('[data-side="response"] [data-view="events"]').click();
    await page.locator(".events li").nth(2).waitFor();
    assert.equal(await page.locator(".events li").count(), 3);
    await page.locator('[data-side="request"] [data-view="body"]').click();
    await page.locator('[data-side="request"] pre.body').waitFor();
    // Sidebar outline: expand a domain and an app, and filter by a path.
    await page.locator('[data-group="domains"] .tree-toggle').nth(1).click();
    await page.locator('[data-group="apps"] .tree-toggle').first().click();
    await page
      .locator('[data-group="domains"] .tree-item[data-path="/v1"]')
      .click();
    assert.equal(
      await page.locator('#domains .tree-row[data-selected="true"]').count(),
      1,
    );
    assert(
      await page
        .locator('[data-group="domains"] .tree-row', { hasText: "denied" })
        .locator(".sd.bad")
        .count(),
    );
    await shot("sidebar-tree-light");
    await page.locator("#all-domains").click();
    await page.locator(".traffic-row", { hasText: "/v1/messages" }).click();
    await shot("traffic-sse-light");
    await page.locator(".traffic-row", { hasText: "/items" }).click();
    await page.locator('[data-side="request"] [data-view="query"]').click();
    assert.match(
      await page.locator('[data-side="request"] .kv').innerText(),
      /per_page\s+20/,
    );
    await page.locator('[data-side="response"] [data-view="body"]').click();
    await page.locator('[data-side="response"] pre.body .k').first().waitFor();
    await page.locator('[data-side="response"] [data-view="raw"]').click();
    await page.locator('[data-side="response"] .raw-line').first().waitFor();
    check("inspector views");

    // Keyboard: ↓ moves the selection, Esc clears it.
    await page.locator("#rows-viewport").focus();
    const before = await page.evaluate(
      () => window.trafficView.state().selected,
    );
    await page.keyboard.press("ArrowDown");
    const after = await page.evaluate(
      () => window.trafficView.state().selected,
    );
    assert.notEqual(after, before);
    await page.keyboard.press("Escape");
    assert.equal(
      await page.evaluate(() => window.trafficView.state().selected),
      0,
    );
    await page.keyboard.press("Meta+k");
    assert.equal(
      await page.evaluate(() => document.activeElement.id),
      "search",
    );
    check("keyboard");

    // Tunnel detail offers decryption.
    await page.locator(".traffic-row", { hasText: "localhost:" }).click();
    await page.locator("#tunnel-detail #enable-decryption").waitFor();
    check("tunnel");

    // Context menu.
    await page
      .locator(".traffic-row", { hasText: "/items" })
      .click({ button: "right" });
    await page.locator("#popup").getByText("复制 URL").waitFor();
    await page.keyboard.press("Escape");
    check("context menu");

    // Dark theme screenshot of traffic.
    await page.locator(".traffic-row", { hasText: "/denied" }).click();
    await page.locator("#theme").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "dark",
    );
    await shot("traffic-dark");
    await page.locator("#theme").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "light",
    );

    // More popover: unpin traffic, then it lives under 更多.
    await page.locator("#nav-more").click();
    await page.locator("#more-pop [data-action=pin-navigation]").click();
    await wait(async () => !(await page.locator("#nav-traffic").count()));
    await page.locator("#more-traffic").click();
    assert.equal(
      await page.locator("#nav-more").getAttribute("aria-current"),
      "page",
    );
    await page.locator("#nav-more").click();
    await page.locator("#more-pop [data-action=pin-navigation]").click();
    await page.keyboard.press("Escape");
    await page.locator("#nav-traffic").waitFor();
    check("more popover");

    // Extensions: disable hides the rail entry; re-enable restores it.
    await page.locator("#nav-extensions").click();
    await page.locator("#feature-traffic").click();
    await wait(async () => !(await page.locator("#nav-traffic").count()));
    await page.locator(".xd-status").waitFor();
    await shot("extensions-off-light");
    await page.locator("#feature-traffic").click();
    await page.locator("#nav-traffic").waitFor();
    await page.locator("#extension-menu").click();
    await page.locator("#popup").getByText("代理设置").click();
    check("extensions");

    // Settings: wildcard accepted, misplaced wildcard rejected.
    await page.locator("#settings-proxy").waitFor();
    await page.locator("#decryption-domain-input").fill("*.example.com");
    await page.locator("#add-decryption-domain").click();
    await page.locator(".hosts", { hasText: "*.example.com" }).waitFor();
    await page.locator("#decryption-domain-input").fill("api.*.com");
    await page.locator("#add-decryption-domain").click();
    await page.locator("#decryption-domain-error").waitFor();
    await shot("settings-proxy-light");
    await page.locator("#settings-general").click();
    await page.locator("#preference-theme-dark").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "dark",
    );
    await shot("settings-general-dark");
    check("settings");

    // History: back returns to the proxy page.
    await page.locator("#history-back").click();
    await page.locator("#decryption-domain-input").waitFor();
    check("history");

    await page.locator("#nav-overview").click();
    await page.locator("#connect-agent").click();
    await page.locator("#client-codex").click();
    assert.match(await page.locator("#snippet").innerText(), /mcp_servers/);
    await shot("connect-dark");
    check("connect");

    assert.deepEqual(errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
    report.errors = errors;
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    await browser.close();
    proc.kill("SIGTERM");
    upstream.close();
    console.log(JSON.stringify(report, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
