// Failure modes: the generated asset differs from the accepted 30% SVG;
// frames resize/shift the body, alter tail width/length, or depend on stale
// generated files. This exercises the real generator and manual SVG page.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");
const { pathToFileURL } = require("node:url");
const { chromium } = require(process.env.AICAT_PLAYWRIGHT || "playwright");

const paths = (svg) =>
  Array.from(svg.matchAll(/<path\b[^>]*\bd="([^"]+)"/g), (match) => match[1]);
const coordinates = (d) =>
  Array.from(d.matchAll(/[ML]([-\d.]+) ([-\d.]+)/g), (match) => [
    Number(match[1]),
    Number(match[2]),
  ]);
const hash = (file) =>
  crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");

(async () => {
  const root = path.resolve(
    process.argv[2] || fs.mkdtempSync("build/tray-assets-"),
  );
  fs.mkdirSync(root, { recursive: true });
  const generated = path.join(root, "generated"),
    vectors = path.join(root, "svg");
  execFileSync("go", [
    "run",
    "./build/icon/gen.go",
    "./build/icon/tail.go",
    "tray-motion",
    "assets/logo.svg",
    generated,
    vectors,
  ]);
  const files = [
    "tray.png",
    "tray-active.png",
    "tray-active.svg",
    "tray-motion.png",
    "tray-motion.json",
  ];
  for (const file of files)
    assert.deepEqual(
      fs.readFileSync(path.join(generated, file)),
      fs.readFileSync(path.join("assets", file)),
      "stale or nondeterministic " + file,
    );
  const manifest = JSON.parse(
    fs.readFileSync(path.join(generated, "tray-motion.json")),
  );
  assert.equal(manifest.toCurl, 30);
  assert.equal(manifest.fromCurl, 100);
  assert.equal(manifest.durationMs, 600);
  assert.equal(manifest.width, 48);
  assert.equal(
    manifest.height,
    36,
    "18pt menu-bar icon must have 2x Retina pixels",
  );
  assert.equal(manifest.sourceSHA256, hash("assets/logo.svg"));
  const canonical = paths(fs.readFileSync("assets/logo.svg", "utf8"));
  const selected = paths(
    fs.readFileSync("docs/design/tray-tail-selected.svg", "utf8"),
  );
  assert.equal(selected[0], canonical[0]);
  const browser = await chromium.launch({
    headless: true,
    executablePath:
      process.env.AICAT_CHROMIUM ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  });
  const report = {
    passed: false,
    mode: "headless Chromium and real SVG/PNG generator",
    manifest,
    files: Object.fromEntries(
      files.map((file) => [file, hash(path.join(generated, file))]),
    ),
    frames: [],
  };
  try {
    const page = await browser.newPage({
      viewport: { width: 1000, height: 850 },
    });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(
      pathToFileURL(path.resolve("docs/design/tray-motion-editor.html")).href,
    );
    const widths = [],
      lengths = [];
    for (let i = 0; i < manifest.frameCount; i++) {
      const percent =
        Math.round((100 - (70 * i) / (manifest.frameCount - 1)) * 10) / 10;
      const vector = paths(
        fs.readFileSync(
          path.join(vectors, "frame-" + String(i).padStart(2, "0") + ".svg"),
          "utf8",
        ),
      );
      assert.equal(vector[0], canonical[0], "body path must stay exact");
      await page.locator("#curl").fill(String(percent));
      await page.locator("#curl").dispatchEvent("input");
      const actual = await page.evaluate(() => ({
        tail: document.getElementById("tail-adjusted").getAttribute("d"),
        length: document.getElementById("tail-centerline").getTotalLength(),
      }));
      const points = coordinates(vector[1]);
      assert.equal(points.length, 545);
      if (i > 0) {
        const expected = coordinates(actual.tail);
        assert.equal(expected.length, points.length);
        assert(
          points.every(
            (point, j) =>
              Math.hypot(point[0] - expected[j][0], point[1] - expected[j][1]) <
              0.000003,
          ),
          "generated geometry differs from manual preview at " + percent,
        );
      }
      if (i === manifest.frameCount - 1) {
        const chosen = coordinates(selected[1]);
        assert.equal(points.length, chosen.length);
        assert(
          points.every(
            (point, j) =>
              Math.hypot(point[0] - chosen[j][0], point[1] - chosen[j][1]) <
              0.000003,
          ),
          "30% must match the selected SVG within six-decimal rounding precision",
        );
      }
      widths.push(
        points
          .slice(0, 241)
          .map((point, j) =>
            Math.hypot(
              point[0] - points[j === 240 ? 304 : 544 - j][0],
              point[1] - points[j === 240 ? 304 : 544 - j][1],
            ),
          ),
      );
      lengths.push(actual.length);
      report.frames.push({ index: i, curl: percent, length: actual.length });
    }
    assert(
      Math.max(...lengths) - Math.min(...lengths) < 0.002,
      "tail length changed",
    );
    assert(
      widths.every((frame) =>
        frame.every((width, j) => Math.abs(width - widths[0][j]) < 0.000004),
      ),
      "tail cross-section changed",
    );
    const sprite = fs.readFileSync("assets/tray-motion.png").toString("base64");
    const raster = await page.evaluate(
      async ({ sprite, manifest }) => {
        const image = new Image();
        await new Promise((resolve, reject) => {
          image.onload = resolve;
          image.onerror = reject;
          image.src = "data:image/png;base64," + sprite;
        });
        const canvas = document.createElement("canvas");
        canvas.width = image.width;
        canvas.height = image.height;
        const context = canvas.getContext("2d");
        context.drawImage(image, 0, 0);
        const data = context.getImageData(
          0,
          0,
          canvas.width,
          canvas.height,
        ).data;
        let changed = 0,
          minX = manifest.width,
          minY = manifest.height,
          maxX = 0,
          maxY = 0;
        for (let frame = 1; frame < manifest.frameCount; frame++)
          for (let y = 0; y < manifest.height; y++)
            for (let x = 0; x < manifest.width; x++) {
              const a = (y * canvas.width + x) * 4,
                b = (y * canvas.width + frame * manifest.width + x) * 4;
              if (data[a + 3] !== data[b + 3]) {
                changed++;
                minX = Math.min(minX, x);
                minY = Math.min(minY, y);
                maxX = Math.max(maxX, x);
                maxY = Math.max(maxY, y);
              }
            }
        const frameCanvas = document.createElement("canvas");
        frameCanvas.width = manifest.width;
        frameCanvas.height = manifest.height;
        const painter = frameCanvas.getContext("2d");
        const endpoints = [0, manifest.frameCount - 1].map((index) => {
          painter.clearRect(0, 0, manifest.width, manifest.height);
          painter.drawImage(
            image,
            index * manifest.width,
            0,
            manifest.width,
            manifest.height,
            0,
            0,
            manifest.width,
            manifest.height,
          );
          return frameCanvas.toDataURL();
        });
        return { changed, bounds: { minX, minY, maxX, maxY }, endpoints };
      },
      { sprite, manifest },
    );
    assert(raster.changed > 0);
    assert(
      raster.bounds.minX >= 19 && raster.bounds.minY >= 15,
      "pixels outside the tail region moved",
    );
    const cards = raster.endpoints
      .map(
        (url, i) =>
          "<section><h2>" +
          (i ? "已接管 · 卷曲 30%" : "未接管 · 原始卷尾") +
          '</h2><div class="large"><img src="' +
          url +
          '"></div><div class="bar"><img src="' +
          url +
          '"></div><div class="bar dark"><img src="' +
          url +
          '"></div></section>',
      )
      .join("");
    const html =
      '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>实际托盘图标资源</title><style>body{margin:0;padding:30px;background:#fafbf8;color:#202720;font:14px -apple-system,sans-serif}.grid{display:grid;grid-template-columns:1fr 1fr;gap:24px}section{padding:20px;border:1px solid #d5ddd2;border-radius:10px}h2{font-size:15px;font-weight:500;margin:0}.large{display:flex;align-items:center;justify-content:center;height:150px}.large img{height:120px;width:auto}.bar{display:flex;align-items:center;justify-content:center;height:48px;background:#edf0e9;border-radius:6px;margin-top:12px}.bar img{height:18px;width:auto}.dark{background:#29312c}.dark img{filter:invert(1)}</style><div class="grid">' +
      cards +
      "</div></html>";
    fs.writeFileSync(path.join(root, "icons.html"), html);
    await page.setContent(html);
    await page.screenshot({
      path: path.join(root, "icons.png"),
      fullPage: true,
    });
    assert.deepEqual(errors, []);
    report.raster = {
      changed: raster.changed,
      bounds: raster.bounds,
      bodyPixelsFixed: true,
    };
    report.checks = [
      "deterministic SVG/PNG generation",
      "exact 30% selected SVG",
      "all 25 frames agree with manual geometry",
      "tail length and cross-section preserved",
      "fixed canvas and body pixels",
      "template previews at 18px on light/dark bars",
    ];
    report.passed = true;
  } finally {
    fs.writeFileSync(
      path.join(root, "report.json"),
      JSON.stringify(report, null, 2),
    );
    await browser.close();
  }
  console.log(JSON.stringify({ passed: report.passed, artifacts: root }));
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
