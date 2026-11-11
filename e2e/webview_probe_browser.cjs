const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || 'playwright');

(async () => {
  const [url, output] = process.argv.slice(2);
  fs.mkdirSync(output, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1180, height: 900 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const report = { passed: false, engine: 'Chromium', checks: [] };
  try {
    await page.goto(url);
    await page.waitForFunction(() => window.probe?.state().total > 100);
    if (await page.evaluate(() => !!window.mygo)) throw new Error('unexpected MyGo bridge in browser');
    const search = page.locator('#search');
    await search.click();
    await search.pressSequentially('/body');
    await page.waitForFunction(() => window.probe.state().visible === 1);
    await page.locator('.traffic-row').click();
    await page.waitForFunction(() => window.probe.state().detailRevision > 0);
    await page.locator('[data-side=response] [data-view=raw]').click();
    await page.waitForFunction(() => document.querySelector('[data-side=response] .raw').textContent.includes('body-fixture'));
    if (await page.evaluate(() => !!window.payloadExecuted)) throw new Error('body executed as HTML');
    await search.fill('');
    await page.waitForFunction(() => window.probe.state().visible > 100);
    await page.locator('#rows-viewport').hover();
    await page.mouse.wheel(0, 1500);
    await page.waitForFunction(() => window.probe.state().scrollTop > 100);
    if (await page.locator('.traffic-row').count() > 80) throw new Error('unbounded DOM rows');
    await page.screenshot({ path: path.join(output, 'traffic.png') });
    await page.locator('#theme').click();
    await page.screenshot({ path: path.join(output, 'traffic-dark.png') });
    if (errors.length) throw new Error(errors.join('\n'));
    report.checks.push('same frontend loads over HTTP without MyGo', 'real keyboard search, row click, Raw body, wheel scroll and theme work');
    report.passed = true;
  } catch (error) {
    report.error = String(error);
    await page.screenshot({ path: path.join(output, 'failure.png') });
    process.exitCode = 1;
  } finally {
    fs.writeFileSync(path.join(output, 'report.json'), JSON.stringify(report, null, 2));
    await browser.close();
  }
})();
