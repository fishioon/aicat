const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const crypto = require("node:crypto");
const { spawn } = require("node:child_process");
const assert = require("node:assert/strict");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] ||
    "build/extension-categories-" +
      new Date().toISOString().replace(/[:.]/g, "-"),
);
const home = path.join(root, "home");
fs.mkdirSync(home, { recursive: true, mode: 0o700 });
const previewFile = path.join(root, "preview-url");
const report = {
  passed: false,
  checks: [],
  command: process.argv,
  binarySHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  coverage:
    "Headless Chromium and real Desktop service; hidden native app; no native window acceptance",
};
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
    const search = page.locator("#extension-search");
    const ids = ["wecom-local", "wecom", "wpk"];
    const enabledSection = page.locator("#module-sidebar > section").filter({
      has: page.locator("#extensions-enabled"),
    });
    const recommendedSection = page
      .locator("#module-sidebar > section")
      .filter({
        has: page.locator("#extensions-recommended"),
      });
    async function checkGroups(enabledIds) {
      const state = await wait(async () => {
        const state = await rpc("State");
        return (
          JSON.stringify(
            state.extensions.filter((x) => x.enabled).map((x) => x.id),
          ) === JSON.stringify(enabledIds) && state
        );
      });
      const recommendedIds = state.extensions
        .filter((x) => !enabledIds.includes(x.id))
        .map((x) => x.id);
      for (const [section, expected] of [
        [enabledSection, enabledIds],
        [recommendedSection, recommendedIds],
      ]) {
        await wait(
          async () =>
            (await section.locator(".xitem").count()) === expected.length,
        );
        assert.deepEqual(
          await section
            .locator(".xitem")
            .evaluateAll((items) =>
              items.map((item) => item.id.replace("extension-", "")),
            ),
          expected,
        );
        assert.equal(
          await section.locator(".num").innerText(),
          String(expected.length),
        );
      }
      assert.deepEqual(
        state.extensions.filter((x) => x.enabled).map((x) => x.id),
        enabledIds,
      );
      assert.equal(
        await page.locator(".xitem").count(),
        state.extensions.length,
      );
    }
    for (const language of ["zh-Hans", "en"]) {
      const category = language === "en" ? "Message Channels" : "消息渠道";
      for (const theme of ["light", "dark"]) {
        await rpc("SetPreferences", [{ language, theme }]);
        await page.waitForFunction(
          ({ language, theme }) =>
            document.documentElement.lang === language &&
            document.documentElement.dataset.theme === theme,
          { language, theme },
        );
        await search.fill(category);
        await wait(async () => (await page.locator(".xitem").count()) === 3);
        for (const id of ids) {
          const item = page.locator("#extension-" + id);
          assert.equal(await item.locator(".tag, .tags").count(), 0);
          assert(await item.locator(".desc").isVisible());
          assert.equal(await item.locator(".st").count(), 0);
          await item.click();
          await page
            .locator(`.extension-detail[data-extension="${id}"]`)
            .waitFor();
          assert.equal(
            await page
              .locator(
                ".extension-detail [role=tablist], .extension-detail .feat, h1 .tag",
              )
              .count(),
            0,
          );
          const info = page.locator("#extension-information");
          assert.equal(await info.getAttribute("open"), null);
          assert.equal(await info.locator("dl").isVisible(), false);
          await info.locator("summary").click();
          assert.equal(
            await info.locator("dd").nth(2).innerText(),
            id === "wecom"
              ? "OpenClaw"
              : "NPM",
          );
          await info.locator("summary").click();
          for (const width of [1180, 860]) {
            await page.setViewportSize({ width, height: 1000 });
            const nameBox = await item.locator("strong").boundingBox();
            const itemBox = await item.boundingBox();
            assert(
              nameBox.width > 0 &&
                nameBox.x >= itemBox.x &&
                nameBox.x + nameBox.width <= itemBox.x + itemBox.width,
            );
            await page.screenshot({
              path: path.join(root, `${id}-${language}-${theme}-${width}.png`),
              fullPage: true,
            });
          }
        }
        await search.fill("");
        await wait(async () => (await page.locator(".xitem").count()) === 5);
        await page.setViewportSize({ width: 1180, height: 900 });
        assert.equal(
          await page
            .locator("#module-sidebar .tag, #module-sidebar .badge")
            .count(),
          0,
        );
        await page.screenshot({
          path: path.join(root, `extensions-${language}-${theme}.png`),
        });
        report.checks.push(
          `${language} ${theme}: category search finds all three NPM message channels; list and title contain no tags; name and summary remain; repeated status and tabs are absent; technical information is collapsed and can be opened; 1180/860px screenshots saved`,
        );
      }
      await search.fill(
        language === "en" ? "  mEsSaGe ChAnNeLs  " : "  消息渠道  ",
      );
      await wait(async () => (await page.locator(".xitem").count()) === 3);
      await search.fill("no-such-extension-category");
      await wait(async () => (await page.locator(".xitem").count()) === 0);
      await search.fill("");
      await wait(async () => (await page.locator(".xitem").count()) === 5);
      await search.fill("wecom-local");
      await wait(async () => (await page.locator(".xitem").count()) === 1);
      assert.equal(
        await page.locator(".xitem").getAttribute("id"),
        "extension-wecom-local",
      );
      await search.fill(language === "en" ? "Network" : "网络");
      await wait(async () => (await page.locator(".xitem").count()) === 1);
      assert.equal(
        await page.locator(".xitem").getAttribute("id"),
        "extension-traffic",
      );
      await search.fill("");
      await checkGroups(["traffic"]);
      assert.equal(
        await enabledSection.locator(".label").innerText(),
        language === "en" ? "Enabled" : "已启用",
      );
      assert.equal(await recommendedSection.locator(".st").count(), 0);
      await page.locator("#extension-codex").click();
      assert(await page.locator("#codex-snippet").isVisible());
      assert(
        (await page.locator("#codex-snippet").innerText()).includes(
          `/backend-api/codex`,
        ),
      );
      assert(await page.locator("#codex-copy").isEnabled());
      assert.equal(
        await page.locator('.extension-detail [role="tablist"]').count(),
        0,
      );
      await page.screenshot({ path: path.join(root, `codex-${language}.png`) });
      assert.equal(
        (await rpc("State")).extensions.find((x) => x.id === "codex").installed,
        true,
      );
      for (const section of [enabledSection, recommendedSection]) {
        await section.locator(".xsec").click();
        assert.equal(await section.locator(".xitem").count(), 0);
        assert.equal(
          await section.locator(".xsec").getAttribute("aria-expanded"),
          "false",
        );
        await section.locator(".xsec").click();
      }
      await page.locator("#extension-traffic").click();
      await page.locator("#feature-traffic").click();
      await checkGroups([]);
      assert(await enabledSection.locator(".xnote").isVisible());
      assert(
        await page
          .locator('.extension-detail[data-extension="traffic"]')
          .isVisible(),
      );
      assert.equal(
        await recommendedSection
          .locator("#extension-traffic")
          .getAttribute("aria-current"),
        "true",
      );
      assert.equal(await page.locator("#nav-traffic").count(), 0);
      await search.fill(language === "en" ? "Network" : "网络");
      await wait(
        async () => (await recommendedSection.locator(".xitem").count()) === 1,
      );
      assert.equal(await recommendedSection.locator(".num").innerText(), "1");
      await search.fill("");
      await page.screenshot({
        path: path.join(root, `extensions-disabled-${language}.png`),
      });
      await page.reload();
      await page.waitForFunction(() => !!window.desktop?.state());
      await page.locator("#nav-extensions").click();
      await checkGroups([]);
      await page.locator("#extension-traffic").click();
      await page.locator("#feature-traffic").click();
      await checkGroups(["traffic"]);
      assert.equal(
        await enabledSection
          .locator("#extension-traffic")
          .getAttribute("aria-current"),
        "true",
      );
      assert(await page.locator("#nav-traffic").isVisible());
      await page.screenshot({
        path: path.join(root, `extensions-enabled-${language}.png`),
      });
      report.checks.push(
        `${language}: enabled/recommended groups ignore installation status; counts, collapse, empty state, search, disable/re-enable, selection, navigation and reload work`,
      );
      report.checks.push(
        `${language}: whitespace/case normalization, unmatched query, clearing query, identifier and existing category search work`,
      );
    }
    const state = await rpc("State");
    for (const id of ids) {
      const extension = state.extensions.find((x) => x.id === id);
      assert.deepEqual(extension.categories, ["消息渠道"]);
      assert.equal(extension.enabled, false);
      assert.equal(extension.installed, false);
    }
    assert.deepEqual(errors, []);
    assert.deepEqual(external, []);
    assert.equal(fs.existsSync(path.join(home, "state/channel-host")), false);
    report.checks.push(
      "categories come from real Desktop metadata; no remote requests, installation or channel activation",
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
