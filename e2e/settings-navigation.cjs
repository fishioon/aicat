const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] || "build/settings-navigation-" + Date.now(),
);
const home = path.join(root, "home");
const previewFile = path.join(root, "preview-url");
const osState = path.join(root, "os-state.json");
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function main() {
  fs.mkdirSync(home, { recursive: true, mode: 0o700 });
  const listener = net.createServer();
  await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
  const port = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\nextensions:\n  wecom-local:\n    enabled: false\n    local:\n      accountId: navigation-fixture\n      dataDir: /tmp/aicat-navigation-fixture\n`,
  );
  fs.writeFileSync(osState, JSON.stringify({ services: [], trusted: false }));
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
  const report = {
    passed: false,
    command: process.argv,
    binarySHA256: crypto
      .createHash("sha256")
      .update(fs.readFileSync(binary))
      .digest("hex"),
    coverage:
      "Headless Chromium and real Desktop service; hidden native app; no native window acceptance",
    checks: [],
    errors: [],
  };
  let browser;
  try {
    const deadline = Date.now() + 25000;
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
    page.setDefaultTimeout(15000);
    page.on("pageerror", (error) => report.errors.push(error.message));
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.locator("#nav-settings").click();
    await page.locator("#settings-general[aria-current=true]").waitFor();
    await page.locator("#settings-proxy").click();
    await page.locator("#listener-port").waitFor();
    assert.equal(await page.locator("#module-sidebar .side-item").count(), 2);
    assert.equal(await page.locator("#settings-extensions").count(), 0);
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom").click();
    await page.locator("#extension-install").waitFor();
    assert.equal(await page.locator("#extension-settings-page").count(), 0);
    await page.locator("#extension-wecom-local").click();
    await page
      .locator('#extension-settings-page[data-extension="wecom-local"]')
      .waitFor();
    await page.locator("#wecom-local-config").waitFor();
    assert.equal(
      await page.locator("#nav-extensions").getAttribute("aria-current"),
      "page",
    );
    assert.equal(
      await page.locator("#nav-settings").getAttribute("aria-current"),
      null,
    );
    assert.equal(
      await page.locator("#breadcrumb").innerText(),
      "扩展\n企业微信客户端",
    );
    assert.equal(
      await page.locator('#module-sidebar [aria-current="true"]').count(),
      1,
    );
    await page
      .locator("#wecom-local-agent-binary")
      .fill("/tmp/aicat-config-navigation");
    await page.locator("#wecom-local-agent-save").click();
    await page.waitForFunction(
      () =>
        window.desktop.state().messageChannels["wecom-local"].agent.binary ===
        "/tmp/aicat-config-navigation",
    );
    assert(
      fs
        .readFileSync(path.join(home, "aicat.yaml"), "utf8")
        .includes("/tmp/aicat-config-navigation"),
    );
    report.checks.push(
      "Settings only contains General and Proxy; uninstalled plugin has no config; installed extension displays and saves its own form in place",
    );
    await page.locator("#extension-diagnostics").click();
    await page.locator("#extension-diagnostics-page").waitFor();
    assert.equal(await page.locator("#wecom-local-config").count(), 0);
    await page.locator("#extension-wecom-local[aria-current=true]").waitFor();
    assert.equal(
      await page.locator("#nav-extensions").getAttribute("aria-current"),
      "page",
    );
    assert((await page.locator("#breadcrumb").innerText()).endsWith("诊断"));
    await page.locator("#history-back").click();
    await page.locator("#wecom-local-config").waitFor();
    await page.locator("#history-forward").click();
    await page.locator("#extension-diagnostics-page").waitFor();
    await page.locator("#extension-diagnostics-configure").click();
    await page.locator("#wecom-local-config").waitFor();
    await page.locator("#extension-codex").click();
    assert.equal(await page.locator("#extension-settings-page").count(), 0);
    await page.locator("#history-back").click();
    await page.locator("#wecom-local-config").waitFor();
    assert.equal(
      await page.locator("#wecom-local-agent-binary").inputValue(),
      "/tmp/aicat-config-navigation",
    );
    await page.locator("#nav-settings").click();
    await page.locator("#settings-general[aria-current=true]").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#wecom-local-config").waitFor();
    await page.locator("#history-forward").click();
    await page.locator("#settings-general[aria-current=true]").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#wecom-local-config").waitFor();
    await page.locator("#extension-menu").click();
    await page.locator('[data-action="extension-menu-diagnostics"]').click();
    await page.locator("#extension-diagnostics-page").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#wecom-local-config").waitFor();
    report.checks.push(
      "Diagnostics button/menu, return to config, extension switching and global settings history preserve module, extension, and breadcrumbs",
    );
    for (const theme of ["light", "dark"]) {
      if (theme === "dark") await page.locator("#theme").click();
      await page.waitForFunction(
        (value) => document.documentElement.dataset.theme === value,
        theme,
      );
      for (const width of [1180, 860]) {
        await page.setViewportSize({ width, height: 820 });
        const input = await page.locator("#wecom-local-dir").boundingBox();
        const panel = await page
          .locator("#extension-settings-page")
          .boundingBox();
        assert(input.width >= 200);
        assert(input.x + input.width <= panel.x + panel.width + 1);
        await page.screenshot({
          path: path.join(root, `settings-${theme}-${width}.png`),
        });
        assert(await page.locator("#extension-wecom-local").isVisible());
      }
    }
    report.checks.push("Light and dark screenshots at 1180 and 860 pixels");
    assert.deepEqual(report.errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
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
