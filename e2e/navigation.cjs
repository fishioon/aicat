const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { run } = require("./traffic-default.cjs");

run({
  scenario: "navigation",
  verify: async ({
    page,
    root,
    home,
    proxyPort,
    wait,
    current,
    stop,
    launch,
  }) => {
    const checks = [];
    const config = path.join(home, "aicat.yaml");
    const base = `gateway:\n  addr: 127.0.0.1:${proxyPort}\nui:\n  language: zh-Hans\n  theme: light\n`;
    const navigation = async (enabled = true) => {
      assert.deepEqual(
        await page
          .locator("#navigation button")
          .evaluateAll((buttons) => buttons.map((button) => button.id)),
        [
          "nav-overview",
          "nav-messages",
          ...(enabled ? ["nav-traffic"] : []),
          "nav-extensions",
          "nav-settings",
        ],
      );
      assert.equal(
        await page
          .locator("#nav-more, #more-pop, [data-action=pin-navigation]")
          .count(),
        0,
      );
    };
    const toggleTraffic = async (enabled) => {
      await page.locator("#nav-extensions").click();
      await page.locator("#extension-traffic").click();
      await page.locator("#feature-traffic").click();
      await wait(async () => (await current()).trafficEnabled === enabled);
      await wait(
        async () =>
          (await page.locator("#nav-traffic").count()) === (enabled ? 1 : 0),
      );
      await navigation(enabled);
    };
    await navigation();
    for (const [id, target] of [
      ["overview", "#overview"],
      ["messages", "#module-sidebar"],
      ["traffic", "#rows-viewport"],
      ["extensions", "#extension-traffic"],
      ["settings", "#settings-general"],
    ]) {
      await page.locator("#nav-" + id).click();
      await page.locator(target).waitFor();
      assert.equal(
        await page.locator("#nav-" + id).getAttribute("aria-current"),
        "page",
      );
      assert.equal(
        await page.locator('#navigation [aria-current="page"]').count(),
        1,
      );
      await navigation();
    }
    checks.push(
      "Default navigation opens each page with a single current entry; More and navigation pin actions are absent",
    );

    for (const [name, section] of [
      ["empty-pins", "navigation:\n  pinned: []\n"],
      ["other-pins", "navigation:\n  pinned: [other-module]\n"],
    ]) {
      await stop();
      fs.writeFileSync(config, base + section, { mode: 0o600 });
      await launch();
      await navigation();
      await page.locator("#nav-traffic").click();
      await page.locator("#rows-viewport").waitFor();
      assert.equal(
        await page.locator("#nav-traffic").getAttribute("aria-current"),
        "page",
      );
      checks.push(name + ": legacy navigation pins do not hide traffic");
    }

    await page.locator("#nav-messages").click();
    await page.locator("#nav-traffic").click();
    await toggleTraffic(false);
    for (let step = 0; step < 4; step++) {
      await page.locator("#history-back").click();
      await navigation(false);
      if (await page.locator('#nav-messages[aria-current="page"]').count())
        break;
    }
    await page.locator('#nav-messages[aria-current="page"]').waitFor();
    await navigation(false);
    await stop();
    await launch();
    assert.equal((await current()).trafficEnabled, false);
    await navigation(false);
    await page.screenshot({ path: path.join(root, "traffic-disabled.png") });
    await toggleTraffic(true);
    await stop();
    await launch();
    await navigation();
    checks.push(
      "Disabling traffic hides its entry and skips it in history; disabled state persists across restart; re-enabling restores the entry across restart",
    );

    for (const theme of ["light", "dark"]) {
      await page.locator("#nav-settings").click();
      await page.locator("#preference-theme-" + theme).click();
      await page.waitForFunction(
        (value) => document.documentElement.dataset.theme === value,
        theme,
      );
      for (const width of [1180, 860]) {
        await page.setViewportSize({ width, height: 820 });
        await page.locator("#nav-traffic").click();
        await navigation();
        await page.screenshot({
          path: path.join(root, `navigation-${theme}-${width}.png`),
        });
      }
    }
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    await page.locator("#nav-traffic").click();
    assert.equal(
      await page.locator("#nav-traffic").getAttribute("aria-label"),
      "Traffic",
    );
    await navigation();
    await page.screenshot({ path: path.join(root, "navigation-en.png") });
    checks.push(
      "Default entries remain available in light/dark themes, 1180/860 widths and English",
    );
    return {
      passed: true,
      checks,
      sources: Object.fromEntries(
        ["ui/src/App.vue", "ui/src/app.css"].map((file) => [
          file,
          crypto
            .createHash("sha256")
            .update(fs.readFileSync(file))
            .digest("hex"),
        ]),
      ),
    };
  },
}).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
