const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] || "build/branding-" + Date.now(),
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
        "README.md",
        "README_CN.md",
        "ui/src/components/BrandIntro.vue",
        "ui/src/components/OverviewPage.vue",
        "ui/src/components/SettingsPage.vue",
        "ui/src/translations.json",
        "ui/src/app.css",
        "e2e/branding.cjs",
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
    page.setDefaultTimeout(15000);
    const readmeZh = fs.readFileSync("README_CN.md", "utf8");
    const readmeEn = fs.readFileSync("README.md", "utf8");
    for (const text of ["你的通用 AI 连接工具", "模块化设计，可扩展、可组合。", "灵感源自 netcat。", "[English](README.md)"])
      assert(readmeZh.includes(text), text);
    for (const text of ["Your General-Purpose AI Connector", "Modular by design. Extensible and composable.", "Inspired by netcat.", "[简体中文](README_CN.md)"])
      assert(readmeEn.includes(text), text);
    assert(!readmeZh.includes("Your General-Purpose AI Connector"));
    assert(!readmeZh.includes("Inspired by netcat."));
    assert(!readmeZh.includes("Modular by design. Extensible and composable."));
    assert(!readmeEn.includes("你的通用 AI 连接工具"));
    assert(!readmeEn.includes("灵感源自 netcat。"));
    assert(!readmeEn.includes("模块化设计，可扩展、可组合。"));
    report.checks.push("Chinese and English READMEs use their own language and link to each other");
    for (const language of ["zh-Hans", "en"]) {
      await page.locator("#nav-settings").click();
      await page.locator("#preference-language-" + language).click();
      await page.waitForFunction(
        (value) => document.documentElement.lang === value,
        language,
      );
      const positioning = language === "en"
        ? "Your General-Purpose AI Connector"
        : "你的通用 AI 连接工具";
      const inspiration = language === "en"
        ? "Inspired by netcat."
        : "灵感源自 netcat。";
      const principles = language === "en"
        ? "Modular by design. Extensible and composable."
        : "模块化设计，可扩展、可组合。";
      for (const theme of ["light", "dark"]) {
        if ((await page.locator("html").getAttribute("data-theme")) !== theme)
          await page.locator("#theme").click();
        await page.waitForFunction(
          (value) => document.documentElement.dataset.theme === value,
          theme,
        );
        for (const width of [1180, 860]) {
          await page.setViewportSize({ width, height: 820 });
          for (const route of ["overview", "settings"]) {
            await page.locator("#nav-" + route).click();
            const brand = page.locator(".app-brand");
            await brand.waitFor();
            assert.equal(await brand.getByRole("heading").innerText(), "aicat");
            assert.equal(await brand.locator('[data-brand="positioning"]').innerText(), positioning);
            assert.equal(await brand.locator('[data-brand="inspiration"]').innerText(), inspiration);
            assert.equal(await brand.locator('[data-brand="principles"]').innerText(), principles);
            const bounds = await brand.boundingBox();
            assert(bounds.x >= 0 && bounds.x + bounds.width <= width);
            assert(bounds.y >= 0 && bounds.y + bounds.height <= 820);
            assert(await brand.evaluate((element) =>
              [...element.querySelectorAll("p")].every((p) => p.scrollWidth <= p.clientWidth)
            ));
            await page.screenshot({
              path: path.join(root, `${route}-${language}-${theme}-${width}.png`),
            });
            report.checks.push({ route, language, theme, width, positioning, principles, inspiration });
          }
        }
      }
    }
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
