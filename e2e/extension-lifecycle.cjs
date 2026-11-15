const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const net = require("node:net");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(
  process.argv[3] || "build/extension-lifecycle-" + Date.now(),
);
const home = path.join(root, "home");
fs.mkdirSync(home, { recursive: true, mode: 0o700 });
const report = {
  passed: false,
  command: process.argv,
  binarySHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  scenarioSHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(__filename))
    .digest("hex"),
  coverage:
    "Headless Chromium with hidden native app, real Desktop/config/package services and Bun installer; local registry fixtures; no native window or platform acceptance",
  checks: [],
  errors: [],
};
const pause = (ms) => new Promise((r) => setTimeout(r, ms));
async function wait(fn, timeout = 20000) {
  const until = Date.now() + timeout;
  while (Date.now() < until) {
    try {
      const value = await fn();
      if (value) return value;
    } catch {}
    await pause(50);
  }
  throw Error("timeout: " + fn);
}
const packageName = "@wecom/wecom-openclaw-plugin";
const versions = ["1.0.0", "1.0.1", "1.0.2"];
const archives = new Map();
for (const version of versions) {
  const dir = path.join(root, "fixtures", version, "package");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(
    path.join(dir, "package.json"),
    JSON.stringify({
      name: packageName,
      version,
      type: "module",
      openclaw: { extensions: ["index.mjs"] },
      scripts: {
        postinstall:
          "node -e \"require('fs').writeFileSync('INSTALL_SCRIPT_RAN','bad')\"",
      },
    }),
  );
  fs.writeFileSync(
    path.join(dir, "openclaw.plugin.json"),
    JSON.stringify({ id: "wecom", channels: ["wecom"] }),
  );
  fs.writeFileSync(
    path.join(dir, "index.mjs"),
    'throw Error("INSTALL_MUST_NOT_LOAD_PLUGIN");\n',
  );
  const tgz = path.join(root, `fixture-${version}.tgz`);
  execFileSync("tar", ["-czf", tgz, "-C", path.dirname(dir), "package"]);
  archives.set(version, fs.readFileSync(tgz));
}
let unavailable = false,
  stall = false,
  stalled = false;
const server = http.createServer((req, res) => {
  if (unavailable) {
    res.writeHead(503);
    res.end("fixture unavailable");
    return;
  }
  if (stall) {
    stalled = true;
    req.on("close", () => res.destroy());
    return;
  }
  const requested = versions.find((v) => req.url === `/${v}.tgz`);
  if (requested) {
    res.end(archives.get(requested));
    return;
  }
  const origin = `http://127.0.0.1:${server.address().port}`;
  res.setHeader("Content-Type", "application/json");
  res.end(
    JSON.stringify({
      name: packageName,
      "dist-tags": { latest: "1.0.2" },
      versions: Object.fromEntries(
        versions.map((version) => [
          version,
          {
            name: packageName,
            version,
            dist: {
              tarball: `${origin}/${version}.tgz`,
              integrity:
                "sha512-" +
                crypto
                  .createHash("sha512")
                  .update(archives.get(version))
                  .digest("base64"),
            },
          },
        ]),
      ),
    }),
  );
});
let proc,
  browser,
  page,
  origin,
  token,
  run = 0;
async function rpc(method, args = []) {
  const r = await fetch(origin + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
  });
  const value = await r.json();
  if (!r.ok) throw Error(value.error);
  return value;
}
async function stop() {
  if (!proc || proc.exitCode !== null) return;
  const exited = new Promise((r) => proc.once("exit", r));
  proc.kill("SIGTERM");
  await Promise.race([exited, pause(5000)]);
  if (proc.exitCode === null) {
    proc.kill("SIGKILL");
    await exited;
  }
}
async function start(version) {
  const file = path.join(root, `preview-${++run}`);
  const log = fs.openSync(path.join(root, `app-${run}.log`), "w");
  proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_PREVIEW: file,
      AICAT_E2E_OS_STATE: path.join(root, "os-state.json"),
      AICAT_E2E_PLUGIN_REGISTRY: `http://127.0.0.1:${server.address().port}`,
      AICAT_E2E_PLUGIN_VERSION: version,
    },
    stdio: ["ignore", log, log],
  });
  fs.closeSync(log);
  await wait(() => {
    if (proc.exitCode !== null) throw Error("app exited");
    return fs.existsSync(file);
  });
  const url = new URL(fs.readFileSync(file, "utf8"));
  origin = url.origin;
  token = url.searchParams.get("token");
  await wait(() => rpc("State"));
  await page.goto(url.href);
  await page.waitForFunction(() => !!window.desktop?.state());
  await page.locator("#nav-extensions").click();
  await page.locator("#extension-wecom").click();
}
async function state() {
  return (await rpc("State")).extensions.find((x) => x.id === "wecom");
}
const installedFile = path.join(home, "extensions/wecom/current.json");
async function main() {
  try {
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    const listener = net.createServer();
    await new Promise((r) => listener.listen(0, "127.0.0.1", r));
    const port = listener.address().port;
    await new Promise((r) => listener.close(r));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\nextensions:\n  wecom:\n    enabled: false\n    retained: keep-me\n  codex:\n    enabled: false\n`,
    );
    fs.writeFileSync(
      path.join(root, "os-state.json"),
      JSON.stringify({ services: [], trusted: false }),
    );
    browser = await chromium.launch({
      headless: true,
      executablePath:
        process.env.AICAT_CHROMIUM ||
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    });
    page = await browser.newPage({ viewport: { width: 1180, height: 900 } });
    page.setDefaultTimeout(20000);
    page.on("pageerror", (e) => report.errors.push(e.message));
    await start("1.0.0");
    await page.locator("#extension-information summary").click();
    const source = await page.locator("#extension-package-source").innerText();
    assert(source.includes(`http://127.0.0.1:${server.address().port}`));
    assert(source.includes(packageName + "@1.0.0"));
    stall = true;
    await page.locator("#extension-install").click();
    await wait(() => stalled);
    await wait(async () => (await state()).operation.stage === "downloading");
    await page.locator("#extension-install-status progress").waitFor();
    assert(await page.locator("#extension-install").isDisabled());
    assert(
      !((await page.locator("body").getAttribute("class")) || "")
        .split(" ")
        .includes("busy"),
    );
    await assert.rejects(
      Promise.race([
        rpc("InstallExtension", ["wecom"]),
        pause(1500).then(() => {
          throw Error("duplicate request blocked");
        }),
      ]),
      (error) => !error.message.includes("duplicate request blocked"),
    );
    await page.locator("#nav-overview").click();
    await page
      .getByRole("heading", { name: "网关运行中", exact: true })
      .waitFor();
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-wecom").click();
    await page.locator("#extension-install-cancel").waitFor();
    await wait(async () =>
      /已用 [1-9]\d* 秒/.test(
        await page.locator("#extension-install-status").innerText(),
      ),
    );
    await page.locator("#extension-information summary").click();
    assert(await page.locator("#extension-package-source").isVisible());
    const headingBox = await page.locator(".xd-head h1").boundingBox();
    for (const selector of ["#extension-install-status"]) {
      const box = await page.locator(selector).boundingBox();
      assert(Math.abs(box.x - headingBox.x) < 2 && box.width > 300, selector);
    }
    await page.screenshot({ path: path.join(root, "install-downloading.png") });
    await rpc("SetPreferences", [{ language: "en", theme: "dark" }]);
    await wait(async () =>
      (await page.locator("#extension-install-status").innerText()).includes(
        "Downloading plugin",
      ),
    );
    await page.setViewportSize({ width: 860, height: 900 });
    await page.screenshot({
      path: path.join(root, "install-downloading-en-dark.png"),
    });
    await page.locator("#extension-install-cancel").click();
    await wait(
      async () => (await state()).operation.status === "cancelled",
      5000,
    );
    await wait(async () =>
      (await page.locator("#extension-install-status").innerText()).includes(
        "Installation cancelled",
      ),
    );
    assert(!fs.existsSync(installedFile));
    assert(
      !fs
        .readdirSync(path.dirname(installedFile))
        .some((n) => n.startsWith(".install-")),
    );
    await rpc("SetPreferences", [{ language: "zh-Hans", theme: "light" }]);
    await page.setViewportSize({ width: 1180, height: 900 });
    stall = false;
    stalled = false;
    report.checks.push(
      "slow install shows real download stage, elapsed time and indeterminate progress in Chinese/light and English/dark; navigation stays responsive; state survives leaving and returning; duplicates fail promptly; UI cancellation stops Bun and cleans staging",
    );
    unavailable = true;
    await page.locator("#extension-install").click();
    await wait(async () => (await state()).operation.status === "failed");
    await page.locator("#extension-install-status summary").click();
    await page.locator("#extension-install-status pre").waitFor();
    assert(
      (
        await page.locator("#extension-install-status pre").innerText()
      ).includes("插件安装失败"),
    );
    await page.screenshot({ path: path.join(root, "install-failed.png") });
    assert(!fs.existsSync(installedFile));
    assert(
      !fs
        .readdirSync(path.dirname(installedFile))
        .some((n) => n.startsWith(".install-")),
    );
    unavailable = false;
    await page.locator("#extension-install").click();
    await wait(async () => (await state()).installed);
    await wait(async () => (await state()).operation.status === "completed");
    await wait(async () =>
      (await page.locator("#extension-install-status").innerText()).includes(
        "插件已安装",
      ),
    );
    assert.equal((await state()).installedVersion, "1.0.0");
    assert(
      !fs.existsSync(
        path.join(
          home,
          "extensions/wecom/1.0.0/node_modules",
          packageName,
          "INSTALL_SCRIPT_RAN",
        ),
      ),
    );
    assert.equal((await rpc("State")).messageChannels.wecom.pid, 0);
    const stamp = fs.readFileSync(installedFile, "utf8");
    await rpc("InstallExtension", ["wecom"]);
    assert.equal(fs.readFileSync(installedFile, "utf8"), stamp);
    report.checks.push(
      "registry failure cleans staging; retry installs specified NPM version without scripts or plugin execution; installation is idempotent",
    );
    assert.equal(
      await page.locator("#wecom-agent-binary,#wecom-agent-enabled").count(),
      0,
    );
    let config = await rpc("ReadExtensionConfig", ["wecom"]);
    assert.equal(config.retained, "keep-me");
    const before = fs.readFileSync(path.join(home, "aicat.yaml"), "utf8");
    for (const patch of [
      { enabled: true },
      { secret: "forbidden" },
      { agent: { binary: 5 } },
      { agent: { unexpected: true } },
    ])
      await assert.rejects(rpc("UpdateExtensionConfig", ["wecom", patch]));
    assert.equal(
      fs.readFileSync(path.join(home, "aicat.yaml"), "utf8"),
      before,
    );
    await assert.rejects(
      rpc("UpdateExtensionConfig", ["codex", { agent: { enabled: true } }]),
    );
    await assert.rejects(rpc("UpdateExtension", ["codex"]));
    await assert.rejects(rpc("UninstallExtension", ["codex"]));
    report.checks.push(
      "channel config preserves sibling fields and excludes execution settings, rejects reserved/secret/invalid fields without saving, and keeps built-in package operations unavailable",
    );
    fs.mkdirSync(path.join(home, "credentials"), { recursive: true });
    const credential = JSON.stringify({
      accountId: "retained-bot",
      secret: "retained-private-secret",
    });
    fs.writeFileSync(path.join(home, "credentials/wecom.json"), credential, {
      mode: 0o600,
    });
    fs.writeFileSync(
      path.join(home, "credentials/wecom.json.validated"),
      crypto
        .createHash("sha256")
        .update(
          credential +
            "\0" +
            "1.0.0" +
            "\0" +
            (process.env.AICAT_E2E_WECOM_URL || ""),
        )
        .digest("hex"),
      { mode: 0o600 },
    );
    await stop();
    await start("1.0.1");
    assert.equal((await state()).installedVersion, "1.0.0");
    assert.equal((await state()).updateAvailable, true);
    assert.equal((await rpc("State")).messageChannels.wecom.validated, true);
    assert.equal(
      (await rpc("ReadExtensionConfig", ["wecom"])).retained,
      "keep-me",
    );
    await page.locator("#extension-update").waitFor();
    unavailable = true;
    await page.locator("#extension-update").click();

    await wait(() => page.locator("#extension-update").isEnabled());
    assert.equal(fs.readFileSync(installedFile, "utf8"), stamp);
    assert.equal((await state()).installedVersion, "1.0.0");
    assert.equal((await rpc("State")).messageChannels.wecom.validated, true);
    unavailable = false;
    await page.locator("#extension-update").click();
    await wait(async () => (await state()).installedVersion === "1.0.1");
    assert.equal((await state()).enabled, false);
    assert.equal((await state()).updateAvailable, false);
    assert.equal((await rpc("State")).messageChannels.wecom.validated, false);
    assert(fs.existsSync(path.join(home, "extensions/wecom/1.0.0")));
    await page.screenshot({ path: path.join(root, "updated.png") });
    report.checks.push(
      "catalog target and installed version are separate after restart; failed update preserves old installation; UI update switches only after prepare and requires validation",
    );
    await stop();
    await start("1.0.2");
    stall = true;
    const request = rpc("UpdateExtension", ["wecom"]).catch(() => {});
    await wait(() => stalled);
    proc.kill("SIGKILL");
    await new Promise((r) => proc.once("exit", r));
    await request;
    stall = false;
    await start("1.0.2");
    assert.equal((await state()).installedVersion, "1.0.1");
    assert.equal((await state()).updateAvailable, true);
    report.checks.push(
      "process interruption during package prepare leaves the current version usable after restart",
    );
    fs.mkdirSync(path.join(home, "credentials"), { recursive: true });
    fs.writeFileSync(
      path.join(home, "credentials/wecom.json"),
      JSON.stringify({
        accountId: "retained-bot",
        secret: "retained-private-secret",
      }),
      { mode: 0o600 },
    );
    fs.mkdirSync(path.join(home, "state/wecom"), { recursive: true });
    fs.writeFileSync(
      path.join(home, "state/wecom/cursor.json"),
      "retained cursor",
    );
    fs.mkdirSync(path.join(home, "rooms/retained"), { recursive: true });
    fs.writeFileSync(
      path.join(home, "rooms/retained/messages.jsonl"),
      "retained message",
    );
    const blockedDirectory = path.join(
      home,
      "extensions/wecom/1.0.1/node_modules",
    );
    fs.chmodSync(blockedDirectory, 0);
    try {
      await assert.rejects(rpc("UninstallExtension", ["wecom"]));
      assert(!fs.existsSync(installedFile));
      assert.equal((await state()).installed, false);
      assert.equal((await state()).packagePresent, true);
    } finally {
      fs.chmodSync(blockedDirectory, 0o700);
    }
    await page.locator("#extension-menu").click();
    await page.locator("[data-action=extension-menu-uninstall]").click();
    await page
      .locator("#dialog")
      .getByRole("button", { name: "确定", exact: true })
      .click();
    await wait(async () => !(await state()).packagePresent);
    await page.locator("#extension-install").waitFor();
    assert(!fs.existsSync(path.join(home, "extensions/wecom")));
    for (const file of [
      "credentials/wecom.json",
      "state/wecom/cursor.json",
      "rooms/retained/messages.jsonl",
    ])
      assert(fs.existsSync(path.join(home, file)));
    assert.equal(
      (await rpc("ReadExtensionConfig", ["wecom"])).retained,
      "keep-me",
    );
    await rpc("UninstallExtension", ["wecom"]);
    await page.screenshot({ path: path.join(root, "uninstalled.png") });
    await stop();
    await start("1.0.2");
    assert.equal((await state()).installed, false);
    assert.equal((await state()).enabled, false);
    await page.locator("#extension-install").click();
    await wait(async () => (await state()).installedVersion === "1.0.2");
    assert.equal(
      (await rpc("ReadExtensionConfig", ["wecom"])).retained,
      "keep-me",
    );
    report.checks.push(
      "UI uninstall persists disabled state, removes all package versions, preserves configuration/credentials/state/Rooms, survives restart, retries cleanup after permission failure and allows reinstall",
    );
    assert.deepEqual(report.errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    await page
      ?.screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  } finally {
    await browser?.close();
    await stop();
    server.closeAllConnections();
    await new Promise((r) => server.close(r));
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify({ root, ...report }, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
