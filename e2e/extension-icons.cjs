const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const { spawn } = require("node:child_process");
const assert = require("node:assert/strict");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] ||
    "build/extension-icons-" + new Date().toISOString().replace(/[:.]/g, "-"),
);
const home = path.join(root, "home");
fs.mkdirSync(home, { recursive: true, mode: 0o700 });
const previewFile = path.join(root, "preview-url");
const report = { passed: false, checks: [], command: process.argv };
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function wait(fn) {
  const deadline = Date.now() + 15000;
  let last;
  while (Date.now() < deadline) {
    try {
      const result = await fn();
      if (result) return result;
    } catch (error) {
      last = error;
    }
    await delay(50);
  }
  throw Error("timeout: " + (last?.stack || fn.toString()));
}
let proc, browser, page, origin, token;
const errors = [],
  external = [];
async function rpc(method, args = []) {
  const response = await fetch(origin + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
  });
  if (!response.ok) throw Error(await response.text());
  return response.json();
}
(async () => {
  try {
    const server = net.createServer();
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const port = server.address().port;
    await new Promise((resolve) => server.close(resolve));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `ui:\n  language: zh-Hans\n  theme: light\ngateway:\n  addr: 127.0.0.1:${port}\n`,
    );
    const osState = path.join(root, "os-state.json");
    fs.writeFileSync(
      osState,
      JSON.stringify({
        trusted: false,
        proxy: { http: { enabled: false }, https: { enabled: false } },
      }),
    );
    const log = fs.openSync(path.join(root, "app.log"), "a");
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
    await wait(() => fs.existsSync(previewFile));
    const url = new URL(fs.readFileSync(previewFile, "utf8"));
    origin = url.origin;
    token = url.searchParams.get("token");
    await wait(() => rpc("State"));
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    page = await browser.newPage({ viewport: { width: 1180, height: 1000 } });
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("request", (request) => {
      if (
        !request.url().startsWith(origin) &&
        !request.url().startsWith("data:")
      )
        external.push(request.url());
    });
    await page.goto(url.href);
    await page.waitForFunction(() => !!window.desktop?.state());
    await page.locator("#nav-extensions").click();
    for (const theme of ["light", "dark"]) {
      await rpc("SetPreferences", [{ language: "zh-Hans", theme }]);
      await page.waitForFunction(
        (theme) => document.documentElement.dataset.theme === theme,
        theme,
      );
      for (const id of ["codex", "wecom"]) {
        await page.locator("#extension-" + id).click();
        const detail = page.locator(
          `.extension-detail[data-extension="${id}"] .extension-emblem`,
        );
        await wait(() =>
          detail
            .locator("img")
            .evaluate((image) => image.complete && image.naturalWidth === 256),
        );
        const small = page.locator("#extension-" + id + " .xicon");
        const source = await detail.locator("img").getAttribute("src");
        assert(source.startsWith("/assets/"));
        assert(source.includes(id === "codex" ? "codex-" + theme : "wecom"));
        assert.equal(await small.locator("img").getAttribute("src"), source);
        for (const [icon, size] of [
          [small, 36],
          [detail, 64],
        ]) {
          const box = await icon.boundingBox();
          assert.equal(box.width, size);
          assert.equal(box.height, size);
        }
        await page.screenshot({ path: path.join(root, `${id}-${theme}.png`) });
        report.checks.push(
          `${id} ${theme}: official bundled image loads in list and detail, 36/64px layout preserved`,
        );
        if (id === "wecom") {
          if (
            !(await rpc("State")).extensions.find((x) => x.id === id).installed
          )
            await rpc("InstallExtension", [id]);
          await page.locator("#extension-configure").click();
          await page.locator("#wecom-settings").waitFor();
          assert.equal(await page.locator("#wecom-send-form").count(), 0);
          await page.locator("#settings-general").click();
          await page.locator("#preference-theme-" + theme).waitFor();
          await page.locator("#settings-proxy").click();
          await page.locator("#listener-address").waitFor();
          await page.locator("#settings-extension-wecom").click();
          await page.locator("#wecom-settings").waitFor();
          assert.equal(await page.locator("#listener-address").count(), 0);
          await page.screenshot({
            path: path.join(root, `wecom-settings-${theme}.png`),
          });
          await page.locator("#extension-settings-diagnostics").click();
          await page.locator("#wecom-diagnostics").waitFor();
          assert.equal(await page.locator("#wecom-bot-id").count(), 0);
          assert.equal(await page.locator("#wecom-send").isDisabled(), true);
          await page.screenshot({
            path: path.join(root, `wecom-diagnostics-${theme}.png`),
          });
          await page.locator("#extension-manage").click();
          await page
            .locator('.extension-detail[data-extension="wecom"]')
            .waitFor();
          assert.equal(await page.locator("#wecom-config").count(), 0);
          report.checks.push(
            `settings ${theme}: general/proxy preserved; extension settings and diagnostics use distinct forms and return to details`,
          );
        }
      }
      await page.locator("#extension-traffic").click();
      assert.equal(
        await page
          .locator(
            '.extension-detail[data-extension="traffic"] .extension-emblem svg',
          )
          .count(),
        1,
      );
      assert.equal(
        await page
          .locator(
            '.extension-detail[data-extension="traffic"] .extension-emblem img',
          )
          .count(),
        0,
      );
      report.checks.push(
        `traffic ${theme}: existing functional icon preserved`,
      );
    }
    assert.deepEqual(errors, []);
    assert.deepEqual(external, []);
    assert.equal(fs.existsSync(path.join(home, "state/channel-host")), false);
    report.checks.push(
      "icons load locally without remote requests or channel activation",
    );
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    if (page)
      await page
        .screenshot({ path: path.join(root, "failure.png") })
        .catch(() => {});
  } finally {
    await browser?.close();
    if (proc && proc.exitCode === null && proc.signalCode === null) {
      const exited = new Promise((resolve) => proc.once("exit", resolve));
      proc.kill("SIGTERM");
      await exited;
    }
    report.errors = errors;
    report.external = external;
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify({ ...report, artifacts: root }, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
