// Failure modes: capture labels change instead of checkbox state; proxy and
// capture states are coupled; icons do not follow explicit refresh; inspecting
// externally changed settings overwrites them. No native menu is opened.
// Tail motion must preserve the chosen 30% endpoint, fixed body/canvas and
// length/width; clicks must return to the current proxy state, handle repeated
// clicks, and stop stale frames if proxy state changes or the app exits.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

async function verify({
  page,
  root,
  preview,
  fixture,
  wait,
  api,
  send,
  current,
  stop,
}) {
  const checks = [];
  const menu = async (action) => {
    const response = await preview("/tray", action ? { action } : undefined);
    assert.equal(response.status, 200, response.data);
    return JSON.parse(response.data);
  };
  const item = (snapshot, id) => {
    const find = (entries) => {
      for (const entry of entries) {
        if (entry.id === id) return entry;
        const match = find(entry.submenu ?? []);
        if (match) return match;
      }
    };
    return find(snapshot.items);
  };
  const save = (name, value) =>
    fs.writeFileSync(
      path.join(root, name + ".json"),
      JSON.stringify(value, null, 2),
    );
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const animate = async (name, taken) => {
    const before = await menu();
    await menu("click");
    await wait(async () => (await menu()).generation > before.generation);
    let result = await menu();
    const generation = result.generation;
    await wait(async () => !(await menu()).animating);
    result = await menu();
    const events = result.trace.filter(
      (entry) => entry.generation === generation,
    );
    const from = taken ? result.frameCount - 1 : 0;
    const to = result.frameCount - 1 - from;
    assert.equal(result.taken, taken);
    assert.equal(result.frame, from);
    assert.equal(result.currentIcon, result.icon);
    if (!result.reduceMotion) {
      assert.equal(events[0].frame, from);
      assert.equal(events.at(-1).frame, from);
      assert(
        events.some((entry) => entry.frame === to),
        "round trip reaches the other chosen endpoint",
      );
      assert(new Set(events.map((entry) => entry.frame)).size >= 8);
      assert(events.at(-1).atMs - events[0].atMs >= 550);
    }
    save(name, result);
    return result;
  };
  await page.locator("#nav-settings").click();
  await page.locator("#settings-general").click();
  await page.locator("#preference-language-zh-Hans").click();
  await wait(async () => (await menu()).items[0].label === "打开 aicat");
  let initial = await menu("refresh");
  assert.deepEqual(
    initial.items.filter((x) => x.id).map((x) => x.id),
    [
      "open",
      "capture",
      "proxy",
      "listener",
      "connect",
      "extensions",
      "settings",
      "help",
      "quit",
    ],
  );
  assert.equal(item(initial, "capture").label, "采集流量");
  assert.equal(item(initial, "capture").type, "checkbox");
  assert.equal(item(initial, "capture").checked, false);
  assert.equal(initial.taken, false);
  assert.equal(
    initial.icon,
    fs.readFileSync("assets/tray.png").toString("base64"),
  );
  assert.equal(item(initial, "proxy").label, "系统代理：未接管");
  assert.match(item(initial, "listener").label, /监听于 127\.0\.0\.1:/);
  fs.writeFileSync(
    path.join(root, "tray-inactive.png"),
    Buffer.from(initial.icon, "base64"),
  );
  save("initial-menu", initial);
  assert.equal(initial.frameCount, 25);
  assert.equal(initial.durationMs, 600);
  assert.equal(initial.frame, 0);
  await animate("inactive-click-round-trip", false);
  const firstClick = await menu();
  await menu("click");
  await wait(async () => (await menu()).generation > firstClick.generation);
  const repeatedStart = await menu();
  await sleep(70);
  await menu("click");
  await wait(async () => (await menu()).generation > repeatedStart.generation);
  const repeatedGeneration = (await menu()).generation;
  await wait(async () => !(await menu()).animating);
  const repeated = await menu();
  assert.equal(repeated.frame, 0);
  const newStart = repeated.trace.findIndex(
    (entry) => entry.generation === repeatedGeneration,
  );
  assert(newStart >= 0);
  assert(
    repeated.trace
      .slice(newStart)
      .every((entry) => entry.generation === repeatedGeneration),
    "superseded clicks cannot display stale frames",
  );
  save("repeated-click", repeated);
  checks.push(
    "100%→30%→100% native icon updates and rapid-click cancellation return to the inactive endpoint",
  );

  await menu("capture");
  await wait(async () => item(await menu(), "capture").checked);
  await send("/tray-capture-on");
  await wait(async () =>
    (await api()).some((row) => row.url.endsWith("/tray-capture-on")),
  );
  assert.equal(item(await menu(), "capture").label, "采集流量");
  assert.equal((await menu()).taken, false);
  await menu("capture");
  await wait(async () => !item(await menu(), "capture").checked);
  await send("/tray-capture-off");
  assert(!(await api()).some((row) => row.url.endsWith("/tray-capture-off")));
  checks.push(
    "native capture item keeps its label; checkmark controls real capture independently of system proxy",
  );

  await page.locator("#settings-proxy").click();
  await page.locator("#certificate-install").click();
  await wait(async () => (await current()).certificate.trusted);
  await page.locator("#system-proxy-toggle").click();
  await page.locator("#dialog").getByText("取消", { exact: true }).click();
  assert.equal((await menu()).taken, false);
  await page.locator("#system-proxy-toggle").click();
  await page.locator("#dialog").getByText("确定", { exact: true }).click();
  await wait(async () => (await menu()).taken);
  const active = await menu();
  assert.equal(item(active, "proxy").label, "系统代理：已接管");
  assert.notEqual(active.icon, initial.icon);
  assert.deepEqual(
    Buffer.from(active.icon, "base64").subarray(16, 24),
    Buffer.from(initial.icon, "base64").subarray(16, 24),
    "both tail states must share the original logo canvas",
  );
  assert.equal(item(active, "capture").checked, false);
  fs.writeFileSync(
    path.join(root, "tray-active.png"),
    Buffer.from(active.icon, "base64"),
  );
  save("active-menu", active);
  assert.equal(
    active.icon,
    fs.readFileSync("assets/tray-active.png").toString("base64"),
  );
  assert.equal(active.frame, active.frameCount - 1);
  await animate("active-click-round-trip", true);
  await page.screenshot({ path: path.join(root, "proxy-active.png") });
  checks.push(
    "proxy takeover requires confirmation, changes icon and leaves capture off",
  );

  const owned = fixture({});
  const changed = structuredClone(owned.services);
  changed[0].proxies.HTTPProxy = "127.0.0.2";
  const edited = fixture({ services: changed });
  assert.equal(
    (await menu()).taken,
    true,
    "no continuous refresh before user action",
  );
  // The same click path refreshes actual proxy state. Start the animation
  // before changing the fixture so explicit refresh interrupts a live trip.
  fixture({ services: owned.services });
  await menu("click");
  if (!(await menu()).reduceMotion)
    await wait(async () => (await menu()).animating);
  fixture({ services: changed });
  const refreshed = await menu("refresh");
  assert.equal(refreshed.taken, false);
  assert.equal(refreshed.icon, initial.icon);
  assert.equal(item(refreshed, "proxy").label, "系统代理：未接管");
  assert.equal(item(refreshed, "proxy-toggle").label, "处理代理设置…");
  assert.equal(refreshed.animating, false);
  assert.equal(refreshed.frame, 0);
  await sleep(700);
  assert.equal(
    (await menu()).currentIcon,
    initial.icon,
    "stale frames must not restore the previously active icon",
  );
  assert.deepEqual(
    fixture({}),
    edited,
    "read-only refresh must not reclaim proxy settings",
  );
  save("changed-settings-menu", refreshed);
  checks.push(
    "manual settings changes are read only on explicit refresh; inactive icon and no attribution or automatic takeover",
  );
  // Put the fixture back in the owned state so normal shutdown can restore it.
  fixture({ services: owned.services });
  await menu("refresh");
  await page.locator("#system-proxy-toggle").click();
  await wait(async () => !(await menu()).taken);
  await page.locator("#settings-general").click();
  await page.locator("#preference-language-en").click();
  await wait(
    async () => item(await menu(), "capture").label === "Record Traffic",
  );
  const english = await menu();
  assert.equal(item(english, "proxy").label, "System Proxy: Not Overridden");
  assert.equal(item(english, "connect").label, "Connect Agent…");
  save("english-menu", english);
  checks.push(
    "proxy restoration and English labels update existing native menu items",
  );
  assert.equal((await current()).recording, false);
  await menu("click");
  if (!(await menu()).reduceMotion)
    await wait(async () => (await menu()).animating);
  await stop();
  checks.push(
    "30%→100%→30% uses the native SetIcon path; a newer proxy state and shutdown cancel pending frames",
  );
  return {
    passed: true,
    checks,
    coverage:
      "actual native menu model and native SetIcon updates via the shared tray click handler in a hidden app; browser inputs; no native popup/status-bar pixel rendering",
  };
}

require("./traffic-default.cjs")
  .run({
    verify,
    scenario: "tray",
    fixture: {
      services: [
        {
          id: "wifi",
          name: "Wi-Fi",
          proxies: { HTTPEnable: 0, HTTPSEnable: 0 },
        },
      ],
      trusted: true,
      recordProxyWrites: true,
    },
  })
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
