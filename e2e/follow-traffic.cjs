const assert = require("node:assert/strict"),
  fs = require("node:fs"),
  path = require("node:path"),
  crypto = require("node:crypto");

require("./traffic-default.cjs")
  .run({
    scenario: "follow-traffic",
    verify: async ({ page, root, wait, current, send }) => {
      await page.locator("#nav-traffic").click();
      if (!(await current()).recording)
        await page.locator("#recording").click();
      for (let index = 0; index < 85; index++)
        await send("/follow-latest/" + index);
      await page.waitForFunction(() => window.trafficView.state().total >= 85);
      const box = page.locator("#rows-viewport"),
        follow = page.locator("#traffic-follow-latest");
      const position = () =>
        box.evaluate((e) => ({
          top: e.scrollTop,
          gap: e.scrollHeight - e.clientHeight - e.scrollTop,
        }));
      const atLatest = () =>
        wait(async () => Math.abs((await position()).gap) < 2);
      await atLatest();
      assert(await follow.isChecked());
      await send("/follow-latest/live");
      await page.waitForFunction(() =>
        [...document.querySelectorAll(".traffic-row")].some((e) =>
          e.title.endsWith("/live"),
        ),
      );
      await atLatest();
      const selected = await page
        .locator(".traffic-row")
        .last()
        .getAttribute("data-id");
      await page.locator('.traffic-row[data-id="' + selected + '"]').click();
      await page.locator("#details").waitFor();
      await atLatest();
      await send("/follow-latest/selected");
      await atLatest();
      assert.equal(
        await page
          .locator('.traffic-row[data-id="' + selected + '"]')
          .getAttribute("aria-selected"),
        "true",
      );
      await box.hover();
      await page.mouse.wheel(0, -400);
      await wait(async () => !(await follow.isChecked()));
      const paused = (await position()).top;
      await send("/follow-latest/paused");
      await page.waitForFunction(() => window.trafficView.state().total >= 88);
      assert(Math.abs((await position()).top - paused) < 2);
      await follow.check();
      await atLatest();
      await follow.uncheck();
      await send("/follow-latest/manual-pause");
      await page.waitForFunction(() => window.trafficView.state().total >= 89);
      assert(!(await follow.isChecked()));
      assert((await position()).gap > 8);
      await box.evaluate((e) => {
        e.scrollTop = e.scrollHeight;
      });
      await wait(async () => await follow.isChecked());
      await send("/follow-latest/resumed");
      await atLatest();
      await page.setViewportSize({ width: 860, height: 650 });
      await atLatest();
      await page.screenshot({
        path: path.join(root, "traffic-follow-latest.png"),
        fullPage: true,
      });
      await page.locator("#search").fill("does-not-exist");
      await page.locator("#list-empty").waitFor();
      await page.locator("#search").fill("");
      await atLatest();
      await page.locator("#clear").click();
      await page.waitForFunction(() => window.trafficView.state().total === 0);
      await send("/follow-latest/after-clear");
      await page.waitForFunction(() => window.trafficView.state().total === 1);
      await atLatest();
      const sources = Object.fromEntries(
        [
          "ui/src/state/follow-latest.ts",
          "ui/src/components/TrafficPage.vue",
          __filename,
        ].map((file) => [
          file,
          crypto
            .createHash("sha256")
            .update(fs.readFileSync(file))
            .digest("hex"),
        ]),
      );
      return {
        passed: true,
        checks: [
          "live HTTP requests follow the newest virtual row without changing the selected request",
          "manual scrolling and explicit pause retain history position; returning to bottom or rechecking resumes follow",
          "resize, detail pane, empty filters and clear preserve usable follow behavior",
        ],
        sources,
      };
    },
  })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
