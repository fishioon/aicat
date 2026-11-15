const assert = require("node:assert/strict");
const path = require("node:path");
const fs = require("node:fs");
const { execFileSync } = require("node:child_process");

async function verify({ page, root, current, wait }) {
  const clipboard = execFileSync("/usr/bin/pbpaste");
  const copiedAddress = "http://" + (await current()).gateway.addr;
  for (const file of ["ui/src/state/desktop.ts", "ui/src/state/traffic.ts", "ui/src/app.css", "ui/src/components/SettingsPage.vue", "ui/src/components/Overlays.vue", "ui/src/components/Seg.vue"]) {
    const target = path.join(root, "sources", file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(file, target);
  }
  let release;
  let calls = 0;
  let holding = true;
  let fail = false;
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en" && !document.body.classList.contains("busy"));
  await page.route("**/rpc", async (route) => {
    const body = route.request().postDataJSON();
    if (body.method !== "Desktop.SetPreferences") return route.continue();
    calls++;
    if (holding) await new Promise((resolve) => { release = resolve; });
    if (fail) return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Preference save interrupted" }) });
    return route.continue();
  });
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-theme-dark").click();
    await wait(() => !!release);
    await page.waitForFunction(() => document.body.classList.contains("busy"));
    await page.locator("#gateway-address").click();
    await wait(() => execFileSync("/usr/bin/pbpaste", { encoding: "utf8" }) === copiedAddress);
    await page.locator("#notice").filter({ hasText: /^Copied$/ }).waitFor();
    assert.equal(await page.locator("#theme").isDisabled(), true);
    await page.screenshot({ path: path.join(root, "pending.png") });
    await page.locator("#nav-extensions").click({ timeout: 1500 });
    assert.equal(await page.locator("#extension-search").isEnabled(), true);
    await page.locator("#extension-search").fill("traffic");
    assert.equal(await page.locator("#feature-traffic").isDisabled(), true);
    await page.locator("#nav-settings").click();
    assert.equal(await page.locator("#preference-language-en").isDisabled(), true);
    await page.locator("#preference-theme-dark").press("ArrowLeft");
    assert.equal(calls, 1);
    assert.equal(await page.locator("#theme").isDisabled(), true);
    await page.locator("#settings-proxy").click();
    assert.equal(await page.locator("#system-proxy-toggle").isDisabled(), true);
    assert.equal(await page.locator("#certificate-install").isDisabled(), true);
    await page.locator("#nav-traffic").click();
    assert.equal(await page.locator("#recording").isDisabled(), true);
    await page.locator("#search").fill("draft during pending save");
    await page.locator("#nav-overview").click();
    holding = false;
    release();
    await page.waitForFunction(() => !document.body.classList.contains("busy"));
    assert.equal((await current()).preferences.theme, "dark");
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-zh-Hans").click();
    await wait(async () => (await current()).preferences.language === "zh-Hans");
    await page.waitForFunction(() => !document.body.classList.contains("busy"));
    assert.equal(calls, 2);
    holding = true;
    fail = true;
    release = undefined;
    await page.locator("#preference-theme-light").click();
    await wait(() => !!release);
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-search").fill("");
    release();
    await page.waitForFunction(() => !document.body.classList.contains("busy"));
    await page.locator("#notice", { hasText: "Preference save interrupted" }).waitFor();
    assert.equal((await current()).preferences.theme, "dark");
    assert.equal(await page.locator("#extension-search").isVisible(), true);
    assert.equal(await page.locator("#feature-traffic").isEnabled(), true);
    assert.equal(calls, 3);
    await page.screenshot({ path: path.join(root, "completed.png") });
    return { passed: true, checks: ["后台真实设置操作等待期间导航、搜索、真实地址复制可用，冲突操作以原生 disabled 拒绝鼠标与键盘", "延迟完成保留当前页面，操作解锁后真实语言设置生效；保存失败不改变设置且恢复操作"], nativeCoverage: "not run" };
  } finally {
    release?.();
    if (execFileSync("/usr/bin/pbpaste", { encoding: "utf8" }) === copiedAddress)
      execFileSync("/usr/bin/pbcopy", { input: clipboard });
  }
}
require("./traffic-default.cjs").run({ verify, scenario: "busy-state" }).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
