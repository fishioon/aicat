const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");

async function verify({ page, root, home, current, stop, launch, preview }) {
  const server = http.createServer((request, response) => {
    response.writeHead(404, { "Content-Type": "application/json" });
    response.end('{"error":"not found"}');
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const original = process.env.AICAT_E2E_PLUGIN_REGISTRY;
  const registry = "http://127.0.0.1:" + server.address().port;
  const parent = path.join(home, "extensions/wecom");
  const installed = path.join(parent, "2026.10.30");
  const choose = async () => {
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom").click();
  };
  const settle = () =>
    page.waitForFunction(() => !document.body.classList.contains("busy"));
  const installRPC = async () => {
    const response = await preview("/rpc", {
      method: "Desktop.InstallExtension",
      args: ["wecom"],
    });
    assert.equal(response.status, 200);
  };
  try {
    process.env.AICAT_E2E_PLUGIN_REGISTRY = registry;
    await stop();
    await launch();
    await choose();
    await page.locator("#extension-install").click();
    await settle();
    assert.equal(
      (await current()).extensions.find((x) => x.id === "wecom").installed,
      false,
    );
    assert(!fs.existsSync(installed));
    assert(!fs.readdirSync(parent).some((x) => x.startsWith(".install-")));
    assert.equal(await page.locator("#extension-install").isEnabled(), true);
    await page.screenshot({ path: path.join(root, "installation-failed.png") });
    delete process.env.AICAT_E2E_PLUGIN_REGISTRY;
    await stop();
    await launch();
    await choose();
    await page.locator("#extension-install").click();
    await page.locator("#extension-configure").waitFor({ timeout: 120000 });
    await settle();
    assert.equal(
      (await current()).extensions.find((x) => x.id === "wecom").installed,
      true,
    );
    assert.equal(await page.locator("#feature-wecom").isDisabled(), true);
    assert(!fs.existsSync(path.join(home, "state/channel-host")));
    await page.screenshot({
      path: path.join(root, "installation-recovered.png"),
    });
    const stamp = path.join(installed, "installation.json");
    const before = fs.readFileSync(stamp, "utf8");
    process.env.AICAT_E2E_PLUGIN_REGISTRY = registry;
    await stop();
    await launch();
    await installRPC();
    assert.equal(fs.readFileSync(stamp, "utf8"), before);
    const compatibility = path.join(installed, "node_modules/openclaw");
    const shared = path.resolve("bin/channel-host/shim");
    assert.equal(fs.realpathSync(compatibility), fs.realpathSync(shared));
    fs.rmSync(compatibility, { recursive: true, force: true });
    fs.cpSync(shared, compatibility, { recursive: true });
    fs.appendFileSync(
      path.join(compatibility, "sdk.mjs"),
      "\nthrow Error('STALE_COPY');\n",
    );
    await installRPC();
    assert.equal(fs.realpathSync(compatibility), fs.realpathSync(shared));
    assert.equal(fs.readFileSync(stamp, "utf8"), before);
    fs.writeFileSync(stamp, "{}");
    delete process.env.AICAT_E2E_PLUGIN_REGISTRY;
    await stop();
    await launch();
    await choose();
    await page.locator("#extension-install").click();
    await page.locator("#extension-configure").waitFor({ timeout: 120000 });
    await settle();
    assert.equal(
      JSON.parse(fs.readFileSync(stamp, "utf8")).version,
      "2026.10.30",
    );
    assert(!fs.readdirSync(parent).some((x) => x.startsWith(".replaced-")));
    return {
      passed: true,
      checks: [
        "实际安装按钮遇 registry 错误可恢复，无半安装或暂存残留",
        "重试安装官方包但不连接渠道，未验证账号不能启用",
        "重复安装与旧 shim 迁移无需访问 registry，损坏安装信息可经界面重新安装修复",
      ],
      nativeCoverage: "not run",
    };
  } finally {
    if (original === undefined) delete process.env.AICAT_E2E_PLUGIN_REGISTRY;
    else process.env.AICAT_E2E_PLUGIN_REGISTRY = original;
    await new Promise((resolve) => server.close(resolve));
  }
}
require("./traffic-default.cjs")
  .run({ verify, scenario: "channel-install-failure" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
