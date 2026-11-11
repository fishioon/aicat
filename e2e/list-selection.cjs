// Failure modes: selection navigations resetting a scrolled virtual list,
// first clicks targeting the wrong request, right-clicks jumping away, and
// fixing selection scroll at the cost of filter/search reset or history.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

module.exports = async function verifyListSelection({
  page,
  root,
  send,
  wait,
  evaluate,
  auto,
}) {
  await page.locator("#nav-traffic").click();
  await page.locator("#clear").click();
  await page.waitForFunction(() => window.trafficView.state().total === 0);
  for (let i = 0; i < 100; i++)
    await send("/list-selection/" + String(i).padStart(3, "0"));
  await page.waitForFunction(() => window.trafficView.state().total === 100);

  const snapshot = () =>
    page.evaluate(() => ({
      ...window.trafficView.state(),
      domScrollTop: document.querySelector("#rows-viewport").scrollTop,
    }));
  const targetAt = (offset) =>
    page.evaluate((offset) => {
      const viewport = document
        .querySelector("#rows-viewport")
        .getBoundingClientRect();
      const rows = [...document.querySelectorAll(".traffic-row")].filter(
        (row) => {
          const rect = row.getBoundingClientRect();
          return rect.top >= viewport.top && rect.bottom <= viewport.bottom;
        },
      );
      const row = rows[offset];
      const rect = row.getBoundingClientRect();
      return {
        id: Number(row.dataset.id),
        x: rect.x + 60,
        y: rect.y + rect.height / 2,
      };
    }, offset);
  const expectSelected = async (id) => {
    await page.waitForFunction((id) => {
      const state = window.trafficView.state();
      return state.selected === id && state.detailRevision > 0;
    }, id);
    assert.equal(
      await page
        .locator(`.traffic-row[data-id="${id}"]`)
        .getAttribute("aria-selected"),
      "true",
    );
  };
  const expectPosition = async (expected) => {
    const current = await snapshot();
    assert(
      Math.abs(current.domScrollTop - expected) <= 1,
      `scroll moved from ${expected} to ${current.domScrollTop}`,
    );
    assert(Math.abs(current.scrollTop - expected) <= 1);
  };

  await page.locator("#rows-viewport").hover();
  await page.mouse.wheel(0, 1500);
  await page.waitForFunction(
    () => window.trafficView.state().scrollTop >= 1200,
  );
  // Wait for wheel scrolling to finish before comparing positions.
  await page.evaluate(
    () =>
      new Promise((resolve) => {
        const viewport = document.querySelector("#rows-viewport");
        let previous = -1,
          stable = 0;
        function frame() {
          stable = viewport.scrollTop === previous ? stable + 1 : 0;
          previous = viewport.scrollTop;
          if (stable >= 5) resolve();
          else requestAnimationFrame(frame);
        }
        requestAnimationFrame(frame);
      }),
  );
  const before = await snapshot();
  const first = await targetAt(2);
  await page.screenshot({ path: path.join(root, "list-before-selection.png") });
  await page.mouse.click(first.x, first.y);
  await page.waitForFunction(
    (id) =>
      window.trafficView.state().selected === id &&
      window.trafficView.state().detailRevision > 0,
    first.id,
  );
  const after = await snapshot();
  fs.writeFileSync(
    path.join(root, "list-selection-first-click.json"),
    JSON.stringify({ target: first, before, after }, null, 2),
  );
  await expectPosition(before.domScrollTop);
  await expectSelected(first.id);
  await page.screenshot({ path: path.join(root, "list-after-selection.png") });

  const second = await targetAt(4);
  await page.mouse.click(second.x, second.y);
  await expectSelected(second.id);
  await expectPosition(before.domScrollTop);
  await page.locator("#history-back").click();
  await expectSelected(first.id);
  await expectPosition(before.domScrollTop);
  await page.locator("#history-forward").click();
  await expectSelected(second.id);
  await expectPosition(before.domScrollTop);

  await page.mouse.click(first.x, first.y, { button: "right" });
  await expectSelected(first.id);
  await page
    .locator("#popup")
    .getByRole("menuitem", { name: "复制 URL", exact: true })
    .waitFor();
  await expectPosition(before.domScrollTop);
  await page.keyboard.press("Escape");
  await expectPosition(before.domScrollTop);

  // Native coverage uses real WebView DOM events, not native pointer input.
  // The WebView keeps its own filters from earlier scenarios; start unfiltered.
  await evaluate("(window.desktop.navigate('traffic', { query: '' }),true)");
  await wait(
    async () => await evaluate("window.trafficView.state().total === 100"),
  );
  await evaluate(
    "(document.querySelector('#rows-viewport').scrollTop = 1500, true)",
  );
  await wait(
    async () =>
      (await evaluate("window.trafficView.state().scrollTop")) >= 1200,
  );
  const native = await evaluate(`(() => {
    const viewport = document.querySelector('#rows-viewport');
    const rect = viewport.getBoundingClientRect();
    const row = [...document.querySelectorAll('.traffic-row')].find(row => {
      const r = row.getBoundingClientRect();
      return r.top >= rect.top && r.bottom <= rect.bottom;
    });
    const result = { id: Number(row.dataset.id), scrollTop: viewport.scrollTop };
    row.click();
    return result;
  })()`);
  await wait(
    async () =>
      await evaluate(
        `window.trafficView.state().selected === ${native.id} && window.trafficView.state().detailRevision > 0`,
      ),
  );
  assert.equal(
    await evaluate("document.querySelector('#rows-viewport').scrollTop"),
    native.scrollTop,
  );
  assert.equal(
    await evaluate("window.trafficView.state().scrollTop"),
    native.scrollTop,
  );
  const capture = await auto.call("capture");
  fs.writeFileSync(
    path.join(root, "list-selection-wkwebview.png"),
    Buffer.from(capture.png, "base64"),
  );

  await page.locator("#search").fill("/list-selection/099");
  await page.waitForFunction(() => window.trafficView.state().visible === 1);
  await expectPosition(0);
  await page.locator("#search").fill("");
  const domain = page
    .locator('[data-group="domains"] .tree-item[data-path=""]')
    .first();
  await domain.click();
  await page.locator("#rows-viewport").hover();
  await page.mouse.wheel(0, 1200);
  await page.waitForFunction(() => window.trafficView.state().scrollTop > 600);
  await page.locator('[data-group="domains"] .group-title').click();
  await expectPosition(0);
  assert.deepEqual(await evaluate("window.trafficView.metrics.errors"), []);
  await page.locator("#clear").click();
  await page.waitForFunction(() => window.trafficView.state().total === 0);
  return {
    passed: true,
    firstClick: {
      before: before.domScrollTop,
      after: after.domScrollTop,
      selected: first.id,
    },
    native,
    chromium: "wheel and pointer input",
    wkwebview: "DOM scroll and click",
  };
};
