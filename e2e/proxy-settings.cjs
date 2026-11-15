const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const assert = require("node:assert/strict");

async function verify({ page, root, home, current, wait, stop, launch }) {
  const checks = [];
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#settings-proxy").click();
  const input = page.locator("#decryption-domain-input");
  const add = page.locator("#add-decryption-domain");
  const calls = [];
  let mode = "hold";
  let held;
  await page.route("**/rpc", async (route) => {
    const request = route.request().postDataJSON();
    if (request.method !== "Desktop.SetDecryptionDomain" || !request.args[1])
      return route.continue();
    const call = { domain: request.args[0], mode, finished: false };
    calls.push(call);
    if (mode.startsWith("hold")) {
      const response = mode === "hold" ? await route.fetch() : null;
      await new Promise((resolve) => {
        held = resolve;
      });
      if (response) await route.fulfill({ response });
      else
        await route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "Domain connection interrupted" }),
        });
    } else await route.continue();
    call.finished = true;
  });
  const shot = (name) =>
    page.screenshot({ path: path.join(root, name + ".png") });
  await input.fill("First.Example.com");
  await add.click();
  await wait(() => held);
  await input.fill("next.example.com");
  held();
  await page
    .locator("#notice")
    .filter({ hasText: "已添加 first.example.com" })
    .waitFor();
  assert.equal(
    await input.inputValue(),
    "next.example.com",
    "late completion must preserve a newer domain draft",
  );
  assert((await current()).domains.includes("first.example.com"));
  checks.push("添加成功规范化已提交域名，不清空等待期间的新输入");

  mode = "hold-fail";
  held = undefined;
  await add.click();
  await wait(() => held);
  assert(await add.isDisabled());
  const count = calls.length;
  await input.press("Enter");
  await input.press("Enter");
  assert.equal(calls.length, count, "repeated Enter does not resubmit");
  held();
  await page
    .locator("#decryption-domain-error", {
      hasText: "Domain connection interrupted",
    })
    .waitFor();
  assert.equal(await input.inputValue(), "next.example.com");
  assert(!(await current()).domains.includes("next.example.com"));
  await shot("domain-failure-retains-draft");
  mode = "pass";
  await add.click();
  await page.locator('[id="remove-decryption-next.example.com"]').waitFor();
  await wait(async () => (await input.inputValue()) === "");
  checks.push("添加中禁止重复提交，失败保留输入，直接重试可成功");

  const beforeDuplicate = calls.length;
  await input.fill(" NEXT.EXAMPLE.COM ");
  await add.click();
  await page.locator("#decryption-domain-error").waitFor();
  assert.equal(calls.length, beforeDuplicate);
  mode = "hold-fail";
  held = undefined;
  await input.fill("failed.example.com");
  await add.click();
  await wait(() => held);
  await input.fill("new-draft.example.com");
  held();
  await page
    .locator("#notice.bad")
    .filter({ hasText: "Domain connection interrupted" })
    .waitFor();
  assert.equal(await input.inputValue(), "new-draft.example.com");
  assert.equal(
    await page.locator("#decryption-domain-error").count(),
    0,
    "a previous request must not mark the new draft invalid",
  );
  checks.push("重复域名就地提示且不发请求，旧请求失败不污染新输入的校验状态");

  mode = "pass";
  await input.fill("api.*.com");
  await add.click();
  await page.locator("#decryption-domain-error").waitFor();
  assert(!(await current()).domains.includes("api.*.com"));
  await input.fill("*.example.com");
  await add.click();
  await page.locator('[id="remove-decryption-*.example.com"]').waitFor();
  await wait(async () => (await input.inputValue()) === "");
  await page.locator('[id="remove-decryption-first.example.com"]').click();
  await wait(
    async () => !(await current()).domains.includes("first.example.com"),
  );
  assert(
    fs
      .readFileSync(path.join(home, "aicat.yaml"), "utf8")
      .includes("*.example.com"),
  );
  checks.push("非法通配写法不保存，通配域名添加与精确域名移除写入真实配置");

  const configPath = path.join(home, "aicat.yaml");
  fs.renameSync(configPath, configPath + ".saved");
  fs.mkdirSync(configPath);
  try {
    await input.fill("disk-failure.example.com");
    await add.click();
    await page.locator("#decryption-domain-error").waitFor();
    assert.equal(await input.inputValue(), "disk-failure.example.com");
    assert(!(await current()).domains.includes("disk-failure.example.com"));
  } finally {
    fs.rmdirSync(configPath);
    fs.renameSync(configPath + ".saved", configPath);
  }
  await add.click();
  await page
    .locator('[id="remove-decryption-disk-failure.example.com"]')
    .waitFor();
  await wait(async () => (await input.inputValue()) === "");
  checks.push(
    "真实配置读取/写入受阻时不改运行名单且保留输入，恢复文件后可直接重试",
  );

  const port = page.locator("#listener-port");
  const apply = page.locator("#listener-apply");
  assert(
    await apply.isDisabled(),
    "unchanged listener does not offer a meaningless apply",
  );
  for (const invalid of ["0", "65536", "abc"]) {
    await port.fill(invalid);
    assert(await apply.isDisabled());
    await page.locator("#listener-error").waitFor();
  }
  const occupied = net.createServer();
  await new Promise((resolve) => occupied.listen(0, "127.0.0.1", resolve));
  const original = (await current()).gateway.addr;
  try {
    await port.fill(String(occupied.address().port));
    await apply.click();
    await page.waitForFunction(() => !document.body.classList.contains("busy"));
    await page.locator("#notice.bad").waitFor();
    assert.equal((await current()).gateway.addr, original);
    assert.equal((await fetch(`http://${original}/api/traffic`)).status, 200);
    assert(
      !fs
        .readFileSync(path.join(home, "aicat.yaml"), "utf8")
        .includes(`addr: 127.0.0.1:${occupied.address().port}`),
    );
  } finally {
    await new Promise((resolve) => occupied.close(resolve));
  }
  const reserve = net.createServer();
  await new Promise((resolve) => reserve.listen(0, "127.0.0.1", resolve));
  const nextPort = reserve.address().port;
  await new Promise((resolve) => reserve.close(resolve));
  const fingerprint = (await current()).certificate.fingerprint;
  await page.locator("#listener-address").selectOption("0.0.0.0");
  await port.fill(String(nextPort));
  await apply.click();
  await wait(
    async () => (await current()).gateway.addr === `0.0.0.0:${nextPort}`,
  );
  assert.equal(
    (await fetch(`http://127.0.0.1:${nextPort}/api/traffic`)).status,
    200,
  );
  assert.equal((await current()).certificate.fingerprint, fingerprint);
  await stop();
  await launch({ configuredListener: true });
  assert.equal((await current()).gateway.addr, `0.0.0.0:${nextPort}`);
  assert.equal((await current()).certificate.fingerprint, fingerprint);
  assert((await current()).domains.includes("*.example.com"));
  await page.locator("#nav-settings").click();
  await page.locator("#settings-proxy").click();
  await shot("proxy-settings-restored");
  checks.push(
    "端口校验、占用失败回退、通配监听切换与重启持久化有效，保留证书与域名配置",
  );
  fs.copyFileSync(
    "ui/src/components/SettingsPage.vue",
    path.join(root, "SettingsPage.vue"),
  );
  fs.writeFileSync(
    path.join(root, "domain-rpc-calls.json"),
    JSON.stringify(calls, null, 2),
  );
  return {
    passed: true,
    checks,
    faultInjection:
      "Only decryption-domain preview transport is delayed or failed; configuration, listener and occupied port are real",
  };
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "proxy-settings" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
