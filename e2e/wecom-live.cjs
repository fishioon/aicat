const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const net = require("node:net");
const crypto = require("node:crypto");
const { spawn, execFileSync } = require("node:child_process");
const root = path.resolve(
  process.argv[2] ||
    "build/wecom-live-" + new Date().toISOString().replace(/[:.]/g, "-"),
);
fs.mkdirSync(root, { recursive: true, mode: 0o700 });
fs.chmodSync(root, 0o700);
const home = path.join(root, "home");
fs.mkdirSync(path.join(home, "credentials"), { recursive: true, mode: 0o700 });
const sourceHome =
  process.env.AICAT_LIVE_SOURCE_HOME || path.join(os.homedir(), ".aicat");
const binary = path.resolve(process.env.AICAT_LIVE_BINARY || "bin/aicat-e2e");
const marker =
  process.env.AICAT_LIVE_MARKER ||
  "AICAT-LIVE-" + crypto.randomBytes(3).toString("hex").toUpperCase();
const report = {
  passed: false,
  marker,
  startedAt: new Date().toISOString(),
  checks: [],
  dailyConnectionUntouched: true,
  binarySHA256: crypto
    .createHash("sha256")
    .update(fs.readFileSync(binary))
    .digest("hex"),
  realPlatform: true,
  realCodex: true,
};
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let proc,
  preview,
  token,
  stopping = false;
const stateFile = path.join(root, "state.json");
const writeReport = () =>
  fs.writeFileSync(
    path.join(root, "report.json"),
    JSON.stringify(report, null, 2) + "\n",
    { mode: 0o600 },
  );
async function rpc(method, args = []) {
  const r = await fetch(preview + "/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Aicat-Preview": token },
    body: JSON.stringify({ method: "Desktop." + method, args }),
    signal: AbortSignal.timeout(35000),
  });
  const result = await r.json();
  if (!r.ok) throw Error(result.error);
  return result;
}
function assertAccountIdle(source) {
  const state = path.join(source, "state/channel-host");
  const lock = path.join(state, "host.lock");
  if (!fs.existsSync(lock)) return;
  const pid = Number(fs.readFileSync(lock, "utf8"));
  if (!Number.isInteger(pid) || pid < 1)
    throw Error("Invalid original channel host lock");
  try {
    process.kill(pid, 0);
  } catch (error) {
    if (error.code === "ESRCH") return;
    throw error;
  }
  const command = execFileSync(
    "/bin/ps",
    ["-p", String(pid), "-o", "command="],
    { encoding: "utf8" },
  );
  if (
    !command.includes("/channel-host/host.mjs ") ||
    !(
      command.includes(" " + state + " ") ||
      command.trimEnd().endsWith(" " + state)
    )
  )
    throw Error("Original channel host identity mismatch");
  throw Error(
    "原机器人连接正在运行，请先在 AICat 停用该扩展，再运行真实验收；脚本不会接管日常连接",
  );
}

async function cleanup() {
  if (stopping) return;
  stopping = true;
  try {
    if (proc && proc.exitCode === null && proc.signalCode === null) {
      await rpc("SetExtensionEnabled", ["wecom", false]).catch(() => {});
      const ended = new Promise((resolve) => proc.once("exit", resolve));
      proc.kill("SIGTERM");
      await Promise.race([ended, pause(10000)]);
      if (proc.exitCode === null && proc.signalCode === null) {
        proc.kill("SIGKILL");
        await ended;
      }
    }
  } catch (error) {
    report.passed = false;
    report.recoveryError = error.stack;
  } finally {
    fs.rmSync(path.join(home, "credentials/wecom.json"), { force: true });
    report.finishedAt = new Date().toISOString();
    writeReport();
  }
}
process.on("SIGTERM", async () => {
  report.error = "Live run interrupted";
  await cleanup();
  process.exit(1);
});
process.on("SIGINT", async () => {
  report.error = "Live run interrupted";
  await cleanup();
  process.exit(1);
});
(async () => {
  try {
    assertAccountIdle(sourceHome);
    const credentials = JSON.parse(
      fs.readFileSync(path.join(sourceHome, "credentials/wecom.json")),
    );
    if (!credentials.accountId || !credentials.secret)
      throw Error("Missing real WeCom credentials");
    report.botIdSuffix = credentials.accountId.slice(-6);
    fs.writeFileSync(
      path.join(home, "credentials/wecom.json"),
      JSON.stringify(credentials),
      { mode: 0o600 },
    );
    const listener = net.createServer();
    await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
    const port = listener.address().port;
    await new Promise((resolve) => listener.close(resolve));
    fs.writeFileSync(
      path.join(home, "aicat.yaml"),
      `gateway:\n  addr: 127.0.0.1:${port}\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  codex:\n    enabled: false\n  wecom:\n    enabled: false\n    agent:\n      enabled: true\n`,
      { mode: 0o600 },
    );
    fs.writeFileSync(
      path.join(root, "os-state.json"),
      JSON.stringify({
        trusted: false,
        proxy: { http: { enabled: false }, https: { enabled: false } },
      }),
      { mode: 0o600 },
    );
    writeReport();
    const fd = fs.openSync(path.join(root, "app.log"), "a", 0o600);
    const env = {
      ...process.env,
      AICAT_E2E_BACKGROUND: "1",
      AICAT_E2E_PREVIEW: path.join(root, "preview-url"),
      AICAT_E2E_OS_STATE: path.join(root, "os-state.json"),
    };
    delete env.AICAT_E2E_WECOM_URL;
    proc = spawn(binary, ["--home", home], { stdio: ["ignore", fd, fd], env });
    fs.closeSync(fd);
    report.pid = proc.pid;
    const deadline = Date.now() + 10 * 60 * 1000;
    while (!fs.existsSync(env.AICAT_E2E_PREVIEW)) {
      if (proc.exitCode !== null || Date.now() > deadline)
        throw Error("Live app failed to start");
      await pause(100);
    }
    const url = new URL(fs.readFileSync(env.AICAT_E2E_PREVIEW, "utf8"));
    preview = url.origin;
    token = url.searchParams.get("token");
    let setup = false;
    let connected = false,
      lastSummary = "";
    while (Date.now() < deadline) {
      if (proc.exitCode !== null || proc.signalCode !== null)
        throw Error("Live app exited");
      let snapshot;
      try {
        snapshot = await rpc("State");
      } catch (error) {
        if (!connected) {
          await pause(200);
          continue;
        }
        throw error;
      }
      if (!setup) {
        await rpc("InstallExtension", ["wecom"]);
        report.checks.push(
          "official plugin installed from registry into isolated Home",
        );
        await rpc("ConfigureMessageChannel", ["wecom", credentials]);
        report.checks.push("real Bot credentials validated before enabling");
        await rpc("SetExtensionEnabled", ["wecom", true]);
        setup = true;
        continue;
      }
      const channel = snapshot.messageChannels.wecom;
      fs.writeFileSync(stateFile, JSON.stringify(channel, null, 2) + "\n", {
        mode: 0o600,
      });
      if (channel.phase === "connected" && !connected) {
        connected = true;
        report.checks.push("real WeCom WebSocket authenticated");
        writeReport();
        console.log(
          JSON.stringify({
            ready: true,
            marker,
            root,
            botIdSuffix: report.botIdSuffix,
          }),
        );
      }
      const summary = JSON.stringify({
        phase: channel.phase,
        error: channel.error,
        records: channel.records.length,
        tasks: channel.tasks.map((t) => ({
          id: t.id,
          status: t.status,
          error: t.error,
        })),
      });
      if (summary !== lastSummary) {
        lastSummary = summary;
        console.log(summary);
      }
      const incoming = channel.records.find(
        (r) =>
          r.event.type === "org.aicat.message.received.v1" &&
          r.event.data.text.content.includes(marker),
      );
      if (incoming) {
        report.inbound = {
          id: incoming.id,
          eventId: incoming.event.id,
          msgtime: incoming.event.data.msgtime,
          receivedAt: incoming.recordedAt,
        };
        if (!report.checks.includes("real private test message persisted"))
          report.checks.push("real private test message persisted");
        const task = channel.tasks.find((t) => t.id === incoming.id);
        if (task) {
          report.task = { id: task.id, status: task.status, error: task.error };
        }
        const sent = channel.records.find(
          (r) =>
            r.event.source === "aicat-agent://codex" &&
            r.event.data.text.content.includes(marker) &&
            r.status === "sent",
        );
        if (sent && task?.status === "completed") {
          report.reply = {
            text: sent.event.data.text.content,
            receiptStatus: sent.status,
          };
          report.checks.push(
            "real Codex task completed",
            "Codex messages_send reply acknowledged by real WeCom",
          );
          report.passed = true;
          writeReport();
          console.log(
            JSON.stringify({
              passed: true,
              report: path.join(root, "report.json"),
            }),
          );
          break;
        }
        writeReport();
      }
      await pause(1000);
    }
    if (!report.passed)
      throw Error("Live run timed out before confirmed automatic reply");
  } catch (error) {
    report.error = error.stack;
    console.log(JSON.stringify({ error: error.message, root }));
  } finally {
    await cleanup();
    process.exitCode = report.passed ? 0 : 1;
  }
})();
