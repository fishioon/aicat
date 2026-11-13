import hashlib
import json
import os
from pathlib import Path
import selectors
import shutil
import subprocess
import sys

binary = shutil.which(sys.argv[1] if len(sys.argv) > 1 else "codex")
if not binary:
    raise SystemExit("Codex executable not found")
root = Path(sys.argv[2] if len(sys.argv) > 2 else "build/codex-agent-protocol").resolve()
root.mkdir(parents=True, exist_ok=True)
home = root / "codex-home"
home.mkdir(mode=0o700, exist_ok=True)
report = {
    "passed": False,
    "checks": [],
    "modelTurnStarted": False,
    "version": subprocess.check_output([binary, "--version"], text=True).strip(),
    "scriptSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
}
frames = []
with (root / "stderr.log").open("w") as stderr:
    child = subprocess.Popen(
        [binary, "app-server", "--stdio", "-c",
         'permissions.aicat-room={filesystem={":project_roots"="write",":root"="read",":tmpdir"="write",":slash_tmp"="write"},network={enabled=true}}',
         "-c", 'default_permissions="aicat-room"'],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr,
        env={**os.environ, "CODEX_HOME": str(home)}, cwd=root, text=True,
    )
    selector = selectors.DefaultSelector()
    selector.register(child.stdout, selectors.EVENT_READ)

    def call(request_id, method, params):
        child.stdin.write(json.dumps({"id": request_id, "method": method, "params": params}) + "\n")
        child.stdin.flush()
        while selector.select(20):
            line = child.stdout.readline()
            if not line:
                raise RuntimeError("app-server exited")
            frame = json.loads(line)
            frames.append(frame)
            if frame.get("id") == request_id:
                if "error" in frame:
                    raise RuntimeError(str(frame["error"]))
                return frame["result"]
        raise RuntimeError("app-server response timeout")

    try:
        call(1, "initialize", {
            "clientInfo": {"name": "aicat_protocol_probe", "version": "1"},
            "capabilities": {"experimentalApi": True},
        })
        child.stdin.write('{"method":"initialized","params":{}}\n')
        child.stdin.flush()
        report["checks"].append("real Codex initialize handshake")
        result = call(2, "thread/start", {
            "cwd": str(root), "approvalPolicy": "never", "permissions": "aicat-room",
            "runtimeWorkspaceRoots": [str(root)], "ephemeral": False,
            "developerInstructions": "Protocol validation only. No model turn will be started.",
            "dynamicTools": [{"type": "function", "name": "messages_send", "description": "Test",
                              "inputSchema": {"type": "object", "properties": {"event": {"type": "object"}}, "required": ["event"]}}],
        })
        assert result["thread"]["id"]
        report["checks"].append("real thread/start accepts named permissions, Room roots and messages_send dynamic tool")
        report["threadId"] = result["thread"]["id"]
        report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
    finally:
        child.terminate()
        try:
            child.wait(timeout=5)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        selector.close()
        (root / "responses.json").write_text(json.dumps(frames, ensure_ascii=False, indent=2) + "\n")
        (root / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
print(json.dumps(report, ensure_ascii=False, indent=2))
sys.exit(0 if report["passed"] else 1)
