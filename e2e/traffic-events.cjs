const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const { execFileSync } = require("node:child_process");

async function verify({ page, root, proxyPort, api, wait }) {
  let release;
  let copied = "";
  const initial = ": hello\n\nevent: only-name\n\nevent: update\ndata: one\n\n";
  const ending =
    "data:  two\r\ndata:three\r\n\r\nevent: empty\rdata:\r\revent: unfinished\ndata: tail";
  const upstream = http.createServer(async (request, response) => {
    if (request.url === "/consumer") {
      response.writeHead(200, { "Content-Type": "text/html" });
      return response.end(
        "<!doctype html><title>EventSource verification</title>",
      );
    }
    response.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Access-Control-Allow-Origin": "*",
    });
    if (request.url === "/copied") return response.end(copied);
    response.write(initial);
    await new Promise((resolve) => {
      release = resolve;
    });
    response.end(ending);
  });
  await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${upstream.address().port}`;
  let streaming;
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-zh-Hans").click();
    await page.waitForFunction(
      () => document.documentElement.lang === "zh-Hans",
    );
    await page.locator("#nav-traffic").click();
    await page.locator("#recording").click();
    streaming = new Promise((resolve, reject) => {
      const request = http.get(
        { host: "127.0.0.1", port: proxyPort, path: origin + "/events" },
        (response) => {
          let data = "";
          response.on("data", (chunk) => {
            data += chunk;
          });
          response.on("end", () => resolve(data));
          response.on("error", reject);
        },
      );
      request.on("error", reject);
    });
    await wait(() => release);
    await wait(async () =>
      (await api()).some(
        (row) => row.url === origin + "/events" && row.responseBytes > 0,
      ),
    );
    const record = (await api()).find((row) => row.url === origin + "/events");
    await page.locator(`.traffic-row[data-id="${record.id}"]`).click();
    const panel = page.locator('[data-side="response"]');
    await panel.locator('[data-view="events"]').click();
    await panel.locator(".events li").first().waitFor();
    assert.deepEqual(
      await panel.locator(".events .ev").allTextContents(),
      ["update"],
      "event-only blocks do not dispatch messages",
    );
    await page.screenshot({ path: path.join(root, "events-streaming.png") });
    release();
    assert.equal(await streaming, initial + ending);
    await wait(
      async () =>
        (await api()).find((row) => row.id === record.id)?.state ===
        "completed",
    );
    await wait(async () => (await panel.locator(".events li").count()) === 3);
    assert.deepEqual(await panel.locator(".events .ev").allTextContents(), [
      "update",
      "message",
      "empty",
    ]);
    assert.deepEqual(await panel.locator(".events .d").allTextContents(), [
      "one",
      " two\nthree",
      "",
    ]);
    await panel.locator('[data-side-copy="response"]').click();
    await wait(() => {
      copied = execFileSync("/usr/bin/pbpaste", { encoding: "utf8" });
      return copied.startsWith("event: update");
    });
    assert.equal(
      copied,
      "event: update\ndata: one\n\nevent: message\ndata:  two\ndata: three\n\nevent: empty\ndata: \n\n",
    );
    const consumer = await page.context().browser().newPage();
    await consumer.goto(origin + "/consumer");
    const received = await consumer.evaluate(
      (url) =>
        new Promise((resolve, reject) => {
          const source = new EventSource(url);
          const result = [];
          const timeout = setTimeout(() => {
            source.close();
            reject(new Error("copied events did not dispatch"));
          }, 5000);
          for (const type of ["update", "message", "empty"])
            source.addEventListener(type, (event) => {
              result.push({ type: event.type, data: event.data });
              if (result.length === 3) {
                clearTimeout(timeout);
                source.close();
                resolve(result);
              }
            });
        }),
      origin + "/copied",
    );
    await consumer.close();
    assert.deepEqual(received, [
      { type: "update", data: "one" },
      { type: "message", data: " two\nthree" },
      { type: "empty", data: "" },
    ]);
    await panel.locator('[data-view="raw"]').click();
    await panel.locator("pre.raw").waitFor();
    assert(
      (await panel.locator("pre.raw").getAttribute("data-text")).endsWith(
        initial + ending,
      ),
    );
    await page.screenshot({ path: path.join(root, "events-raw.png") });
    fs.writeFileSync(path.join(root, "copied-events.txt"), copied);
    fs.copyFileSync(
      "ui/src/components/Inspector.vue",
      path.join(root, "Inspector.vue"),
    );
    return {
      passed: true,
      checks: [
        "真实流式转发完整；事件名块与未终止事件不显示为已完成事件",
        "CR/LF/CRLF、空 data、额外空白与多行 data 正确",
        "复制结果经浏览器 EventSource 重新读取且内容一致；Raw 保留完整字节文本",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    if (release) release();
    if (streaming) await streaming.catch(() => {});
    upstream.closeAllConnections();
    await new Promise((resolve) => upstream.close(resolve));
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "traffic-events" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
