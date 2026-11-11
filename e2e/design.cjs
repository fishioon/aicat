// Compare production layout metrics/tokens with the checked-in HTML design.
const fs = require("node:fs"),
  path = require("node:path"),
  crypto = require("node:crypto");
const assert = require("node:assert/strict");
module.exports = async function verifyDesign(page, browser, output) {
  const source = fs.readFileSync("docs/design/aicat-shell.html", "utf8");
  const reference = await browser.newPage({
    viewport: { width: 1180, height: 900 },
  });
  await reference.evaluate(
    () =>
      (window.openai = {
        widgetState: {
          privateContent: {
            trafficEnabled: true,
            theme: "light",
            locale: "zh-CN",
          },
        },
      }),
  );
  await reference.setContent(source);
  await reference.locator("[data-module=traffic]").click();
  await reference
    .locator("[data-inspector-side=response][data-inspector-view=raw]")
    .click();
  const pairs = [
    ["titlebar", "#titlebar", ".ac-titlebar", "height"],
    ["navigation", "#navigation", ".ac-rail", "width"],
    ["sidebar", "#sidebar", ".ac-sidebar", "width"],
    ["toolbar", ".toolbar", ".ac-toolbar", "height"],
    ["tableHeader", ".table-head", ".ac-table th", "height"],
    ["row", ".traffic-row", ".ac-table td", "height"],
    ["detailSummary", "#detail-title", ".ac-detail-heading", "height"],
    ["inspectorBar", ".ac-inspector-bar", ".ac-inspector-bar", "height"],
    ["copyButton", "[data-side-copy]", ".ac-inspector-bar .ac-icon", "width"],
    ["rawGutter", ".ac-line-number", ".ac-line-number", "width"],
  ];
  const report = {
    passed: false,
    reference: "docs/design/aicat-shell.html",
    referenceSHA256: crypto.createHash("sha256").update(source).digest("hex"),
    metrics: {},
    colors: {},
  };
  try {
    for (const [name, actualSelector, referenceSelector, axis] of pairs) {
      const a = await page.locator(actualSelector).first().boundingBox(),
        b = await reference.locator(referenceSelector).first().boundingBox();
      assert(a && b, name);
      report.metrics[name] = { actual: a[axis], reference: b[axis] };
      assert(
        Math.abs(a[axis] - b[axis]) <= 1,
        name + " mismatch " + JSON.stringify(report.metrics[name]),
      );
    }
    for (const theme of ["light", "dark"]) {
      if ((await page.locator("html").getAttribute("data-theme")) !== theme) {
        await page.locator("#theme").click();
        await page.waitForFunction(
          (theme) => document.documentElement.dataset.theme === theme,
          theme,
        );
      }
      await reference
        .locator("#aicat-shell-draft")
        .evaluate((root, theme) => (root.dataset.theme = theme), theme);
      const actual = await page
          .locator(".tab.active")
          .first()
          .evaluate((e) => getComputedStyle(e).color),
        expected = await reference
          .locator(".ac-inspector-tab[aria-selected=true]")
          .first()
          .evaluate((e) => getComputedStyle(e).color);
      assert.equal(actual, expected, theme + " brand text");
      report.colors[theme] = { actual, reference: expected };
      await page.screenshot({
        path: path.join(output, "design-actual-" + theme + ".png"),
      });
      await reference.screenshot({
        path: path.join(output, "design-reference-" + theme + ".png"),
      });
    }
    await page.locator("#theme").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "light",
    );
    report.passed = true;
  } finally {
    await reference.close();
    fs.writeFileSync(
      path.join(output, "design-report.json"),
      JSON.stringify(report, null, 2),
    );
  }
  return report;
};
