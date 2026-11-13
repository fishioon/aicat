/* Focused scenario for 接入供应商: aicat holds a vendor key and forwards local
 * agents' requests with it.
 * Failure modes: key written to aicat.yaml or a world-readable file; key
 * echoed to the page or leaked into traffic records; the agent's own
 * credential reaching the vendor; plain-http endpoints accepted; routes
 * answering before a provider is saved or after it is removed; streamed
 * responses buffered or not recorded; browser origins accepted; a providers.json
 * from before providers were named lost on upgrade; model discovery guessing another path
 * or silently changing the configured API base; a refused key shown as a working provider;
 * a model's provider prefix reaching the vendor or picking the wrong provider;
 * a protocol being rejected locally instead of returning the upstream error;
 * preset/ID/protocol fields or onboarding steps reappearing; misaligned fields;
 * public icon discovery sending a credential or blocking saving; an
 * unknown model forwarded anywhere; the old per-provider paths still served;
 * a provider saved whose key is refused or that lists no models; an
 * unticked model still offered in the list or matched by its bare name, or
 * refused when named with its prefix; a call's tokens, first-token time or
 * agent missing from usage, a streamed Chat call left without its usage, or
 * the usage file readable by others or holding a key; a saved provider with
 * no calls missing from the overview, leaving its model picker unreachable.
 * Usage: node e2e/provider.cjs bin/aicat-e2e build/provider-e2e [--appearance|--model-path|--overview-provider]
 */
const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  net = require("node:net"),
  crypto = require("node:crypto");
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
  const report = {
    passed: false,
    checks: [],
    command: process.argv.slice(1),
    binarySHA256: crypto
      .createHash("sha256")
      .update(fs.readFileSync(binary))
      .digest("hex"),
  };
  const check = (name) => report.checks.push(name);
  const seen = [];
  let modelListStatus = 200;
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
      if (req.method === "GET" && req.url === "/favicon.ico") {
        assert.equal(req.headers.authorization, undefined);
        assert.equal(req.headers["x-api-key"], undefined);
        res.writeHead(200, { "Content-Type": "image/png" });
        res.end(
          Buffer.from(
            "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=",
            "base64",
          ),
        );
        return;
      }
      if (
        req.headers["x-api-key"] !== KEY &&
        req.headers.authorization !== "Bearer " + KEY &&
        req.headers.authorization !== "Bearer " + LEGACY_KEY
      ) {
        res.writeHead(401, { "Content-Type": "application/json" });
        res.end('{"error":{"message":"bad key"}}');
        return;
      }
      // The model list lives under /v1 only.
      if (req.method === "GET") {
        const found = req.url === "/v1/models";
        res.writeHead(found ? modelListStatus : 404, {
          "Content-Type": "application/json",
        });
        res.end(
          found
            ? JSON.stringify({
                data:
                  req.headers.authorization === "Bearer " + LEGACY_KEY
                    ? [{ id: "legacy-test" }, { id: "legacy-other" }]
                    : [
                        { id: "claude-test" },
                        {
                          id: "claude-other",
                          supported_endpoints: ["/chat/completions"],
                        },
                      ],
              })
            : "{}",
        );
        return;
      }
      if (req.url.startsWith("/v1/responses")) {
        res.writeHead(422, {
          "Content-Type": "application/json",
          "X-Upstream-Error": "unsupported-api",
        });
        res.end(
          '{"error":{"message":"Responses is not supported","type":"unsupported_api"}}',
        );
        return;
      }
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.write(
        'event: message_start\ndata: {"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":120,"cache_read_input_tokens":300,"cache_creation_input_tokens":30,"output_tokens":1}}}\n\n',
      );
      await sleep(400);
      res.write(
        'event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}\n\n',
      );
      res.end(
        'event: message_delta\ndata: {"type":"message_delta","usage":{"output_tokens":45}}\n\nevent: message_stop\ndata: {"type":"message_stop"}\n\n',
      );
    });
  });
  await new Promise((r) => upstream.listen(0, "127.0.0.1", r));
  const fixture = upstream.address().port;
  const iconSeen = [];
  const iconSite = http.createServer((req, res) => {
    iconSeen.push({ url: req.url, headers: req.headers });
    if (req.url === "/v1/models") {
      res.writeHead(req.headers.authorization === "Bearer " + KEY ? 200 : 401, {
        "Content-Type": "application/json",
      });
      return res.end(
        req.headers.authorization === "Bearer " + KEY
          ? '{"data":[{"id":"icon-test"}]}'
          : '{"error":{"message":"bad key"}}',
      );
    }
    if (
      req.headers.host.startsWith("localhost:") ||
      req.url === "/favicon.ico"
    ) {
      res.writeHead(404, { "Content-Type": "text/html" });
      return res.end("no icon");
    }
    if (req.url === "/site.png?v=1") {
      res.writeHead(200, { "Content-Type": "image/png" });
      return res.end(
        Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=",
          "base64",
        ),
      );
    }
    res.writeHead(200, { "Content-Type": "text/html" });
    res.end('<link rel="icon" href="/site.png?v=1">');
  });
  await new Promise((r) => iconSite.listen(0, "127.0.0.1", r));
  const iconPort = iconSite.address().port;
  const temp = net.createServer();
  await new Promise((r) => temp.listen(0, "127.0.0.1", r));
  const port = temp.address().port;
  await new Promise((r) => temp.close(r));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `traffic:\n  recording: true\nextensions:\n  traffic:\n    enabled: true\ngateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\n`,
  );
  // A file from before providers were named: one entry per vendor kind, and
  // an explicitly configured API base.
  fs.writeFileSync(
    path.join(home, "providers.json"),
    JSON.stringify({
      openai: { baseUrl: `http://127.0.0.1:${fixture}/v1`, key: LEGACY_KEY },
    }),
    { mode: 0o600 },
  );
  fs.writeFileSync(path.join(home, "provider-models.json"), "null");
  if (process.argv.includes("--overview-provider")) {
    fs.writeFileSync(
      path.join(home, "usage.jsonl"),
      ["", "removed-provider", "openai"].map((provider) => JSON.stringify({
        t: new Date().toISOString(), provider, agent: "codex", model: "sample-model",
        api: "responses", status: 200, in: 100, ms: 100,
      })).join("\n") + "\n",
      { mode: 0o600 },
    );
  }
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
  // Every provider is reached at the one endpoint; the model names it.
  const message = (model = "anthropic/claude-test") =>
    call(port, "/v1/messages?beta=true", {
      method: "POST",
      body: `{"model":"${model}","stream":true}`,
      headers: {
        "Content-Type": "application/json",
        "x-api-key": AGENT_KEY,
        Authorization: "Bearer " + AGENT_KEY,
        "anthropic-version": "2023-06-01",
        "User-Agent": "claude-cli/2.1.0 (external, cli)",
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

    if (process.argv.includes("--appearance")) {
      const row = page.locator(
        '#connected-providers [data-provider-id="openai"]',
      );
      await row.waitFor();
      assert.equal(await row.locator(".provider-icon svg rect").count(), 0);
      assert.equal(await row.locator(".provider-icon svg path").count(), 2);
      await shot("provider-default-icon-list-light");
      await row.click();
      assert.equal(
        await page.locator(".provider-heading .provider-icon svg path").count(),
        2,
      );
      await shot("provider-default-icon-detail-light");
      await page.locator("#provider-back").click();
      await page.locator("#connect-provider").click();
      assert.equal(await page.locator("#provider .provider-icon").count(), 0);
      const boxes = await Promise.all(
        ["#provider-name", "#provider-url", "#provider-key"].map((s) =>
          page.locator(s).boundingBox(),
        ),
      );
      assert(
        boxes.every((b) => b.x === boxes[0].x && b.width === boxes[0].width),
      );
      await shot("provider-add-no-icon-light");
      await page.locator("#theme").click();
      await page.waitForFunction(
        () => document.documentElement.dataset.theme === "dark",
      );
      assert.equal(await page.locator("#provider .provider-icon").count(), 0);
      await shot("provider-add-no-icon-dark");
      assert.equal(seen.length, 0, "viewing must not fetch models or icons");
      check(
        "saved supplier uses cube fallback in list and detail; add form has no icon; inputs stay aligned in both themes; viewing is read-only",
      );
      assert.deepEqual(errors, []);
      report.passed = true;
      return;
    }

    if (process.argv.includes("--overview-provider")) {
      const rows = page.locator("#overview-providers .provider");
      await wait(async () => (await rows.count()) === 3);
      const unlinked = rows.filter({ hasText: "未关联供应商" });
      const removed = rows.filter({ hasText: "removed-provider" });
      for (const row of [unlinked, removed]) {
        assert.equal(await row.evaluate((el) => el.tagName), "DIV");
        assert.equal(await row.locator("button, a").count(), 0);
        assert(!(await row.locator(".who").innerText()).trim().startsWith("·"));
        await row.click();
        await page.locator("#overview").waitFor();
        assert.equal(await page.locator("#provider").count(), 0);
      }
      check("unlinked and removed provider statistics have labels and cannot navigate to the add form");
      await shot("overview-provider-statistics-light");
      await page.locator("#theme").click();
      await page.waitForFunction(() => document.documentElement.dataset.theme === "dark");
      await shot("overview-provider-statistics-dark");
      const linked = rows.filter({ hasText: "OpenAI" });
      assert.equal(await linked.evaluate((el) => el.tagName), "BUTTON");
      await linked.click();
      await page.locator(".provider-heading", { hasText: "OpenAI" }).waitFor();
      assert.equal(await page.locator("#provider-name").inputValue(), "OpenAI");
      check("existing provider statistics still open that provider's management page");
      assert.equal(seen.length, 0, "viewing statistics and opening management do not fetch upstream");
      check("statistics and navigation remain read-only");
      assert.deepEqual(errors, []);
      report.passed = true;
      return;
    }

    if (process.argv.includes("--model-path")) {
      const requests = () => seen.filter((r) => r.headers.authorization || r.headers["x-api-key"]);
      const failSave = async (base, key, expectedPath, message) => {
        seen.length = 0;
        await page.locator("#provider-url").fill(base);
        await page.locator("#provider-key").fill(key);
        await page.locator("#provider-save").click();
        await page.locator("#provider-status.bad", { hasText: message }).waitFor();
        assert.deepEqual(requests().map((r) => r.url), [expectedPath]);
        assert.equal((await state()).providers.length, 1);
      };
      await page.locator("#connect-provider").click();
      await page.locator("#provider-name").fill("Model Path");
      await failSave(`http://127.0.0.1:${fixture}`, KEY, "/models", "没有模型列表");
      await shot("root-models-error");
      check("missing root models returns an error without trying the working /v1/models endpoint");
      await failSave(`http://127.0.0.1:${fixture}/tenant/api/`, KEY, "/tenant/api/models", "没有模型列表");
      check("nested API base stays within its configured path with no parent or version fallback");
      await failSave(`http://127.0.0.1:${fixture}/v1`, WRONG_KEY, "/v1/models", "拒绝了这把密钥");
      check("authentication error is shown after a single request and provider is not saved");
      seen.length = 0;
      await page.locator("#provider-url").fill(`http://127.0.0.1:${fixture}/v1/`);
      await page.locator("#provider-key").fill(KEY);
      await page.locator("#provider-save").click();
      await page.locator("#provider-status.ok").waitFor();
      const saved = (await state()).providers.find((p) => p.id === "model-path");
      assert.equal(saved.baseUrl, `http://127.0.0.1:${fixture}/v1`);
      assert.equal(saved.models.source, saved.baseUrl + "/models");
      assert.equal(saved.models.models.length, 2);
      assert.deepEqual(requests().map((r) => r.url), ["/v1/models"]);
      check("save appends /models once to the configured base and displays the model list");
      await shot("configured-base-success");
      seen.length = 0;
      await page.locator("#provider-sync").click();
      await wait(async () => (await state()).providers.find((p) => p.id === saved.id).models.syncedAt !== saved.models.syncedAt);
      await page.locator("#provider-status.ok").waitFor();
      assert.deepEqual(requests().map((r) => r.url), ["/v1/models"]);
      check("manual sync requests only the configured base plus /models");
      modelListStatus = 503;
      seen.length = 0;
      await page.locator("#provider-sync").click();
      await page.locator("#provider-status.warn").waitFor();
      const failed = (await state()).providers.find((p) => p.id === saved.id);
      assert.match(failed.models.error, /503/);
      assert.equal(failed.baseUrl, saved.baseUrl);
      assert.deepEqual(failed.models.models, saved.models.models);
      assert.deepEqual(requests().map((r) => r.url), ["/v1/models"]);
      await shot("sync-error-keeps-models");
      check("sync failure displays upstream error, preserves previous models and does not rewrite the base");
      assert.deepEqual(errors, []);
      report.passed = true;
      return;
    }

    // Nothing is routed before a provider is saved.
    assert.equal((await message()).status, 400);
    assert.equal(messages().length, 0);
    check("unsaved provider is not routed");

    // Startup and viewing a saved provider do not fetch models or icons.
    await page
      .locator('#connected-providers [data-provider-id="openai"]')
      .waitFor();
    await page.waitForTimeout(650);
    assert.equal(seen.length, 0);
    await page
      .locator('#connected-providers [data-provider-id="openai"]')
      .click();
    await page.locator("#provider-sync").click();
    await page.locator("#provider-status.ok").waitFor();
    await page.locator("#provider-back").click();
    check(
      "startup and opening a provider are read-only; explicit sync loads models and icon",
    );

    // The old file is read as a named provider; its configured API base
    // stays unchanged after syncing.
    await wait(
      async () => (await state()).providers[0]?.models.models.length === 2,
    );
    const legacy = (await state()).providers[0];
    assert.deepEqual(
      [legacy.id, legacy.name, legacy.baseUrl],
      ["openai", "OpenAI", `http://127.0.0.1:${fixture}/v1`],
    );
    assert.equal(
      JSON.parse(fs.readFileSync(secrets)).providers[0].id,
      "openai",
    );
    // A connected provider is manageable before its first model call,
    // including after the page is reloaded.
    await page.reload();
    await page
      .locator('#connected-providers [data-provider-id="openai"]')
      .waitFor();
    assert.equal(await page.locator("#usage-tokens").count(), 0);
    await shot("provider-no-calls-light");
    await page.locator("#theme").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "dark",
    );
    await shot("provider-no-calls-dark");
    await page.locator("#theme").click();
    await page.waitForFunction(
      () => document.documentElement.dataset.theme === "light",
    );
    await page
      .locator('#connected-providers [data-provider-id="openai"]')
      .click();
    assert.equal(await page.locator("#provider-name").inputValue(), "OpenAI");
    assert.equal(
      await page.locator("#provider-models input[type=checkbox]").count(),
      2,
    );
    await page.locator("#provider-back").click();
    check(
      "saved provider without calls survives reload and opens its model picker",
    );
    const chat = await call(port, "/v1/chat/completions", {
      method: "POST",
      body: '{"model":"openai/gpt-x","n":1}',
      headers: { Authorization: "Bearer " + AGENT_KEY },
    });
    assert.equal(chat.status, 200);
    assert.equal(seen.at(-1).url, "/v1/chat/completions");
    // The provider is named by the prefix and never sees it.
    assert.equal(seen.at(-1).body, '{"model":"gpt-x","n":1}');
    assert.equal(seen.at(-1).headers.authorization, "Bearer " + LEGACY_KEY);
    assert.equal(seen.at(-1).headers["x-api-key"], undefined);
    // Old protocol settings do not reject requests. A prefixed model goes
    // directly to the upstream, even when absent from its listed models.
    const mismatch = await message("openai/claude-test");
    assert.equal(mismatch.status, 401);
    assert.equal(seen.at(-1).url, "/v1/messages?beta=true");
    assert.equal(seen.at(-1).headers["x-api-key"], LEGACY_KEY);
    assert.equal(seen.at(-1).headers.authorization, undefined);
    const reached = seen.length;
    assert.equal((await message("nobody-has-this")).status, 400);
    for (const [url, status] of [
      ["/v1/embeddings", 404],
      ["/openai/v1/chat/completions", 404],
      ["/p/openai/chat/completions", 404],
    ])
      assert.equal(
        (
          await call(port, url, {
            method: "POST",
            body: '{"model":"openai/gpt-x"}',
          })
        ).status,
        status,
        url,
      );
    assert.equal(seen.length, reached);
    // A streamed Chat call is asked to end with its usage.
    await call(port, "/v1/chat/completions", {
      method: "POST",
      body: '{"model":"openai/gpt-x","stream":true}',
    });
    assert.deepEqual(JSON.parse(seen.at(-1).body), {
      model: "gpt-x",
      stream: true,
      stream_options: { include_usage: true },
    });
    check("legacy providers.json kept as a named provider, configured API base preserved");
    check(
      "routed by model and request path; upstream auth rejection preserved; unknown models and old paths refused",
    );

    await page.locator("#connect-provider").click();
    assert.equal(
      await page
        .locator(
          "#provider-presets, #provider-protocol-openai, #provider-id, #provider-snippet, #provider .steps",
        )
        .count(),
      0,
    );
    assert.equal(await page.locator("#provider input").count(), 3);
    assert.equal(await page.locator("#provider-models").count(), 0);
    assert.equal(await page.locator("#provider-name").inputValue(), "");
    assert.equal(await page.locator("#provider-url").inputValue(), "");
    const boxes = await Promise.all(
      ["#provider-name", "#provider-url", "#provider-key"].map((s) =>
        page.locator(s).boundingBox(),
      ),
    );
    assert(
      boxes.every((b) => b.x === boxes[0].x && b.width === boxes[0].width),
    );
    await shot("provider-add-light");
    await page.locator("#provider-name").fill("Anthropic");
    assert.equal(await page.locator("#provider-save").isDisabled(), true);
    check(
      "direct three-field form with aligned inputs, no presets, ID, protocol or onboarding steps",
    );

    // A key must not travel in the clear to another machine.
    await page.locator("#provider-url").fill("http://example.com");
    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await page.locator("#provider-status.bad", { hasText: "https" }).waitFor();
    assert(!fs.readFileSync(secrets, "utf8").includes(KEY));
    assert.equal((await state()).providers.length, 1);
    check("plain http endpoint rejected");

    // A provider is one whose model list can be had: a key the vendor
    // refuses, or an address without a list, is not saved, and says why.
    const anthropic = async () =>
      (await state()).providers.find((p) => p.id === "anthropic");
    const refused = async (url, key, why) => {
      await page.locator("#provider-url").fill(url);
      await page.locator("#provider-key").fill(key);
      await page.locator("#provider-save").click();
      await page.locator("#provider-status.bad", { hasText: why }).waitFor();
      assert.equal((await state()).providers.length, 1);
      assert(!fs.readFileSync(secrets, "utf8").includes(key));
      assert.equal((await message()).status, 400);
    };
    await refused(`http://127.0.0.1:${fixture}/`, WRONG_KEY, "拒绝了这把密钥");
    await shot("provider-refused-light");
    await refused(`http://127.0.0.1:${fixture}/nolist`, KEY, "没有模型列表");
    check("refused key and an address without a model list are not saved");

    await page.locator("#provider-url").fill(`http://127.0.0.1:${fixture}/v1/`);
    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await wait(async () => (await anthropic())?.models.models.length === 2);
    await page.locator("#provider-status.ok").waitFor();
    // The model list fields are covered by e2e/codex.cjs.
    const { id, name, baseUrl, keyHint } = await anthropic();
    assert.deepEqual(
      { id, name, baseUrl, keyHint },
      {
        id: "anthropic",
        name: "Anthropic",
        baseUrl: `http://127.0.0.1:${fixture}/v1`,
        keyHint: "9f3a",
      },
    );
    assert.equal(await page.locator("#provider-key").inputValue(), "");
    await page.locator(".provider-heading .provider-icon img").waitFor();
    assert.match(
      await page.locator(".provider-icon img").getAttribute("src"),
      /^data:image\/png;base64,/,
    );
    assert(
      await page
        .locator(".provider-icon img")
        .evaluate((img) => img.complete && img.naturalWidth > 0),
    );
    const publicIcons = seen.filter((s) => s.url === "/favicon.ico");
    assert(
      publicIcons.length > 0 &&
        publicIcons.every(
          (s) => !s.headers.authorization && !s.headers["x-api-key"],
        ),
    );
    check(
      "icon discovered automatically without provider or agent credentials",
    );
    await shot("provider-saved-light");
    await page.locator("#provider-back").click();
    const connected = page.locator(
      '#connected-providers [data-provider-id="anthropic"]',
    );
    await connected.waitFor();
    assert.match(await connected.innerText(), /已启用 2 \/ 2/);
    await page.locator("#usage-period-7d").click();
    await connected.waitFor();
    await page.locator("#usage-period-today").click();
    await connected.click();
    assert.equal(
      await page.locator("#provider-name").inputValue(),
      "Anthropic",
    );
    await page.locator("#provider-models-enabled").waitFor();
    check("new provider opens its models without calls across usage periods");

    await page.locator("#provider-back").click();
    await page.locator('#connected-providers [data-provider-id="openai"]').click();
    await page.locator("#provider-back").click();
    await page.locator('#connected-providers [data-provider-id="anthropic"]').click();
    await page.locator("#history-back").click();
    await page.locator("#overview").waitFor();
    await page.locator("#history-back").click();
    await page.locator("#provider").waitFor();
    assert.equal(await page.locator("#provider-name").inputValue(),"OpenAI");
    await page.locator("#history-forward").click();
    await page.locator("#history-forward").click();
    assert.equal(await page.locator("#provider-name").inputValue(),"Anthropic");
    check("provider navigation history restores the selected provider in both directions");

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
    // A model only one provider of this API lists needs no prefix: the
    // OpenAI provider lists it too, and is not asked.
    assert.equal((await message("claude-test")).status, 200);
    assert.equal(messages().at(-1).headers["x-api-key"], KEY);
    // The model list is every provider's, by the caller's API, prefixed.
    const ids = async (headers) =>
      JSON.parse((await call(port, "/v1/models", { headers })).text).data.map(
        (m) => m.id,
      );
    assert.deepEqual(await ids({ "anthropic-version": "2023-06-01" }), [
      "openai/legacy-test",
      "openai/legacy-other",
      "anthropic/claude-test",
      "anthropic/claude-other",
    ]);
    assert.deepEqual(await ids({}), [
      "openai/legacy-test",
      "openai/legacy-other",
      "anthropic/claude-test",
      "anthropic/claude-other",
    ]);
    check(
      "request forwarded with the saved key, streamed; bare model and merged list",
    );

    // Every listed model starts ticked. An unticked one leaves the list and
    // its bare name, and still answers to its prefix.
    const ticks = page.locator("#provider-models input[type=checkbox]");
    assert.equal(await ticks.count(), 2);
    assert.match(
      await page.locator("#provider-models-enabled").innerText(),
      /已启用 2 \/ 2/,
    );
    await ticks.first().uncheck();
    await wait(
      async () => (await anthropic()).enabled.join() === "claude-other",
    );
    assert.equal((await anthropic()).picked, true);
    assert.deepEqual(
      JSON.parse(fs.readFileSync(secrets)).providers.find(
        (p) => p.id === "anthropic",
      ).enabled,
      ["claude-other"],
    );
    assert.deepEqual(await ids({ "anthropic-version": "2023-06-01" }), [
      "openai/legacy-test",
      "openai/legacy-other",
      "anthropic/claude-other",
    ]);
    assert.equal((await message("claude-test")).status, 400);
    assert.equal((await message("anthropic/claude-test")).status, 200);
    await page.locator("#provider-models-none").click();
    await wait(async () => (await anthropic()).enabled.length === 0);
    assert.deepEqual(await ids({ "anthropic-version": "2023-06-01" }), [
      "openai/legacy-test",
      "openai/legacy-other",
    ]);
    // All ticks what the filter shows.
    await page.locator("#provider-models-filter").fill("other");
    assert.equal(await ticks.count(), 1);
    await page.locator("#provider-models-all").click();
    await wait(
      async () => (await anthropic()).enabled.join() === "claude-other",
    );
    await page.locator("#provider-models-filter").fill("");
    await shot("provider-models-picked-light");
    await page.locator("#provider-models-all").click();
    await wait(async () => (await anthropic()).enabled.length === 2);
    assert.equal((await message("claude-test")).status, 200);
    check("ticked models are offered; unticked ones only by their prefix");

    // The same provider accepts requests on all three paths. No probing,
    // conversion or retries; an unsupported API's status, headers and body survive.
    const beforeResponses = seen.length;
    const unsupported = await call(port, "/v1/responses?beta=true", {
      method: "POST",
      body: '{"model":"anthropic/claude-other","input":"hi"}',
      headers: { Authorization: "Bearer " + AGENT_KEY, "x-api-key": AGENT_KEY },
    });
    assert.equal(unsupported.status, 422);
    assert.equal(unsupported.headers["x-upstream-error"], "unsupported-api");
    assert.equal(
      unsupported.text,
      '{"error":{"message":"Responses is not supported","type":"unsupported_api"}}',
    );
    assert.equal(seen.length, beforeResponses + 1);
    assert.equal(seen.at(-1).headers.authorization, "Bearer " + KEY);
    assert.equal(seen.at(-1).headers["x-api-key"], undefined);
    assert.equal(seen.at(-1).url, "/v1/responses?beta=true");
    assert.deepEqual(JSON.parse(seen.at(-1).body), {
      model: "claude-other",
      input: "hi",
    });
    const chatOnSameProvider = await call(port, "/v1/chat/completions", {
      method: "POST",
      body: '{"model":"anthropic/claude-other","messages":[]}',
    });
    assert.equal(chatOnSameProvider.status, 200);
    assert.equal(seen.at(-1).headers.authorization, "Bearer " + KEY);
    check(
      "all APIs route without protocol checks; upstream errors relayed unchanged without retry",
    );

    // Browser pages may not use the route.
    assert.equal(
      (
        await call(port, "/v1/messages", {
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

    // Each forwarded call is in the usage, with its tokens, first token and
    // agent, apart from the traffic records.
    const usage = async () =>
      JSON.parse((await call(port, "/api/usage?period=today")).text);
    const held = await wait(async () => {
      const p = (await usage()).providers.find((g) => g.key === "anthropic");
      return p?.requests >= 3 && p;
    });
    const calls = held.input / 120;
    assert(Number.isInteger(calls) && calls >= 3, JSON.stringify(held));
    assert.equal(held.name, "Anthropic");
    assert.deepEqual(
      [held.cacheRead, held.cacheWrite, held.output],
      [300 * calls, 30 * calls, 45 * calls],
    );
    assert(held.ttftMs >= 300, "first token time " + held.ttftMs);
    const summary = await usage();
    assert(summary.agents.some((a) => a.name === "Claude Code"));
    assert(summary.models.some((m) => m.key === "claude-test"));
    const ledger = path.join(home, "usage.jsonl");
    assert.equal(fs.statSync(ledger).mode & 0o777, 0o600);
    const lines = fs.readFileSync(ledger, "utf8");
    assert(!lines.includes(KEY) && !lines.includes(AGENT_KEY));
    assert.equal((await call(port, "/api/usage?period=1y")).status, 400);
    await page.locator("#nav-overview").click();
    await wait(
      async () =>
        (await page.locator("#usage-tokens").innerText()).trim() !== "0",
    );
    await page.locator("#overview-providers").getByText("Anthropic").waitFor();
    await page.locator("#overview-agents").getByText("Claude Code").waitFor();
    await page.locator("#usage-period-7d").click();
    await page.locator("#usage-period-7d[aria-checked=true]").waitFor();
    await page.locator("#usage-period-today").click();
    await shot("provider-usage-light");
    check("calls summed in usage and on the overview");

    // Changing the endpoint keeps the saved key.
    await page.locator("#nav-overview").click();
    // The overview row opens the provider, by its own button or a 管理 one.
    const row = page.locator(".provider").filter({ hasText: "Anthropic" });
    await row.first().waitFor();
    const manage = row.getByText("管理");
    await ((await manage.count()) ? manage : row).first().click();
    await page.locator("#provider-url").fill(`http://localhost:${fixture}/v1`);
    await page.locator("#provider-save").click();
    await wait(
      async () =>
        (await anthropic()).baseUrl === `http://localhost:${fixture}/v1`,
    );
    assert.equal((await message()).status, 200);
    assert.equal(messages().at(-1).headers["x-api-key"], KEY);
    check("endpoint edit keeps the key");

    // Removing the provider stops forwarding and erases the key.
    await page.locator("#provider-delete").click();
    await page.locator("#dialog button").filter({ hasText: "确定" }).click();
    await wait(async () => (await state()).providers.length === 1);
    await page.locator("#overview").waitFor();
    assert.equal(
      await page
        .locator('#connected-providers [data-provider-id="anthropic"]')
        .count(),
      0,
    );
    await page
      .locator('#connected-providers [data-provider-id="openai"]')
      .waitFor();
    assert(!fs.readFileSync(secrets, "utf8").includes(KEY));
    const before = messages().length;
    assert.equal((await message()).status, 400);
    assert.equal(messages().length, before);
    check("remove erases the key and the route");

    for (const expected of ["localhost", "localhost-2"]) {
      await page.locator("#connect-provider").click();
      await page.locator("#provider-name").fill("我的中转站");
      await page
        .locator("#provider-url")
        .fill(`http://localhost:${fixture}/v1`);
      await page.locator("#provider-key").fill(KEY);
      await page.locator("#provider-save").click();
      await page.locator("#provider-status.ok").waitFor();
      assert(
        (await state()).providers.some(
          (p) => p.id === expected && p.name === "我的中转站",
        ),
      );
      await page.locator("#provider-name").fill("改名后的中转站");
      await page.locator("#provider-save").click();
      await wait(async () =>
        (await state()).providers.some(
          (p) => p.id === expected && p.name === "改名后的中转站",
        ),
      );
      await page.locator("#provider-back").click();
    }
    assert.equal((await message("claude-test")).status, 400);
    check(
      "Chinese names derive ASCII IDs from host; duplicate IDs numbered; rename keeps ID; ambiguous models refused",
    );
    await page.locator("#connect-provider").click();
    await page.locator("#provider-name").fill("Icon Timing");
    await page.locator("#provider-url").fill(`http://127.0.0.1:${iconPort}/v1`);
    await page.locator("#provider-key").fill(WRONG_KEY);
    await page.waitForTimeout(650);
    assert.equal(iconSeen.length, 0, "typing must not fetch models or icons");
    await page.locator("#provider-save").click();
    await page.locator("#provider-status.bad").waitFor();
    assert.deepEqual(
      iconSeen.map((r) => r.url),
      ["/v1/models"],
    );
    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await page.locator("#provider-status.ok").waitFor();
    const timing = (await state()).providers.find(
      (p) => p.id === "icon-timing",
    );
    assert.match(timing.icon, /^data:image\/png;base64,/);
    assert(
      iconSeen.findIndex((r) => r.url === "/favicon.ico") >
        iconSeen.findLastIndex((r) => r.url === "/v1/models"),
    );
    const diskIcon = JSON.parse(fs.readFileSync(secrets)).providers.find(
      (p) => p.id === "icon-timing",
    ).icon;
    assert.equal(diskIcon, timing.icon);
    const afterSave = iconSeen.length;
    await page.locator("#provider-back").click();
    await page
      .locator(
        '#connected-providers [data-provider-id="icon-timing"] .provider-icon img',
      )
      .waitFor();
    await page.reload();
    await page
      .locator(
        '#connected-providers [data-provider-id="icon-timing"] .provider-icon img',
      )
      .waitFor();
    await page
      .locator('#connected-providers [data-provider-id="icon-timing"]')
      .click();
    await page.waitForTimeout(650);
    assert.equal(
      iconSeen.length,
      afterSave,
      "viewing and reloading must only read saved data",
    );
    await page.locator("#provider-name").fill("Icon Timing Renamed");
    await page.locator("#provider-save").click();
    await wait(async () =>
      (await state()).providers.some((p) => p.name === "Icon Timing Renamed"),
    );
    assert.equal(iconSeen.length, afterSave, "renaming must not fetch");
    await page.locator("#provider-sync").click();
    await wait(() => iconSeen.length === afterSave + 4);
    await page.locator("#provider-status.ok").waitFor();
    assert(
      iconSeen
        .filter((r) => r.url !== "/v1/models")
        .every((r) => !r.headers.authorization && !r.headers["x-api-key"]),
    );
    check(
      "models and icon fetched together only on sync; typing, failed saves, page views, reload and rename do not fetch; persisted icon appears in provider list",
    );

    // Legacy named Anthropic connections use an origin base, unlike the
    // unified API base. Restart in another isolated Home to exercise migration.
    const migratedHome = path.join(root, "legacy-anthropic-home");
    fs.mkdirSync(migratedHome, { recursive: true, mode: 0o700 });
    const spare = net.createServer();
    await new Promise((r) => spare.listen(0, "127.0.0.1", r));
    const migratedPort = spare.address().port;
    await new Promise((r) => spare.close(r));
    fs.writeFileSync(
      path.join(migratedHome, "aicat.yaml"),
      `gateway:\n  addr: 127.0.0.1:${migratedPort}\nui:\n  language: zh-Hans\n`,
    );
    fs.writeFileSync(
      path.join(migratedHome, "providers.json"),
      JSON.stringify({
        providers: [
          {
            id: "old-claude",
            name: "旧供应商",
            protocol: "anthropic",
            baseUrl: `http://127.0.0.1:${fixture}`,
            key: KEY,
            enabled: ["claude-test"],
          },
        ],
      }),
      { mode: 0o600 },
    );
    const migratedPreview = path.join(root, "legacy-preview-url");
    const migrated = spawn(binary, ["--home", migratedHome], {
      env: {
        ...process.env,
        AICAT_E2E_BACKGROUND: "1",
        AICAT_E2E_OS_STATE: stateFile,
        AICAT_E2E_PREVIEW: migratedPreview,
      },
      stdio: ["ignore", log, log],
    });
    try {
      await wait(() => fs.existsSync(migratedPreview));
      const nextPage = await browser.newPage();
      await nextPage.goto(fs.readFileSync(migratedPreview, "utf8"));
      await nextPage.waitForFunction(() => !!window.desktop?.state());
      assert.equal(
        await nextPage.evaluate(
          () => window.desktop.state().providers[0].models.models.length,
        ),
        0,
      );
      await nextPage
        .locator('#connected-providers [data-provider-id="old-claude"]')
        .click();
      await nextPage.locator("#provider-sync").click();
      await nextPage.locator("#provider-status.ok").waitFor();
      const migratedState = await nextPage.evaluate(
        () => window.desktop.state().providers[0],
      );
      assert.equal(migratedState.id, "old-claude");
      assert.equal(migratedState.baseUrl, `http://127.0.0.1:${fixture}/v1`);
      assert.deepEqual(migratedState.enabled, ["claude-test"]);
      assert.equal(
        (
          await call(migratedPort, "/v1/messages", {
            method: "POST",
            body: '{"model":"old-claude/claude-test"}',
          })
        ).status,
        200,
      );
      await nextPage.locator("#provider-name").fill("旧供应商改名");
      await nextPage.locator("#provider-save").click();
      await wait(
        () =>
          JSON.parse(fs.readFileSync(path.join(migratedHome, "providers.json")))
            .providers[0].name === "旧供应商改名",
      );
      const disk = JSON.parse(
        fs.readFileSync(path.join(migratedHome, "providers.json")),
      ).providers[0];
      assert.equal(disk.id, "old-claude");
      assert.equal(disk.protocol, undefined);
      assert.equal(disk.key, KEY);
      assert.deepEqual(disk.enabled, ["claude-test"]);
      await nextPage.close();
    } finally {
      migrated.kill("SIGTERM");
    }
    check(
      "legacy Anthropic API base migrated; ID, saved key and model choices preserved after save",
    );
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
    iconSite.close();
    console.log(JSON.stringify(report, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
