// Compare production layout metrics and color tokens with the checked-in
// redesign HTML. Failure modes: shell sizes drifting from the design (title
// bar, rail, sidebar, rows, inspector), and palette tokens diverging in either
// theme. Expects the page on traffic with a request selected.
const fs = require("node:fs"),
  path = require("node:path"),
  crypto = require("node:crypto");
const assert = require("node:assert/strict");
const SOURCE = "docs/design/aicat-redesign.html";
const TOKENS = [
  "--chrome",
  "--surface",
  "--sunken",
  "--ink",
  "--ink-2",
  "--ink-3",
  "--line",
  "--line-strong",
  "--hover",
  "--mark",
  "--mark-line",
  "--mark-soft",
  "--on-mark",
  "--ok",
  "--warn",
  "--bad",
  "--warn-soft",
  "--bad-soft",
];
module.exports = async function verifyDesign(page, browser, output) {
  const source = fs.readFileSync(SOURCE, "utf8");
  // The design frames the window with 16px padding, a 1px border and caps it
  // at 1240px; this viewport gives the same 1178px content width as the app.
  const reference = await browser.newPage({
    viewport: { width: 1212, height: 900 },
  });
  const open = (theme) =>
    reference.goto(
      "file://" +
        path.resolve(SOURCE) +
        `#page=traffic&sel=2236&theme=${theme}&reqTab=header&resTab=raw`,
    );
  const pairs = [
    ["titlebar", "#titlebar", ".bar", "height"],
    ["navigation", "#navigation", ".rail", "width"],
    ["railButton", "#nav-overview", ".rail button", "width"],
    ["sidebar", "#traffic-sidebar", "#tside", "width"],
    ["sideItem", "#all-domains", "#tside .side-item", "height"],
    ["tableHeader", ".thead", ".list th", "height"],
    ["row", ".traffic-row", ".list td", "height"],
    ["detailSummary", "#detail-title", ".sum", "height"],
    ["paneHead", ".pane-head", ".pane-head", "height"],
    ["copyButton", "[data-side-copy]", ".pane-head .icon-btn", "width"],
    ["footer", ".foot", ".foot", "height"],
  ];
  const report = {
    passed: false,
    reference: SOURCE,
    referenceSHA256: crypto.createHash("sha256").update(source).digest("hex"),
    metrics: {},
    colors: {},
  };
  try {
    await open("light");
    await reference.locator(".sum").waitFor();
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
    // Resolve each token through a probe so #fff and #ffffff compare equal.
    const tokens = (target) =>
      target.evaluate((names) => {
        const probe = document.createElement("i");
        document.body.append(probe);
        const result = Object.fromEntries(
          names.map((name) => {
            probe.style.color = `var(${name})`;
            return [name, getComputedStyle(probe).color];
          }),
        );
        probe.remove();
        return result;
      }, TOKENS);
    for (const theme of ["light", "dark"]) {
      if ((await page.locator("html").getAttribute("data-theme")) !== theme) {
        await page.locator("#theme").click();
        await page.waitForFunction(
          (theme) => document.documentElement.dataset.theme === theme,
          theme,
        );
      }
      await open(theme);
      await reference.reload();
      await reference.waitForFunction(
        (theme) => document.documentElement.dataset.theme === theme,
        theme,
      );
      const actual = await tokens(page),
        expected = await tokens(reference);
      report.colors[theme] = { actual, reference: expected };
      assert.deepEqual(actual, expected, theme + " palette");
      const current = (target, selector) =>
        target
          .locator(selector)
          .first()
          .evaluate((e) => {
            const style = getComputedStyle(e);
            return [style.color, style.backgroundColor];
          });
      assert.deepEqual(
        await current(page, '#navigation [aria-current="page"]'),
        await current(reference, '.rail [aria-current="page"]'),
        theme + " current rail item",
      );
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
