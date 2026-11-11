// Render docs/design/aicat-redesign.html scenes in light and dark for review.
// Scenes use the design's hash entry (page, sel, set, connect, ...).
// Usage: node e2e/design-shots.cjs [output-dir] [scene...]
const fs = require("node:fs"),
  path = require("node:path");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const SOURCE = path.resolve("docs/design/aicat-redesign.html");
const scenes = {
  overview: "page=overview",
  "overview-first": "page=overview&state=first",
  connect: "page=overview&connect=1",
  provider: "page=overview&connect=provider",
  traffic: "page=traffic&sel=2231&resTab=events",
  "traffic-json": "page=traffic&sel=2236&reqTab=query&resTab=body",
  "traffic-tunnel": "page=traffic&sel=2233",
  extensions: "page=ext",
  "settings-general": "page=settings&set=general",
  "settings-proxy": "page=settings&set=proxy",
};

async function main() {
  const [outArg, ...picked] = process.argv.slice(2);
  const stamp = new Date().toISOString().replace(/\D/g, "").slice(0, 14);
  const output = path.resolve(outArg || `build/design-shots-${stamp}`);
  fs.mkdirSync(output, { recursive: true });
  const browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AICAT_CHROMIUM ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  const failures = [];
  try {
    for (const name of picked.length ? picked : Object.keys(scenes)) {
      if (!scenes[name]) {
        failures.push(`${name}: unknown scene`);
        continue;
      }
      for (const theme of ["light", "dark"]) {
        const page = await browser.newPage({
          viewport: { width: 1272, height: 852 },
          deviceScaleFactor: 2,
        });
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const file = path.join(output, `${name}-${theme}.png`);
        try {
          await page.goto(`file://${SOURCE}#${scenes[name]}&theme=${theme}`);
          await page.mouse.move(0, 0);
          await page.waitForTimeout(150);
          await page.screenshot({ path: file });
          if (errors.length) failures.push(`${name}-${theme}: ${errors}`);
        } catch (e) {
          failures.push(`${name}-${theme}: ${e.message.split("\n")[0]}`);
        }
        await page.close();
      }
    }
  } finally {
    await browser.close();
  }
  console.log(output);
  for (const failure of failures) console.error("FAIL " + failure);
  process.exitCode = failures.length ? 1 : 0;
}

main();
