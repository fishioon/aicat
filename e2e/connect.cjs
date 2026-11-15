const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const assert = require("node:assert/strict");

async function verify({ page, root, home, current, wait, stop, launch }) {
  const checks = [];
  const sources = [
    "ui/src/components/ConnectPage.vue",
    "internal/app/desktop.go",
  ];
  for (const file of sources) {
    const target = path.join(root, "sources", file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(file, target);
  }
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#nav-overview").click();
  await page.locator("#connect-agent").click();
  const tokenPath = path.join(home, "mcp-token");
  assert.equal((await current()).mcpTokenPath, tokenPath);
  const token = fs.readFileSync(tokenPath, "utf8").trim();
  const actualToken = (value) =>
    assert(value === token, "command reads only this instance's credential");
  const snapshot = () => page.locator("#connect").innerText();
  const noLeak = async () => {
    assert(!(await snapshot()).includes(token), "credential is not rendered");
    assert(
      !JSON.stringify(await current()).includes(token),
      "state contains path, not credential",
    );
  };
  const copied = async (button, expected) => {
    await page.locator(button).click();
    await wait(
      () => execFileSync("/usr/bin/pbpaste", { encoding: "utf8" }) === expected,
    );
  };
  const shot = async (name) => {
    await page.locator("#notice").waitFor({ state: "hidden" });
    await page.screenshot({ path: path.join(root, name + ".png") });
  };
  const cli = path.join(root, "stub-bin");
  fs.mkdirSync(cli);
  fs.writeFileSync(
    path.join(cli, "claude"),
    "#!/usr/bin/env node\nprocess.stdout.write(JSON.stringify(process.argv.slice(2)));\n",
    { mode: 0o700 },
  );
  const shell = (code) =>
    execFileSync("/bin/zsh", ["-f", "-c", code], {
      cwd: root,
      env: { ...process.env, PATH: cli + path.delimiter + process.env.PATH },
      encoding: "utf8",
      timeout: 5000,
    });
  const command = await page.locator("#snippet").innerText();
  assert(
    command.includes("cat "),
    "Claude command reads credential at execution time",
  );
  assert(!command.includes("~/.aicat/mcp-token"), "custom Home is respected");
  await copied("#copy-snippet", command);
  const args = JSON.parse(shell(command));
  assert.deepEqual(args.slice(0, 5), [
    "mcp",
    "add",
    "--transport",
    "http",
    "aicat",
  ]);
  const endpoint = args[5];
  assert.equal(new URL(endpoint).pathname, "/mcp");
  assert.equal(args[6], "--header");
  actualToken(args[7].replace(/^Authorization: Bearer /, ""));
  await noLeak();
  await shot("claude-zh-light");
  checks.push(
    "Claude 配置复制后通过命令接收器检查参数；自定义 Home 与 shell 特殊字符正确，未修改真实 Claude 配置",
  );

  await page.locator("#client-codex").click();
  const config = await page.locator("#snippet").innerText();
  assert(config.includes(`url = "${endpoint}"`));
  assert(config.includes('bearer_token_env_var = "AICAT_MCP_TOKEN"'));
  await copied("#copy-snippet", config);
  const environment = await page.locator("#token-command").innerText();
  await copied("#copy-token-command", environment);
  actualToken(
    shell(
      environment +
        '\nnode -e "process.stdout.write(process.env.AICAT_MCP_TOKEN)"',
    ),
  );
  await noLeak();
  await shot("codex-zh-light");
  checks.push(
    "Codex 的配置与环境变量命令均可复制，命令读取当前实例令牌而非默认 Home",
  );

  await page.locator("#client-cursor").click();
  const cursor = await page.locator("#snippet").innerText();
  await copied("#copy-snippet", cursor);
  const data = JSON.parse(cursor).mcpServers.aicat;
  assert.equal(data.url, endpoint);
  assert.match(data.headers.Authorization, /^Bearer </);
  const readToken = await page.locator("#token-command").innerText();
  await copied("#copy-token-command", readToken);
  actualToken(shell(readToken).trim());
  await noLeak();
  checks.push("Cursor JSON 有效，读取命令与凭据占位内容形成完整操作路径");

  await page.locator("#client-other").click();
  assert((await page.locator("#snippet").innerText()).includes(endpoint));
  actualToken(shell(await page.locator("#token-command").innerText()).trim());
  for (const button of await page.locator("#connect-endpoints button").all()) {
    const expected = await button.locator("..").locator("code").innerText();
    await button.click();
    await wait(
      () => execFileSync("/usr/bin/pbpaste", { encoding: "utf8" }) === expected,
    );
  }
  checks.push("通用 MCP 指引可读取令牌，四个接入地址复制结果与显示一致");

  const rpc = async (method, credential, headers = {}) => {
    const response = await fetch(endpoint, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
        ...(credential ? { Authorization: "Bearer " + credential } : {}),
        ...headers,
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method,
        ...(method === "initialize"
          ? {
              params: {
                protocolVersion: "2025-06-18",
                capabilities: {},
                clientInfo: { name: "connect-e2e", version: "1" },
              },
            }
          : {}),
      }),
    });
    return { status: response.status, text: await response.text() };
  };
  const initialized = await rpc("initialize", token);
  assert.equal(initialized.status, 200);
  assert.equal(
    JSON.parse(initialized.text).result.protocolVersion,
    "2025-06-18",
  );
  const tools = await rpc("tools/list", token);
  assert.equal(tools.status, 200);
  const names = JSON.parse(tools.text)
    .result.tools.map((tool) => tool.name)
    .sort();
  assert.deepEqual(names, [
    "messages_send",
    "rooms_reply",
    "traffic_body",
    "traffic_get",
    "traffic_list",
  ]);
  assert.equal((await rpc("tools/list", "")).status, 401);
  assert.equal((await rpc("tools/list", "0".repeat(64))).status, 401);
  assert.equal(
    (await rpc("tools/list", token, { Origin: "http://localhost" })).status,
    403,
  );
  checks.push(
    "页面地址与命令凭据可真实初始化 MCP 并发现工具；缺失/错误凭据返回 401，浏览器 Origin 返回 403",
  );
  assert(
    !fs.existsSync(path.join(root, "AICAT_UNEXPECTED")),
    "path text never executes as a command",
  );

  await page.locator("#nav-settings").click();
  await page.locator("#preference-theme-dark").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#nav-overview").click();
  await page.locator("#connect-agent").click();
  await page.locator("#client-cursor").click();
  assert((await snapshot()).includes("Read the token in a terminal"));
  assert.match(
    JSON.parse(await page.locator("#snippet").innerText()).mcpServers.aicat
      .headers.Authorization,
    /contents of the token file/,
  );
  await noLeak();
  await shot("cursor-en-dark");
  await stop();
  await launch();
  await page.locator("#connect-agent").click();
  const restartedArgs = JSON.parse(
    shell(await page.locator("#snippet").innerText()),
  );
  actualToken(restartedArgs[7].replace(/^Authorization: Bearer /, ""));
  assert.equal((await current()).mcpTokenPath, tokenPath);
  checks.push("英文与深色指引有效，重启后令牌路径和认证身份保留");
  return {
    passed: true,
    checks,
    clientCoverage:
      "Real Desktop services, OS clipboard, zsh command execution, MCP HTTP authentication; Claude arguments captured by a local stub, Codex/Cursor apps are not launched",
  };
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "connect" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
