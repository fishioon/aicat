const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] || "build/titlebar-navigation-" + Date.now(),
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
        "e2e/titlebar-navigation.cjs",
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
  let browser;
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
    for (const theme of ["light", "dark"]) {
      if ((await page.locator("html").getAttribute("data-theme")) !== theme)
        await page.locator("#theme").click();
      await page.waitForFunction(
        (value) => document.documentElement.dataset.theme === value,
        theme,
      );
      for (const width of [1180, 860]) {
        await page.setViewportSize({ width, height: 820 });
        for (const module of ["traffic", "extensions", "settings"]) {
          await page.locator("#nav-" + module).click();
          const side = page.locator("#module-page > .split > .side:visible");
          await side.waitFor();
          const expanded = await page.locator("#breadcrumb").boundingBox();
          const content = await page
            .locator("#module-page > .split > .main:visible")
            .boundingBox();
          const controls = await page.locator("#sidebar-toggle").boundingBox();
          assert.equal(expanded.x, content.x);
          assert.equal(content.x, 268);
          await page.locator("#sidebar-toggle").click();
          await side.waitFor({ state: "hidden" });
          const collapsed = await page.locator("#breadcrumb").boundingBox();
          const collapsedContent = await page
            .locator("#module-page > .split > .main:visible")
            .boundingBox();
          assert(collapsed.x < expanded.x);
          assert(collapsed.x >= controls.x + controls.width + 12);
          assert.equal(collapsedContent.x, 44);
          assert.deepEqual(
            await page.locator("#sidebar-toggle").boundingBox(),
            controls,
          );
          const address = await page.locator("#gateway-address").boundingBox();
          assert(collapsed.x + collapsed.width <= address.x);
          await page.screenshot({
            path: path.join(root, `${module}-${theme}-${width}-collapsed.png`),
          });
          await page.locator("#sidebar-toggle").press("Enter");
          await side.waitFor();
          assert.equal(
            (await page.locator("#breadcrumb").boundingBox()).x,
            expanded.x,
          );
          await page.screenshot({
            path: path.join(root, `${module}-${theme}-${width}-expanded.png`),
          });
          report.checks.push({
            module,
            theme,
            width,
            expandedX: expanded.x,
            collapsedX: collapsed.x,
          });
        }
      }
    }
    await page.locator("#sidebar-toggle").click();
    await page.locator("#nav-extensions").click();
    const collapsedX = (await page.locator("#breadcrumb").boundingBox()).x;
    assert.equal(
      await page.locator("#sidebar-toggle").getAttribute("aria-expanded"),
      "false",
    );
    await page.locator("#history-back").click();
    assert.equal(
      (await page.locator("#breadcrumb").boundingBox()).x,
      collapsedX,
    );
    await page.locator("#nav-overview").click();
    await page.locator("#overview").waitFor();
    assert(await page.locator("#sidebar-toggle").isDisabled());
    assert.equal(
      (await page.locator("#breadcrumb").boundingBox()).x,
      collapsedX,
    );
    await page.locator("#nav-settings").click();
    await page.locator("#sidebar-toggle").click();
    await page.locator("#nav-overview").click();
    await page.locator("#overview").waitFor();
    assert.equal(
      (await page.locator("#breadcrumb").boundingBox()).x,
      collapsedX,
    );
    report.checks.push(
      "Collapsed alignment survives navigation/history; pages without sidebars use compact alignment regardless of saved sidebar state",
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
