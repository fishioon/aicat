const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");

async function verify({ page, root, current, wait, proxyPort }) {
  let models = ["alpha", "beta"];
  let modelStatus = 200;
  let failNetwork = false;
  let mode = "pass";
  let held;
  let holdDelete = false;
  let deleted;
  let selections = 0;
  let inference = [];
  const upstream = http.createServer((request, response) => {
    if (request.url.endsWith("/models")) {
      if (failNetwork) return request.socket.destroy();
      response.writeHead(modelStatus, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify(
          modelStatus === 200
            ? { data: models.map((id) => ({ id })) }
            : { error: { message: "models unavailable" } },
        ),
      );
      return;
    }
    if (request.url.endsWith("/chat/completions")) {
      let body = "";
      request.on("data", (chunk) => {
        body += chunk;
      });
      request.on("end", () => {
        inference.push({
          url: request.url,
          body: JSON.parse(body),
          auth: request.headers.authorization,
        });
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ model: JSON.parse(body).model }));
      });
      return;
    }
    response.writeHead(404);
    response.end();
  });
  await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${upstream.address().port}`;
  const gateway = (pathname, body) =>
    new Promise((resolve, reject) => {
      const request = http.request(
        {
          host: "127.0.0.1",
          port: proxyPort,
          path: pathname,
          method: body ? "POST" : "GET",
          headers: {
            "Content-Type": "application/json",
            Authorization: "Bearer agent-key",
          },
        },
        (response) => {
          let data = "";
          response.on("data", (chunk) => {
            data += chunk;
          });
          response.on("end", () =>
            resolve({ status: response.statusCode, data: JSON.parse(data) }),
          );
        },
      );
      request.on("error", reject);
      request.end(body ? JSON.stringify(body) : undefined);
    });
  const settled = () =>
    page.waitForFunction(() => !document.body.classList.contains("busy"));
  const tick = (id) =>
    page.locator(`#provider-models input[type="checkbox"][value="${id}"]`);
  await page.route("**/rpc", async (route) => {
    const request = route.request().postDataJSON();
    if (request.method === "Desktop.DeleteProvider" && holdDelete) {
      const response = await route.fetch();
      await new Promise((resolve) => {
        deleted = resolve;
      });
      return route.fulfill({ response });
    }
    if (request.method !== "Desktop.SetProviderModels") return route.continue();
    selections++;
    if (mode === "fail")
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "Model selection interrupted" }),
      });
    if (mode === "hold")
      await new Promise((resolve) => {
        held = resolve;
      });
    return route.continue();
  });
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-zh-Hans").click();
    await page.waitForFunction(
      () => document.documentElement.lang === "zh-Hans",
    );
    await page.locator("#nav-overview").click();
    await page.locator("#connect-provider").click();
    await page.locator("#provider-name").fill("Model provider");
    await page.locator("#provider-url").fill(origin + "/a/v1");
    await page.locator("#provider-key").fill("fixture-model-key");
    await page.locator("#provider-save").click();
    await page.locator("#provider-models").waitFor();
    const id = (await current()).providers.find(
      (provider) => provider.name === "Model provider",
    ).id;
    const saved = async () =>
      (await current()).providers.find((provider) => provider.id === id);
    if (process.argv[4] !== "delete-only") {
      mode = "fail";
      await tick("alpha").click();
      await page
        .locator("#notice", { hasText: "Model selection interrupted" })
        .waitFor();
      await settled();
      assert.deepEqual((await saved()).enabled, ["alpha", "beta"]);
      assert(
        await tick("alpha").isChecked(),
        "failed model selection must show the actual enabled models",
      );
      assert(
        await tick("alpha").evaluate(
          (input) => document.activeElement === input,
        ),
        "model selection restores keyboard focus after failure",
      );
      await page.screenshot({ path: path.join(root, "selection-failed.png") });
      mode = "pass";
      await tick("alpha").uncheck();
      await wait(async () => (await saved()).enabled.join() === "beta");
      await settled();
      assert.equal(
        (await gateway("/v1/chat/completions", { model: "alpha" })).status,
        400,
      );
      assert.equal(
        (await gateway("/v1/chat/completions", { model: id + "/alpha" }))
          .status,
        200,
      );
      assert.deepEqual(inference.at(-1), {
        url: "/a/v1/chat/completions",
        body: { model: "alpha" },
        auth: "Bearer fixture-model-key",
      });
      mode = "hold";
      await tick("alpha").check();
      await wait(() => held);
      for (const selector of [
        "#provider-models-all",
        "#provider-models-none",
        "#provider-save",
        "#provider-delete",
        "#provider-sync",
      ])
        assert(
          await page.locator(selector).isDisabled(),
          selector + " must be unavailable during model selection",
        );
      assert(await tick("beta").isDisabled());
      mode = "pass";
      held();
      held = undefined;
      await wait(async () => (await saved()).enabled.length === 2);
      await settled();
      assert(await tick("alpha").isChecked());
      await page.locator("#provider-models-filter").fill("alpha");
      assert(await page.locator("#provider-models-all").isDisabled());
      await page.locator("#provider-models-none").click();
      await wait(async () => (await saved()).enabled.join() === "beta");
      await settled();
      assert(await page.locator("#provider-models-none").isDisabled());
      await page.locator("#provider-models-all").click();
      await wait(async () => (await saved()).enabled.length === 2);
      await settled();
      await page.locator("#provider-models-filter").fill("missing model");
      assert(await page.locator("#provider-models-all").isDisabled());
      assert(await page.locator("#provider-models-none").isDisabled());
      await page.getByText("没有匹配的模型", { exact: true }).waitFor();
      const before = selections;
      await page.screenshot({
        path: path.join(root, "models-filter-empty.png"),
      });
      await page.locator("#provider-models-filter").fill("");
      assert.equal(selections, before);
      models = ["alpha", "beta", "gamma"];
      await page.locator("#provider-sync").click();
      await wait(async () => (await saved()).models.models.length === 3);
      assert.deepEqual((await saved()).enabled, ["alpha", "beta"]);
      assert(!(await tick("gamma").isChecked()));
      assert(
        !(await page.locator("#provider-status").innerText()).includes(
          "密钥有效",
        ),
        "a public model list cannot prove an inference key is valid",
      );
      modelStatus = 503;
      await page.locator("#provider-sync").click();
      await page.locator("#provider-status.warn").waitFor();
      assert.equal((await saved()).models.models.length, 3);
      modelStatus = 401;
      await page.locator("#provider-sync").click();
      await wait(async () => (await saved()).models.errorKind === "auth");
      await page
        .locator("#provider-status.bad", { hasText: "API Key" })
        .waitFor();
      assert.equal((await saved()).models.models.length, 3);
      modelStatus = 200;
      failNetwork = true;
      await page.locator("#provider-sync").click();
      await wait(async () => (await saved()).models.errorKind === "network");
      await page
        .locator("#provider-status.bad", { hasText: "连不上" })
        .waitFor();
      assert.equal((await saved()).models.models.length, 3);
      await page.screenshot({
        path: path.join(root, "models-network-failed.png"),
      });
      failNetwork = false;
      await page.locator("#provider-sync").click();
      await page.locator("#provider-status.ok").waitFor();
      await page.locator("#provider-key").fill("fixture-replacement-key");
      await page.locator("#provider-save").click();
      await wait(async () => (await saved()).keyHint.endsWith("-key"));
      await page.waitForFunction(
        () => !document.querySelector("#provider-name").disabled,
      );
      assert.deepEqual((await saved()).enabled, ["alpha", "beta"]);
      await page.locator("#provider-url").fill(origin + "/b/v1");
      await page.locator("#provider-save").click();
      await wait(async () => (await saved()).baseUrl === origin + "/b/v1");
      await page.waitForFunction(
        () => !document.querySelector("#provider-name").disabled,
      );
      assert.deepEqual((await saved()).enabled, ["alpha", "beta", "gamma"]);
      assert.equal((await saved()).picked, false);
      assert.deepEqual(
        (await gateway("/v1/models")).data.data.map((model) => model.id),
        models.map((model) => id + "/" + model),
      );
      assert.equal(
        (await gateway("/v1/chat/completions", { model: "gamma" })).status,
        200,
      );
      assert.equal(inference.at(-1).url, "/b/v1/chat/completions");
      assert.equal(inference.at(-1).auth, "Bearer fixture-replacement-key");
    }
    holdDelete = true;
    await page.locator("#provider-delete").click();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "确定", exact: true })
      .click();
    await wait(() => deleted);
    await page.locator("#history-back").click();
    await page.locator("#history-back").click();
    await page.locator("#settings-general").waitFor();
    deleted();
    deleted = undefined;
    await settled();
    assert.equal(
      await page.locator("#settings-general").count(),
      1,
      "late provider removal must not leave the page reached through history",
    );
    assert(!(await current()).providers.some((provider) => provider.id === id));
    assert.equal(
      (await gateway("/v1/chat/completions", { model: id + "/alpha" })).status,
      400,
    );
    await page.screenshot({
      path: path.join(root, "removed-history-preserved.png"),
    });
    fs.copyFileSync(
      "ui/src/components/ProviderPage.vue",
      path.join(root, "ProviderPage.vue"),
    );
    return {
      passed: true,
      checks: [
        ...(process.argv[4] === "delete-only"
          ? []
          : [
              "勾选失败恢复真实选择；保存期间禁用冲突操作；筛选全选/全不选只影响可见项，空结果操作不可用",
              "重新同步保留手选并默认不启用新模型；改密钥保留选择，改地址重置选择，与实际列表和推理路由一致",
              "模型同步的接口/鉴权/网络失败均保留列表且可恢复；可公开读取的列表不被描述为密钥有效",
            ]),
        "迟到移除只删除原供应商，不离开历史导航到达的设置；旧路由不可再调用",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    if (held) held();
    if (deleted) deleted();
    upstream.closeAllConnections();
    await new Promise((resolve) => upstream.close(resolve));
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "provider-model-state" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
