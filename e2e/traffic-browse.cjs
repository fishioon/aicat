const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const assert = require("node:assert/strict");

async function verify(context) {
  const { page, root, wait, current, home, stop, launch } = context;
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#preference-theme-light").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "light",
  );
  await page.locator("#nav-traffic").click();
  if (!(await current()).recording) await page.locator("#recording").click();
  const selection = await require("./list-selection.cjs")(context);
  for (const group of ["pins", "domains", "apps"]) {
    const title = page.locator(`[data-group="${group}"] .group-title`);
    if ((await title.getAttribute("aria-expanded")) === "false")
      await title.click();
  }
  const tree = await require("./app-tree.cjs")({
    ...context,
    navigate: async () => page.locator("#nav-traffic").click(),
    settled: () =>
      page.waitForFunction(() => !document.body.classList.contains("busy")),
    request: (port, url) =>
      new Promise((resolve, reject) => {
        const request = http.get(
          { host: "127.0.0.1", port, path: url },
          (response) => {
            response.resume();
            response.on("end", () => resolve({ status: response.statusCode }));
          },
        );
        request.on("error", reject);
      }),
  });
  const pins = (await current()).pinnedDomains;
  assert(pins.length > 0);
  const config = fs.readFileSync(path.join(home, "aicat.yaml"), "utf8");
  assert(pins.every((pin) => config.includes(pin)));
  await stop();
  await launch();
  assert.deepEqual((await current()).pinnedDomains, pins);
  await page.locator("#nav-traffic").click();
  await wait(
    async () =>
      (await page.locator('[data-group="pins"] .tree-item').count()) ===
      pins.length,
  );
  for (const source of [
    "e2e/list-selection.cjs",
    "e2e/app-tree.cjs",
    "ui/src/state/traffic.ts",
    "ui/src/state/sidebar.ts",
    "ui/src/components/TreeRow.vue",
  ]) {
    const target = path.join(root, "sources", source);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(source, target);
  }
  return {
    passed: true,
    selection,
    tree,
    checks: [
      "滚动、首次单击、右键、键盘与历史选择通过复用场景验证",
      "真实两个进程的应用/域名/路径筛选、搜索叠加、双击与折叠通过",
      "固定域名在清空和重启后仍保留",
    ],
    nativeCoverage: "not run",
  };
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "traffic-browse" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
