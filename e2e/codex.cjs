/* Focused scenario for the built-in Codex extension: provider models join
 * Codex's model list, and a turn for one goes to the provider with the key
 * aicat holds, through both entries (the decrypted chatgpt.com and
 * openai_base_url pointing at aicat).
 * Failure modes: the extension serving while off, or turning on before the
 * certificate is trusted; model lists not synced on save, or keeping
 * embedding/image/chat-only models; models.dev not filling in what the
 * provider leaves out, or passing a reasoning level Codex rejects; a provider
 * turn carrying ChatGPT's credentials or OpenAI-only fields, buffered, or
 * falling through to ChatGPT; ChatGPT's own turns altered or not passed on;
 * WebSocket not turned down; the key leaking into traffic records; chatgpt.com
 * still decrypted after the extension is turned off.
 * Remote origins (ChatGPT's backend, the provider, models.dev) are local
 * fixtures; the window, services, certificate flow and gateway are real.
 * Usage: node e2e/codex.cjs bin/aicat-e2e build/codex-e2e
 */
const fs = require("node:fs"),
  path = require("node:path"),
  http = require("node:http"),
  https = require("node:https"),
  tls = require("node:tls"),
  net = require("node:net"),
  zlib = require("node:zlib"),
  crypto = require("node:crypto");
const { spawn, execFileSync } = require("node:child_process");
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
const root = path.resolve(process.argv[3] || "build/codex-e2e");
fs.rmSync(root, { recursive: true, force: true });
fs.mkdirSync(path.join(root, "home"), { recursive: true, mode: 0o700 });
const home = path.join(root, "home"),
  previewFile = path.join(root, "preview-url"),
  stateFile = path.join(root, "os-state.json");
const KEY = "sk-compat-HELD-BY-AICAT-7c21";
const CHATGPT = "Bearer chatgpt-session-token-must-not-reach-vendor";

// A local root and a leaf for "localhost" stand in for chatgpt.com's chain.
const openssl = (...a) => execFileSync("openssl", a, { stdio: "ignore" });
fs.writeFileSync(
  path.join(root, "root.cnf"),
  "[req]\ndistinguished_name=dn\nx509_extensions=v3_ca\nprompt=no\n[dn]\nCN=Codex E2E Root\n[v3_ca]\nbasicConstraints=critical,CA:true\nkeyUsage=critical,keyCertSign,cRLSign\n",
);
openssl(
  "req",
  "-x509",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-keyout",
  path.join(root, "root.key"),
  "-out",
  path.join(root, "root.pem"),
  "-days",
  "2",
  "-config",
  path.join(root, "root.cnf"),
);
openssl(
  "req",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-keyout",
  path.join(root, "leaf.key"),
  "-out",
  path.join(root, "leaf.csr"),
  "-subj",
  "/CN=localhost",
);
fs.writeFileSync(
  path.join(root, "leaf.ext"),
  "subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n",
);
openssl(
  "x509",
  "-req",
  "-in",
  path.join(root, "leaf.csr"),
  "-CA",
  path.join(root, "root.pem"),
  "-CAkey",
  path.join(root, "root.key"),
  "-CAcreateserial",
  "-out",
  path.join(root, "leaf.pem"),
  "-days",
  "2",
  "-extfile",
  path.join(root, "leaf.ext"),
);

function body(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (b) => chunks.push(b));
    req.on("end", () => {
      let raw = Buffer.concat(chunks);
      const enc = req.headers["content-encoding"];
      if (enc === "zstd") raw = zlib.zstdDecompressSync(raw);
      if (enc === "gzip") raw = zlib.gunzipSync(raw);
      resolve(raw.toString());
    });
  });
}
async function stream(res, events) {
  res.writeHead(200, {
    "Content-Type": "text/event-stream",
    "X-Models-Etag": '"backend-1"',
  });
  for (const [i, e] of events.entries()) {
    if (i) await sleep(400);
    res.write(`event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`);
  }
  res.end();
}

function call(port, url, { method = "GET", body = "", headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const r = http.request(
      { host: "127.0.0.1", port, path: url, method, headers },
      collect(resolve),
    );
    r.on("error", reject);
    r.end(body);
  });
}
function collect(resolve) {
  return (res) => {
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
  };
}

(async () => {
  const report = { passed: false, checks: [], command: process.argv.slice(1) };
  report.binarySHA256 = crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex");
  const check = (name) => report.checks.push(name);
  const backendSeen = [],
    vendorSeen = [];
  let extraModel = false;

  // ChatGPT's backend: its own model list and turns, for Codex's sign-in only.
  const backend = https.createServer(
    {
      key: fs.readFileSync(path.join(root, "leaf.key")),
      cert: fs.readFileSync(path.join(root, "leaf.pem")),
    },
    async (req, res) => {
      const text = await body(req);
      backendSeen.push({
        method: req.method,
        url: req.url,
        headers: req.headers,
        body: text,
      });
      if (req.headers.authorization !== CHATGPT) {
        res.writeHead(401, { "Content-Type": "application/json" });
        return res.end('{"error":{"message":"sign in again"}}');
      }
      if (
        req.method === "GET" &&
        req.url.startsWith("/backend-api/codex/models")
      ) {
        res.writeHead(200, {
          "Content-Type": "application/json",
          ETag: '"backend-1"',
        });
        return res.end(
          JSON.stringify({
            models: [
              { slug: "gpt-native", display_name: "Native", priority: 1 },
            ],
          }),
        );
      }
      if (req.method === "POST" && req.url === "/backend-api/codex/responses")
        return stream(res, [
          { type: "response.created", model: "gpt-native" },
          { type: "response.completed", model: "gpt-native" },
        ]);
      res.writeHead(404);
      res.end();
    },
  );
  // An OpenAI-compatible provider: its list, and Responses for its key only.
  const vendor = http.createServer(async (req, res) => {
    const text = await body(req);
    vendorSeen.push({
      method: req.method,
      url: req.url,
      headers: req.headers,
      body: text,
    });
    if (req.headers.authorization !== "Bearer " + KEY) {
      res.writeHead(401, { "Content-Type": "application/json" });
      return res.end('{"error":{"message":"bad key"}}');
    }
    if (req.method === "GET" && req.url === "/v1/models") {
      const data = [
        { id: "good-model" },
        { id: "text-embedding-3-large" },
        { id: "gpt-image-1" },
        { id: "chat-only", supported_endpoints: ["/chat/completions"] },
        {
          id: "vision-x",
          display_name: "Vision X",
          context_length: 64000,
          modalities: { input: ["text", "image"] },
        },
      ];
      if (extraModel) data.push({ id: "late-model" });
      res.writeHead(200, { "Content-Type": "application/json" });
      return res.end(JSON.stringify({ object: "list", data }));
    }
    if (req.method === "POST" && req.url === "/v1/responses")
      return stream(res, [
        { type: "response.created", model: "good-model" },
        { type: "response.output_text.delta", delta: "hi" },
        { type: "response.completed", model: "good-model" },
      ]);
    res.writeHead(404);
    res.end();
  });
  // models.dev: what the provider's list leaves out.
  const catalogHits = [];
  const modelsDev = http.createServer((req, res) => {
    catalogHits.push(req.url);
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({
        openai: {
          models: {
            "good-model": {
              name: "Good Model",
              reasoning_options: [
                { type: "effort", values: ["low", "medium", "high", "max"] },
              ],
              limit: { context: 400000, input: 272000, output: 128000 },
              modalities: { input: ["text", "image"], output: ["text"] },
            },
          },
        },
      }),
    );
  });
  const listen = (s) =>
    new Promise((r) => s.listen(0, "127.0.0.1", () => r(s.address().port)));
  const bp = await listen(backend),
    vp = await listen(vendor),
    mp = await listen(modelsDev);
  const temp = net.createServer();
  const port = await listen(temp);
  await new Promise((r) => temp.close(r));
  fs.writeFileSync(
    path.join(home, "aicat.yaml"),
    `gateway:\n  addr: 127.0.0.1:${port}\nui:\n  language: zh-Hans\n  theme: light\n`,
  );
  fs.writeFileSync(
    stateFile,
    JSON.stringify({ services: [], trusted: false, recordProxyWrites: true }),
    {
      mode: 0o600,
    },
  );
  const log = fs.openSync(path.join(root, "app.log"), "a");
  const proc = spawn(binary, ["--home", home], {
    env: {
      ...process.env,
      AICAT_E2E_OS_STATE: stateFile,
      AICAT_E2E_PREVIEW: previewFile,
      // Preview-only scenario: keep the native window out of the way.
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_ROOT: path.join(root, "root.pem"),
      GODEBUG: "x509usefallbackroots=1",
      AICAT_E2E_CODEX_BACKEND: `https://localhost:${bp}`,
      AICAT_E2E_MODELS_DEV: `http://127.0.0.1:${mp}/api.json`,
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
  const config = () => fs.readFileSync(path.join(home, "aicat.yaml"), "utf8");
  // A request Codex sends through the gateway to its backend: CONNECT, then
  // TLS trusting only `ca`.
  function viaGateway(
    ca,
    url,
    { method = "GET", body: payload = "", headers = {} } = {},
  ) {
    return new Promise((resolve, reject) => {
      const connect = http.request({
        host: "127.0.0.1",
        port,
        method: "CONNECT",
        path: `localhost:${bp}`,
      });
      connect.on("connect", (res, socket) => {
        if (res.statusCode !== 200)
          return reject(new Error("CONNECT " + res.statusCode));
        const secure = tls.connect({ socket, servername: "localhost", ca });
        secure.on("error", reject);
        secure.on("secureConnect", () => {
          const issuer = secure.getPeerCertificate().issuer?.CN;
          // agent:false would ignore createConnection and dial directly
          const agent = new https.Agent();
          agent.createConnection = () => secure;
          const r = https.request(
            { host: "localhost", port: bp, path: url, method, headers, agent },
            (res) => collect((v) => resolve({ ...v, issuer }))(res),
          );
          r.on("error", reject);
          r.end(payload);
        });
      });
      connect.on("error", reject);
      connect.end();
    });
  }
  const turn = (model, extra = {}) =>
    JSON.stringify({
      model,
      stream: true,
      store: false,
      stream_options: { reasoning_summary_delivery: "streamed" },
      input: [
        {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "say <hi> & bye" }],
          internal_chat_message_metadata_passthrough: { turn: 1 },
        },
        { type: "configuration_update", value: 1 },
      ],
      ...extra,
    });
  const codexHeaders = {
    "Content-Type": "application/json",
    Accept: "text/event-stream",
    Authorization: CHATGPT,
    "ChatGPT-Account-Id": "acct-1",
    "OpenAI-Beta": "responses=experimental",
  };
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

    // Off by default: the local entry refuses and chatgpt.com is not decrypted.
    assert.equal((await state()).codexEnabled, false);
    assert.equal(
      (
        await call(port, "/backend-api/codex/models", {
          headers: { Authorization: CHATGPT },
        })
      ).status,
      503,
    );
    assert.equal(backendSeen.length, 0);
    const rootPem = fs.readFileSync(path.join(root, "root.pem"));
    assert.equal(
      (
        await viaGateway(rootPem, "/backend-api/codex/models", {
          headers: { Authorization: CHATGPT },
        })
      ).issuer,
      "Codex E2E Root",
    );
    check("off by default: local entry refused, chatgpt.com tunnelled");

    // Turning it on asks for the certificate first, then turns it on.
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-codex").click();
    await page.locator('.extension-detail[data-extension="codex"]').waitFor();
    await shot("codex-off-light");
    await page.locator("#feature-codex").click();
    await page.locator("#dialog").getByText("需要先安装证书").waitFor();
    await shot("codex-certificate-prompt-light");
    await page.locator("#dialog button").filter({ hasText: "取消" }).click();
    assert.equal((await state()).codexEnabled, false);
    await page.locator("#feature-codex").click();
    await page
      .locator("#dialog button")
      .filter({ hasText: "安装证书" })
      .click();
    await wait(async () => (await state()).codexEnabled);
    assert.match(config(), /codex: true/);
    await page.locator("#codex-status").waitFor();
    check("enabling prompts for the certificate, then enables and persists");

    // Saving a provider syncs its list; non-chat models are left out.
    await page.locator("#nav-overview").click();
    await page.locator("#connect-provider").click();
    await page.locator("#provider-compat").click();
    await page.locator("#provider-url").fill(`http://127.0.0.1:${vp}/v1`);
    await page.locator("#provider-key").fill(KEY);
    await page.locator("#provider-save").click();
    await wait(
      async () => (await state()).providers[0]?.models.models.length === 3,
    );
    const saved = (await state()).providers[0];
    assert.deepEqual(
      saved.models.models.map((m) => m.id),
      ["good-model", "chat-only", "vision-x"],
    );
    assert.equal(saved.models.source, `http://127.0.0.1:${vp}/v1/models`);
    assert.equal(saved.responses, true);
    await page.locator("#provider-models li").nth(2).waitFor();
    assert.match(
      await page.locator("#provider-models-status").innerText(),
      /3 个模型/,
    );
    const list = JSON.parse(
      fs.readFileSync(path.join(home, "provider-models.json"), "utf8"),
    );
    assert(!JSON.stringify(list).includes(KEY));
    await shot("codex-provider-models-light");
    check("provider list synced on save, non-chat models left out");

    // models.dev was fetched when the extension turned on.
    await wait(() =>
      fs.existsSync(path.join(home, "cache", "models-dev.json")),
    );
    assert.deepEqual(catalogHits, ["/api.json"]);
    check("models.dev catalog downloaded");

    // Local entry: the backend's list plus the providers' Responses models.
    const models = await call(
      port,
      "/backend-api/codex/models?client_version=0.150.0",
      {
        headers: {
          Authorization: CHATGPT,
          "If-None-Match": '"old"',
          "Accept-Encoding": "zstd",
        },
      },
    );
    assert.equal(models.status, 200);
    const listed = JSON.parse(models.text).models;
    assert.deepEqual(
      listed.map((m) => m.slug),
      ["gpt-native", "compat/good-model", "compat/vision-x"],
    );
    const good = listed[1],
      vision = listed[2];
    assert.equal(good.display_name, "Good Model · OpenAI-compatible");
    assert.equal(good.context_window, 272000);
    assert.deepEqual(
      good.supported_reasoning_levels.map((l) => l.effort),
      ["low", "medium", "high"],
    );
    assert.equal(good.default_reasoning_level, "medium");
    assert.deepEqual(good.input_modalities, ["text", "image"]);
    assert(good.base_instructions.startsWith("You are Codex"));
    assert.equal(vision.display_name, "Vision X · OpenAI-compatible");
    assert.equal(vision.context_window, 64000);
    assert.equal(vision.default_reasoning_level, null);
    assert.deepEqual(vision.input_modalities, ["text", "image"]);
    assert.match(models.headers.etag, /^"backend-1\+aicat-[0-9a-f]{12}"$/);
    const asked = backendSeen.at(-1);
    assert.equal(asked.url, "/backend-api/codex/models?client_version=0.150.0");
    assert.equal(asked.headers.authorization, CHATGPT);
    assert.equal(asked.headers["if-none-match"], undefined);
    check("model list merged with provider models filled in from models.dev");

    // A provider turn: the provider's key, none of ChatGPT's credentials,
    // OpenAI-only fields gone, streamed as it arrives.
    const before = backendSeen.length;
    const vendorTurn = await call(port, "/backend-api/codex/responses", {
      method: "POST",
      headers: { ...codexHeaders, "Content-Encoding": "zstd" },
      body: zlib.zstdCompressSync(Buffer.from(turn("compat/good-model"))),
    });
    assert.equal(vendorTurn.status, 200);
    assert.match(
      vendorTurn.text,
      /response\.created[\s\S]*response\.completed/,
    );
    assert(
      vendorTurn.chunks.at(-1).at - vendorTurn.chunks[0].at >= 600,
      "provider stream was buffered",
    );
    const sent = vendorSeen.at(-1);
    assert.equal(sent.url, "/v1/responses");
    assert.equal(sent.headers.authorization, "Bearer " + KEY);
    for (const h of [
      "chatgpt-account-id",
      "openai-beta",
      "cookie",
      "content-encoding",
    ])
      assert.equal(sent.headers[h], undefined, h);
    const forwarded = JSON.parse(sent.body);
    assert.equal(forwarded.model, "good-model");
    assert.equal(forwarded.stream_options, undefined);
    assert.equal(forwarded.input.length, 1);
    assert.equal(
      forwarded.input[0].internal_chat_message_metadata_passthrough,
      undefined,
    );
    assert.equal(forwarded.input[0].content[0].text, "say <hi> & bye");
    assert(sent.body.includes("<hi> &"), "text was HTML-escaped");
    assert.equal(backendSeen.length, before);
    check(
      "provider turn: provider key only, OpenAI-only fields dropped, streamed",
    );

    // ChatGPT's own turn goes on as it came, and its X-Models-Etag carries
    // aicat's tag like the list does.
    const nativeBody = turn("gpt-native");
    const native = await call(port, "/backend-api/codex/responses", {
      method: "POST",
      headers: codexHeaders,
      body: nativeBody,
    });
    assert.equal(native.status, 200);
    assert.equal(backendSeen.at(-1).body, nativeBody);
    assert.equal(backendSeen.at(-1).headers["chatgpt-account-id"], "acct-1");
    assert.equal(native.headers["x-models-etag"], models.headers.etag);
    check("ChatGPT turn passed through unchanged, X-Models-Etag tagged");

    // The provider namespace never falls through to ChatGPT.
    const vendorBefore = vendorSeen.length;
    for (const model of ["compat/nope", "foo/bar", "compat/chat-only"]) {
      const r = await call(port, "/backend-api/codex/responses", {
        method: "POST",
        headers: codexHeaders,
        body: turn(model),
      });
      assert.equal(r.status, 400, model);
      assert.match(JSON.parse(r.text).error.message, /不可用/);
    }
    const compact = await call(port, "/backend-api/codex/responses/compact", {
      method: "POST",
      headers: codexHeaders,
      body: turn("compat/good-model"),
    });
    assert.equal(compact.status, 400);
    const trigger = await call(port, "/backend-api/codex/responses", {
      method: "POST",
      headers: codexHeaders,
      body: turn("compat/good-model", {
        input: [{ type: "compaction_trigger" }],
      }),
    });
    assert.equal(trigger.status, 400);
    assert.match(JSON.parse(trigger.text).error.message, /压缩/);
    assert.equal(vendorSeen.length, vendorBefore);
    assert.equal(backendSeen.length, before + 1);
    check("unknown, chat-only and compacting provider turns refused locally");

    const ws = await call(port, "/backend-api/codex/responses", {
      headers: {
        Connection: "Upgrade",
        Upgrade: "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
        Authorization: CHATGPT,
      },
    });
    assert.equal(ws.status, 426);
    check("local entry: WebSocket answered 426");

    // Through the gateway: chatgpt.com is decrypted with aicat's CA.
    const aicatCA = fs.readFileSync(
      path.join(home, "ca", "mitmproxy-ca-cert.pem"),
    );
    const mitmModels = await viaGateway(
      aicatCA,
      "/backend-api/codex/models?client_version=0.150.0",
      {
        headers: { Authorization: CHATGPT },
      },
    );
    assert.equal(mitmModels.status, 200);
    assert.notEqual(mitmModels.issuer, "Codex E2E Root");
    assert.deepEqual(
      JSON.parse(mitmModels.text).models.map((m) => m.slug),
      ["gpt-native", "compat/good-model", "compat/vision-x"],
    );
    const mitmVendor = await viaGateway(
      aicatCA,
      "/backend-api/codex/responses",
      {
        method: "POST",
        headers: codexHeaders,
        body: turn("compat/vision-x"),
      },
    );
    assert.equal(mitmVendor.status, 200);
    assert(
      mitmVendor.chunks.at(-1).at - mitmVendor.chunks[0].at >= 600,
      "provider stream was buffered through the gateway",
    );
    assert.equal(JSON.parse(vendorSeen.at(-1).body).model, "vision-x");
    assert.equal(vendorSeen.at(-1).headers.authorization, "Bearer " + KEY);
    const mitmNative = await viaGateway(
      aicatCA,
      "/backend-api/codex/responses",
      {
        method: "POST",
        headers: codexHeaders,
        body: nativeBody,
      },
    );
    assert.equal(mitmNative.status, 200);
    assert.equal(backendSeen.at(-1).body, nativeBody);
    assert.equal(mitmNative.headers["x-models-etag"], models.headers.etag);
    const mitmWS = await viaGateway(aicatCA, "/backend-api/codex/responses", {
      headers: {
        Connection: "Upgrade",
        Upgrade: "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==",
        Authorization: CHATGPT,
      },
    });
    assert.equal(mitmWS.status, 426);
    assert(
      !(await state()).domains.includes("localhost"),
      "decryption list was changed",
    );
    check(
      "gateway entry: list merged, provider and ChatGPT turns routed, WebSocket 426",
    );

    // Traffic: provider turns by the provider's URL, the key masked; the
    // decrypted ChatGPT turn captured by the ordinary pipeline.
    const items = JSON.parse(
      (await call(port, "/api/traffic?limit=200")).text,
    ).items;
    const vendorRows = items.filter(
      (r) => r.url === `http://127.0.0.1:${vp}/v1/responses`,
    );
    assert.equal(vendorRows.length, 2);
    const nativeRows = items.filter(
      (r) =>
        r.url === `https://localhost:${bp}/backend-api/codex/responses` &&
        r.statusCode === 200,
    );
    assert(nativeRows.length >= 2, "ChatGPT turns not captured");
    for (const row of items) {
      const record = (await call(port, "/api/traffic/" + row.id)).text;
      assert(!record.includes(KEY), "key leaked into traffic record " + row.id);
    }
    const localModels = items.find((r) =>
      r.url.startsWith(`https://localhost:${bp}/backend-api/codex/models`),
    );
    const modelsRecord = JSON.parse(
      (await call(port, "/api/traffic/" + localModels.id)).text,
    );
    assert(!JSON.stringify(modelsRecord).includes(CHATGPT.slice(7)));
    check("traffic records without the provider key");

    // A manual sync picks up the provider's new model.
    extraModel = true;
    await page.locator("#provider-sync").click();
    await wait(
      async () => (await state()).providers[0].models.models.length === 4,
    );
    await page
      .locator("#provider-models li", { hasText: "late-model" })
      .waitFor();
    check("manual sync updates the list");

    // The extension reports what Codex is offered.
    await page.locator("#nav-extensions").click();
    await page.locator("#extension-codex").click();
    await page.locator("#codex-status").waitFor();
    assert.match(
      await page.locator("#codex-status").innerText(),
      /3 个供应商模型/,
    );
    await shot("codex-on-light");

    // Off again: local entry refused, new tunnels to the backend not decrypted.
    await page.locator("#feature-codex").click();
    await wait(async () => !(await state()).codexEnabled);
    assert.match(config(), /codex: false/);
    assert.equal(
      (
        await call(port, "/backend-api/codex/models", {
          headers: { Authorization: CHATGPT },
        })
      ).status,
      503,
    );
    assert.equal(
      (
        await viaGateway(rootPem, "/backend-api/codex/models", {
          headers: { Authorization: CHATGPT },
        })
      ).issuer,
      "Codex E2E Root",
    );
    check("turning it off stops both entries for new connections");

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
    backend.close();
    vendor.close();
    modelsDev.close();
    console.log(JSON.stringify(report, null, 2));
    process.exitCode = report.passed ? 0 : 1;
  }
})();
