const fs = require("node:fs");
const path = require("node:path");
const http = require("node:http");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");

async function createRegistry(root, manifest, source) {
  const directory = path.join(root, "fixture-package");
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, "package.json"), JSON.stringify(manifest));
  fs.writeFileSync(path.join(directory, "index.mjs"), source);
  const archiveFile = path.join(root, "fixture.tgz");
  execFileSync("tar", ["-czf", archiveFile, "-C", root, "fixture-package"]);
  const archive = fs.readFileSync(archiveFile);
  const shasum = crypto.createHash("sha1").update(archive).digest("hex");
  const server = http.createServer((request, response) => {
    if (request.url === "/fixture.tgz") return response.end(archive);
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify({
      name: manifest.name,
      "dist-tags": { latest: manifest.version },
      versions: {
        [manifest.version]: {
          ...manifest,
          dist: { tarball: `http://127.0.0.1:${server.address().port}/fixture.tgz`, shasum },
        },
      },
    }));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    url: `http://127.0.0.1:${server.address().port}`,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}

module.exports = { createRegistry };
