const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  assert = require("node:assert/strict"),
  { spawn } = require("node:child_process");
const root = path.resolve(process.argv[2] || "build/channel-install-failure");
fs.mkdirSync(root, { recursive: true });
const home = path.join(root, "home"),
  binary = path.resolve("bin/install-plugin-e2e");
const report = { passed: false, checks: [] };
const server = http.createServer((req, res) => {
  res.writeHead(404, { "Content-Type": "application/json" });
  res.end('{"error":"not found"}');
});
async function run(registry) {
  return new Promise((resolve) => {
    const child = spawn(binary, [home], {
      env: {
        ...process.env,
        ...(registry ? { AICAT_E2E_PLUGIN_REGISTRY: registry } : {}),
      },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let output = "";
    child.stdout.on("data", (x) => (output += x));
    child.stderr.on("data", (x) => (output += x));
    child.on("exit", (code) => resolve({ code, output }));
  });
}
(async () => {
  try {
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const failed = await run("http://127.0.0.1:" + server.address().port);
    fs.writeFileSync(path.join(root, "failed.log"), failed.output);
    assert.notEqual(failed.code, 0);
    const parent = path.join(home, "extensions/wecom"),
      dir = path.join(parent, "2026.10.30");
    assert(!fs.existsSync(dir));
    assert(!fs.readdirSync(parent).some((x) => x.startsWith(".install-")));
    report.checks.push(
      "registry failure leaves no installed package or staging directory",
    );
    const success = await run();
    fs.writeFileSync(path.join(root, "installed.log"), success.output);
    assert.equal(success.code, 0, success.output);
    assert(!fs.existsSync(path.join(home, "state/channel-host")));
    report.checks.push(
      "retry installs original official package without starting host",
    );
    const stamp = path.join(dir, "installation.json"),
      before = fs.readFileSync(stamp, "utf8");
    assert.equal((await run()).code, 0);
    assert.equal(fs.readFileSync(stamp, "utf8"), before);
    report.checks.push("repeated installation is idempotent");
    const compatibility = path.join(dir, "node_modules/openclaw");
    assert.equal(
      fs.realpathSync(compatibility),
      fs.realpathSync("bin/channel-host/shim"),
    );
    fs.rmSync(compatibility, { recursive: true, force: true });
    fs.cpSync("bin/channel-host/shim", compatibility, { recursive: true });
    fs.appendFileSync(
      path.join(compatibility, "sdk.mjs"),
      "\nthrow Error('STALE_COPY');\n",
    );
    assert.equal(
      (await run("http://127.0.0.1:" + server.address().port)).code,
      0,
    );
    assert.equal(
      fs.realpathSync(compatibility),
      fs.realpathSync("bin/channel-host/shim"),
    );
    assert.equal(fs.readFileSync(stamp, "utf8"), before);
    report.checks.push(
      "legacy copied shim is rebound to the app's shared SDK without registry access or package reinstallation",
    );
    fs.writeFileSync(stamp, "{}");
    const repaired = await run();
    assert.equal(repaired.code, 0, repaired.output);
    assert.equal(JSON.parse(fs.readFileSync(stamp)).version, "2026.10.30");
    assert(!fs.readdirSync(parent).some((x) => x.startsWith(".replaced-")));
    report.checks.push(
      "invalid installation metadata can be repaired with a fresh atomic installation",
    );
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
    await new Promise((resolve) => server.close(resolve));
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    console.log(JSON.stringify(report, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
