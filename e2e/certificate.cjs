const fs = require("node:fs");
const path = require("node:path");
const { X509Certificate } = require("node:crypto");
const assert = require("node:assert/strict");

async function verify({
  page,
  root,
  home,
  current,
  wait,
  fixture,
  stop,
  launch,
}) {
  const checks = [];
  const settings = async () => {
    await page.locator("#nav-settings").click();
    await page.locator("#settings-proxy").click();
  };
  const settle = () =>
    page.waitForFunction(() => !document.body.classList.contains("busy"));
  const details = async () => {
    const element = page.locator("#certificate-details");
    if ((await element.getAttribute("open")) === null)
      await element.locator("summary").click();
  };
  const click = async (id) => {
    await page.locator("#" + id).click();
    await settle();
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
  await page.locator("#nav-settings").click();
  await page.locator("#preference-language-zh-Hans").click();
  await page.waitForFunction(() => document.documentElement.lang === "zh-Hans");
  await page.locator("#settings-proxy").click();
  assert.equal(
    await page.locator("#certificate-details").getAttribute("open"),
    null,
  );
  assert(!(await page.locator("#certificate-export").isVisible()));
  const initial = (await current()).certificate.fingerprint;
  fixture({ cancelInstall: true });
  await click("certificate-install");
  assert(!(await current()).certificate.trusted);
  assert.equal((await current()).certificate.fingerprint, initial);
  fixture({ cancelInstall: false, failInstall: true });
  await click("certificate-install");
  assert(!(await current()).certificate.trusted);
  assert.equal((await current()).certificate.fingerprint, initial);
  fixture({ failInstall: false, installWithoutTrust: true });
  await click("certificate-install");
  assert(
    !(await current()).certificate.trusted,
    "successful OS call alone is not proof of trust",
  );
  fixture({ installWithoutTrust: false });
  await click("certificate-install");
  assert((await current()).certificate.trusted);
  assert.equal((await current()).certificate.fingerprint, initial);
  checks.push(
    "授权取消、失败或未建立信任均不冒充已信任；重试沿用同一证书身份，成功后显示实际信任",
  );

  await details();
  assert((await page.locator(".cert").innerText()).includes(initial));
  fixture({ cancelExport: true });
  await click("certificate-export");
  const exported = path.join(root, "aicat-root-ca.pem");
  assert(!fs.existsSync(exported));
  fixture({ cancelExport: false });
  await click("certificate-export");
  const pem = fs.readFileSync(exported, "utf8");
  assert(!pem.includes("PRIVATE KEY"));
  assert.equal(
    new X509Certificate(pem).fingerprint256.replace(/:/g, ""),
    initial,
  );
  await click("certificate-keychain");
  assert(fixture({}).keychainOpened);
  fixture({ trusted: false });
  await click("certificate-recheck");
  assert(!(await current()).certificate.trusted);
  await click("certificate-install");
  assert((await current()).certificate.trusted);
  checks.push(
    "详情默认收起且可展开，导出取消不落盘，实际公开证书指纹正确且不含私钥；钥匙串入口与重新检测有效",
  );

  await details();
  await page.locator("#certificate-delete").click();
  await page
    .locator("#dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  assert.equal((await current()).certificate.fingerprint, initial);
  fixture({ failRemove: true });
  await page.locator("#certificate-delete").click();
  await confirm();
  assert((await current()).certificate.pendingDeletion);
  assert.equal(await page.locator("#certificate-details").count(), 0);
  await shot("deletion-pending");
  await stop();
  await launch();
  assert((await current()).certificate.pendingDeletion);
  await settings();
  fixture({ failRemove: false });
  await page.locator("#certificate-delete").click();
  await confirm();
  assert(!(await current()).certificate.exists);
  assert(!fs.existsSync(path.join(home, "ca", "mitmproxy-ca.pem")));
  await stop();
  await launch();
  assert(
    !(await current()).certificate.exists,
    "restart never silently recreates a deleted identity",
  );
  await settings();
  await click("certificate-install");
  assert((await current()).certificate.trusted);
  assert.notEqual((await current()).certificate.fingerprint, initial);
  checks.push(
    "删除确认可取消，删除失败保留可重试状态并跨重启恢复；完成后不自动重建，主动重装生成新身份",
  );

  await stop();
  require("./certificate-fixture.cjs")(root, path.join(home, "ca"), true);
  await launch();
  await settings();
  assert((await current()).certificate.expired);
  const expired = (await current()).certificate.fingerprint;
  assert.equal(await page.locator("#certificate-install").count(), 0);
  await page.locator("#certificate-replace").click();
  await page
    .locator("#dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  assert.equal((await current()).certificate.fingerprint, expired);
  fixture({ failInstall: true });
  await page.locator("#certificate-replace").click();
  await confirm();
  assert(!(await current()).certificate.expired);
  assert(!(await current()).certificate.trusted);
  assert(!(await current()).certificate.pendingDeletion);
  const replacement = (await current()).certificate.fingerprint;
  assert.notEqual(replacement, expired);
  fixture({ failInstall: false });
  await click("certificate-install");
  assert((await current()).certificate.trusted);
  assert.equal((await current()).certificate.fingerprint, replacement);
  await shot("expired-certificate-replaced");
  checks.push(
    "真实过期证书有明确更换入口，取消不改变身份；确认后更换身份，安装失败可重试并沿用新身份",
  );

  await page.locator("#settings-general").click();
  await page.locator("#preference-theme-dark").click();
  await page.waitForFunction(
    () => document.documentElement.dataset.theme === "dark",
  );
  await page.locator("#preference-language-en").click();
  await page.waitForFunction(() => document.documentElement.lang === "en");
  await page.locator("#settings-proxy").click();
  await details();
  assert.match(
    await page.locator("#certificate-details > summary").innerText(),
    /Certificate details/,
  );
  await page.locator("#notice").waitFor({ state: "hidden" });
  await shot("certificate-details-en-dark");
  fs.copyFileSync(
    "ui/src/components/SettingsPage.vue",
    path.join(root, "SettingsPage.vue"),
  );
  return {
    passed: true,
    checks,
    coverage:
      "Real identity files, expired CA fixture, persistence, public export and Desktop services; only native authorization/keychain/save panels use OS adapters",
  };
}

require("./traffic-default.cjs")
  .run({ verify, scenario: "certificate" })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
