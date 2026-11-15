const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

async function verify({ page, root, send, api, wait }) {
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(
    () =>
      document.documentElement.lang === "zh-Hans" &&
      !document.body.classList.contains("busy"),
  );
  await page.locator("#nav-extensions").click();
  await page.locator("#extension-codex").click();
  await page.locator("#feature-codex").click();
  await page.locator("#dialog[open]").waitFor();
  const breadcrumb = await page.locator("#breadcrumb").innerText();
  for (const key of ["Meta+,", "Meta+[", "Meta+]"]) {
    await page.keyboard.press(key);
    const actual = await page.locator("#breadcrumb").innerText();
    if (actual !== breadcrumb) {
      fs.writeFileSync(
        path.join(root, "modal-navigation-proof.json"),
        JSON.stringify({ key, before: breadcrumb, after: actual }, null, 2),
      );
      await page.screenshot({ path: path.join(root, "modal-navigation.png") });
    }
    assert.equal(
      actual,
      breadcrumb,
      "modal must keep navigation behind it unchanged",
    );
  }
  await page
    .getByRole("dialog", { name: "需要先安装证书", exact: true })
    .waitFor();
  await page.keyboard.press("Escape");
  await page.locator("#dialog[open]").waitFor({ state: "hidden" });
  assert.equal(
    await page.evaluate(() => document.activeElement?.id),
    "feature-codex",
  );
  await page.keyboard.press("Meta+,");
  await page.locator("#settings-general").waitFor();
  await page.keyboard.press("Meta+[");
  await page.locator("#feature-codex").waitFor();
  assert.equal(await page.locator("#nav-more, #more-pop").count(), 0);
  await page.locator("#nav-extensions").click();
  await page.locator("#extension-codex").click();
  await page.locator("#extension-menu").click();
  await page.locator("#popup").waitFor();
  await page.keyboard.press("Meta+,");
  await page.locator("#settings-general").waitFor();
  assert.equal(
    await page.locator("#popup").count(),
    0,
    "navigation must dismiss an old context menu",
  );
  await page.locator("#nav-traffic").click();
  await page.locator("#recording").click();
  await send("/shell-layout?view=detail");
  await wait(async () => (await api()).length === 1);
  await page.locator(".traffic-row").click();
  await page.locator("#details").waitFor();
  const sizes = {};
  for (const [id, dx, dy] of [
    ["list-split", 0, 100],
    ["detail-split", 130, 0],
  ]) {
    const splitter = page.locator("#" + id);
    const before = Number(await splitter.getAttribute("aria-valuenow"));
    const box = await splitter.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(
      box.x + box.width / 2 + dx,
      box.y + box.height / 2 + dy,
      { steps: 6 },
    );
    await page.mouse.up();
    const after = Number(await splitter.getAttribute("aria-valuenow"));
    assert(after > before, id + " drag must resize the selected panes");
    await splitter.focus();
    await page.keyboard.press(id === "list-split" ? "ArrowUp" : "ArrowLeft");
    assert.equal(
      Number(await splitter.getAttribute("aria-valuenow")),
      after - 1,
    );
    sizes[id] = { before, after };
  }
  await page.screenshot({ path: path.join(root, "layout-resized.png") });
  await page.keyboard.press("Meta+k");
  assert.equal(await page.evaluate(() => document.activeElement?.id), "search");
  await page.locator("#rows-viewport").focus();
  await page.keyboard.press("Escape");
  await page.locator("#details").waitFor({ state: "hidden" });
  assert.equal(
    await page.evaluate(() => document.activeElement?.id),
    "rows-viewport",
  );
  fs.writeFileSync(
    path.join(root, "layout-proof.json"),
    JSON.stringify(sizes, null, 2),
  );
  return {
    passed: true,
    checks: [
      "证书确认弹窗有可访问名称，阻止背景快捷导航；Escape 取消并返回原按钮",
      "弹出菜单随导航关闭，弹窗外设置/历史/搜索快捷键正常",
      "真实流量详情双分隔条支持鼠标拖动与键盘调整，Escape 关闭详情并保留列表焦点",
    ],
    nativeCoverage: "not run",
  };
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "desktop-shell" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
