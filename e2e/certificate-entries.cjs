const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const https = require("node:https");
const tls = require("node:tls");
const assert = require("node:assert/strict");
const writeCertificate = require("./certificate-fixture.cjs");

async function verify({
  page,
  root,
  home,
  current,
  wait,
  fixture,
  stop,
  launch,
  proxyPort,
  api,
}) {
  const checks = [];
  const upstreamCertificate = writeCertificate(
    root,
    path.join(root, "upstream-ca"),
  );
  const server = https.createServer(
    upstreamCertificate,
    (request, response) => {
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify({ path: request.url }));
    },
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = server.address().port;
  const previousRoot = process.env.AICAT_E2E_ROOT;
  const previousDebug = process.env.GODEBUG;
  const previousCatalog = process.env.AICAT_E2E_MODELS_DEV;
  process.env.AICAT_E2E_ROOT = upstreamCertificate.path;
  process.env.GODEBUG = "x509usefallbackroots=1";
  process.env.AICAT_E2E_MODELS_DEV = `https://localhost:${port}/catalog`;
  const probe = (hostname, label, decrypted = false, inspect = false) =>
    new Promise((resolve, reject) => {
      const request = http.request({
        host: "127.0.0.1",
        port: proxyPort,
        method: "CONNECT",
        path: `${hostname}:${port}`,
      });
      request.on("error", reject);
      request.on("connect", (response, socket) => {
        if (response.statusCode !== 200) {
          socket.destroy();
          reject(new Error("CONNECT failed"));
          return;
        }
        const secure = tls.connect({
          socket,
          servername: hostname,
          rejectUnauthorized: !inspect,
          ca: decrypted
            ? fs.readFileSync(path.join(home, "ca", "mitmproxy-ca-cert.pem"))
            : upstreamCertificate.cert,
        });
        secure.setTimeout(5000, () =>
          secure.destroy(new Error("TLS fixture timed out")),
        );
        secure.on("error", reject);
        let peer,
          text = "";
        secure.on("secureConnect", () => {
          peer = secure.getPeerCertificate().raw;
          secure.write(
            `GET /${label} HTTP/1.1\r\nHost: ${hostname}:${port}\r\nConnection: close\r\n\r\n`,
          );
        });
        secure.on("data", (chunk) => {
          text += chunk;
        });
        secure.on("end", () => {
          try {
            assert.match(text, /HTTP\/1.1 200/);
            assert(text.includes(`/${label}`));
            const same = peer.equals(
              new (require("node:crypto").X509Certificate)(
                upstreamCertificate.cert,
              ).raw,
            );
            assert.equal(
              same,
              !decrypted,
              inspect
                ? "expired root must leave TLS untouched"
                : "TLS identity must match the configured policy",
            );
            resolve();
          } catch (error) {
            reject(error);
          }
        });
      });
      request.setTimeout(7000, () =>
        request.destroy(new Error("CONNECT fixture timed out")),
      );
      request.end();
    });
  const settle = () =>
    page.waitForFunction(() => !document.body.classList.contains("busy"));
  const capture = async () => {
    await page.locator("#nav-traffic").click();
    if (!(await current()).recording) {
      await page.locator("#recording").click();
      await settle();
    }
  };
  const settings = async () => {
    await page.locator("#nav-settings").click();
    await page.locator("#settings-proxy").click();
  };
  const codex = async () => {
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-codex").click();
  };
  const enableCodex = async (title) => {
    await codex();
    await page.locator("#feature-codex").click();
    await page
      .locator("#dialog")
      .getByRole("heading", { name: title, exact: true })
      .waitFor();
    assert.equal(
      await page
        .locator("#dialog")
        .getByRole("button", { name: "安装证书", exact: true })
        .count(),
      0,
    );
    assert(
      !(await current()).extensions.find((item) => item.id === "codex").enabled,
    );
  };
  const backToTunnel = async (id) => {
    await page.locator("#nav-traffic").click();
    await page.locator(`.traffic-row[data-id="${id}"]`).click();
    await page.locator("#tunnel-detail").waitFor();
  };
  const confirm = async () => {
    await page
      .locator("#dialog")
      .getByRole("button", { name: "确定", exact: true })
      .click();
    await settle();
  };
  const shot = (name) =>
    page.screenshot({ path: path.join(root, name + ".png") });
  try {
    await page.locator("#nav-settings").click();
    await page.locator("#preference-language-zh-Hans").click();
    await page.waitForFunction(
      () => document.documentElement.lang === "zh-Hans",
    );
    await stop();
    writeCertificate(root, path.join(home, "ca"), true);
    await launch();
    await settings();
    await page.locator("#decryption-domain-input").fill("localhost");
    await page.locator("#add-decryption-domain").click();
    await page.locator('[id="remove-decryption-localhost"]').waitFor();
    await capture();
    await probe("localhost", "expired-root", false, true);
    await probe("localhost", "expired-root-verified");
    await wait(async () =>
      (await api()).some(
        (record) => record.tunnel && record.url.includes(`localhost:${port}`),
      ),
    );
    const tunnel = (await api()).find(
      (record) => record.tunnel && record.url.includes(`localhost:${port}`),
    );
    await enableCodex("根证书已过期");
    await page
      .locator("#dialog")
      .getByRole("button", { name: "代理设置", exact: true })
      .click();
    await page.locator("#certificate-replace").waitFor();
    await page.locator("#history-back").click();
    await page.locator('#extension-codex[aria-current="true"]').waitFor();
    await backToTunnel(tunnel.id);
    await page.locator("#repair-certificate").click();
    await page
      .locator("#dialog")
      .getByRole("heading", { name: "根证书已过期", exact: true })
      .waitFor();
    assert.equal(
      await page
        .locator("#dialog")
        .getByRole("button", { name: "客户端已单独信任", exact: true })
        .count(),
      0,
    );
    await shot("expired-tunnel-recovery");
    await page
      .locator("#dialog")
      .getByRole("button", { name: "代理设置", exact: true })
      .click();
    await page.locator("#certificate-replace").click();
    await confirm();
    assert((await current()).certificate.trusted);
    await page.locator("#history-back").click();
    await page
      .locator(`.traffic-row[data-id="${tunnel.id}"][aria-selected="true"]`)
      .waitFor();
    await page.locator("#decryption-covered").waitFor();
    assert.equal(await page.locator("#repair-certificate").count(), 0);
    assert.deepEqual((await current()).domains, ["localhost"]);
    await probe("localhost", "after-replacement", true);
    assert((await api()).find((record) => record.id === tunnel.id).tunnel);
    checks.push(
      "过期 CA 不再签发，新连接透传真实上游；扩展和历史 CONNECT 均引导到更换入口，返回后目标与历史保留，新的 TLS 请求可真实解密",
    );

    await settings();
    await page.locator("#certificate-details > summary").click();
    fixture({ failRemove: true });
    await page.locator("#certificate-delete").click();
    await confirm();
    assert((await current()).certificate.pendingDeletion);
    await capture();
    await probe("localhost", "pending-root");
    await enableCodex("根证书删除未完成");
    await page
      .locator("#dialog")
      .getByRole("button", { name: "取消", exact: true })
      .click();
    await backToTunnel(tunnel.id);
    await page.locator("#repair-certificate").click();
    await page
      .locator("#dialog")
      .getByRole("heading", { name: "根证书删除未完成", exact: true })
      .waitFor();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "代理设置", exact: true })
      .click();
    fixture({ failRemove: false });
    await page.locator("#certificate-delete").click();
    await confirm();
    assert(!(await current()).certificate.exists);
    await page.locator("#history-back").click();
    await page.locator("#repair-certificate").click();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "安装证书", exact: true })
      .waitFor();
    assert.equal(
      await page
        .locator("#dialog")
        .getByRole("button", { name: "客户端已单独信任", exact: true })
        .count(),
      0,
    );
    await page
      .locator("#dialog")
      .getByRole("button", { name: "安装证书", exact: true })
      .click();
    await settle();
    assert((await current()).certificate.trusted);
    assert.deepEqual((await current()).domains, ["localhost"]);
    await probe("localhost", "after-deletion-recovery", true);
    checks.push(
      "删除未完成时两个入口均拒绝无效安装/单独信任操作，处理后缺失身份可从原域名直接重建安装并恢复解密",
    );

    await settings();
    await page.locator("#certificate-details > summary").click();
    fixture({ trusted: false });
    await page.locator("#certificate-recheck").click();
    await settle();
    await capture();
    await probe("a.localhost", "target-before-install");
    await wait(async () =>
      (await api()).some(
        (record) => record.tunnel && record.url.includes(`a.localhost:${port}`),
      ),
    );
    const target = (await api()).find(
      (record) => record.tunnel && record.url.includes(`a.localhost:${port}`),
    );
    await backToTunnel(target.id);
    await page.locator("#enable-decryption").click();
    let releaseInstall;
    await page.route("**/rpc", async (route) => {
      if (
        route.request().postDataJSON().method !== "Desktop.InstallCertificate"
      )
        return route.continue();
      const response = await route.fetch();
      await new Promise((resolve) => {
        releaseInstall = resolve;
      });
      await route.fulfill({ response });
    });
    await page
      .locator("#dialog")
      .getByRole("button", { name: "安装证书", exact: true })
      .click();
    await wait(() => releaseInstall);
    const firstId = Math.min(...(await api()).map((record) => record.id));
    await page.locator("#rows-viewport").focus();
    await page.keyboard.press("Home");
    await page
      .locator(`.traffic-row[data-id="${firstId}"][aria-selected="true"]`)
      .waitFor();
    releaseInstall();
    await wait(async () => (await current()).domains.includes("a.localhost"));
    await settle();
    assert((await current()).certificate.trusted);
    await probe("a.localhost", "target-after-install", true);
    checks.push("安装期间切换到另一历史请求，完成后仍只启用原目标域名");

    await settings();
    await page.locator('[id="remove-decryption-a.localhost"]').click();
    await settle();
    await settings();
    await page.locator('[id="remove-decryption-localhost"]').click();
    await settle();
    await page.locator("#decryption-domain-input").fill("*.localhost");
    await page.locator("#add-decryption-domain").click();
    await page.locator('[id="remove-decryption-*.localhost"]').waitFor();
    await probe("a.localhost", "wildcard-one", true);
    await probe("a.b.localhost", "wildcard-two", true);
    await probe("localhost", "wildcard-excludes-base");
    await page.locator('[id="remove-decryption-*.localhost"]').click();
    await settle();
    await probe("a.localhost", "wildcard-removed");
    assert.deepEqual((await current()).domains, []);
    checks.push(
      "实际 TLS 验证通配条目覆盖一层和多层子域、不含根域，移除后新连接恢复直通",
    );
    await page.locator("#settings-general").click();
    await page.locator("#preference-language-en").click();
    await page.waitForFunction(() => document.documentElement.lang === "en");
    await page.locator("#preference-theme-dark").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "dark",
    );
    await page.locator("#settings-proxy").click();
    await page.locator("#certificate-details > summary").click();
    fixture({ failRemove: true });
    await page.locator("#certificate-delete").click();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "Confirm", exact: true })
      .click();
    await settle();
    assert((await current()).certificate.pendingDeletion);
    await backToTunnel(tunnel.id);
    await page.locator("#enable-decryption").click();
    await page
      .locator("#dialog")
      .getByRole("heading", {
        name: "Root certificate deletion incomplete",
        exact: true,
      })
      .waitFor();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "Proxy settings", exact: true })
      .waitFor();
    await shot("pending-recovery-en-dark");
    checks.push("英文深色下异常证书提示和恢复入口可用");
    fs.writeFileSync(
      path.join(root, "checks.json"),
      JSON.stringify(checks, null, 2),
    );
    for (const source of [
      "internal/certificate/authority.go",
      "internal/certificate/status.go",
      "e2e/certificate-fixture.cjs",
      "ui/src/components/TunnelView.vue",
      "ui/src/components/ExtensionsPage.vue",
      "ui/src/state/desktop.ts",
    ]) {
      const target = path.join(root, "sources", source);
      fs.mkdirSync(path.dirname(target), { recursive: true });
      fs.copyFileSync(source, target);
    }
    return {
      passed: true,
      checks,
      coverage:
        "Real CONNECT/TLS and CA identities, local HTTPS upstream, headless entry navigation; only native certificate authorization/removal uses OS adapters",
    };
  } finally {
    await new Promise((resolve) => server.close(resolve));
    for (const [key, value] of [
      ["AICAT_E2E_ROOT", previousRoot],
      ["GODEBUG", previousDebug],
      ["AICAT_E2E_MODELS_DEV", previousCatalog],
    ]) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "certificate-entries" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
