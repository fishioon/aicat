const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");

async function verify({ page, root, home, proxyPort, wait }) {
  let release;
  const vendorKey = "fixture-vendor-control-key";
  const agentKey = "fixture-agent-control-key";
  const seen = [];
  const server = http.createServer((request, response) => {
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      seen.push({ path: request.url, auth: request.headers.authorization });
      if (request.url === "/v1/models") {
        response.setHeader("Content-Type", "application/json");
        return response.end(JSON.stringify({ data: [{ id: "control-model" }] }));
      }
      if (request.url === "/v1/chat/completions") {
        assert.equal(request.headers.authorization, "Bearer " + vendorKey);
        assert.equal(JSON.parse(Buffer.concat(chunks)).model, "control-model");
        response.writeHead(200, { "Content-Type": "text/event-stream" });
        response.write('data: {"choices":[{"delta":{"content":"first"}}]}\n\n');
        release = () => {
          if (!response.writableEnded && !response.destroyed)
            response.end("data: [DONE]\n\n");
        };
        return;
      }
      response.end("foreign control-shaped path");
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const foreign = `http://127.0.0.1:${server.address().port}`;
  const self = `http://127.0.0.1:${proxyPort}`;
  const call = (target, body, headers = {}, onData) =>
    new Promise((resolve, reject) => {
      const request = http.request({
        host: "127.0.0.1", port: proxyPort, path: target,
        method: body === undefined ? "GET" : "POST", headers,
      }, (response) => {
        const chunks = [];
        response.on("data", (chunk) => {
          chunks.push(chunk);
          onData?.(Buffer.concat(chunks).toString());
        });
        response.on("error", reject);
        response.on("end", () => resolve({ status: response.statusCode, text: Buffer.concat(chunks).toString() }));
      });
      request.setTimeout(8000, () => request.destroy(new Error("control request timed out")));
      request.on("error", reject);
      request.end(body);
    });
  const api = async (target = "/api/traffic?limit=2000") => {
    const result = await call(target);
    assert.equal(result.status, 200);
    return JSON.parse(result.text);
  };
  try {
    await page.locator("#nav-traffic").click();
    await page.locator("#recording").click();
    await page.locator("#nav-overview").click();
    await page.locator("#connect-provider").click();
    await page.locator("#provider-name").fill("Control fixture");
    await page.locator("#provider-url").fill(foreign + "/v1");
    await page.locator("#provider-key").fill(vendorKey);
    await page.locator("#provider-save").click();
    await wait(async () => (await api(self + "/v1/models")).data?.length === 1);
    const models = await api(self + "/v1/models");
    const token = fs.readFileSync(path.join(home, "mcp-token"), "utf8").trim();
    const body = JSON.stringify({ jsonrpc: "2.0", id: 1, method: "initialize", params: {
      protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "control-fixture", version: "1" },
    } });
    const headers = { "Content-Type": "application/json", Accept: "application/json, text/event-stream" };
    assert.equal((await call(self + "/mcp", body, headers)).status, 401);
    assert.equal((await call(self + "/mcp", body, { ...headers, Authorization: "Bearer " + token, Origin: "http://fixture.invalid" })).status, 403);
    assert.equal((await call(self + "/mcp", body, { ...headers, Authorization: "Bearer " + token })).status, 200);
    assert.equal((await api()).items.length, 0);
    let first = "";
    const stream = call(self + "/v1/chat/completions", JSON.stringify({
      model: models.data[0].id, stream: true, messages: [{ role: "user", content: "fixture" }],
    }), { "Content-Type": "application/json", Authorization: "Bearer " + agentKey }, (value) => { first = value; });
    stream.catch(() => {});
    await wait(() => first.includes("first"));
    assert(!first.includes("[DONE]"));
    await wait(async () => (await api()).items.length === 1);
    release();
    const streamed = await stream;
    assert.equal(streamed.status, 200);
    assert(streamed.text.includes("[DONE]"));
    await wait(async () => (await api()).items[0].state === "completed");
    const detail = await api("/api/traffic/" + (await api()).items[0].id);
    for (const secret of [token, vendorKey, agentKey])
      assert(!JSON.stringify(detail).includes(secret), "model record must mask credentials");
    for (const pathname of ["/mcp", "/api/traffic"]) {
      const result = await call(foreign + pathname, undefined, { Authorization: "Bearer fixture-foreign" });
      assert.equal(result.text, "foreign control-shaped path");
    }
    await wait(async () => (await api()).items.length === 3);
    assert.equal(seen.filter((item) => item.path === "/v1/chat/completions").length, 1);
    assert(seen.filter((item) => ["/mcp", "/api/traffic"].includes(item.path)).every((item) => item.auth === "Bearer fixture-foreign"));
    await page.locator("#nav-traffic").click();
    await page.screenshot({ path: path.join(root, "control-capture.png") });
    return { passed: true, checks: [
      "完整 URL 的自身控制入口复用认证与 Origin 检查，MCP/API 不进入抓包",
      "真实供应商流首段在上游结束前到达，只产生一条脱敏模型记录",
      "外部同名路径仍转发并采集，凭据只到达所选本地上游",
    ], nativeCoverage: "not run" };
  } finally {
    release?.();
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
require("./traffic-default.cjs").run({ verify, scenario: "control-traffic" }).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
