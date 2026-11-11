// Failure modes: domains leaking between apps, duplicate selection highlights,
// collapse dropping app filters, shared expansion keys, and search hiding apps
// whose domain/path matches, or sidebar rows clearing the URL search. Double clicks must toggle once, keep app filters,
// leave leaves closed, and avoid duplicate navigation history entries.
// Use real proxy requests from two live processes.
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
        `button.tree-item[data-app=${JSON.stringify(key)}]` +
          (host === undefined
            ? ":not([data-host])"
            : `[data-host=${JSON.stringify(host)}][data-path=${JSON.stringify(pathname)}]`),
      );
    const toggle = async (host, pathname) => item(host, pathname).dblclick();
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
    assert.equal(await item().locator(".count").textContent(), "5");
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
    assert.equal(await item(domain, "").locator(".count").textContent(), "4");
    await toggle(domain, "");
    await toggle(domain, "/app-tree");
    await toggle(domain, "/app-tree/v1");
    await item(domain, "/app-tree/v1/a").click();
    assert.equal(
      await item(domain, "/app-tree/v1/a").locator(".count").textContent(),
      "2",
    );
    await expectRows(
      appRows.filter((row) =>
        /\/app-tree\/v1\/a(?:\/|$)/.test(new URL(row.url).pathname),
      ),
    );
    assert.equal(
      await apps.locator('.tree-row[data-selected="true"]').count(),
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
    await page.locator('[data-group="domains"] .group-title').click();
    assert.deepEqual(await visibleIDs(), before);
    await page.locator('[data-group="domains"] .group-title').click();
    const global = page.locator(
      `[data-group="domains"] button[data-host=${JSON.stringify(domain)}][data-path=""]`,
    );
    assert.equal(
      await global
        .locator("..")
        .locator(".tree-toggle")
        .getAttribute("aria-expanded"),
      "false",
    );
    await global.click();
    await expectRows(rows.filter((row) => new URL(row.url).host === domain));

    // URL search and sidebar filters compose; choosing a row keeps the search.
    const search = page.locator("#search");
    const child = (row) => new URL(row.url).pathname.endsWith("/child");
    const inDomain = (row) => new URL(row.url).host === domain;
    await search.fill("child");
    await expectRows(rows.filter((row) => inDomain(row) && child(row)));
    await item(domain, "").click();
    await expectRows(appRows.filter((row) => inDomain(row) && child(row)));
    await global.dblclick();
    await global.dblclick();
    assert.equal(await search.inputValue(), "child");
    await expectRows(rows.filter((row) => inDomain(row) && child(row)));
    await search.fill("");
    await expectRows(rows.filter(inDomain));

    const globalToggle = global.locator("..").locator(".tree-toggle");
    await global.dblclick();
    assert.equal(await globalToggle.getAttribute("aria-expanded"), "true");
    await global.dblclick();
    assert.equal(await globalToggle.getAttribute("aria-expanded"), "false");
    // The arrow still supports a single click; a double click toggles only once.
    await globalToggle.click();
    assert.equal(await globalToggle.getAttribute("aria-expanded"), "true");
    await globalToggle.dblclick();
    assert.equal(await globalToggle.getAttribute("aria-expanded"), "false");

    await item(domain, "/app-tree/v1/ab").dblclick();
    // A leaf has a spacer instead of an arrow, so nothing can open.
    assert.equal(
      await item(domain, "/app-tree/v1/ab")
        .locator("..")
        .locator(".twist.none")
        .count(),
      1,
    );
    await expectRows(
      appRows.filter((row) => row.url.endsWith("/app-tree/v1/ab")),
    );
    await page.locator("#history-back").click();
    await expectRows(rows.filter((row) => new URL(row.url).host === domain));

    await global.click({ button: "right" });
    await page
      .locator("#popup")
      .getByRole("menuitem", { name: "固定", exact: true })
      .click();
    await settled();
    const pinned = page.locator(
      `[data-group="pins"] button[data-host=${JSON.stringify(domain)}][data-path=""]`,
    );
    for (const open of ["true", "false"]) {
      await pinned.dblclick();
      assert.equal(
        await pinned
          .locator("..")
          .locator(".tree-toggle")
          .getAttribute("aria-expanded"),
        open,
      );
    }
    for (const group of ["pins", "domains", "apps"]) {
      const title = page.locator(`[data-group="${group}"] .group-title`);
      for (const open of ["false", "true"]) {
        await title.dblclick();
        assert.equal(await title.getAttribute("aria-expanded"), open);
      }
    }

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
    assert.equal(await apps.locator(".tree-item").count(), 0);
    await page.locator("#domain-search").fill("");
    await item(domain, "/app-tree/v1/a").click();
    await page.screenshot({ path: path.join(root, "app-tree-light.png") });
    await page.locator("#theme").click();
    await settled();
    await page.screenshot({ path: path.join(root, "app-tree-dark.png") });

    // The native WebView uses the same live data, with its own local tree state.
    const nativeClick = async (selector, toggle = false) => {
      await evaluate(
        `(() => { const node = document.querySelector(${JSON.stringify(selector)}); ${toggle ? '["click", "click", "dblclick"].forEach((type, i) => node.dispatchEvent(new MouseEvent(type, { bubbles: true, detail: i === 0 ? 1 : 2 })))' : "node.click()"}; return true; })()`,
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
    assert.equal(
      await evaluate(
        `document.querySelector(${JSON.stringify(nativeRoot)}).parentElement.querySelector('.tree-toggle').getAttribute('aria-expanded')`,
      ),
      "true",
    );
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
        'document.querySelectorAll(\'[data-group="apps"] .tree-row[data-selected="true"]\').length',
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
    assert.equal(await apps.locator(".tree-item").count(), 0);
    return {
      passed: true,
      requestCount: rows.length,
      appCount: 2,
      includesUnknownApp: rows.some((row) => !appKey(row)),
      doubleClick: {
        chromium:
          "pointer: groups, app/domain/path/pinned rows, arrows, leaf and history",
        wkwebview: "DOM events: app/domain/path rows",
      },
      nativeErrors,
    };
  } finally {
    python.stdin.end();
    if (python.exitCode === null) python.kill();
  }
};
