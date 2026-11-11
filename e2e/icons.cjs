// Geometry regression: SVG baseline shifts, inherited text line-height,
// unequal icon containers, and menu/copy alignment changing across scenes.
const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");
// Native Eval probes expressions before statement bodies. Keep automation
// actions as expressions so WebKit does not report that syntax probe as an error.

function measureIcons() {
  return [
    ...document.querySelectorAll(
      "#titlebar svg, #navigation svg, #domains svg, [data-side-copy] svg, .extension-symbol svg, .extension-emblem svg, #popup svg",
    ),
  ]
    .filter(
      (svg) =>
        svg.getBoundingClientRect().width > 0 &&
        svg.getBoundingClientRect().height > 0,
    )
    .map((svg) => {
      const container =
        svg.closest(".extension-symbol, .extension-emblem") ??
        svg.closest("button");
      const icon = svg.getBoundingClientRect(),
        box = container.getBoundingClientRect();
      const label =
        container.id ||
        container.dataset.sideCopy ||
        container.dataset.action ||
        container.getAttribute("aria-label") ||
        container.className;
      return {
        label,
        icon: { x: icon.x, y: icon.y, width: icon.width, height: icon.height },
        container: { x: box.x, y: box.y, width: box.width, height: box.height },
        dx: icon.x + icon.width / 2 - box.x - box.width / 2,
        dy: icon.y + icon.height / 2 - box.y - box.height / 2,
        clipped:
          icon.x < box.x - 0.75 ||
          icon.y < box.y - 0.75 ||
          icon.right > box.right + 0.75 ||
          icon.bottom > box.bottom + 0.75 ||
          icon.x < -0.75 ||
          icon.y < -0.75 ||
          icon.right > innerWidth + 0.75 ||
          icon.bottom > innerHeight + 0.75,
      };
    });
}

module.exports = async function verifyIcons({
  page,
  root,
  send,
  navigate,
  settled,
  evaluate,
  wait,
  auto,
  click,
}) {
  const report = {
    passed: false,
    toleranceCSSPixels: 0.75,
    scenes: [],
    failures: [],
  };
  const sample = async (name, native = false) => {
    const icons = native
      ? await evaluate(`(${measureIcons.toString()})()`)
      : await page.evaluate(measureIcons);
    assert(icons.length > 0, name + " has no visible icons");
    report.scenes.push({
      name,
      renderer: native ? "WKWebView" : "Chromium",
      nativeErrorCount: await evaluate(
        "window.trafficView.metrics.errors.length",
      ),
      icons,
    });
    for (const icon of icons) {
      if (Math.abs(icon.dx) > 0.75 || Math.abs(icon.dy) > 0.75 || icon.clipped)
        report.failures.push({ scene: name, ...icon });
    }
  };
  try {
    await send("/icons/tree/leaf");
    await page.waitForFunction(() => document.querySelector(".traffic-row"));
    await page.locator(".traffic-row").first().click();
    await page.waitForFunction(
      () => window.trafficView.state().detailRevision > 0,
    );
    await page
      .locator('[data-group="domains"] .ac-tree-toggle')
      .first()
      .click();
    await page.locator('[data-group="domains"] .ac-tree-toggle').nth(1).click();
    await sample("expanded-domain-tree");
    for (const theme of ["light", "dark"]) {
      if ((await page.locator("html").getAttribute("data-theme")) !== theme) {
        await click("theme");
        await settled();
      }
      await sample("traffic-" + theme);
      await click("nav-more");
      await sample("more-menu-" + theme);
      await page.screenshot({
        path: path.join(root, "icons-menu-" + theme + ".png"),
      });
      await page.keyboard.press("Escape");
      await navigate("extensions");
      await sample("extensions-" + theme);
      await navigate("community");
      await sample("community-" + theme);
      await navigate("traffic");
      await page.locator(".traffic-row").first().click();
      await page.waitForFunction(
        () => window.trafficView.state().detailRevision > 0,
      );
      await page.screenshot({
        path: path.join(root, "icons-traffic-" + theme + ".png"),
      });
    }
    await click("nav-more");
    await page.locator('[data-action="pin-navigation"]').click();
    await settled();
    await click("nav-more");
    await sample("unpinned-menu");
    await page.locator('[data-action="pin-navigation"]').click();
    await settled();
    for (const width of [900, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await sample("traffic-width-" + width);
      await click("sidebar-toggle");
      await sample("collapsed-width-" + width);
      await click("sidebar-toggle");
    }
    await page.setViewportSize({ width: 1180, height: 900 });
    await navigate("general");
    await page.selectOption("#preference-language", "en");
    await settled();
    await navigate("traffic");
    await sample("traffic-english");
    await evaluate("(window.desktop.navigate('traffic'),true)");
    await wait(
      async () => await evaluate("!!document.querySelector('.traffic-row')"),
    );
    await evaluate("(document.querySelector('.traffic-row').click(),true)");
    await wait(
      async () => await evaluate("window.trafficView.state().detailRevision>0"),
    );
    await evaluate(
      `(document.querySelector('[data-group="domains"] .ac-tree-toggle').click(),true)`,
    );
    report.nativeTitlebar = await evaluate(`(() => {
      const header = document.querySelector('#titlebar').getBoundingClientRect();
      const height = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--mygo-titlebar-height'));
      return { height, centerOffset: height / 2 - header.height / 2 };
    })()`);
    assert(
      Math.abs(report.nativeTitlebar.centerOffset) <= 0.75,
      "native window controls must share the titlebar center",
    );
    await sample("native-traffic", true);
    await evaluate("(document.querySelector('#nav-more').click(),true)");
    await sample("native-menu", true);
    await auto
      .call("capture")
      .then((result) =>
        fs.writeFileSync(
          path.join(root, "icons-wkwebview.png"),
          Buffer.from(result.png, "base64"),
        ),
      );
    await evaluate("(document.querySelector('#popup button').click(),true)");
    await evaluate("(window.desktop.navigate('extensions'),true)");
    await wait(
      async () =>
        await evaluate("!!document.querySelector('.extension-emblem')"),
    );
    await sample("native-extensions", true);
    assert.deepEqual(await evaluate("window.trafficView.metrics.errors"), []);
    assert.equal(
      report.failures.length,
      0,
      JSON.stringify(
        report.failures.map(({ scene, label, dx, dy, clipped }) => ({
          scene,
          label,
          dx,
          dy,
          clipped,
        })),
      ),
    );
    report.passed = true;
  } finally {
    fs.writeFileSync(
      path.join(root, "icon-report.json"),
      JSON.stringify(report, null, 2),
    );
  }
  await navigate("general");
  await page.selectOption("#preference-language", "zh-Hans");
  await settled();
  await navigate("traffic");
  if ((await page.locator("html").getAttribute("data-theme")) !== "light") {
    await click("theme");
    await settled();
  }
  await evaluate("(window.desktop.navigate('traffic'),true)");
  await click("clear");
  await page.waitForFunction(() => window.trafficView.state().total === 0);
  return report;
};
