const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const { execFile, execFileSync } = require("node:child_process");
const exec = require("node:util").promisify(execFile);

async function verify({
  page,
  root,
  home,
  proxyPort,
  api,
  wait,
  stop,
  launch,
  fixture,
}) {
  await stop();
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    "extensions:\n  traffic:\n    enabled: true\nui:\n  language: zh-Hans\n  theme: light\ntraffic:\n  recording: true\n  maxRecords: 12\n  maxBodyBytes: 256\n  maxTotalBodyBytes: 512\n",
  );
  await launch();
  await page.locator("#nav-traffic").click();
  const requests = [];
  const payload = "line1\n'quote' $(touch CURL_UNEXPECTED) 中文";
  const upstream = http.createServer((request, response) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      const data = Buffer.concat(chunks);
      requests.push({
        url: request.url,
        method: request.method,
        headers: request.rawHeaders,
        body: data.toString(),
      });
      const values = {
        "/json": ["application/json", '{"answer":42,"ok":true}'],
        "/binary": ["application/octet-stream", Buffer.from([0, 1, 255, 65])],
        "/html": ["text/html", "<script>window.bodyExecuted=true</script>"],
        "/binary-large": ["application/octet-stream", Buffer.alloc(1024, 0)],
        "/large": ["text/plain", "x".repeat(1024)],
      };
      const [type, body] = values[request.url] || [
        "text/plain",
        data.length ? data : "small",
      ];
      response.writeHead(200, {
        "Content-Type": type,
        "Set-Cookie": ["one=1", "two=2"],
      });
      response.end(body);
    });
  });
  await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${upstream.address().port}`;
  const send = (url, data = "") =>
    new Promise((resolve, reject) => {
      const request = http.request(
        {
          host: "127.0.0.1",
          port: proxyPort,
          path: origin + url,
          method: data ? "POST" : "GET",
          headers: [
            "Host",
            new URL(origin).host,
            "X-Multi",
            "first",
            "X-Multi",
            "second",
            "Content-Type",
            "text/plain",
          ],
        },
        (response) => {
          const chunks = [];
          response.on("data", (chunk) => chunks.push(chunk));
          response.on("end", () => resolve(Buffer.concat(chunks)));
          response.on("error", reject);
        },
      );
      request.on("error", reject);
      request.end(data);
    });
  const panel = page.locator('[data-side="response"]');
  const clipboard = () =>
    execFileSync("/usr/bin/pbpaste", { encoding: "utf8" });
  const settled = () =>
    page.waitForFunction(() => !document.body.classList.contains("busy"));
  const clear = async () => {
    const response = page.waitForResponse(
      (response) =>
        response.url().endsWith("/rpc") &&
        response.request().postDataJSON()?.method === "Desktop.ClearTraffic",
    );
    await page.locator("#clear").click();
    await (await response).finished();
    await page.evaluate(() => new Promise(requestAnimationFrame));
    await wait(async () => !(await api()).length);
  };
  const select = async (url) => {
    await wait(async () =>
      (await api()).some(
        (row) => row.url === origin + url && row.state === "completed",
      ),
    );
    const record = (await api()).find((row) => row.url === origin + url);
    await page.locator(`.traffic-row[data-id="${record.id}"]`).click();
    await panel.locator('[data-view="body"]').click();
    await panel.locator("pre.body").waitFor({ state: "attached" });
    return record;
  };
  try {
    assert.equal((await send("/echo", payload)).toString(), payload);
    const echo = await select("/echo");
    assert.equal(await panel.locator("pre.body").textContent(), payload);
    await panel.locator('[data-view="headers"]').click();
    assert.equal(
      await panel.locator("tr", { hasText: "Set-Cookie" }).count(),
      2,
    );
    await page
      .locator(`.traffic-row[data-id="${echo.id}"]`)
      .click({ button: "right" });
    await page
      .locator("#popup")
      .getByRole("menuitem", { name: "复制 URL", exact: true })
      .click();
    await wait(() => clipboard() === origin + "/echo");
    await page
      .locator(`.traffic-row[data-id="${echo.id}"]`)
      .click({ button: "right" });
    await page
      .locator("#popup")
      .getByRole("menuitem", { name: "复制 cURL", exact: true })
      .click();
    await wait(() => clipboard().startsWith("curl "));
    const command = clipboard();
    const result = await exec("/bin/sh", ["-c", command], {
      cwd: root,
      timeout: 10000,
      encoding: "utf8",
      env: { PATH: process.env.PATH, NO_PROXY: "*", no_proxy: "*" },
    });
    assert.equal(result.stdout, payload);
    assert.equal(requests.at(-1).method, "POST");
    assert.equal(requests.at(-1).body, payload);
    assert.equal(
      requests
        .at(-1)
        .headers.filter((value) => value.toLowerCase() === "x-multi").length,
      2,
    );
    assert(!fs.existsSync(path.join(root, "CURL_UNEXPECTED")));
    await page.locator("#search").fill("/echo");
    await page.locator("#export").click();
    const target = path.join(root, "aicat-traffic.json");
    await wait(() => fs.existsSync(target));
    const exported = JSON.parse(fs.readFileSync(target));
    assert.equal(exported.length, 1);
    assert.equal(exported[0].id, echo.id);
    assert.equal(exported[0].url, origin + "/echo");
    assert(!("requestReceived" in exported[0]));
    await settled();
    const previous = fs.readFileSync(target, "utf8");
    fs.renameSync(target, target + ".verified");
    fixture({ cancelExport: true });
    await page.locator("#export").click();
    await settled();
    assert(
      !fs.existsSync(target),
      "canceling the save panel does not create an export",
    );
    assert.equal(fs.readFileSync(target + ".verified", "utf8"), previous);
    fixture({ cancelExport: false });
    fs.mkdirSync(target);
    await page.locator("#export").click();
    await settled();
    await wait(async () =>
      (await page.locator("#notice").innerText()).includes("directory"),
    );
    fs.rmdirSync(target);
    await page.locator("#export").click();
    await wait(() => fs.existsSync(target));
    await page.screenshot({ path: path.join(root, "export-retried.png") });
    await clear();
    for (const url of ["/json", "/binary", "/html"]) {
      await send(url);
      await select(url);
      if (url === "/json")
        assert.deepEqual(
          JSON.parse(await panel.locator("pre.body").textContent()),
          { answer: 42, ok: true },
        );
      if (url === "/binary") {
        assert.equal(
          await panel.locator("pre.body").textContent(),
          "00 01 ff 41",
        );
        assert(
          (await panel.locator(".body-notice").innerText()).includes(
            "十六进制",
          ),
        );
        await panel.locator('[data-side-copy="response"]').click();
        await wait(() => clipboard() === "00 01 ff 41");
      }
      if (url === "/html") {
        assert.equal(
          await panel.locator("pre.body").textContent(),
          "<script>window.bodyExecuted=true</script>",
        );
        assert.equal(await page.evaluate(() => window.bodyExecuted), undefined);
      }
    }
    await clear();
    assert.equal((await send("/large")).length, 1024);
    const large = await select("/large");
    assert.equal(
      await panel.locator("pre.body").textContent(),
      "x".repeat(256),
    );
    assert.equal(large.responseBytes, 1024);
    assert((await panel.locator(".body-notice").innerText()).includes("256"));
    await send("/large");
    await send("/quota");
    await select("/quota");
    assert.equal(await panel.locator("pre.body").textContent(), "");
    const notice = await panel.locator(".body-notice").innerText();
    assert(notice.includes("0") && notice.includes("5"));
    assert(
      !notice.includes("过大"),
      "a small response is not too large when the total quota is exhausted",
    );
    await page.screenshot({ path: path.join(root, "quota-exhausted.png") });
    await clear();
    await send("/binary-large");
    await select("/binary-large");
    const binaryNote = await panel.locator(".body-notice").innerText();
    assert(binaryNote.includes("十六进制") && binaryNote.includes("256"));
    await clear();
    await send("/large-request", "z".repeat(1024));
    const requestRecord = await select("/large-request");
    await page
      .locator(`.traffic-row[data-id="${requestRecord.id}"]`)
      .click({ button: "right" });
    assert(
      await page
        .locator("#popup")
        .getByRole("menuitem", { name: "复制 cURL", exact: true })
        .isDisabled(),
    );
    await page.keyboard.press("Escape");
    await clear();
    await send("/selected");
    const selected = await select("/selected");
    for (let i = 0; i < 13; i++) await send(`/evict-${i}`);
    await wait(
      async () => !(await api()).some((row) => row.id === selected.id),
    );
    await wait(async () => !(await page.locator("#details").count()));
    await clear();
    await page.locator("#search").fill("no-results");
    assert(
      await page.locator("#export").isDisabled(),
      "empty filtered export has no useful output",
    );
    assert(
      await page.locator("#clear").isEnabled(),
      "clear can still reset an active filter",
    );
    await clear();
    assert.equal(await page.locator("#search").inputValue(), "");
    assert(await page.locator("#clear").isDisabled());
    for (const source of [
      "ui/src/components/Inspector.vue",
      "ui/src/components/TrafficPage.vue",
      "internal/app/curl.go",
    ])
      fs.copyFileSync(source, path.join(root, path.basename(source)));
    return {
      passed: true,
      checks: [
        "文本/JSON/二进制/HTML 输出与双值响应头符合真实内容",
        "复制 URL 和 cURL 后实际向本地上游执行，方法、重复头、特殊字符正文保持一致",
        "筛选摘要导出落盘、取消、写入失败重试",
        "单正文与总容量限制不影响转发，二进制截断仍说明格式，非完整请求禁止 cURL",
        "选中记录淘汰后关闭详情；空结果操作状态正确",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    upstream.closeAllConnections();
    await new Promise((resolve) => upstream.close(resolve));
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "traffic-content" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
