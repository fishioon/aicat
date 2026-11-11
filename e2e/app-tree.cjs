// Failure modes: domains leaking between apps, duplicate selection highlights,
// collapse dropping app filters, shared expansion keys, and search hiding apps
// whose domain/path matches. Use real proxy requests from two live processes.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

module.exports = async function verifyAppTree({
  page,
  root,
  send,
  request,
  api,
  fixturePort,
  proxyPort,
  navigate,
  settled,
  evaluate,
  wait,
  auto,
}) {
  await navigate("traffic");
  await page.locator("#clear").click();
  await page.waitForFunction(() => window.trafficView.state().total === 0);
  const domain = `127.0.0.1:${fixturePort}`;
  for (const target of [
    "/app-tree/v1/a",
    "/app-tree/v1/ab",
    "/app-tree/v1/a/child",
    "/app-tree/v10/sibling",
  ])
    await send(target);
  await request(proxyPort, `http://localhost:${fixturePort}/other-host`);

  const python = spawn(
    "python3",
    [
      "-u",
      "-c",
      `
import http.client, sys
c = http.client.HTTPConnection('127.0.0.1', ${proxyPort})
for p in ['/app-tree/v1/a', '/python-only']:
    c.request('GET', 'http://${domain}' + p)
    r = c.getresponse()
    assert r.status == 200
    r.read()
print('ready', flush=True)
sys.stdin.read()
c.close()
`,
    ],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  let output = "",
    stderr = "";
  python.stdout.on("data", (data) => (output += data));
  python.stderr.on("data", (data) => (stderr += data));
  try {
    await wait(() => {
      assert(python.exitCode === null, stderr || "Python fixture exited early");
      return output.includes("ready");
    });
    let rows;
    const appKey = (row) =>
      row.clientApp?.bundleId
        ? "bundle:" + row.clientApp.bundleId.toLowerCase()
        : row.clientApp?.name
          ? "process:" + row.clientApp.name.toLowerCase()
          : "";
    await wait(async () => {
      rows = await api();
      return (
        rows.length === 7 &&
        new Set(rows.map(appKey)).size === 2 &&
        rows.some((row) => row.url.endsWith("/python-only") && appKey(row))
      );
    });
    fs.writeFileSync(
      path.join(root, "app-tree-requests.json"),
      JSON.stringify(rows, null, 2),
    );
    const first = rows.find((row) => row.url.endsWith("/other-host"));
    const key = appKey(first);
    const appRows = rows.filter((row) => appKey(row) === key);
    assert.equal(appRows.length, 5);
    await page.waitForFunction(() => window.trafficView.state().total === 7);

    const apps = page.locator('[data-group="apps"]');
    const item = (host, pathname) =>
      apps.locator(
        `button.ac-side-item[data-app=${JSON.stringify(key)}]` +
          (host === undefined
            ? ":not([data-host])"
            : `[data-host=${JSON.stringify(host)}][data-path=${JSON.stringify(pathname)}]`),
      );
    const toggle = async (host, pathname) =>
      item(host, pathname).locator("..").locator(".ac-tree-toggle").click();
    const visibleIDs = () =>
      page.evaluate(() => window.trafficView.state().ids);
    const expectRows = async (expected) => {
      const ids = expected.map((row) => row.id).sort((a, b) => a - b);
      await wait(
        async () => JSON.stringify(await visibleIDs()) === JSON.stringify(ids),
      );
    };
    await item().click();
    await expectRows(appRows);
    assert.equal(await item().locator(".ac-count").textContent(), "5");
    await toggle();
    assert.equal(
      await apps
        .locator(
          `button[data-app=${JSON.stringify(key)}][data-host][data-path=""]`,
        )
        .count(),
      2,
    );
    await item(domain, "").click();
    await expectRows(appRows.filter((row) => new URL(row.url).host === domain));
    assert.equal(
      await item(domain, "").locator(".ac-count").textContent(),
      "4",
    );
    await toggle(domain, "");
    await toggle(domain, "/app-tree");
    await toggle(domain, "/app-tree/v1");
    await item(domain, "/app-tree/v1/a").click();
    assert.equal(
      await item(domain, "/app-tree/v1/a").locator(".ac-count").textContent(),
      "2",
    );
    await expectRows(
      appRows.filter((row) =>
        /\/app-tree\/v1\/a(?:\/|$)/.test(new URL(row.url).pathname),
      ),
    );
    assert.equal(
      await apps.locator('.ac-tree-row[data-selected="true"]').count(),
      1,
    );
    await toggle(domain, "/app-tree/v1");
    await expectRows(
      appRows.filter((row) =>
        new URL(row.url).pathname.startsWith("/app-tree/v1/"),
      ),
    );
    await page.locator("#history-back").click();
    await expectRows(
      appRows.filter((row) =>
        /\/app-tree\/v1\/a(?:\/|$)/.test(new URL(row.url).pathname),
      ),
    );
    await page.locator("#history-forward").click();
    await expectRows(
      appRows.filter((row) =>
        new URL(row.url).pathname.startsWith("/app-tree/v1/"),
      ),
    );
    await toggle(domain, "");
    await expectRows(appRows.filter((row) => new URL(row.url).host === domain));
    await toggle();
    await expectRows(appRows);
    await toggle();
    await toggle(domain, "");
    await toggle(domain, "/app-tree/v1");
    await item(domain, "/app-tree/v1/a").click();
    // Folding the independent Domain group must not clear an App subtree filter.
    const before = await visibleIDs();
    await page.locator('[data-group="domains"] .ac-domain-toggle').click();
    assert.deepEqual(await visibleIDs(), before);
    await page.locator('[data-group="domains"] .ac-domain-toggle').click();
    const global = page.locator(
      `[data-group="domains"] button[data-host=${JSON.stringify(domain)}][data-path=""]`,
    );
    assert.equal(
      await global
        .locator("..")
        .locator(".ac-tree-toggle")
        .getAttribute("aria-expanded"),
      "false",
    );
    await global.click();
    await expectRows(rows.filter((row) => new URL(row.url).host === domain));

    for (const query of [
      "localhost",
      "python-only",
      first.clientApp?.name ?? "未知应用",
    ]) {
      await page.locator("#domain-search").fill(query);
      assert(
        (await apps.locator("button[data-app]:not([data-host])").count()) > 0,
      );
    }
    await page.locator("#domain-search").fill("no-such-app-or-domain");
    assert.equal(await apps.locator(".ac-side-item").count(), 0);
    await page.locator("#domain-search").fill("");
    await item(domain, "/app-tree/v1/a").click();
    await page.screenshot({ path: path.join(root, "app-tree-light.png") });
    await page.locator("#theme").click();
    await settled();
    await page.screenshot({ path: path.join(root, "app-tree-dark.png") });

    // The native WebView uses the same live data, with its own local tree state.
    const nativeClick = async (selector, toggle = false) => {
      await evaluate(
        `(() => { const node = document.querySelector(${JSON.stringify(selector)}); ${toggle ? 'node.parentElement.querySelector(".ac-tree-toggle")' : "node"}.click(); return true; })()`,
      );
    };
    const nativeRoot = `[data-group="apps"] button[data-app=${JSON.stringify(key)}]:not([data-host])`;
    await wait(
      async () =>
        await evaluate(
          `!!document.querySelector(${JSON.stringify(nativeRoot)})`,
        ),
    );
    await nativeClick(nativeRoot, true);
    const nativeBranch = (pathname) =>
      `[data-group="apps"] button[data-app=${JSON.stringify(key)}][data-host=${JSON.stringify(domain)}][data-path=${JSON.stringify(pathname)}]`;
    for (const pathname of ["", "/app-tree", "/app-tree/v1"])
      await nativeClick(nativeBranch(pathname), true);
    await nativeClick(nativeBranch("/app-tree/v1/a"));
    await wait(
      async () => (await evaluate("window.trafficView.state().visible")) === 2,
    );
    assert.equal(
      await evaluate(
        'document.querySelectorAll(\'[data-group="apps"] .ac-tree-row[data-selected="true"]\').length',
      ),
      1,
    );
    const nativeErrors = await evaluate("window.trafficView.metrics.errors");
    assert.deepEqual(nativeErrors, []);
    const capture = await auto.call("capture");
    fs.writeFileSync(
      path.join(root, "app-tree-wkwebview.png"),
      Buffer.from(capture.png, "base64"),
    );
    await page.locator("#theme").click();
    await settled();
    await page.locator("#clear").click();
    await page.waitForFunction(() => window.trafficView.state().total === 0);
    assert.equal(await apps.locator(".ac-side-item").count(), 0);
    return {
      passed: true,
      requestCount: rows.length,
      appCount: 2,
      includesUnknownApp: rows.some((row) => !appKey(row)),
      nativeErrors,
    };
  } finally {
    python.stdin.end();
    if (python.exitCode === null) python.kill();
  }
};
