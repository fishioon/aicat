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
  process.argv[3] || "build/extension-source-" + Date.now(),
);
const home = path.join(root, "home");
fs.mkdirSync(home, { recursive: true, mode: 0o700 });
const sha = (data) => crypto.createHash("sha256").update(data).digest("hex");
const report = {
  passed: false,
  command: process.argv,
  binarySHA256: sha(fs.readFileSync(binary)),
  scenarioSHA256: sha(fs.readFileSync(__filename)),
  checks: [],
  errors: [],
  coverage:
    "Hidden native app and headless Chromium, real Desktop/package services and bundled Bun; local anonymous NPM, GitHub API and download fixtures; no real accounts, native window input or live GitHub installation",
};
const archives = new Map();
function archive(
  key,
  {
    id = "fixture.source",
    name = "@fixture/source-channel",
    version = "1.0.0",
    api = 1,
    prefix = "package",
    entry = "index.mjs",
    marker = key,
  } = {},
) {
  const base = path.join(root, "fixtures", key);
  const dir = path.join(base, prefix);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(
    path.join(dir, "package.json"),
    JSON.stringify({
      name,
      version,
      description: "通用来源安装夹具",
      type: "module",
      scripts: {
        postinstall: "touch INSTALL_SCRIPT_RAN",
        prepare: "touch PREPARE_SCRIPT_RAN",
      },
      aicat: {
        api,
        id,
        entry,
        identityFields: [],
        fields: [],
        capabilities: {},
        room: { trigger: { chat: { enabled: true } } },
      },
    }),
  );
  fs.writeFileSync(
    path.join(dir, "index.mjs"),
    'throw Error("INSTALL_MUST_NOT_EXECUTE_EXTENSION");\n',
  );
  fs.writeFileSync(path.join(dir, "content.txt"), marker);
  const file = path.join(root, key + ".tgz");
  execFileSync("tar", ["-czf", file, "-C", base, prefix], {
    env: { ...process.env, COPYFILE_DISABLE: "1" },
  });
  archives.set(key, fs.readFileSync(file));
}
archive("npm-v1");
archive("npm-v2", { version: "1.1.0" });
archive("github-v1", {
  id: "fixture.github",
  name: "@fixture/github-channel",
  prefix: "repository-" + "a".repeat(40),
  marker: "first commit",
});
archive("github-v2", {
  id: "fixture.github",
  name: "@fixture/github-channel",
  prefix: "repository-" + "b".repeat(40),
  marker: "second commit, same package version",
});
archive("archive", { id: "fixture.archive", name: "@fixture/archive-channel" });
archive("slow", { id: "fixture.slow", name: "@fixture/slow-channel" });
archive("invalid", {
  id: "fixture.invalid",
  name: "@fixture/invalid-channel",
  api: 99,
});
archive("collision", { id: "codex", name: "@fixture/collision" });
archive("identity", {
  id: "fixture.archive",
  name: "@fixture/different-package",
});
archive("wecom-local", { id: "wecom-local", name: "@aicat/wecom-local", version: "0.1.0" });
const malicious = path.join(root, "unsafe.tgz");
execFileSync("python3", [
  "-c",
  "import tarfile,sys; t=tarfile.open(sys.argv[1],'w:gz'); h=tarfile.TarInfo('package/escape'); h.type=tarfile.SYMTYPE; h.linkname='../../outside'; t.addfile(h); t.close()",
  malicious,
]);
archives.set("unsafe", fs.readFileSync(malicious));
let registry,
  server,
  proc,
  browser,
  page,
  origin,
  token,
  run = 0;
let slow = true,
  githubRevision = "a".repeat(40),
  githubFailure = false,
  npmFailure = false,
  integrityFailure = false;
const requests = [];
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function wait(fn, timeout = 25000) {
  const until = Date.now() + timeout;
  let last;
  while (Date.now() < until) {
    try {
      const result = await fn();
      if (result) return result;
    } catch (error) {
      last = error;
    }
    await sleep(50);
  }
  throw Error("timeout: " + (last?.stack || fn.toString()));
}
async function rpc(method, ...args) {
  const response = await fetch(origin + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
  });
  const value = await response.json();
  if (!response.ok) throw Error(value.error);
  return value;
}
const state = () => rpc("State");
const extension = async (id) =>
  (await state()).extensions.find((item) => item.id === id);
const currentFile = (id) => path.join(home, "extensions", id, "current.json");
const installedPath = (id) => {
  const record = JSON.parse(fs.readFileSync(currentFile(id), "utf8"));
  return path.join(home, "extensions", id, record.directory || record.version);
};
async function stop() {
  if (!proc || proc.exitCode !== null) return;
  const child = proc;
  const exited = new Promise((resolve) => child.once("exit", resolve));
  child.kill("SIGTERM");
  await Promise.race([exited, sleep(8000)]);
  if (child.exitCode === null) {
    child.kill("SIGKILL");
    await exited;
  }
  assert.equal(child.exitCode, 0, "background app exits cleanly");
  proc = undefined;
}
async function start() {
  const preview = path.join(root, "preview-" + ++run);
  const osState = path.join(root, "os-state.json");
  fs.writeFileSync(
    osState,
    JSON.stringify({ services: [], trusted: false, recordProxyWrites: true }),
  );
  const reserve = net.createServer();
  await new Promise((resolve) => reserve.listen(0, "127.0.0.1", resolve));
  const port = reserve.address().port;
  await new Promise((resolve) => reserve.close(resolve));
  const log = fs.openSync(path.join(root, "app-" + run + ".log"), "w");
  proc = spawn(binary, ["--home", home, "--addr", "127.0.0.1:" + port], {
    env: {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_OS_STATE: osState,
      AICAT_E2E_PREVIEW: preview,
      AICAT_E2E_PLUGIN_REGISTRY: registry,
      AICAT_E2E_GITHUB_URL: registry + "/github",
    },
    stdio: ["ignore", log, log],
  });
  fs.closeSync(log);
  await wait(() => {
    assert.equal(proc.exitCode, null);
    return fs.existsSync(preview);
  });
  const address = new URL(fs.readFileSync(preview, "utf8"));
  origin = address.origin;
  token = address.searchParams.get("token");
  await wait(async () => !!(await state()).extensions.length);
  await page.goto(address.href);
  await page.locator("#nav-extensions").waitFor();
  await rpc("SetPreferences", { language: "zh-Hans", theme: "light" });
}
async function openSource(source, version = "") {
  await page.locator("#nav-extensions").click();
  await page.locator("#channel-package-add").click();
  await page.locator("#custom-source").fill(source);
  await wait(() => page.locator("#custom-install").isEnabled());
  const more = page.locator("#custom-more-options");
  if (await more.count()) {
    if (!(await more.getAttribute("open")))
      await more.locator("summary").click();
    await page.locator("#custom-version").fill(version);
    await more.locator("summary").click();
  }
}
async function install(source, id, version = "") {
  await openSource(source, version);
  await page.locator("#custom-install").click();
  await page
    .locator(`.extension-detail[data-extension="${id}"]`)
    .waitFor({ timeout: 30000 });
  assert.equal((await extension(id)).installed, true);
  assert.equal((await extension(id)).enabled, false);
}
async function rejection(source, expected, version = "") {
  await assert.rejects(
    rpc("InstallExtensionSource", { source, version }),
    expected,
  );
}
function assertClean() {
  assert(
    !fs
      .readdirSync(path.join(home, "extensions"))
      .some((name) => name.startsWith(".source-")),
  );
}
async function main() {
  server = http.createServer((req, res) => {
    const url = new URL(req.url, registry || "http://127.0.0.1");
    requests.push({
      pathname: url.pathname,
      authorization: req.headers.authorization,
      cookie: req.headers.cookie,
      referer: req.headers.referer,
    });
    const reject = (status) => {
      res.writeHead(status);
      res.end("fixture error must not expose a source query");
    };
    const json = (value) => {
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify(value));
    };
    if (url.pathname === "/unauthorized") return reject(401);
    if (url.pathname === "/forbidden") return reject(403);
    if (url.pathname === "/not-found") return reject(404);
    if (url.pathname === "/redirect-auth") {
      res.writeHead(302, { Location: registry + "/unauthorized" });
      return res.end();
    }
    if (url.pathname === "/signed-redirect") {
      res.writeHead(302, { Location: registry + "/archive.tgz" });
      return res.end();
    }
    if (url.pathname === "/signed") {
      if (url.searchParams.get("signature") !== "fixture-signed-secret")
        return reject(403);
      return res.end(archives.get("archive"));
    }
    if (url.pathname.startsWith("/github/")) {
      if (githubFailure) return reject(403);
      if (url.pathname.includes("/commits/"))
        return json({ sha: githubRevision });
      if (url.pathname.includes("/tarball/")) {
        res.writeHead(302, {
          Location:
            registry +
            "/github-download/" +
            (url.pathname.endsWith("b".repeat(40)) ? "github-v2" : "github-v1"),
        });
        return res.end();
      }
      return reject(404);
    }
    if (url.pathname.startsWith("/github-download/"))
      return res.end(archives.get(url.pathname.split("/").pop()));
    const key = url.pathname.replace(/^\//, "").replace(/\.tgz$/, "");
    if (archives.has(key)) {
      if (key === "slow" && slow) {
        req.on("close", () => res.destroy());
        return;
      }
      return res.end(archives.get(key));
    }
    if (url.pathname.includes("source-channel")) {
      if (npmFailure) return reject(503);
      const version = url.pathname.endsWith("/1.0.0") ? "1.0.0" : "1.1.0";
      const archiveKey = version === "1.0.0" ? "npm-v1" : "npm-v2";
      return json({
        name: "@fixture/source-channel",
        version,
        dist: {
          tarball: registry + "/" + archiveKey + ".tgz",
          integrity:
            "sha512-" +
            crypto
              .createHash("sha512")
              .update(integrityFailure ? "incorrect" : archives.get(archiveKey))
              .digest("base64"),
        },
      });
    }
    reject(404);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  registry = "http://127.0.0.1:" + server.address().port;
  browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AICAT_CHROMIUM ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  page = await browser.newPage({ viewport: { width: 1180, height: 800 } });
  page.on("pageerror", (error) => report.errors.push(error.message));
  try {
    await start();
    await page.locator("#nav-extensions").click();
    const side = await page.locator("#module-sidebar").boundingBox();
    const entry = await page.locator("#channel-package-add").boundingBox();
    assert.equal(entry.width, side.width - 17);
    assert(entry.y > side.y + side.height - 65);
    assert.equal(
      await page
        .locator("#channel-package-add")
        .evaluate((el) => getComputedStyle(el).borderWidth),
      "0px",
    );
    await page.locator("#channel-package-add").click();
    assert.equal(await page.locator("#custom-source").count(), 1);
    assert.equal(
      await page.locator("#custom-more-options").getAttribute("open"),
      null,
    );
    assert.equal(await page.locator('input[type="password"]').count(), 0);
    assert.equal(await page.locator("#custom-install").isDisabled(), true);
    await page.screenshot({ path: path.join(root, "source-form.png") });
    report.checks.push(
      "侧栏底部整行入口、唯一来源框、更多选项默认折叠、无凭据输入及空值禁用",
    );

    await install("@fixture/source-channel", "fixture.source", "1.0.0");
    assert.equal((await extension("fixture.source")).installedVersion, "1.0.0");
    await openSource("@fixture/source-channel");
    assert.match(await page.locator("#custom-source-hint").innerText(), /NPM/);
    await page.screenshot({ path: path.join(root, "npm-recognized.png") });
    await page.locator("#custom-install").click();
    await page
      .locator('.extension-detail[data-extension="fixture.source"]')
      .waitFor({ timeout: 30000 });
    assert.equal((await extension("fixture.source")).installedVersion, "1.1.0");
    assert(fs.existsSync(path.join(home, "extensions/fixture.source")));
    report.checks.push(
      "公开 NPM 名称自动识别；更多选项固定版本安装，空版本解析 latest；使用包声明中的真实 ID 登记",
    );

    await install("github:fixture/channel", "fixture.github");
    assert(requests.some((r) => r.pathname.endsWith("/commits/HEAD")));
    const firstGit = fs.readFileSync(currentFile("fixture.github"), "utf8");
    const firstDir = installedPath("fixture.github");
    githubFailure = true;
    await assert.rejects(
      rpc("UpdateExtension", "fixture.github"),
      /不支持安装凭据/,
    );
    assert.equal(
      fs.readFileSync(currentFile("fixture.github"), "utf8"),
      firstGit,
    );
    githubFailure = false;
    githubRevision = "b".repeat(40);
    await rpc("UpdateExtension", "fixture.github");
    assert.equal(
      JSON.parse(fs.readFileSync(currentFile("fixture.github"), "utf8"))
        .revision,
      githubRevision,
    );
    assert.notEqual(installedPath("fixture.github"), firstDir);
    assert(fs.existsSync(firstDir));
    assert.equal(
      fs.readFileSync(
        path.join(
          installedPath("fixture.github"),
          "node_modules/@fixture/github-channel/content.txt",
        ),
        "utf8",
      ),
      "second commit, same package version",
    );
    await install(
      "https://github.com/fixture/channel.git#release",
      "fixture.github",
    );
    assert(requests.some((r) => r.pathname.endsWith("/commits/release")));
    report.checks.push(
      "GitHub 简写、HTTPS、.git 和 #ref 识别，默认分支经公开 API 固定 commit 后下载；失败更新保留旧版，相同包版本的新 commit 使用独立目录原子切换",
    );

    const signed = registry + "/signed?signature=fixture-signed-secret";
    await install(signed, "fixture.archive");
    await page.locator("#extension-information summary").click();
    assert(
      !(await page.locator("#extension-package-source").innerText()).includes(
        "fixture-signed-secret",
      ),
    );
    assert.equal((await extension("fixture.archive")).packageSource, "archive");
    await page.screenshot({ path: path.join(root, "archive-installed.png") });
    await openSource(signed);
    assert.equal(await page.locator("#custom-more-options").count(), 0);
    await page.screenshot({ path: path.join(root, "archive-recognized.png") });
    report.checks.push(
      "不要求 .tgz 后缀的安装包地址与临时授权链接可用；安装包不显示独立版本字段，界面来源不暴露查询参数",
    );

    await rpc("InstallExtensionSource", {
      source: registry + "/signed-redirect?signature=fixture-signed-secret",
      version: "",
    });

    const before = fs.readFileSync(
      path.join(home, "channel-catalog.json"),
      "utf8",
    );
    const stable = fs.readFileSync(currentFile("fixture.archive"), "utf8");
    for (const code of ["unauthorized", "forbidden", "redirect-auth"])
      await rejection(registry + "/" + code, /不支持安装凭据/);
    await rejection(registry + "/not-found", /不可公开访问/);
    await rejection(registry + "/invalid.tgz", /渠道 API/);
    await rejection(registry + "/collision.tgz", /内置扩展/);
    await rejection(registry + "/identity.tgz", /扩展标识/);
    await rejection(registry + "/unsafe.tgz", /越界路径/);
    await rejection(signed, /单独指定版本/, "1.0.0");
    await rejection("@fixture/source-channel@1.0.0", /版本不一致/, "1.1.0");
    await rejection(
      "https://user:password@github.com/fixture/channel",
      /账号密码/,
    );
    assert.equal(
      (
        await rpc("ParseExtensionSource", {
          source: "http://extensions.internal/channel.tgz",
          version: "",
        })
      ).kind,
      "archive",
    );
    await rejection("git@github.com:fixture/channel.git", /不支持安装凭据/);
    integrityFailure = true;
    await rejection("@fixture/source-channel", /完整性/);
    integrityFailure = false;
    assert.equal(
      fs.readFileSync(path.join(home, "channel-catalog.json"), "utf8"),
      before,
    );
    assert.equal(
      fs.readFileSync(currentFile("fixture.archive"), "utf8"),
      stable,
    );
    assertClean();
    report.checks.push(
      "401/403、重定向后认证、404、无效 API、内置 ID 与包身份冲突、越界链接、版本冲突及完整性错误拒绝；不污染目录或覆盖原安装",
    );

    await openSource(registry + "/unauthorized");
    await page.locator("#custom-install").click();
    await wait(async () =>
      /不支持安装凭据/.test(
        await page.locator("#custom-install-error").innerText(),
      ),
    );
    assert.match(
      await page.locator("#custom-install-error").innerText(),
      /不支持安装凭据/,
    );
    await page.screenshot({
      path: path.join(root, "authentication-not-supported.png"),
    });
    await openSource(registry + "/slow.tgz");
    await page.locator("#custom-install").click();
    await page.locator("#custom-install-cancel").waitFor();
    await assert.rejects(
      rpc("InstallExtensionSource", {
        source: "@fixture/source-channel",
        version: "",
      }),
      /登记其他渠道包|安装操作/,
    );
    await page.locator("#nav-settings").click();
    await page.locator("#nav-extensions").click();
    await page.locator("#channel-package-add").click();
    await page.locator("#custom-install-cancel").waitFor();
    await page.screenshot({ path: path.join(root, "source-downloading.png") });
    await page.locator("#custom-install-cancel").click();
    await wait(
      async () => (await state()).extensionInstallation.status === "cancelled",
    );
    await wait(() => page.locator("#custom-install").isEnabled());
    assertClean();
    slow = false;
    await page.locator("#custom-install").click();
    await page
      .locator('.extension-detail[data-extension="fixture.slow"]')
      .waitFor({ timeout: 30000 });
    report.checks.push(
      "认证失败在表单内明确提示；慢下载期间可导航，返回后显示真实进度和取消；重复提交拒绝，取消后可重试且暂存清理",
    );

    await install(registry + "/wecom-local.tgz", "wecom-local");
    assert.equal(
      (await state()).extensions.filter((x) => x.id === "wecom-local").length,
      1,
    );
    assert.equal((await extension("wecom-local")).name, "企业微信客户端");
    await stop();
    await start();
    for (const id of [
      "fixture.source",
      "fixture.github",
      "fixture.archive",
      "fixture.slow",
      "wecom-local",
    ])
      assert.equal((await extension(id)).installed, true, id);
    assert.equal((await extension("wecom-local")).packageSource, "archive");
    assert.equal(
      (await state()).extensions.filter((x) => x.id === "wecom-local").length,
      1,
    );
    const catalogMode =
      fs.statSync(path.join(home, "channel-catalog.json")).mode & 0o777;
    assert.equal(catalogMode, 0o600);
    await rpc("UninstallExtension", "fixture.archive");
    assert.equal((await extension("fixture.archive")).installed, false);
    await rpc("InstallExtension", "fixture.archive");
    assert.equal((await extension("fixture.archive")).installed, true);
    report.checks.push(
      "同身份推荐扩展可从指定地址安装而不产生重复条目，名称保持目录声明；登记与来源重启恢复，卸载后可由原来源重装，目录权限 0600",
    );

    for (const id of [
      "fixture.source",
      "fixture.github",
      "fixture.archive",
      "fixture.slow",
      "wecom-local",
    ]) {
      const dir = installedPath(id);
      const files = execFileSync(
        "find",
        [
          dir,
          "-name",
          "INSTALL_SCRIPT_RAN",
          "-o",
          "-name",
          "PREPARE_SCRIPT_RAN",
        ],
        { encoding: "utf8" },
      );
      assert.equal(files.trim(), "");
    }
    assert(
      requests.every(
        (request) =>
          !request.authorization &&
          !request.cookie &&
          !request.referer?.includes("fixture-signed-secret"),
      ),
    );
    for (let i = 1; i <= run; i++)
      assert(
        !fs
          .readFileSync(path.join(root, "app-" + i + ".log"), "utf8")
          .includes("fixture-signed-secret"),
      );
    for (let i = 1; i <= run; i++)
      assert(
        !fs
          .readFileSync(path.join(root, "app-" + i + ".log"), "utf8")
          .includes("DATA RACE"),
      );
    report.checks.push(
      "安装与解析不执行扩展入口及生命周期脚本，所有下载不携带 Authorization/Cookie，重定向和日志不泄露临时授权查询值",
    );

    await rpc("SetPreferences", { language: "en", theme: "dark" });
    await page.setViewportSize({ width: 860, height: 800 });
    await openSource(registry + "/unauthorized");
    await page.locator("#custom-install").click();
    await wait(async () =>
      /credentials are not supported/.test(
        await page.locator("#custom-install-error").innerText(),
      ),
    );
    await openSource("https://github.com/fixture/channel");
    assert.equal(
      await page.locator("#channel-package-add").innerText(),
      "Custom install",
    );
    assert.equal(
      await page.locator("#custom-more-options").getAttribute("open"),
      null,
    );
    await page.screenshot({ path: path.join(root, "source-en-dark-860.png") });
    const width = await page.locator("#custom-source").boundingBox();
    const installButton = await page.locator("#custom-install").boundingBox();
    assert(installButton.x + installButton.width <= 860);
    assert(width.width > 150);
    assert.deepEqual(report.errors, []);
    report.checks.push(
      "英文深色及 860px 窗口可用，输入与操作不溢出；无浏览器脚本错误",
    );
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
    await page
      ?.screenshot({ path: path.join(root, "failure.png") })
      .catch(() => {});
  } finally {
    await browser?.close();
    try {
      await stop();
    } catch (error) {
      report.passed = false;
      report.cleanupError = error.stack;
    }
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
    fs.writeFileSync(
      path.join(root, "requests.json"),
      JSON.stringify(requests, null, 2),
    );
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify({ root, ...report }, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
}
main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
