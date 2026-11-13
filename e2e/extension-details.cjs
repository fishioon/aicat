const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const assert = require("node:assert/strict");

async function verify({ page, root, current, wait, proxyPort }) {
  const checks = [];
  const panels = [];
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#nav-extensions").click();
  const extensions = (await current()).extensions;
  assert(extensions.length > 0);
  for (const extension of extensions) {
    await page.locator("#extension-" + extension.id).click();
    await page
      .locator(`.extension-detail[data-extension="${extension.id}"]`)
      .waitFor();
    const panel = page.locator(".xd-body > div").first();
    const available =
      extension.id === "wecom-local" ? ["detail"] : ["detail", "features"];
    if (extension.id === "wecom-local")
      assert.equal(
        await page.locator(".xtabs").count(),
        0,
        "a detail-only extension has no redundant tabs",
      );
    let detailText;
    for (const tab of available) {
      const button = page.locator(`[data-tab="${tab}"]`);
      if (await button.count()) await button.click();
      const text = (await panel.innerText()).trim();
      assert(
        text.length > 0,
        `${extension.id}/${tab} must have useful content`,
      );
      if (tab === "detail") detailText = text;
      else
        assert.notEqual(
          text,
          detailText,
          `${extension.id} must not have duplicate tab content`,
        );
      panels.push({ extension: extension.id, tab, text });
    }
    if (extension.id === "wecom-local")
      await page.screenshot({
        path: path.join(root, "local-detail-without-tabs.png"),
      });
    if (!extension.installed) {
      assert.equal(await page.locator("#extension-install").count(), 1);
      assert.equal(await page.locator("#extension-configure").count(), 0);
      assert.equal(await page.locator("#extension-diagnostics").count(), 0);
    } else {
      assert.equal(
        await page.locator("#feature-" + extension.id).isDisabled(),
        !extension.enabled && !extension.ready,
      );
    }
  }
  checks.push(
    "逐项点击当前登记的全部扩展，可用页签均非空且内容不同，无独立功能说明时不展示重复页签，未安装条目只提供安装，启用入口符合实际就绪状态",
  );
  await page.locator("#extension-search").fill("codex");
  assert.equal(await page.locator(".xitem").count(), 1);
  await page.locator("#extension-search").fill("no-such-extension-20261113");
  assert.equal(await page.locator(".xitem").count(), 0);
  await page.locator(".xnote").waitFor();
  await page.locator("#extension-search").fill("");
  assert.equal(await page.locator(".xitem").count(), extensions.length);
  checks.push("扩展搜索可缩小列表，无匹配时有空状态，清空后恢复全部条目");

  await page.locator("#extension-codex").click();
  await page.locator('[data-tab="detail"]').focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(
    await page.locator('[data-tab="features"]').getAttribute("aria-selected"),
    "true",
  );
  await page.keyboard.press("ArrowLeft");
  assert.equal(
    await page.locator('[data-tab="detail"]').getAttribute("aria-selected"),
    "true",
  );
  const disabled = await page.locator(".xd-status").innerText();
  assert(disabled.includes("openai_base_url"));
  assert(!disabled.includes("原样发往 ChatGPT"));
  const response = await fetch(
    `http://127.0.0.1:${proxyPort}/backend-api/codex/models`,
  );
  assert.equal(response.status, 503);
  assert.equal(await page.locator("#codex-manual").getAttribute("open"), null);
  await page.locator("#codex-manual > summary").click();
  const snippet = await page.locator("#codex-snippet").innerText();
  assert.equal(
    snippet,
    `openai_base_url = "http://127.0.0.1:${proxyPort}/backend-api/codex"`,
  );
  await page.locator("#codex-copy").click();
  await wait(
    () => execFileSync("/usr/bin/pbpaste", { encoding: "utf8" }) === snippet,
  );
  await page.locator("#notice").waitFor({ state: "hidden" });
  await page.screenshot({ path: path.join(root, "codex-manual-zh-light.png") });
  checks.push(
    "Codex 停用时直连入口真实返回 503，提示明确删除手动配置；手动指引默认折叠，复制地址对应当前实例",
  );
  await page.locator("#codex-manage-models").click();
  await page.locator("#provider-name").waitFor();
  await page.locator("#nav-extensions").click();
  await page.locator("#codex-proxy-settings").click();
  await page.locator("#listener-address").waitFor();
  checks.push("Codex 的供应商与代理设置入口可用，页签支持键盘切换");

  await page.locator("#settings-general").click();
  await page.locator("#preference-theme-dark").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#nav-extensions").click();
  assert.match(
    await page.locator(".xd-status").innerText(),
    /Disabled.*openai_base_url/,
  );
  await page.locator("#codex-manual > summary").click();
  assert.match(
    await page.locator("#codex-manual").innerText(),
    /Manual connection/,
  );
  await page.locator("#notice").waitFor({ state: "hidden" });
  await page.screenshot({ path: path.join(root, "codex-manual-en-dark.png") });
  await page.locator("#extension-traffic").click();
  await page.locator('[data-tab="features"]').click();
  assert.match(
    await page.locator('.xd-body > [role="tabpanel"]').innerText(),
    /Capture traffic/,
  );
  fs.writeFileSync(
    path.join(root, "extension-panels.json"),
    JSON.stringify(panels, null, 2),
  );
  for (const file of [
    "ui/src/components/ExtensionsPage.vue",
    "ui/src/extensions/index.ts",
    "ui/src/extensions/types.ts",
    "ui/src/extensions/wecom-local/index.ts",
    "ui/src/extensions/codex/Body.vue",
    "ui/src/extensions/codex/index.ts",
  ]) {
    const target = path.join(root, "sources", file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(file, target);
  }
  checks.push("英文深色指引可读，流量扩展说明与托盘的固定采集名称一致");
  return {
    passed: true,
    checks,
    extensions: extensions.map((extension) => extension.id),
    coverage:
      "Real registered extensions and headless UI; no plugin installation, platform login or live Codex app launch",
  };
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "extension-details" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
