const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");

async function verify({ page, root, proxyPort, wait }) {
  const seen = [];
  const ports = new Set();
  let release;
  const servers = ["a", "b"].map((origin) =>
    http.createServer((request, response) => {
      const chunks = [];
      request.on("data", (chunk) => chunks.push(chunk));
      request.on("end", () => {
        const body = Buffer.concat(chunks);
        seen.push({
          origin,
          method: request.method,
          path: request.url,
          body: body.toString("base64"),
          auth: request.headers.authorization,
        });
        if (request.url === "/events") {
          response.writeHead(200, { "Content-Type": "text/event-stream" });
          response.write("data: first\n\n");
          release = () => {
            if (!response.writableEnded && !response.destroyed)
              response.end("data: last\n\n");
          };
          return;
        }
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(
          JSON.stringify({
            origin,
            method: request.method,
            path: request.url,
            body: body.toString("base64"),
          }),
        );
      });
    }),
  );
  for (const server of servers)
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const bases = servers.map(
    (server) => `http://127.0.0.1:${server.address().port}`,
  );
  const agent = new http.Agent({ keepAlive: true, maxSockets: 1 });
  const call = (target, body, auth, onData) =>
    new Promise((resolve, reject) => {
      const request = http.request(
        {
          host: "127.0.0.1",
          port: proxyPort,
          method: body ? "POST" : "GET",
          path: target,
          agent,
          headers: auth ? { Authorization: "Bearer " + auth } : {},
        },
        (response) => {
          const chunks = [];
          response.on("data", (chunk) => {
            chunks.push(chunk);
            onData?.(Buffer.concat(chunks));
          });
          response.on("end", () =>
            resolve({
              status: response.statusCode,
              data: Buffer.concat(chunks),
            }),
          );
          response.on("error", reject);
        },
      );
      request.on("socket", (socket) => {
        if (socket.localPort) ports.add(socket.localPort);
        else socket.once("connect", () => ports.add(socket.localPort));
      });
      request.setTimeout(5000, () =>
        request.destroy(new Error("origin fixture timed out")),
      );
      request.on("error", reject);
      request.end(body);
    });
  const api = async (pathname = "/api/traffic?limit=2000") => {
    const result = await call(pathname);
    assert.equal(result.status, 200);
    return JSON.parse(result.data);
  };
  try {
    await page.locator("#nav-traffic").click();
    await page.locator("#recording").click();
    const first = await call(
      bases[0] + "/first",
      undefined,
      "fixture-origin-a",
    );
    assert.equal(JSON.parse(first.data).origin, "a");
    await wait(async () => (await api()).items.length === 1);
    const second = await call(
      bases[1] + "/second",
      undefined,
      "fixture-origin-b",
    );
    assert.equal(
      second.status,
      200,
      "switching origins on the same proxy connection must return the requested response",
    );
    assert.equal(
      JSON.parse(second.data).origin,
      "b",
      "a shared proxy connection must select the requested origin for every request",
    );
    const payload = Buffer.from("中文\nquote' and binary\0end");
    for (const [index, origin] of ["a", "b"].entries()) {
      const result = await call(
        bases[index] + "/write",
        payload,
        "fixture-origin-" + origin,
      );
      assert.deepEqual(JSON.parse(result.data), {
        origin,
        method: "POST",
        path: "/write",
        body: payload.toString("base64"),
      });
    }
    let firstChunk;
    const arrived = new Promise((resolve) => {
      firstChunk = resolve;
    });
    const streaming = call(
      bases[0] + "/events",
      undefined,
      "fixture-origin-a",
      (data) => {
        if (data.includes(Buffer.from("data: first\n\n"))) firstChunk();
      },
    );
    streaming.catch(() => {});
    await arrived;
    assert(release);
    release();
    assert.equal(
      (await streaming).data.toString(),
      "data: first\n\ndata: last\n\n",
    );
    assert.equal(
      JSON.parse(
        (await call(bases[1] + "/after-stream", undefined, "fixture-origin-b"))
          .data,
      ).origin,
      "b",
    );
    await wait(
      async () =>
        (await api()).items.length === 6 &&
        (await api()).items.every((row) => row.state === "completed"),
    );
    assert.equal(
      ports.size,
      1,
      "client-to-proxy TCP connection is reused throughout origins, controls and streaming",
    );
    assert.equal(
      seen.filter(
        (request) => request.origin === "a" && request.method === "POST",
      ).length,
      1,
    );
    assert.equal(
      seen.filter(
        (request) => request.origin === "b" && request.method === "POST",
      ).length,
      1,
    );
    assert(
      seen.every(
        (request) => request.auth === "Bearer fixture-origin-" + request.origin,
      ),
    );
    for (const row of (await api()).items) {
      const detail = await api("/api/traffic/" + row.id);
      const response = await call("/api/bodies/" + detail.responseSent.body.id);
      if (row.url.endsWith("/events"))
        assert.equal(response.data.toString(), "data: first\n\ndata: last\n\n");
      else
        assert.equal(
          JSON.parse(response.data).origin,
          row.url.startsWith(bases[0]) ? "a" : "b",
        );
    }
    await page.screenshot({ path: path.join(root, "http-origins.png") });
    for (const source of [
      "internal/gateway/gateway.go",
      "internal/gateway/http_transport.go",
    ])
      fs.copyFileSync(source, path.join(root, path.basename(source)));
    return {
      passed: true,
      checks: [
        "同一代理 TCP 连接交替访问两个站点、控制 API 和 SSE，逐条到达正确站点",
        "二进制/Unicode/换行正文完整，凭据不串站点，POST 各提交一次",
        "抓包响应与实际站点一致，流式内容结束后可继续复用连接",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    if (release) release();
    agent.destroy();
    for (const server of servers) {
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    }
    fs.writeFileSync(
      path.join(root, "upstream-seen.json"),
      JSON.stringify(seen, null, 2),
    );
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "http-origins" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
