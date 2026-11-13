const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const http = require("node:http");
const assert = require("node:assert/strict");

async function verify({ page, root, proxyPort, current, api, wait }) {
  const connections = new Set();
  const upstream = net.createServer((socket) => {
    connections.add(socket);
    socket.on("close", () => connections.delete(socket));
    socket.on("error", () => {});
  });
  await new Promise((resolve) => upstream.listen(0, "::1", resolve));
  const port = upstream.address().port;
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-zh-Hans").click();
    await page.waitForFunction(
      () => document.documentElement.lang === "zh-Hans",
    );
    await page.locator("#nav-traffic").click();
    await page.locator("#recording").click();
    await new Promise((resolve, reject) => {
      const request = http.request({
        host: "127.0.0.1",
        port: proxyPort,
        method: "CONNECT",
        path: `[::1]:${port}`,
      });
      request.on("connect", (response, socket) => {
        try {
          assert.equal(response.statusCode, 200);
          socket.destroy();
          resolve();
        } catch (error) {
          socket.destroy();
          reject(error);
        }
      });
      request.on("error", reject);
      request.end();
    });
    await wait(async () =>
      (await api()).some(
        (record) => record.tunnel && record.url.includes(`[::1]:${port}`),
      ),
    );
    const record = (await api()).find((record) =>
      record.url.includes(`[::1]:${port}`),
    );
    await page.locator("#nav-settings").click();
    await page.locator("#settings-proxy").click();
    await page.locator("#decryption-domain-input").fill("[::1]");
    await page.locator("#add-decryption-domain").click();
    await page.locator('[id="remove-decryption-::1"]').waitFor();
    assert.deepEqual((await current()).domains, ["::1"]);
    await page.locator("#nav-traffic").click();
    await page.locator(`.traffic-row[data-id="${record.id}"]`).click();
    await page.locator("#tunnel-detail").waitFor();
    assert.equal(
      await page.locator("#decryption-covered").count(),
      1,
      "configured IPv6 domain must be recognized on its historical tunnel",
    );
    assert.equal(
      await page.locator("#enable-decryption").count(),
      0,
      "already configured IP must not offer a redundant action",
    );
    assert(
      (await page.locator("#decryption-covered").innerText()).includes("::1"),
    );
    assert(
      !(await current()).certificate.trusted,
      "viewing an existing rule does not install trust",
    );
    await page.screenshot({ path: path.join(root, "ipv6-covered.png") });
    await page.locator("#nav-settings").click();
    await page.locator("#settings-proxy").click();
    await page.locator('[id="remove-decryption-::1"]').click();
    await page.waitForFunction(() => !document.body.classList.contains("busy"));
    await page.locator("#nav-traffic").click();
    await page.locator("#enable-decryption").waitFor();
    await page.locator("#enable-decryption").click();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "客户端已单独信任", exact: true })
      .click();
    await page.locator("#decryption-covered").waitFor();
    assert.deepEqual((await current()).domains, ["::1"]);
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    await page.locator("#nav-traffic").click();
    const status = page.locator(`.traffic-row[data-id="${record.id}"] .status`);
    assert.equal(await status.innerText(), "Tunnel");
    assert(
      await status.evaluate(
        (element) => element.scrollWidth <= element.clientWidth,
      ),
    );
    await page.screenshot({ path: path.join(root, "ipv6-covered-en.png") });
    fs.copyFileSync(
      "ui/src/components/TunnelView.vue",
      path.join(root, "TunnelView.vue"),
    );
    return {
      passed: true,
      checks: [
        "真实 IPv6 CONNECT 与规范化规则对应，已配置时不重复提供启用入口",
        "移除后恢复入口，从详情重新启用仍保存规范化 IP，查看规则不触发系统信任",
      ],
      coverage:
        "Real IPv6 TCP CONNECT and headless UI; no claim of native authorization or IPv6 TLS body inspection",
    };
  } finally {
    for (const socket of connections) socket.destroy();
    await new Promise((resolve) => upstream.close(resolve));
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "ipv6-tunnel" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
