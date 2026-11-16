const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { createRegistry } = require("./fixtures/channel-registry.cjs");
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
    `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\n`,
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
  let browser, registry;
  try {
    const deadline = Date.now() + 25000;
    while (!fs.existsSync(previewFile)) {
      if (proc.exitCode !== null || Date.now() > deadline)
        throw Error("Preview unavailable");
      await pause(100);
    }
    registry = await createRegistry(root, {
      name: "@fixture/navigation", version: "1.0.0", type: "module",
      aicat: {
        api: 1, id: "fixture-navigation", entry: "index.mjs",
        fields: [
          { key: "accountId", label: "账号", type: "string", required: true },
          { key: "dataDir", label: "目录", type: "directory", required: true },
          { key: "key", label: "密钥", type: "secret", required: true },
        ],
        capabilities: {}, room: { mode: "conversation", trigger: { all: true } },
      },
    }, "export function createChannel(){return {open(){},close(){}}}");
    const previewURL = new URL(fs.readFileSync(previewFile, "utf8"));
    const readyDeadline = Date.now() + 25000;
    for (;;) {
      const state = await fetch(new URL("/rpc", previewURL), {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Aicat-Preview": previewURL.searchParams.get("token") },
        body: JSON.stringify({ method: "Desktop.State", args: [] }),
      });
      if (state.ok && (await state.json()).gateway.state === "ready") break;
      if (proc.exitCode !== null || Date.now() > readyDeadline) throw Error("Desktop service unavailable");
      await pause(100);
    }
    const setup = await fetch(new URL("/rpc", previewURL), {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Aicat-Preview": previewURL.searchParams.get("token") },
      body: JSON.stringify({ method: "Desktop.InstallChannelPackage", args: [{
        id: "fixture-navigation", name: "验收渠道", summary: "导航协议夹具",
        package: "@fixture/navigation", version: "1.0.0", format: "aicat",
        registry: registry.url, publisher: "fixture",
      }] }),
    });
    assert(setup.ok, await setup.text());
    const configured = await fetch(new URL("/rpc", previewURL), {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Aicat-Preview": previewURL.searchParams.get("token") },
      body: JSON.stringify({ method: "Desktop.ConfigureMessageChannel", args: ["fixture-navigation", {
        accountId: "", secret: "", values: { accountId: "navigation-fixture", dataDir: "/tmp/aicat-navigation-fixture", key: "fixture-key" },
      }] }),
    });
    assert(configured.ok, await configured.text());
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
    let expectedDataDir = "/tmp/aicat-navigation-fixture";
    async function openNative() {
      const details = page.locator("#native-channel-settings details");
      await details.waitFor({ state: "attached" });
      if (await details.getAttribute("open") === null) await details.locator("summary").click();
      await page.waitForFunction((directory) => document.getElementById("channel-dataDir")?.value === directory, expectedDataDir);
    }
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
    await page.locator("#extension-fixture-navigation").click();
    await page
      .locator('#extension-settings-page[data-extension="fixture-navigation"]')
      .waitFor();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
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
      "扩展\n验收渠道",
    );
    assert.equal(
      await page.locator('#module-sidebar [aria-current="true"]').count(),
      1,
    );
    await openNative();
    await page.locator("#channel-accountId").fill("navigation-fixture");
    await page.locator("#channel-dataDir").fill("/tmp/aicat-config-navigation");
    await page.locator("#channel-key").fill("fixture-key");
    await page.locator("#native-channel-settings form button").click();
    const saveDeadline = Date.now() + 15000;
    while (!fs.readFileSync(path.join(home, "aicat.yaml"), "utf8").includes("/tmp/aicat-config-navigation")) {
      if (Date.now() > saveDeadline) throw Error("Channel settings did not save");
      await pause(50);
    }
    expectedDataDir = "/tmp/aicat-config-navigation";
    report.checks.push(
      "Settings only contains General and Proxy; uninstalled plugin has no config; installed extension displays and saves its own form in place",
    );
    await page.locator("#extension-diagnostics").click();
    await page.locator("#extension-diagnostics-page").waitFor();
    assert.equal(await page.locator("#native-channel-settings").count(), 0);
    await page.locator("#extension-fixture-navigation[aria-current=true]").waitFor();
    assert.equal(
      await page.locator("#nav-extensions").getAttribute("aria-current"),
      "page",
    );
    assert((await page.locator("#breadcrumb").innerText()).endsWith("诊断"));
    await page.locator("#history-back").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
    await page.locator("#history-forward").click();
    await page.locator("#extension-diagnostics-page").waitFor();
    await page.locator("#extension-diagnostics-configure").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
    await page.locator("#extension-codex").click();
    assert.equal(await page.locator("#extension-settings-page").count(), 0);
    await page.locator("#history-back").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
    assert.equal(
      await page.locator("#channel-accountId").inputValue(),
      "navigation-fixture",
    );
    await page.locator("#nav-settings").click();
    await page.locator("#settings-general[aria-current=true]").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
    await page.locator("#history-forward").click();
    await page.locator("#settings-general[aria-current=true]").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
    await page.locator("#extension-menu").click();
    await page.locator('[data-action="extension-menu-diagnostics"]').click();
    await page.locator("#extension-diagnostics-page").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#native-channel-settings").waitFor();
    await openNative();
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
        await openNative();
        const input = await page.locator("#channel-dataDir").boundingBox();
        const panel = await page
          .locator("#extension-settings-page")
          .boundingBox();
        assert(input.width >= 200);
        assert(input.x + input.width <= panel.x + panel.width + 1);
        await page.screenshot({
          path: path.join(root, `settings-${theme}-${width}.png`),
        });
        assert(await page.locator("#extension-fixture-navigation").isVisible());
      }
    }
    report.checks.push("Light and dark screenshots at 1180 and 860 pixels");
    async function sidebarWidth(selector) {
      const side = page.locator(selector);
      await side.waitFor({ state: "visible" });
      const box = await side.boundingBox();
      assert.equal(box.width, 224, selector);
      assert.equal(
        await page.locator("#sidebar-toggle").getAttribute("aria-controls"),
        selector.slice(1),
      );
      assert.equal(
        await page.locator("#sidebar-toggle").getAttribute("aria-expanded"),
        "true",
      );
    }
    for (const theme of ["light", "dark"]) {
      const actual = await page.locator("html").getAttribute("data-theme");
      if (actual !== theme) await page.locator("#theme").click();
      await page.waitForFunction(
        (value) => document.documentElement.dataset.theme === value,
        theme,
      );
      for (const width of [1180, 860]) {
        await page.setViewportSize({ width, height: 820 });
        await page.locator("#nav-traffic").click();
        await sidebarWidth("#traffic-sidebar");
        const before = await page.locator("#traffic-page").boundingBox();
        await page.locator("#sidebar-toggle").click();
        assert(!(await page.locator("#traffic-sidebar").isVisible()));
        assert.equal(
          await page.locator("#sidebar-toggle").getAttribute("aria-expanded"),
          "false",
        );
        const after = await page.locator("#traffic-page").boundingBox();
        assert.equal(after.x, 44);
        assert.equal(after.width - before.width, 224);
        await page.screenshot({
          path: path.join(root, `traffic-collapsed-${theme}-${width}.png`),
        });
        await page.locator("#sidebar-toggle").press("Enter");
        await sidebarWidth("#traffic-sidebar");
        await page.locator("#nav-extensions").click();
        await page.locator("#extension-fixture-navigation").click();
        await sidebarWidth("#module-sidebar");
        await openNative();
        const draft = "/tmp/sidebar-draft-" + width;
        await page.locator("#channel-accountId").fill(draft);
        await page.locator("#sidebar-toggle").click();
        assert(!(await page.locator("#module-sidebar").isVisible()));
        assert.equal(
          await page.locator("#channel-accountId").inputValue(),
          draft,
        );
        await page.locator("#sidebar-toggle").press("Space");
        await sidebarWidth("#module-sidebar");
        assert.equal(
          await page.locator("#channel-accountId").inputValue(),
          draft,
        );
        await page.screenshot({
          path: path.join(root, `extensions-sidebar-${theme}-${width}.png`),
        });
        await page.locator("#extension-diagnostics").click();
        await sidebarWidth("#module-sidebar");
        await page.locator("#sidebar-toggle").click();
        await page.locator("#nav-settings").click();
        assert(!(await page.locator("#module-sidebar").isVisible()));
        assert.equal(
          await page.locator("#sidebar-toggle").getAttribute("aria-expanded"),
          "false",
        );
        await page.locator("#history-back").click();
        await page.locator("#extension-diagnostics-page").waitFor();
        assert(!(await page.locator("#module-sidebar").isVisible()));
        await page.locator("#history-forward").click();
        await page.locator("#sidebar-toggle").click();
        await sidebarWidth("#module-sidebar");
        await page.locator("#settings-proxy").click();
        await sidebarWidth("#module-sidebar");
        await page.screenshot({
          path: path.join(root, `proxy-sidebar-${theme}-${width}.png`),
        });
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth,
          ),
          false,
        );
      }
    }
    await page.locator("#nav-overview").click();
    assert(await page.locator("#sidebar-toggle").isDisabled());
    await page.locator("#connect-agent").click();
    await page.locator("#connect").waitFor();
    assert(await page.locator("#sidebar-toggle").isDisabled());
    await page.locator("#nav-overview").click();
    await page.locator("#connect-provider").click();
    await page.locator("#provider").waitFor();
    assert(await page.locator("#sidebar-toggle").isDisabled());
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    assert.equal(
      await page.locator("#sidebar-toggle").getAttribute("aria-label"),
      "Hide sidebar",
    );
    await page.locator("#sidebar-toggle").click();
    assert.equal(
      await page.locator("#sidebar-toggle").getAttribute("aria-label"),
      "Show sidebar",
    );
    await page.screenshot({
      path: path.join(root, "sidebar-en-collapsed.png"),
    });
    await page.locator("#sidebar-toggle").click();
    await sidebarWidth("#module-sidebar");
    report.checks.push(
      "Traffic, extensions, diagnostics and General/Proxy sidebars share 224px width; titlebar button and keyboard hide/restore without losing drafts; hidden sidebars release all width; visibility survives navigation and back/forward; button is disabled on Overview, Connect and Provider; light/dark and 1180/860 screenshots saved",
    );
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
    await registry?.close();
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
