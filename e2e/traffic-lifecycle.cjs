const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

async function verify({ page, root, send, api, wait }) {
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#nav-traffic").click();
  await page.locator("#recording").click();
  await send("/lifecycle-first");
  await send("/lifecycle-second");
  await wait(
    async () =>
      (await api()).filter((row) => row.state === "completed").length === 2,
  );
  const rows = await api();
  const first = rows.find((row) => row.url.endsWith("/lifecycle-first"));
  const second = rows.find((row) => row.url.endsWith("/lifecycle-second"));
  const row = (id) => page.locator(`.traffic-row[data-id="${id}"]`);
  let mode = "hold";
  let held;
  let clearHeld;
  let holdClear = false;
  let failClear = false;
  const release = async () => {
    const pending = held;
    held = undefined;
    pending.release();
    await pending.done;
    await page.evaluate(() => new Promise(requestAnimationFrame));
  };
  await page.route("**/rpc", async (route) => {
    const request = route.request().postDataJSON();
    if (request.method === "Desktop.ClearTraffic" && failClear)
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "Clear connection interrupted" }),
      });
    if (request.method === "Desktop.ClearTraffic" && holdClear) {
      const response = await route.fetch();
      await new Promise((resolve) => {
        clearHeld = resolve;
      });
      return route.fulfill({ response });
    }
    if (request.method !== "Desktop.Record") return route.continue();
    if (mode === "fail")
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "Detail connection interrupted" }),
      });
    if (mode === "hold") {
      const response = await route.fetch();
      let done;
      const completion = new Promise((resolve) => {
        done = resolve;
      });
      await new Promise((resolve) => {
        held = { release: resolve, done: completion };
      });
      await route.fulfill({ response });
      done();
    } else return route.continue();
  });
  try {
    await row(first.id).click();
    await wait(() => held);
    await page.keyboard.press("Escape");
    mode = "pass";
    await release();
    assert.equal(await page.locator("#details").count(), 0);
    mode = "hold";
    await row(first.id).click();
    await wait(() => held);
    await page.locator("#search").fill("lifecycle-second");
    await page.locator("#details").waitFor({ state: "detached" });
    mode = "pass";
    await row(second.id).click();
    await page
      .locator("#detail-title", { hasText: "/lifecycle-second" })
      .waitFor();
    await release();
    assert(
      (await page.locator("#detail-title").innerText()).includes(
        "/lifecycle-second",
      ),
    );
    await page.locator("#search").fill("");
    mode = "fail";
    await row(first.id).click();
    await page
      .locator("#detail-title", { hasText: "Detail connection interrupted" })
      .waitFor();
    assert(await page.locator('[data-side-copy="request"]').isDisabled());
    assert.equal(
      await page.locator("#retry-detail").count(),
      1,
      "a transient detail read failure needs a direct recovery action",
    );
    await page.screenshot({ path: path.join(root, "detail-failed.png") });
    mode = "pass";
    await page.locator("#retry-detail").click();
    await page
      .locator("#detail-title", { hasText: "/lifecycle-first" })
      .waitFor();
    assert(await page.locator('[data-side-copy="request"]').isEnabled());
    await page.screenshot({ path: path.join(root, "detail-retried.png") });
    await page.keyboard.press("Escape");
    mode = "hold";
    await row(first.id).click();
    await wait(() => held);
    await page.locator("#clear").click();
    await wait(async () => !(await api()).length);
    await page.locator("#details").waitFor({ state: "detached" });
    mode = "pass";
    await release();
    assert.equal(await page.locator("#details").count(), 0);
    await send("/clear-old");
    await wait(async () => (await page.locator(".traffic-row").count()) === 1);
    holdClear = true;
    await page.locator("#clear").click();
    await wait(() => clearHeld);
    assert(await page.locator("#clear").isDisabled());
    assert.equal(
      await page.locator("#clear").getAttribute("aria-busy"),
      "true",
    );
    await page.locator("#search").fill("new-draft");
    await send("/new-draft");
    await wait(async () => (await page.locator(".traffic-row").count()) === 1);
    const next = (await api()).find((item) => item.url.endsWith("/new-draft"));
    await row(next.id).click();
    await page.locator("#detail-title", { hasText: "/new-draft" }).waitFor();
    const response = page.waitForResponse(
      (response) =>
        response.url().endsWith("/rpc") &&
        response.request().postDataJSON()?.method === "Desktop.ClearTraffic",
    );
    clearHeld();
    clearHeld = undefined;
    await (await response).finished();
    await page.evaluate(() => new Promise(requestAnimationFrame));
    assert.equal(
      await page.locator("#search").inputValue(),
      "new-draft",
      "a late clear response must not erase a newer filter",
    );
    assert(
      (await page.locator("#detail-title").innerText()).includes("/new-draft"),
      "a late clear response must not close a newer selection",
    );
    await page.screenshot({ path: path.join(root, "clear-new-selection.png") });
    holdClear = false;
    failClear = true;
    await page.locator("#clear").click();
    await page
      .locator("#notice", { hasText: "Clear connection interrupted" })
      .waitFor();
    await wait(() => page.locator("#clear").isEnabled());
    assert.equal(await page.locator("#search").inputValue(), "new-draft");
    assert.equal((await api()).length, 1);
    assert(
      (await page.locator("#detail-title").innerText()).includes("/new-draft"),
    );
    failClear = false;
    const cleared = page.waitForResponse(
      (response) =>
        response.url().endsWith("/rpc") &&
        response.request().postDataJSON()?.method === "Desktop.ClearTraffic",
    );
    await page.locator("#clear").click();
    await (await cleared).finished();
    await page.evaluate(() => new Promise(requestAnimationFrame));
    await wait(async () => !(await api()).length);
    assert.equal(await page.locator("#search").inputValue(), "");
    assert.equal(await page.locator("#details").count(), 0);
    for (const source of [
      "ui/src/state/traffic.ts",
      "ui/src/components/TrafficPage.vue",
    ])
      fs.copyFileSync(source, path.join(root, path.basename(source)));
    return {
      passed: true,
      checks: [
        "迟到详情在 Escape、筛选排除和清空后不复活旧选择",
        "详情读取失败可原位重试，读取期间不能复制",
        "清空的迟到响应不覆盖用户新筛选或新选择，失败保留状态且可重试",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    if (held) held.release();
    if (clearHeld) clearHeld();
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "traffic-lifecycle" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
