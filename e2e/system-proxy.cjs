// Failure modes: a normal proxy session is reported as recovery; toggling asks
// for extra confirmation or requires certificate trust; shutdown/crash loses the
// original settings; restore failures disappear or external edits are overwritten.
// Usage: node e2e/system-proxy.cjs bin/aicat-e2e build/system-proxy-<run>
// Headless preview and real services with isolated OS writes, no native UI.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const original = [
  {
    id: "wifi",
    name: "Wi-Fi",
    proxies: {
      HTTPEnable: 0,
      HTTPProxy: "",
      HTTPPort: 0,
      HTTPSEnable: 0,
      HTTPSProxy: "",
      HTTPSPort: 0,
      ExceptionsList: ["localhost"],
      CustomValue: "keep",
    },
  },
];

async function verify({
  page,
  root,
  home,
  fixture,
  current,
  wait,
  stop,
  launch,
  send,
}) {
  const checks = [];
  const record = path.join(home, "state/system-proxy.json");
  const settings = async () => {
    await page.locator("#nav-settings").click();
    await page.locator("#settings-proxy").click();
  };
  const state = async (enabled) => {
    await wait(async () => {
      const s = await current();
      return (
        s.proxy.enabled === enabled && !s.proxy.recovery && !s.proxy.problem
      );
    });
    assert.equal((await current()).certificate.trusted, false);
    assert.equal(await page.locator("#dialog[open]").count(), 0);
    assert.equal(await page.locator("#system-proxy-restore").count(), 0);
  };
  const shot = (name) =>
    page.screenshot({ path: path.join(root, name + ".png") });
  await settings();
  await state(false);
  await page.locator("#system-proxy-toggle").click();
  await state(true);
  assert.equal(fixture({}).services[0].proxies.HTTPEnable, 1);
  assert.deepEqual(fixture({}).services[0].proxies.ExceptionsList, [
    "localhost",
  ]);
  await shot("proxy-on");
  await page.locator("#nav-overview").click();
  assert.equal(await page.locator("#overview-todos").count(), 0);
  await shot("overview-no-false-warning-light");
  await settings();
  await page.locator("#system-proxy-toggle").click();
  await state(false);
  assert.deepEqual(fixture({}).services, original);
  assert(!fs.existsSync(record));
  checks.push(
    "single toggle works without certificate trust or confirmation; normal takeover has no recovery warning or duplicate restore entry",
  );

  await page.locator("#system-proxy-toggle").click();
  await state(true);
  await stop();
  assert.deepEqual(fixture({}).services, original);
  assert(!fs.existsSync(record));
  await launch();
  await state(true);
  checks.push(
    "normal exit restores original settings; restart reapplies saved intent automatically",
  );

  const abandoned = JSON.parse(fs.readFileSync(record, "utf8"));
  process.kill(abandoned.ownerPid, "SIGKILL");
  await wait(() => {
    try {
      process.kill(abandoned.ownerPid, 0);
      return false;
    } catch (error) {
      if (error.code === "ESRCH") return true;
      throw error;
    }
  });
  await launch();
  await state(true);
  const recovered = JSON.parse(fs.readFileSync(record, "utf8"));
  assert.notEqual(recovered.ownerPid, abandoned.ownerPid);
  assert.deepEqual(recovered.changes[0].before, original[0].proxies);
  checks.push(
    "SIGKILL leaves recovery record; next startup restores original snapshot before re-enabling with a new owner",
  );

  await settings();
  await page.locator("#settings-general").click();
  await page.locator("#preference-theme-dark").click();
  await page.locator("#nav-overview").click();
  assert.equal(await page.locator("#overview-todos").count(), 0);
  await shot("overview-no-false-warning-dark");
  await settings();
  fixture({ failApply: true });
  await page.locator("#system-proxy-toggle").click();
  await wait(
    async () =>
      (await current()).proxy.recovery && !!(await current()).proxy.problem,
  );
  await page.locator("#system-proxy-restore").waitFor();
  await send("/restore-failed-listener-still-live");
  await shot("restore-failed");
  fixture({ failApply: false });
  await page.locator("#system-proxy-restore").click();
  await state(false);
  assert.deepEqual(fixture({}).services, original);
  checks.push(
    "real restore failure exposes retry and keeps listener usable; retry restores OS state and clears recovery",
  );

  await page.locator("#system-proxy-toggle").click();
  await state(true);
  const owned = fixture({}).services;
  const external = structuredClone(owned);
  external[0].proxies.HTTPProxy = "external-edit.invalid";
  fixture({ services: external });
  await page.evaluate(() => window.desktop.refresh());
  await wait(async () => (await current()).proxy.recovery);
  await page.locator("#system-proxy-restore").click();
  await wait(async () =>
    (await current()).proxy.problem.includes("保留现状与恢复记录"),
  );
  await page.locator(".busy").waitFor({ state: "hidden" });
  assert.deepEqual(fixture({}).services, external);
  assert(fs.existsSync(record));
  fixture({ services: owned });
  await page.locator("#system-proxy-restore").click();
  await state(false);
  assert.deepEqual(fixture({}).services, original);
  checks.push(
    "external proxy edits are preserved; restoring an owned state succeeds without losing the backup",
  );
  return { passed: true, checks };
}

require("./traffic-default.cjs")
  .run({
    verify,
    scenario: "system-proxy",
    fixture: { services: original, trusted: false, recordProxyWrites: true },
  })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
