const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const assert = require("node:assert/strict");

async function verify({ page, root, wait, send, api }) {
  const checks = [];
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#nav-traffic").click();
  await page.locator("#recording").click();
  await send("/copy-first?text=one");
  await send("/copy-second?text=two");
  await wait(
    async () =>
      (await api()).filter((row) => row.state === "completed").length >= 2,
  );
  const row = (name) => page.locator(".traffic-row", { hasText: name });
  const panel = page.locator('[data-side="response"]');
  const copy = panel.locator('[data-side-copy="response"]');
  const readClipboard = () =>
    execFileSync("/usr/bin/pbpaste", { encoding: "utf8" });
  const shot = (name) =>
    page.screenshot({ path: path.join(root, name + ".png") });
  let mode = "hold";
  let pending;
  await page.route("**/rpc", async (route) => {
    const request = route.request().postDataJSON();
    if (request.method !== "Desktop.Body") return route.continue();
    if (mode === "hold") {
      const response = await route.fetch();
      await new Promise((resolve) => {
        pending = resolve;
      });
      await route.fulfill({ response });
    } else if (mode === "fail") {
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "Body connection interrupted" }),
      });
    } else await route.continue();
  });
  await row("/copy-first").click();
  await panel.locator('[data-view="body"]').click();
  await wait(() => pending);
  if (await copy.isEnabled()) {
    execFileSync("/usr/bin/pbcopy", { input: "copy-race-sentinel" });
    await copy.click();
    mode = "pass";
    await row("/copy-second").click();
    await panel.locator("pre.body").waitFor();
    pending();
    await wait(() => readClipboard() !== "copy-race-sentinel");
    assert.equal(
      readClipboard(),
      JSON.stringify({ path: "/copy-first?text=one", ok: true }),
      "a queued copy must never use another record's content",
    );
    assert.fail("copy must be unavailable while the visible body is loading");
  }
  assert(
    await copy.isDisabled(),
    "copy must be unavailable until the displayed body is ready",
  );
  await shot("body-loading");
  mode = "pass";
  await row("/copy-second").click();
  await panel.locator("pre.body").waitFor();
  const second = JSON.stringify({ path: "/copy-second?text=two", ok: true });
  await copy.click();
  await wait(() => readClipboard() === second);
  pending();
  await page.waitForTimeout(200);
  assert.equal(
    JSON.parse(await panel.locator("pre.body").innerText()).path,
    "/copy-second?text=two",
  );
  assert.equal(readClipboard(), second);
  checks.push("读取期间复制禁用，切换记录后迟到的正文不会替换当前视图或剪贴板");

  await row("/copy-first").click();
  await panel.locator("pre.body").waitFor();
  await panel.locator('[data-view="raw"]').click();
  await panel.locator(".raw").waitFor();
  const raw = await panel.locator(".raw").getAttribute("data-text");
  await copy.click();
  await wait(() => readClipboard() === raw);
  assert(raw.startsWith("HTTP/1.1 200"));
  assert(raw.includes('"path":"/copy-first?text=one"'));
  await panel.locator('[data-view="headers"]').click();
  assert.equal(
    await panel.locator(".copied").count(),
    0,
    "copy feedback belongs only to the copied view",
  );
  await copy.click();
  await wait(() => readClipboard().includes("Content-Type: application/json"));
  assert(!readClipboard().includes('"path"'));
  checks.push(
    "Raw 复制包含当前响应而无行号，Header 只复制 headers，成功反馈不串到其他视图",
  );

  mode = "fail";
  await row("/copy-second").click();
  await panel.locator('[data-view="body"]').click();
  await panel
    .locator(".body-notice", { hasText: "Body connection interrupted" })
    .waitFor();
  const previous = readClipboard();
  assert(
    await copy.isDisabled(),
    "failed body read cannot overwrite clipboard with empty content",
  );
  await shot("body-read-error");
  mode = "pass";
  await panel.getByRole("button", { name: "重试", exact: true }).click();
  await panel.locator("pre.body").waitFor();
  assert.equal(await panel.locator(".body-notice").count(), 0);
  assert.equal(
    readClipboard(),
    previous,
    "retry does not copy without a user click",
  );
  await copy.click();
  await wait(() => readClipboard() === second);
  await shot("body-read-recovered");
  checks.push(
    "正文读取失败明确报错且禁止空复制，重试真实服务成功后恢复正文与复制",
  );

  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#preference-theme-dark").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.locator("#nav-traffic").click();
  mode = "fail";
  await row("/copy-first").click();
  await panel
    .locator(".body-notice", { hasText: "Body connection interrupted" })
    .waitFor();
  await panel.getByRole("button", { name: "Retry", exact: true }).waitFor();
  await shot("body-error-en-dark");
  mode = "pass";
  await panel.getByRole("button", { name: "Retry", exact: true }).click();
  await panel.locator("pre.body").waitFor();
  const requestPanel = page.locator('[data-side="request"]');
  await requestPanel.locator('[data-view="query"]').click();
  await requestPanel.locator('[data-side-copy="request"]').click();
  await wait(() => readClipboard() === "text=one");
  checks.push("英文深色下错误与重试可用，请求 Query 复制保持正确");
  fs.copyFileSync(
    "ui/src/components/Inspector.vue",
    path.join(root, "Inspector.vue"),
  );
  return {
    passed: true,
    checks,
    faultInjection:
      "Only Body preview transport fails or delays; requests, capture, body reads and clipboard are real",
  };
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "inspector-copy" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
