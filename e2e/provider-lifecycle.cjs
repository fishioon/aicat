const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const assert = require("node:assert/strict");

async function verify({ page, root, wait, current }) {
  const checks = [];
  let release;
  let entered = false;
  let status = 200;
  const upstream = http.createServer(async (request, response) => {
    if (request.url !== "/v1/models") {
      response.writeHead(404);
      response.end();
      return;
    }
    entered = true;
    await new Promise((resolve) => (release = resolve));
    response.writeHead(status, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify(
        status === 200
          ? { data: [{ id: "lifecycle-model" }] }
          : { error: { message: "fixture unavailable" } },
      ),
    );
  });
  await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  const address = `http://127.0.0.1:${upstream.address().port}/v1`;
  const fill = async (name) => {
    await page.locator("#provider-name").fill(name);
    await page.locator("#provider-url").fill(address);
    await page.locator("#provider-key").fill("fixture-lifecycle-key");
  };
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    await page.locator("#nav-overview").click();
    await page.locator("#connect-provider").click();
    assert.equal(
      await page.locator('label[for="provider-name"]').innerText(),
      "Name",
    );
    await fill("First provider");
    await page.locator("#provider-save").click();
    await wait(() => entered);
    await page.locator("#provider-back").click();
    await page.locator("#connect-provider").click();
    await fill("My next draft");
    release();
    await wait(async () =>
      (await current()).providers.some(
        (provider) => provider.name === "First provider",
      ),
    );
    await page.waitForTimeout(250);
    assert.equal(
      await page.locator("#provider-name").inputValue(),
      "My next draft",
      "late completion must not replace a newer form draft",
    );
    assert(
      (await page.locator("#provider-key").inputValue()) ===
        "fixture-lifecycle-key",
      "late completion preserves the next draft key",
    );
    assert.equal(
      await page.locator("#provider-models").count(),
      0,
      "new form stays a new provider",
    );
    await page.screenshot({
      path: path.join(root, "late-save-preserves-draft.png"),
    });
    checks.push(
      "保存期间离开并添加另一供应商，迟到成功不会更改新表单或清空其密钥",
    );

    status = 503;
    entered = false;
    await page.locator("#provider-save").click();
    await wait(() => entered);
    await page.locator("#provider-back").click();
    release();
    await page.locator("#notice.bad").waitFor();
    assert.equal((await current()).providers.length, 1);
    await page.screenshot({
      path: path.join(root, "late-save-failure-visible.png"),
    });
    checks.push("离开表单后保存失败仍有可见反馈，不持久化失败的供应商");

    await page.locator("#connect-provider").click();
    await fill("Retry provider");
    entered = false;
    await page.locator("#provider-save").click();
    await wait(() => entered);
    assert(await page.locator("#provider-name").isDisabled());
    assert(await page.locator("#provider-url").isDisabled());
    assert(await page.locator("#provider-key").isDisabled());
    release();
    await page.locator("#provider-status.bad").waitFor();
    assert(!(await page.locator("#provider-name").isDisabled()));
    assert.equal(
      await page.locator("#provider-name").inputValue(),
      "Retry provider",
    );
    assert.equal(await page.locator("#provider-url").inputValue(), address);
    assert(
      (await page.locator("#provider-key").inputValue()) ===
        "fixture-lifecycle-key",
      "failed save retains key for retry",
    );
    status = 200;
    entered = false;
    await page.locator("#provider-save").click();
    await wait(() => entered);
    release();
    await page.locator("#provider-status.ok").waitFor();
    assert.equal(await page.locator("#provider-key").inputValue(), "");
    assert.equal((await current()).providers.length, 2);
    await page.screenshot({ path: path.join(root, "retry-success.png") });
    checks.push(
      "保存时锁定输入防止结果与输入不符，失败恢复输入并保留草稿，重试成功显示模型并清除密钥",
    );
    fs.copyFileSync(
      "ui/src/components/ProviderPage.vue",
      path.join(root, "ProviderPage.vue"),
    );
    return {
      passed: true,
      checks,
      coverage:
        "Real local provider /models and real Desktop persistence, headless UI navigation; no artificial RPC success",
    };
  } finally {
    release?.();
    await new Promise((resolve) => upstream.close(resolve));
  }
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "provider-lifecycle" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
