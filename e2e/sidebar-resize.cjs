const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const http = require("node:http");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] || "build/sidebar-resize-" + Date.now(),
);
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const sha256 = (file) =>
  crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");

async function main() {
  const home = path.join(root, "home");
  fs.mkdirSync(home, { recursive: true, mode: 0o700 });
  const previewFile = path.join(root, "preview-url");
  const osState = path.join(root, "os-state.json");
  const listener = net.createServer();
  await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
  const port = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\n`,
  );
  fs.writeFileSync(osState, JSON.stringify({ services: [], trusted: false }));
  const report = {
    passed: false,
    command: process.argv,
    coverage:
      "Headless Chromium with real Desktop service and hidden native app; no native window acceptance",
    hashes: Object.fromEntries(
      [
        binary,
        "ui/src/App.vue",
        "ui/src/app.css",
        "e2e/sidebar-resize.cjs",
        "ui/src/components/Splitter.vue",
        "ui/src/state/desktop.ts",
      ].map((file) => [file, sha256(file)]),
    ),
    checks: [],
    errors: [],
  };
  const log = fs.openSync(path.join(root, "app.log"), "w");
  const proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_PREVIEW: previewFile,
      AICAT_E2E_OS_STATE: osState,
    },
    stdio: ["ignore", log, log],
  });
  fs.closeSync(log);
  let browser, upstream;
  try {
    const deadline = Date.now() + 30000;
    while (!fs.existsSync(previewFile)) {
      if (proc.exitCode !== null || Date.now() > deadline)
        throw Error("Preview unavailable");
      await pause(100);
    }
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    const page = await browser.newPage({
      viewport: { width: 1180, height: 820 },
    });
    page.on("pageerror", (error) => report.errors.push(error.message));
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.locator("#overview").waitFor();
    page.setDefaultTimeout(10000);
    const splitter = page.locator("#sidebar-split");
    const side = page.locator("#module-page > .split:visible > .side");
    const panel = page.locator("#module-page > .split:visible > .main");
    async function verifyWidth(width) {
      assert.equal(Math.round((await side.boundingBox()).width), width);
      assert.equal(Number(await splitter.getAttribute("aria-valuenow")), width);
      const content = await panel.boundingBox();
      assert.equal(content.x, 44 + width);
      assert.equal((await page.locator("#breadcrumb").boundingBox()).x, content.x);
      assert(content.width >= 320);
      assert.equal(await page.locator(".page").evaluate(el => el.scrollWidth <= el.clientWidth), true);
    }
    async function dragTo(width) {
      const box = await splitter.boundingBox();
      await page.mouse.move(box.x + box.width / 2, box.y + 100);
      await page.mouse.down();
      await page.mouse.move(44 + width, box.y + 140, { steps: 8 });
      await page.mouse.up();
      assert(!(await splitter.evaluate(el => el.classList.contains("dragging"))));
    }
    for (const theme of ["light", "dark"]) {
      if ((await page.locator("html").getAttribute("data-theme")) !== theme)
        await page.locator("#theme").click();
      await page.waitForFunction(value => document.documentElement.dataset.theme === value, theme);
      for (const route of ["messages", "traffic", "extensions", "settings"]) {
        await page.locator("#nav-" + route).click();
        await splitter.waitFor();
        await dragTo(120);
        await verifyWidth(224);
        await page.screenshot({ path: path.join(root, `${route}-${theme}-min.png`) });
        await dragTo(700);
        await verifyWidth(480);
        await page.screenshot({ path: path.join(root, `${route}-${theme}-max.png`) });
        await page.locator("#sidebar-toggle").click();
        assert.equal(await splitter.count(), 0);
        assert.equal((await panel.boundingBox()).x, 44);
        await page.locator("#sidebar-toggle").click();
        await verifyWidth(480);
        await splitter.press("Home");
        await verifyWidth(224);
        await splitter.press("ArrowRight");
        await verifyWidth(232);
        await splitter.press("End");
        await verifyWidth(480);
        report.checks.push({ route, theme, min: 224, max: 480, keyboard: true, collapseRestore: true });
      }
    }
    await page.locator("#settings-proxy").click();
    await verifyWidth(480);
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-search").fill("codex");
    await dragTo(336);
    await verifyWidth(336);
    assert.equal(await page.locator("#extension-search").inputValue(), "codex");
    await page.locator("#nav-messages").click();
    await verifyWidth(336);
    await page.locator("#history-back").click();
    await verifyWidth(336);
    assert.equal(await page.locator("#extension-search").inputValue(), "codex");
    await page.locator("#history-forward").click();
    await verifyWidth(336);
    await page.evaluate(() => window.desktop.navigate("extension-diagnostics", { extension: "traffic" }));
    await verifyWidth(336);
    report.checks.push("Shared width survives pages, proxy/diagnostics, history and preserves search");
    await splitter.press("End");
    await page.setViewportSize({ width: 760, height: 820 });
    await page.waitForFunction(() => document.querySelector("#sidebar-split").getAttribute("aria-valuenow") === "396");
    await verifyWidth(396);
    await dragTo(700);
    await verifyWidth(396);
    await page.screenshot({ path: path.join(root, "narrow-max.png") });
    await page.setViewportSize({ width: 1180, height: 820 });
    await splitter.press("Home");
    const box = await splitter.boundingBox();
    await page.mouse.move(box.x, box.y + 100);
    await page.mouse.down();
    await page.mouse.move(44 + 320, box.y + 100, { steps: 4 });
    await splitter.dispatchEvent("pointercancel", { pointerId: 1 });
    await page.mouse.up();
    assert(!(await splitter.evaluate(el => el.classList.contains("dragging"))));
    await dragTo(336);
    await verifyWidth(336);
    report.checks.push("Window narrowing reserves 320px for content; cancelled pointer drag cleans up and can restart");
    for (const route of ["overview", "connect", "provider"]) {
      await page.evaluate(route => window.desktop.navigate(route), route);
      assert.equal(await splitter.count(), 0);
    }
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    assert.equal(await splitter.getAttribute("aria-label"), "Resize sidebar");
    await page.locator("#nav-traffic").click();
    await page.locator("#recording").click();
    upstream = http.createServer((req, res) => res.end("splitter response"));
    await new Promise(resolve => upstream.listen(0, "127.0.0.1", resolve));
    await new Promise((resolve, reject) => http.get({
      host: "127.0.0.1", port, path: `http://127.0.0.1:${upstream.address().port}/splitter-check`
    }, res => { res.resume(); res.on("end", resolve); }).on("error", reject));
    await page.locator(".traffic-row", { hasText: "splitter-check" }).click();
    for (const [id, positive, negative] of [["list-split", "ArrowDown", "ArrowUp"], ["detail-split", "ArrowRight", "ArrowLeft"]]) {
      const control = page.locator("#" + id);
      await control.waitFor();
      const initial = Number(await control.getAttribute("aria-valuenow"));
      await control.press(positive);
      assert.equal(Number(await control.getAttribute("aria-valuenow")), initial + 1);
      await control.press(negative);
      assert.equal(Number(await control.getAttribute("aria-valuenow")), initial);
      const b = await control.boundingBox();
      await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
      await page.mouse.down();
      await page.mouse.move(b.x + b.width / 2 + (id === "detail-split" ? 30 : 0), b.y + b.height / 2 + (id === "list-split" ? 30 : 0), { steps: 4 });
      await page.mouse.up();
      assert.notEqual(Number(await control.getAttribute("aria-valuenow")), initial);
    }
    report.checks.push("English accessible label and existing traffic percentage splitters still support keyboard and drag on a real recorded request");
    assert.deepEqual(report.errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
    if (upstream) await new Promise(resolve => upstream.close(resolve));
    await browser?.close();
    if (proc.exitCode === null) {
      const exited = new Promise((resolve) => proc.once("exit", resolve));
      proc.kill("SIGTERM");
      await Promise.race([exited, pause(5000)]);
      if (proc.exitCode === null) proc.kill("SIGKILL");
    }
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2) + "\n",
    );
    console.log(JSON.stringify({ root, ...report }, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
