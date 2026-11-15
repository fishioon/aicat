const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");

async function verify({ page, root, home, stop, launch, wait }) {
  const checks = [];
  const sourceFiles = [
    "ui/src/components/OverviewPage.vue",
    "ui/src/components/SettingsPage.vue",
    "ui/src/state/usage.ts",
  ];
  for (const file of sourceFiles) {
    const target = path.join(root, "sources", file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(file, target);
  }
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  let mode = "fail";
  let held;
  const calls = [];
  await page.route("**/rpc", async (route) => {
    const body = route.request().postDataJSON();
    if (body.method !== "Desktop.Usage") return route.continue();
    const call = { period: body.args[0], mode, finished: false };
    calls.push(call);
    if (mode === "hold") {
      const response = await route.fetch();
      await new Promise((resolve) => {
        held = { release: resolve, call };
      });
      await route.fulfill({ response });
    } else if (mode === "fail") {
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "Usage connection interrupted" }),
      });
    } else {
      await route.continue();
    }
    call.finished = true;
  });
  const shot = (name) =>
    page.screenshot({ path: path.join(root, name + ".png") });
  await page.reload();
  await page.locator("#usage-error").waitFor();
  assert.equal(await page.locator(".ov-empty").count(), 0);
  await page.locator("#usage-error summary").click();
  assert.match(
    await page.locator("#usage-error").innerText(),
    /Usage connection interrupted/,
  );
  await page.locator("#usage-error summary").click();
  await shot("initial-error");
  checks.push("首次读取失败显示错误与重试，不冒充没有调用");

  mode = "hold";
  await page.locator("#usage-retry").click();
  await wait(() => held);
  await page.locator("#usage-loading").waitFor();
  assert.equal(await page.locator(".ov-empty strong").count(), 0);
  await shot("initial-loading");
  mode = "pass";
  held.release();
  await page.locator(".ov-empty strong").waitFor();
  assert.equal(await page.locator("#usage-error").count(), 0);
  checks.push("重试期间显示加载，真实服务返回空数据后才显示接入提示");

  await stop();
  const ledger =
    [0, 1, 15]
      .map((days, index) => {
        const time = new Date();
        time.setDate(time.getDate() - days);
        return JSON.stringify({
          t: time.toISOString(),
          provider: "review-provider",
          agent: "codex",
          model: "review-model",
          api: "responses",
          status: 200,
          in: 100 * (index + 1),
          ms: 100,
        });
      })
      .join("\n") + "\n";
  fs.writeFileSync(path.join(home, "usage.jsonl"), ledger, { mode: 0o600 });
  await launch();
  await wait(
    async () => (await page.locator("#usage-requests").innerText()) === "1",
  );
  mode = "hold";
  held = undefined;
  await page.locator("#usage-period-7d").click();
  await wait(() => held);
  await page.locator("#usage-loading").waitFor();
  assert.equal(await page.locator("#usage-requests").count(), 0);
  assert.equal(await page.locator(".ov-empty strong").count(), 0);
  await shot("period-loading");
  mode = "pass";
  await page.locator("#usage-period-30d").click();
  await wait(
    async () => (await page.locator("#usage-requests").innerText()) === "3",
  );
  held.release();
  await wait(() => held.call.finished);
  await page.waitForTimeout(200);
  assert.equal(await page.locator("#usage-requests").innerText(), "3");
  checks.push("切换范围隐藏旧数字，迟到的七天结果不会覆盖三十天结果");

  mode = "fail";
  await page.locator("#usage-error").waitFor();
  assert.equal(await page.locator("#usage-requests").innerText(), "3");
  assert.match(await page.locator("#usage-error").innerText(), /上次成功读取/);
  await shot("refresh-error-retains-data");
  mode = "pass";
  await page.locator("#usage-retry").click();
  await page.locator("#usage-error").waitFor({ state: "detached" });
  assert.equal(await page.locator("#usage-requests").innerText(), "3");
  checks.push("刷新失败保留同范围数据并说明，重试成功清除错误");

  await page.locator("#nav-settings").click();
  assert.equal(await page.getByText("私云未接入", { exact: true }).count(), 0);
  assert.equal(
    await page
      .getByText("选择后立即生效，重启后保留。", { exact: true })
      .count(),
    0,
  );
  await page.locator("#preference-theme-dark").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await shot("settings-dark");
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#nav-overview").click();
  mode = "fail";
  await page.locator("#usage-error").waitFor();
  assert.match(
    await page.locator("#usage-error").innerText(),
    /Unable to load usage/,
  );
  await shot("refresh-error-en-dark");
  mode = "pass";
  await page.locator("#usage-retry").click();
  await page.locator("#usage-error").waitFor({ state: "detached" });
  await stop();
  await launch();
  await page.waitForFunction(
    () =>
      document.documentElement.lang === "en" &&
      document.documentElement.dataset.theme === "dark",
  );
  checks.push(
    "通用设置去掉占位提示，主题和语言立即生效并跨重启保留；错误状态支持英文与深色",
  );
  fs.writeFileSync(
    path.join(root, "usage-rpc-calls.json"),
    JSON.stringify(calls, null, 2),
  );
  return {
    passed: true,
    checks,
    faultInjection:
      "Only Usage preview transport fails or delays; successful reports come from real Desktop.Usage and isolated ledger",
  };
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "overview" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
