const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const net = require("node:net");
const crypto = require("node:crypto");

async function verify({
  page,
  root,
  home,
  proxyPort,
  current,
  wait,
  stop,
  launch,
}) {
  await stop();
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    "extensions:\n  traffic:\n    enabled: true\nui:\n  language: zh-Hans\n  theme: light\ntraffic:\n  recording: true\n  maxRecords: 12\n  maxBodyBytes: 2097152\n  maxTotalBodyBytes: 8388608\n",
  );
  await launch();
  const token = fs.readFileSync(path.join(home, "mcp-token"), "utf8").trim();
  assert.equal(fs.statSync(path.join(home, "mcp-token")).mode & 0o777, 0o600);
  let rpcID = 0;
  const transcript = [];
  const releases = new Map();
  const upstream = http.createServer((request, response) => {
    if (request.url === "/waiting") {
      releases.set(request.url, () => {
        if (!response.writableEnded && !response.destroyed)
          response.end("headers arrived");
      });
      return;
    }
    if (request.url.startsWith("/stream")) {
      response.writeHead(200, { "Content-Type": "text/event-stream" });
      response.write("data: first\n\n");
      releases.set(request.url, () => {
        if (!response.writableEnded && !response.destroyed)
          response.end("data: last\n\n");
      });
      return;
    }
    if (request.url === "/reset-upstream") return request.socket.destroy();
    const chunks = [];
    request.on("data", (chunk) => chunks.push(chunk));
    request.on("end", () => {
      response.writeHead(200, {
        "Content-Type": "application/octet-stream",
        "X-Repeat": ["first", "second"],
      });
      response.end(
        request.method === "GET"
          ? Buffer.from(request.url)
          : Buffer.concat(chunks),
      );
    });
  });
  await new Promise((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  const authority = `127.0.0.1:${upstream.address().port}`;
  const ipv6 = http.createServer((request, response) =>
    response.end("ipv6 response"),
  );
  await new Promise((resolve) => ipv6.listen(0, "::1", resolve));
  const ipv6Authority = `[::1]:${ipv6.address().port}`;
  const reserved = net.createServer();
  await new Promise((resolve) => reserved.listen(0, "127.0.0.1", resolve));
  const closedPort = reserved.address().port;
  await new Promise((resolve) => reserved.close(resolve));
  const request = (method, target, body, headers = {}) =>
    new Promise((resolve, reject) => {
      const req = http.request(
        { host: "127.0.0.1", port: proxyPort, path: target, method, headers },
        (res) => {
          const chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () =>
            resolve({
              status: res.statusCode,
              headers: res.headers,
              data: Buffer.concat(chunks),
            }),
          );
          res.on("error", reject);
        },
      );
      req.setTimeout(10000, () =>
        req.destroy(new Error("fixture request timeout")),
      );
      req.on("error", reject);
      req.end(body);
    });
  const api = async (pathname = "/api/traffic?limit=2000") => {
    const response = await request("GET", pathname);
    assert.equal(response.status, 200);
    return JSON.parse(response.data);
  };
  const rpc = async (method, params = {}, target = "/mcp") => {
    const response = await request(
      "POST",
      target,
      JSON.stringify({ jsonrpc: "2.0", id: ++rpcID, method, params }),
      {
        Authorization: "Bearer " + token,
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
      },
    );
    assert.equal(response.status, 200);
    const result = JSON.parse(response.data);
    transcript.push({
      method,
      params,
      result:
        result.error ||
        (result.result?.isError
          ? result.result
          : {
              ids: result.result?.structuredContent?.items?.map(
                (item) => item.id,
              ),
              info: result.result?.structuredContent?.info,
              nextPage: result.result?.structuredContent?.nextPage,
            }),
    });
    return result;
  };
  const tool = async (name, args) => {
    const result = await rpc("tools/call", { name, arguments: args });
    if (result.error || result.result?.isError)
      assert.fail(`${name}: ${JSON.stringify(result)}`);
    return result.result.structuredContent;
  };
  const rejected = async (name, args) => {
    const result = await rpc("tools/call", { name, arguments: args });
    assert(
      result.error || result.result?.isError,
      `${name} must reject ${JSON.stringify(args)}`,
    );
  };
  const send = (pathname, body) =>
    request(body ? "POST" : "GET", `http://${authority}${pathname}`, body, {
      "Content-Type": "application/octet-stream",
      "X-Repeat": ["one", "two"],
    });
  const recordFor = async (pathname) => {
    await wait(async () =>
      (await api()).items.some(
        (item) =>
          item.url.endsWith(pathname) &&
          !["request", "response"].includes(item.state),
      ),
    );
    return (await api()).items.find((item) => item.url.endsWith(pathname));
  };
  const clear = async () => {
    await page.locator("#nav-traffic").click();
    const response = page.waitForResponse(
      (response) =>
        response.url().endsWith("/rpc") &&
        response.request().postDataJSON()?.method === "Desktop.ClearTraffic",
    );
    await page.locator("#clear").click();
    await (await response).finished();
    await page.evaluate(() => new Promise(requestAnimationFrame));
  };
  const info = (body) => ({
    state: body.info.state,
    observed: body.info.observed,
    captured: body.info.captured,
    returnedTruncated: body.returnedTruncated,
    returnedBytes: Buffer.from(body.data, "base64").length,
  });
  const summary = { pages: [], bodies: [] };
  try {
    const initialized = await rpc("initialize", {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "aicat-product-review", version: "1" },
    });
    assert(initialized.result.instructions.includes("untrusted"));
    const advertised = (await rpc("tools/list")).result.tools;
    for (const name of ["traffic_list", "traffic_get", "traffic_body"])
      assert(
        advertised.find((tool) => tool.name === name).annotations.readOnlyHint,
      );
    fs.writeFileSync(
      path.join(root, "tools.json"),
      JSON.stringify(advertised, null, 2),
    );
    await rpc("tools/list", {}, `http://127.0.0.1:${proxyPort}/mcp`);
    const ownControl = (await api()).items;
    if (ownControl.length) {
      const record = await api("/api/traffic/" + ownControl[0].id);
      fs.writeFileSync(
        path.join(root, "own-control-proof.json"),
        JSON.stringify({
          capturedOwnCredential:
            record.requestReceived.headers.Authorization?.includes(
              "Bearer " + token,
            ),
          capturedRequests: ownControl.length,
        }),
      );
    }
    assert.equal(
      ownControl.length,
      0,
      "absolute-form requests to aicat's own control authority must not enter captured traffic",
    );
    const payload = Buffer.from([0, 1, 255, 39, 10, 65]);
    assert.deepEqual((await send("/echo-needle", payload)).data, payload);
    for (let i = 0; i < 4; i++) await send(`/page-${i}`);
    await request("GET", `http://127.0.0.1:${closedPort}/disconnect`);
    const failed = await recordFor("/disconnect");
    assert.equal(failed.state, "failed");
    const first = await tool("traffic_list", { limit: 2 });
    const baselineIDs = (await api()).items.map((item) => item.id);
    assert.equal(typeof first.nextPage, "string");
    await send("/arrived-after-first-page");
    const ids = first.items.map((item) => item.id);
    let cursor = first.nextPage;
    while (cursor) {
      const next = await tool("traffic_list", { limit: 2, before: cursor });
      summary.pages.push(next.items.map((item) => item.id));
      assert(next.items.every((item) => item.id < Number(cursor)));
      ids.push(...next.items.map((item) => item.id));
      cursor = next.nextPage;
    }
    assert.deepEqual(ids, baselineIDs);
    assert.equal(new Set(ids).size, ids.length);
    assert.deepEqual(
      (
        await tool("traffic_list", { limit: 2, before: Number(first.nextPage) })
      ).items.map((item) => item.id),
      summary.pages[0],
    );
    summary.httpPages = [];
    let httpPage = await api("/api/traffic?limit=2");
    const httpIDs = [];
    while (true) {
      summary.httpPages.push(httpPage.items.map((item) => item.id));
      httpIDs.push(...httpPage.items.map((item) => item.id));
      if (!httpPage.nextPage) break;
      httpPage = await api(
        "/api/traffic?limit=2&before=" + encodeURIComponent(httpPage.nextPage),
      );
    }
    assert.deepEqual(
      httpIDs,
      (await api()).items.map((item) => item.id),
    );
    assert.equal((await tool("traffic_list", { limit: 1 })).items.length, 1);
    assert.equal(
      (await tool("traffic_list", { before: "0", state: "" })).items.length,
      baselineIDs.length + 1,
    );
    const byAuthority = await api(
      "/api/traffic?host=" + encodeURIComponent(authority),
    );
    assert.equal(
      byAuthority.items.length,
      (await api()).items.filter((item) => new URL(item.url).host === authority)
        .length,
      "host:port from the captured URL must match the query filter",
    );
    const filtered = await tool("traffic_list", {
      host: authority,
      method: "post",
      query: "needle",
      state: "completed",
    });
    assert.equal(filtered.items.length, 1);
    assert.deepEqual(
      (await tool("traffic_list", { state: "failed" })).items.map(
        (item) => item.id,
      ),
      [failed.id],
    );
    assert.deepEqual(
      (await api("/api/traffic?state=failed")).items.map((item) => item.id),
      [failed.id],
    );
    assert.equal(
      (await tool("traffic_list", { query: "no-such-fixture" })).items.length,
      0,
    );
    for (const limit of [-1, 0, 201, "2"])
      await rejected("traffic_list", { limit });
    for (const before of [
      -1,
      "bad",
      "1.5",
      "18446744073709551616",
      {},
      null,
      false,
    ])
      await rejected("traffic_list", { before });
    await rejected("traffic_list", { state: "not-a-state" });
    const wide = await tool("traffic_list", { before: "18446744073709551615" });
    assert.equal(wide.items.length, baselineIDs.length + 1);
    for (const suffix of [
      "limit=0",
      "limit=2001",
      "limit=bad",
      "before=bad",
      "before=-1",
    ])
      assert.equal(
        (await request("GET", "/api/traffic?" + suffix)).status,
        400,
      );
    for (const args of [{}, { id: 0 }, { id: -1 }, { id: "2" }, { id: 1.5 }])
      await rejected("traffic_get", args);
    for (const args of [{}, { id: "" }, { id: 2 }, { id: "not-retained" }])
      await rejected("traffic_body", args);
    const failure = await tool("traffic_get", { id: failed.id });
    assert(failure.error && !failure.responseSent);
    await request("GET", `http://${ipv6Authority}/ipv6`);
    const ipv6Record = await recordFor("/ipv6");
    for (const host of ["::1", "[::1]", ipv6Authority])
      assert.deepEqual(
        (await tool("traffic_list", { host })).items.map((item) => item.id),
        [ipv6Record.id],
      );
    assert.equal(
      (await tool("traffic_list", { host: "[::1]:1" })).items.length,
      0,
    );
    await request(
      "GET",
      `http://localhost:${upstream.address().port}/case-host`,
    );
    const caseHost = await recordFor("/case-host");
    assert.deepEqual(
      (await tool("traffic_list", { host: "LOCALHOST" })).items.map(
        (item) => item.id,
      ),
      [caseHost.id],
    );
    assert(
      (await tool("traffic_list", { host: "127.0.0.1" })).items.some(
        (item) => item.id === failed.id,
      ),
    );
    const echo = await tool("traffic_get", { id: filtered.items[0].id });
    assert.deepEqual(echo, await api("/api/traffic/" + echo.id));
    for (const point of [
      "requestReceived",
      "requestSent",
      "responseReceived",
      "responseSent",
    ]) {
      const body = await tool("traffic_body", { id: echo[point].body.id });
      assert.deepEqual(Buffer.from(body.data, "base64"), payload);
      assert.equal(body.info.state, "complete");
      assert.equal(body.returnedTruncated, false);
      const raw = await request("GET", "/api/bodies/" + echo[point].body.id);
      assert.deepEqual(raw.data, payload);
      assert.equal(raw.headers["x-aicat-body-state"], "complete");
    }
    assert.deepEqual(echo.requestReceived.headers["X-Repeat"], ["one", "two"]);
    assert.deepEqual(echo.responseSent.headers["X-Repeat"], [
      "first",
      "second",
    ]);
    await page.locator("#nav-traffic").click();
    await page.locator(`.traffic-row[data-id="${echo.id}"]`).click();
    const responsePane = page.locator('[data-side="response"]');
    await responsePane.locator('[data-view="body"]').click();
    await responsePane.locator("pre.body").waitFor();
    assert.equal(
      await responsePane.locator("pre.body").textContent(),
      "00 01 ff 27 0a 41",
    );
    for (const [pathname, length] of [
      ["/large-complete", (1 << 20) + 333],
      ["/large-truncated", (2 << 20) + 333],
    ]) {
      const bytes = Buffer.alloc(length, 120);
      const forwarded = await send(pathname, bytes);
      assert.equal(forwarded.status, 200);
      assert.equal(forwarded.data.length, bytes.length);
      assert(
        crypto.timingSafeEqual(forwarded.data, bytes),
        "large transfer bytes must remain unchanged",
      );
      const record = await tool("traffic_get", {
        id: (await recordFor(pathname)).id,
      });
      const captured = await tool("traffic_body", {
        id: record.responseSent.body.id,
      });
      assert.equal(captured.info.observed, length);
      assert.equal(captured.returnedTruncated, true);
      assert.deepEqual(
        Buffer.from(captured.data, "base64"),
        bytes.subarray(0, 1 << 20),
      );
      assert.equal(
        captured.info.state,
        length > 2 << 20 ? "truncated" : "complete",
      );
      const raw = await request("GET", "/api/bodies/" + captured.info.id);
      assert.equal(raw.data.length, Math.min(length, 2 << 20));
      summary.bodies.push(info(captured));
    }
    const streaming = send("/stream");
    streaming.catch(() => {});
    await wait(() => releases.has("/stream"));
    await wait(async () =>
      (await api()).items.some(
        (item) => item.url.endsWith("/stream") && item.responseBytes > 0,
      ),
    );
    const streamID = (await api()).items.find((item) =>
      item.url.endsWith("/stream"),
    ).id;
    const pending = await tool("traffic_get", { id: streamID });
    assert.equal(pending.state, "response");
    assert(
      (await tool("traffic_list", { state: "response" })).items.some(
        (item) => item.id === streamID,
      ),
    );
    const pendingBody = await tool("traffic_body", {
      id: pending.responseSent.body.id,
    });
    assert.equal(pendingBody.info.state, "pending");
    assert.equal(
      Buffer.from(pendingBody.data, "base64").toString(),
      "data: first\n\n",
    );
    releases.get("/stream")();
    assert.equal(
      (await streaming).data.toString(),
      "data: first\n\ndata: last\n\n",
    );
    const completed = await tool("traffic_get", {
      id: (await recordFor("/stream")).id,
    });
    assert(completed.revision > pending.revision);
    assert.equal(
      (await tool("traffic_body", { id: completed.responseSent.body.id })).info
        .state,
      "complete",
    );
    assert.equal((await send("/reset-upstream")).status, 502);
    const reset = await recordFor("/reset-upstream");
    assert.equal(
      reset.state,
      "failed",
      "an upstream reset must not be reported as a client cancellation",
    );
    const waiting = send("/waiting");
    waiting.catch(() => {});
    await wait(() => releases.has("/waiting"));
    await wait(async () =>
      (await tool("traffic_list", { state: "request" })).items.some((item) =>
        item.url.endsWith("/waiting"),
      ),
    );
    releases.get("/waiting")();
    assert.equal((await waiting).data.toString(), "headers arrived");
    await recordFor("/waiting");
    const firstChunk = new Promise((resolve, reject) => {
      const abort = http.get(
        {
          host: "127.0.0.1",
          port: proxyPort,
          path: `http://${authority}/stream-abort`,
        },
        (response) => {
          response.once("data", () => resolve(abort));
          response.on("error", () => {});
        },
      );
      abort.on("error", () => {});
      abort.setTimeout(5000, () => {
        abort.destroy();
        reject(new Error("abort fixture timed out"));
      });
    });
    const aborted = await firstChunk;
    aborted.destroy();
    releases.get("/stream-abort")();
    const canceled = await recordFor("/stream-abort");
    assert.equal(canceled.state, "canceled");
    const canceledRecord = await tool("traffic_get", { id: canceled.id });
    const canceledBody = await tool("traffic_body", {
      id: canceledRecord.responseSent.body.id,
    });
    assert.equal(
      canceledBody.info.state,
      canceledRecord.responseSent.body.state,
    );
    assert(
      Buffer.from(canceledBody.data, "base64")
        .toString()
        .startsWith("data: first\n\n"),
    );
    summary.canceledBody = info(canceledBody);
    assert(
      (await tool("traffic_list", { state: "canceled" })).items.some(
        (item) => item.id === canceled.id,
      ),
    );
    for (let i = 0; i < 14; i++) await send(`/eviction-${i}`);
    await rejected("traffic_get", { id: echo.id });
    await rejected("traffic_body", { id: echo.requestReceived.body.id });
    assert.equal((await request("GET", "/api/traffic/" + echo.id)).status, 404);
    assert.equal(
      (await request("GET", "/api/bodies/" + echo.requestReceived.body.id))
        .status,
      404,
    );
    const clearing = send("/stream-clear");
    clearing.catch(() => {});
    await wait(() => releases.has("/stream-clear"));
    await wait(async () =>
      (await api()).items.some(
        (item) => item.url.endsWith("/stream-clear") && item.responseBytes > 0,
      ),
    );
    const clearID = (await api()).items.find((item) =>
      item.url.endsWith("/stream-clear"),
    ).id;
    await clear();
    await rejected("traffic_get", { id: clearID });
    releases.get("/stream-clear")();
    assert.equal(
      (await clearing).data.toString(),
      "data: first\n\ndata: last\n\n",
    );
    assert.equal((await tool("traffic_list", {})).items.length, 0);
    await send("/retained-after-clear", payload);
    const retained = await recordFor("/retained-after-clear");
    assert(retained.id > clearID);
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-traffic").click();
    await page.locator("#feature-traffic").click();
    await wait(async () => !(await current()).trafficEnabled);
    assert.deepEqual((await send("/unrecorded", payload)).data, payload);
    assert.deepEqual(
      (await tool("traffic_list", {})).items.map((item) => item.id),
      [retained.id],
    );
    await tool("traffic_get", { id: retained.id });
    await page.locator("#feature-traffic").click();
    await wait(async () => (await current()).trafficEnabled);
    await stop();
    await launch();
    await rpc("initialize", {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "aicat-product-review-restarted", version: "1" },
    });
    assert.equal(
      fs.readFileSync(path.join(home, "mcp-token"), "utf8").trim(),
      token,
    );
    assert.equal((await tool("traffic_list", {})).items.length, 0);
    await rejected("traffic_get", { id: retained.id });
    assert(
      !fs.readFileSync(path.join(root, "app.log"), "utf8").includes(token),
    );
    fs.writeFileSync(
      path.join(root, "query-proof.json"),
      JSON.stringify(summary, null, 2),
    );
    for (const source of [
      "internal/mcpserver/server.go",
      "internal/traffic/service.go",
      "internal/gateway/addons/api/addon.go",
    ])
      fs.copyFileSync(
        source,
        path.join(
          root,
          path.basename(path.dirname(source)) + "-" + path.basename(source),
        ),
      );
    return {
      passed: true,
      checks: [
        "HTTP/MCP 分页无重漏，返回的字符串边界可直接续页且兼容数字；主机端口、方法、状态、文本筛选一致",
        "非法参数明确拒绝，四个观测位置与 HTTP 原始字节及桌面二进制视图一致",
        "MCP 返回上限与采集截断分别标明；流式 pending/complete 随真实转发更新",
        "淘汰/清空使记录与正文失效而转发继续，停用采集保留只读查询，重启清记录但令牌保留",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    for (const release of releases.values()) release();
    ipv6.closeAllConnections();
    await new Promise((resolve) => ipv6.close(resolve));
    upstream.closeAllConnections();
    await new Promise((resolve) => upstream.close(resolve));
    fs.writeFileSync(
      path.join(root, "mcp-queries.ndjson"),
      transcript.map((entry) => JSON.stringify(entry)).join("\n") + "\n",
    );
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "agent-traffic" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
