/* Focused scenario for 接入供应商: aicat holds a vendor key and forwards local
 * agents' requests with it.
 * Failure modes: key written to aicat.yaml or a world-readable file; key
 * echoed to the page or leaked into traffic records; the agent's own
 * credential reaching the vendor; plain-http endpoints accepted; routes
 * answering before a provider is saved or after it is removed; streamed
 * responses buffered or not recorded; browser origins accepted; a providers.json
 * from before providers were named lost on upgrade; an address typed without
 * /v1 forwarding to the wrong path; a refused key shown as a working provider;
 * two providers answering on each other's path.
 * Usage: node e2e/provider.cjs bin/aicat-e2e build/provider-e2e
 */
const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  net = require("node:net");
const { spawn } = require("node:child_process");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");
const assert = require("node:assert/strict");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function wait(fn, ms = 15000) {
  const until = Date.now() + ms;
  let last;
  while (Date.now() < until) {
    try {
      const v = await fn();
      if (v) return v;
    } catch (e) {
      last = e;
    }
    await sleep(50);
  }
  throw new Error("timeout " + (last?.stack || ""));
}
const binary = path.resolve(process.argv[2] || "bin/aicat-e2e");
const root = path.resolve(process.argv[3] || "build/provider-e2e");
fs.rmSync(root, { recursive: true, force: true });
fs.mkdirSync(path.join(root, "home"), { recursive: true, mode: 0o700 });
const home = path.join(root, "home"),
  previewFile = path.join(root, "preview-url"),
  stateFile = path.join(root, "os-state.json");
const KEY = "sk-ant-e2e-HELD-BY-AICAT-9f3a";
const LEGACY_KEY = "sk-legacy-e2e-HELD-BY-AICAT-71c2";
const WRONG_KEY = "nc_ai_another-vendors-key-0000";
const AGENT_KEY = "agent-placeholder-must-not-leave";

function call(port, url, { method = "GET", body = "", headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const r = http.request(
      { host: "127.0.0.1", port, path: url, method, headers },
      (res) => {
        const chunks = [];
        res.on("data", (b) => chunks.push({ at: Date.now(), text: String(b) }));
        res.on("end", () =>
          resolve({
            status: res.statusCode,
            headers: res.headers,
            chunks,
            text: chunks.map((c) => c.text).join(""),
          }),
        );
      },
    );
    r.on("error", reject);
    r.end(body);
  });
}

(async () => {
  const report = { passed: false, checks: [] };
  const check = (name) => report.checks.push(name);
  const seen = [];
  // Saving a provider also asks for its model list; these are the forwarded
  // agent requests.
  const messages = () => seen.filter((s) => s.url.startsWith("/v1/messages"));
  const upstream = http.createServer((req, res) => {
    const chunks = [];
    req.on("data", (b) => chunks.push(b));
    req.on("end", async () => {
      seen.push({
        url: req.url,
        headers: req.headers,
        body: Buffer.concat(chunks).toString(),
      });
      if (
        req.headers["x-api-key"] !== KEY &&
        req.headers.authorization !== "Bearer " + LEGACY_KEY
      ) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end('{"error":{"message":"bad key"}}');
        return;
      }
      // The model list lives under /v1 only.
      if (req.method === "GET") {
        const found = req.url === "/v1/models";
        res.writeHead(found ? 200 : 404, {
          "Content-Type": "application/json",
        });
        res.end(found ? '{"data":[{"id":"claude-test"}]}' : "{}");
        return;
      }
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.write('event: message_start\ndata: {"type":"message_start"}\n\n');
      await sleep(400);
      res.end('event: message_stop\ndata: {"type":"message_stop"}\n\n');
    });
  });
  await new Promise((r) => upstream.listen(0, "127.0.0.1", r));
  const fixture = upstream.address().port;
  const temp = net.createServer();
  await new Promise((r) => temp.listen(0, "127.0.0.1", r));
  const port = temp.address().port;
  await new Promise((r) => temp.close(r));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\n`,
  );
  // A file from before providers were named: one entry per vendor kind, and
  // an address typed without /v1.
  fs.writeFileSync(
    path.join(home, "providers.json"),
    JSON.stringify({
      openai: { baseUrl: `http://127.0.0.1:${fixture}`, key: LEGACY_KEY },
    }),
    { mode: 0o600 },
  );
  fs.writeFileSync(
    stateFile,
    JSON.stringify({ services: [], trusted: false, recordProxyWrites: true }),
    { mode: 0o600 },
  );
  const log = fs.openSync(path.join(root, "app.log"), "a");
  const proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
      // Preview-only scenario: keep the native window out of the way.
      AICAT_E2E_BACKGROUND: "1",
    },
    stdio: ["ignore", log, log],
  });
  const browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AICAT_CHROMIUM ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  const errors = [];
  const secrets = path.join(home, "providers.json");
  const message = () =>
    call(port, "/p/anthropic/v1/messages?beta=true", {
      method: "POST",
      body: '{"model":"claude-test","stream":true}',
      headers: {
        "Content-Type": "application/json",
        "x-api-key": AGENT_KEY,
        Authorization: "Bearer " + AGENT_KEY,
        "anthropic-version": "2023-06-01",
      },
    });
  try {
    await wait(() => fs.existsSync(previewFile));
    const page = await browser.newPage({
      viewport: { width: 1180, height: 820 },
    });
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(fs.readFileSync(previewFile, "utf8"));
    await page.waitForFunction(() => !!window.desktop?.state());
    const shot = (name) =>
      page.screenshot({ path: path.join(root, name + ".png") });
    const state = () => page.evaluate(() => window.desktop.state());

    // Nothing is routed before a provider is saved.
    assert.equal((await message()).status, 503);
    assert.equal(messages().length, 0);
    check("unsaved provider is not routed");

    // The old file is read as a named provider; its model list answered
    // under /v1 only, so the address gains it and requests follow.
    await wait(
      async () => (await state()).providers[0]?.models.models.length === 1,
    );
    const legacy = (await state()).providers[0];
    assert.deepEqual(
      [legacy.id, legacy.name, legacy.protocol, legacy.baseUrl, legacy.mount],
      [
        "openai",
        "OpenAI",
        "openai",
        `http://127.0.0.1:${fixture}/v1`,
        "/p/openai",
      ],
    );
    assert.equal(
      JSON.parse(fs.readFileSync(secrets)).providers[0].id,
      "openai",
    );
    const chat = await call(port, "/p/openai/chat/completions", {
      method: "POST",
      body: "{}",
      headers: { Authorization: "Bearer " + AGENT_KEY },
    });
    assert.equal(chat.status, 200);
    assert.equal(seen.at(-1).url, "/v1/chat/completions");
    assert.equal(seen.at(-1).headers.authorization, "Bearer " + LEGACY_KEY);
    assert.equal(seen.at(-1).headers["x-api-key"], undefined);
    // The paths of the three vendor kinds are gone.
    const gone = seen.length;
    assert.equal(
      (await call(port, "/openai/v1/chat/completions", { method: "POST" }))
        .status,
      404,
    );
    assert.equal(seen.length, gone);
    check("legacy providers.json kept as a named provider, /v1 fixed");

    // A preset fills in the vendor's address; only the key is left.
    await page.locator("#connect-provider").click();
    await page.locator("#provider-presets").waitFor();
    await shot("provider-presets-light");
    await page.locator("#preset-anthropic").click();
    assert.equal(
      await page.locator("#provider-name").inputValue(),
      "Anthropic",
    );
    assert.equal(
      await page.locator("#provider-url").inputValue(),
      "https://api.anthropic.com",
    );
    assert.equal(await page.locator("#provider-save").isDisabled(), true);

    // A key must not travel in the clear to another machine.
    await page.locator("#provider-url").fill("http://example.com");
    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await page.locator("#notice.bad").waitFor();
    assert(!fs.readFileSync(secrets, "utf8").includes(KEY));
    assert.equal((await state()).providers.length, 1);
    check("plain http endpoint rejected");

    // A key the vendor refuses is saved, and said to be refused: the agent
    // step is not offered as ready.
    const anthropic = async () =>
      (await state()).providers.find((p) => p.id === "anthropic");
    await page.locator("#provider-url").fill(`http://127.0.0.1:${fixture}/`);
    await page.locator("#provider-key").fill(WRONG_KEY);
    await page.locator("#provider-save").click();
    await wait(async () => (await anthropic())?.models.errorKind === "auth");
    await page.locator("#provider-status.bad").waitFor();
    assert.equal(
      await page.locator("li.step.now #provider-snippet").count(),
      0,
    );
    assert.equal((await message()).status, 401);
    await shot("provider-refused-light");
    await page.locator("#nav-overview").click();
    await page
      .locator("#overview-apps .provider .bad", { hasText: "密钥无效" })
      .waitFor();
    await page
      .locator("#overview-apps .provider")
      .filter({ hasText: "Anthropic" })
      .getByText("管理")
      .click();
    check("refused key reported, agent step held back");

    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await wait(async () => (await anthropic()).models.models.length === 1);
    await page.locator("#provider-status.ok").waitFor();
    await page.locator("li.step.now #provider-snippet").waitFor();
    // The model list fields are covered by e2e/codex.cjs.
    const { id, name, preset, protocol, baseUrl, mount, keyHint, responses } =
      await anthropic();
    assert.deepEqual(
      { id, name, preset, protocol, baseUrl, mount, keyHint, responses },
      {
        id: "anthropic",
        name: "Anthropic",
        preset: "anthropic",
        protocol: "anthropic",
        baseUrl: `http://127.0.0.1:${fixture}`,
        mount: "/p/anthropic",
        keyHint: "9f3a",
        responses: false,
      },
    );
    assert.equal(await page.locator("#provider-key").inputValue(), "");
    assert.match(
      await page.locator("#provider-snippet").innerText(),
      new RegExp(
        `ANTHROPIC_BASE_URL=http://127\\.0\\.0\\.1:${port}/p/anthropic`,
      ),
    );
    await shot("provider-saved-light");

    // The key lives only in the 0600 file: not in the config, not in the page.
    assert.equal(fs.statSync(secrets).mode & 0o777, 0o600);
    assert(fs.readFileSync(secrets, "utf8").includes(KEY));
    assert(
      !fs.readFileSync(path.join(home, "aicat.yaml"), "utf8").includes(KEY),
    );
    assert(!JSON.stringify(await state()).includes(KEY));
    assert(!(await page.content()).includes(KEY));
    check("key stored in 0600 providers.json only");

    // Forwarding: the saved key replaces whatever the agent sent, and the
    // stream arrives as it is produced.
    const answer = await message();
    assert.equal(answer.status, 200);
    assert.match(answer.text, /message_start[\s\S]*message_stop/);
    assert(
      answer.chunks.at(-1).at - answer.chunks[0].at >= 200,
      "streamed response was buffered",
    );
    const sent = messages().at(-1);
    // The model list sync on save used the saved key the same way.
    const listed = seen.find(
      (s) => s.url === "/v1/models" && s.headers["x-api-key"] === KEY,
    );
    assert(listed, "saving did not sync the model list");
    assert.equal(sent.url, "/v1/messages?beta=true");
    assert.equal(sent.headers["x-api-key"], KEY);
    assert.equal(sent.headers.authorization, undefined);
    assert.equal(sent.headers["anthropic-version"], "2023-06-01");
    assert.equal(sent.body, '{"model":"claude-test","stream":true}');
    check("request forwarded with the saved key, streamed");

    // Browser pages may not use the route.
    assert.equal(
      (
        await call(port, "/p/anthropic/v1/messages", {
          method: "POST",
          headers: { Origin: "https://evil.invalid" },
        })
      ).status,
      403,
    );
    check("browser origin denied");

    // The exchange is in the traffic records, without either credential.
    const items = JSON.parse((await call(port, "/api/traffic")).text).items;
    const item = items.find(
      (row) => row.url.includes("/v1/messages") && row.statusCode === 200,
    );
    assert(item, "forwarded request not recorded");
    assert.equal(item.statusCode, 200);
    assert.equal(item.state, "completed");
    assert.equal(item.contentType, "text/event-stream");
    const record = (await call(port, "/api/traffic/" + item.id)).text;
    assert(!record.includes(KEY) && !record.includes(AGENT_KEY));
    const parsed = JSON.parse(record);
    assert.deepEqual(parsed.requestReceived.headers["X-Api-Key"], ["[aicat]"]);
    assert.equal(parsed.requestSent, undefined);
    assert.equal(
      (await call(port, "/api/bodies/" + parsed.requestReceived.body.id)).text,
      '{"model":"claude-test","stream":true}',
    );
    assert.match(
      (await call(port, "/api/bodies/" + parsed.responseSent.body.id)).text,
      /message_stop/,
    );
    await page.locator("#nav-traffic").click();
    await page.locator(`.traffic-row[data-id="${item.id}"]`).click();
    await page.locator('[data-side="response"] [data-view="events"]').click();
    await page.locator(".events li").nth(1).waitFor();
    assert(!(await page.content()).includes(KEY));
    await shot("provider-traffic-light");
    check("recorded in traffic without credentials");

    // Changing the endpoint keeps the saved key.
    await page.locator("#nav-overview").click();
    await page
      .locator("#overview-apps .provider")
      .filter({ hasText: "Anthropic" })
      .getByText("管理")
      .click();
    await page.locator("#provider-url").fill(`http://localhost:${fixture}`);
    await page.locator("#provider-save").click();
    await wait(
      async () => (await anthropic()).baseUrl === `http://localhost:${fixture}`,
    );
    assert.equal((await message()).status, 200);
    assert.equal(messages().at(-1).headers["x-api-key"], KEY);
    check("endpoint edit keeps the key");

    // Removing the provider stops forwarding and erases the key.
    await page.locator("#provider-delete").click();
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await wait(async () => (await state()).providers.length === 1);
    await page.locator("#overview").waitFor();
    assert(!fs.readFileSync(secrets, "utf8").includes(KEY));
    const before = messages().length;
    assert.equal((await message()).status, 503);
    assert.equal(messages().length, before);
    check("remove erases the key and the route");

    assert.deepEqual(errors, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack;
  } finally {
    report.errors = errors;
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    await browser.close();
    proc.kill("SIGTERM");
    upstream.close();
    console.log(JSON.stringify(report, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
