#!/usr/bin/env python3
"""真实 socket + TLS + built native UI 的 E2E。仅写隔离数据目录与本次工件。"""
import argparse
import base64
import fcntl
import concurrent.futures
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import stat
import ssl
import subprocess
import tempfile
import threading
import time


def wait_for(fn, seconds=8):
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError, AssertionError) as err:
            last = err
        time.sleep(0.025)
    raise AssertionError(f"condition timed out: {last}")


class Fixture(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    release = threading.Event()

    def log_message(self, *_):
        pass

    def body(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            chunks = []
            while True:
                size = int(self.rfile.readline().split(b";")[0], 16)
                if not size:
                    while self.rfile.readline() != b"\r\n":
                        pass
                    return b"".join(chunks)
                chunks.append(self.rfile.read(size))
                assert self.rfile.read(2) == b"\r\n"
        return self.rfile.read(int(self.headers.get("Content-Length", 0)))

    def do_GET(self):
        self.do_POST()

    def do_POST(self):
        body = self.body()
        if self.path == "/events":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(b"data: first\n\n")
            self.wfile.flush()
            self.release.wait(12)
            self.wfile.write(b"data: last\n\n")
            self.wfile.flush()
            self.close_connection = True
            return
        if self.path == "/large-response":
            data = b"large-response-" * 100000
            content_type = "application/octet-stream"
        elif self.path.startswith("/burst"):
            data = b"burst-payload-" * 160
            content_type = "text/plain"
        else:
            value = {"id": "resp_prototype", "ok": True, "path": self.path,
                     "input": body.decode("utf-8", errors="replace") if len(body) < 4096 else None,
                     "length": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                     "trace": self.headers.get_all("X-Trace", [])}
            data = json.dumps(value, ensure_ascii=False).encode()
            content_type = "application/json"
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("X-Fixture", "aicat-e2e")
        self.end_headers()
        self.wfile.write(data)


class Automation:
    def __init__(self, path):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(10)
        self.sock.connect(str(path))
        self.file = self.sock.makefile("rwb")

    def call(self, method, **params):
        self.file.write((json.dumps({"id": 1, "method": method, "params": params}) + "\n").encode())
        self.file.flush()
        reply = json.loads(self.file.readline())
        assert "error" not in reply, reply
        return reply.get("result", {})

    def tree(self):
        return self.call("tree")["nodes"]

    def act(self, key, action="press", **params):
        nodes = self.tree()
        if any(n.get("key") == key for n in nodes):
            # Most mutations finish asynchronously. Accessibility press on a
            # disabled control is a silent no-op, so await the enabled target.
            # History intentionally exercises its disabled no-op behavior below.
            if action == "press" and key not in ("history-back", "history-forward"):
                wait_for(lambda:any(n.get("key") == key and "disabled" not in n.get("states", []) for n in self.tree()))
            result = self.call("act", key=key, action=action, **params)
        else:
            labels = {"traffic-search": "筛选流量", "domain-search": "查找应用或域名"}
            if key not in labels:
                raise AssertionError(f"missing UI action target: {key}")
            node = next(n for n in nodes if n.get("label") == labels[key])
            result = self.call("act", id=node["id"], action=action, **params)
        self.call("idle", timeout=3000)
        return result

    def capture(self, path):
        result = self.call("capture")
        path.write_bytes(base64.b64decode(result["png"]))

    def click(self, key):
        # 用真实命中测试验证交互；act(press) 会绕过窗口拖拽区覆盖的问题。
        node = wait_for(lambda:next((n for n in self.tree() if n.get("key") == key and "disabled" not in n.get("states", [])), None))
        box = node["bounds"]
        self.call("input", events=[{"type": "click", "x": box["x"] + box["width"] / 2,
                                    "y": box["y"] + box["height"] / 2}])
        self.call("idle", timeout=3000)

    def select(self, request_id):
        def find():
            for node in self.tree():
                if node.get("role") == "row" and node.get("label", "").startswith(f"#{request_id} "):
                    return node
        node = wait_for(find)
        box = node["bounds"]
        self.call("input", events=[{"type": "click", "x": box["x"] + min(180, box["width"] / 2),
                                    "y": box["y"] + box["height"] / 2}])
        self.call("idle", timeout=3000)

    def detail(self, key):
        return next((n.get("value", "") for n in self.tree() if n.get("key") == key), "")

    def check_sidebar(self, section):
        nodes = self.tree()
        by_id = {n["id"]: n for n in nodes}
        sidebar = next(n for n in nodes if n.get("key") == "sidebar")
        breadcrumb = next(n for n in nodes if n.get("key") == "breadcrumb-section")
        assert breadcrumb.get("label") == section, breadcrumb

        def inside(node):
            while node.get("parent") in by_id:
                node = by_id[node["parent"]]
                if node["id"] == sidebar["id"]:
                    return True
            return False

        content = [n for n in nodes if inside(n)]
        duplicates = [n for n in content if n.get("label") == section]
        assert not duplicates, f"侧边栏重复显示一级栏目名 {section}: {duplicates}"
        if section == "设置":
            assert any(n.get("label") == "代理" for n in content), content
        return nodes


def main():
    args = argparse.ArgumentParser()
    args.add_argument("--binary", default="bin/aicat-e2e")
    args.add_argument("--port", type=int, default=9981)
    args.add_argument("--output")
    args.add_argument("--preferences-only", action="store_true")
    opts = args.parse_args()
    binary = str(Path(opts.binary).resolve())
    root = Path(opts.output).resolve() if opts.output else Path(tempfile.mkdtemp(prefix="prototype-e2e-", dir=Path("build").resolve()))
    root.mkdir(parents=True, exist_ok=True)
    os.chmod(root, 0o700)
    report = {"passed": False, "artifacts": str(root), "checks": [], "binary": binary, "listenPort": opts.port}
    checks = report["checks"]
    processes, servers = [], []
    log = (root / "app.log").open("w")
    home = root / ".aicat"
    home.mkdir(mode=0o700)
    (home / "aicat.yaml").write_text(f"""ui:
  language: zh-Hans
  theme: system
gateway:
  addr: 127.0.0.1:{opts.port}
  interceptHosts: []
  bufferedBodyLimit: 1048576
traffic:
  maxRecords: 12
  maxBodyBytes: 1024
  maxTotalBodyBytes: 8192
# keep this comment when enabling a domain
futureSetting: keep
""")
    os_state = root / "os-state.json"
    original_services = [{"id": "fixture-wifi", "name": "Wi-Fi", "proxies": {
        "HTTPEnable": 0, "HTTPProxy": "", "HTTPPort": 0, "HTTPSEnable": 0, "HTTPSProxy": "", "HTTPSPort": 0, "ProxyAutoDiscoveryEnable": 0,
        "ProxyAutoConfigEnable": 1, "ProxyAutoConfigURLString": "https://fixture.invalid/proxy.pac",
        "SOCKSEnable": 1, "SOCKSProxy": "socks.fixture.invalid", "SOCKSPort": 1080,
        "ExceptionsList": ["localhost", "*.internal"], "FixtureUnknownField": "keep"}}]
    os_state.write_text(json.dumps({"services": original_services, "trusted": False,
                                   "recordProxyWrites": True, "probeGateway": f"127.0.0.1:{opts.port}"}))
    os_state.chmod(0o600)

    def os_fixture(**updates):
        with open(str(os_state) + ".lock", "a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            state = json.loads(os_state.read_text())
            if updates:
                state.update(updates)
                staging = os_state.with_suffix(".next")
                staging.write_text(json.dumps(state)); staging.chmod(0o600); staging.replace(os_state)
            return state

    proxy = f"127.0.0.1:{opts.port}"
    sock_path = Path(tempfile.gettempdir()) / f"aicat-e2e-{os.getpid()}.sock"
    env = dict(os.environ, MYGO_AUTOMATION=str(sock_path), AICAT_E2E_ROOT=str(root / "root.pem"),
               AICAT_E2E_OS_STATE=str(os_state), GODEBUG="x509usefallbackroots=1", HTTP_PROXY=f"http://{proxy}", HTTPS_PROXY=f"http://{proxy}", NO_PROXY="")

    def api(path="/api/traffic", headers=None):
        c = http.client.HTTPConnection("127.0.0.1", opts.port, timeout=4)
        c.request("GET", path, headers=headers or {})
        r = c.getresponse()
        status, data, response_headers = r.status, r.read(), dict(r.getheaders())
        c.close()
        return status, data, response_headers

    def page():
        status, data, _ = api()
        assert status == 200
        return json.loads(data)["items"]

    def record(id):
        status, data, _ = api(f"/api/traffic/{id}")
        assert status == 200
        return json.loads(data)

    def completed(id):
        return wait_for(lambda: (r if (r := record(id))["state"] in ("completed", "failed", "canceled") else None))

    def request(port, path, body=b"", tls_host=None, trust=None):
        if tls_host:
            ctx = ssl.create_default_context(cafile=str(trust))
            c = http.client.HTTPSConnection("127.0.0.1", opts.port, context=ctx, timeout=7)
            c.set_tunnel(tls_host, port)
            c.connect()
            leaf = c.sock.getpeercert(binary_form=True)
            target = path
        else:
            c = http.client.HTTPConnection("127.0.0.1", opts.port, timeout=7)
            leaf = None
            target = f"http://127.0.0.1:{port}{path}"
        c.putrequest("POST" if body else "GET", target)
        c.putheader("Content-Length", str(len(body)))
        c.putheader("Content-Type", "application/json")
        c.putheader("X-Trace", "one")
        c.putheader("X-Trace", "two")
        c.endheaders(body)
        r = c.getresponse()
        data = r.read()
        assert r.status == 200, (r.status, data)
        assert r.getheader("X-Fixture") == "aicat-e2e"
        c.close()
        return data, leaf

    def launch():
        p = subprocess.Popen([binary, "--home", str(home)], env=env, stdout=log, stderr=log)
        processes.append(p)
        wait_for(lambda: p.poll() is None and sock_path.exists() and api()[0] == 200)
        auto = Automation(sock_path)
        wait_for(lambda: auto.call("windows").get("windows"))
        auto.call("idle", timeout=3000)
        return p, auto

    def stop(p, auto):
        # SIGTERM takes MyGo's ordinary app quit path, then bounded gateway drain.
        p.send_signal(signal.SIGTERM)
        assert p.wait(timeout=8) == 0, (root / "app.log").read_text()
        auto.file.close()
        auto.sock.close()
        wait_for(lambda: not sock_path.exists())

    try:
        def openssl(*args):
            subprocess.run(["openssl", *args], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        (root / "root.cnf").write_text("[req]\ndistinguished_name=dn\nx509_extensions=v3_ca\nprompt=no\n[dn]\nCN=AICat E2E Root\n[v3_ca]\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid:always\nbasicConstraints=critical,CA:true\nkeyUsage=critical,keyCertSign,cRLSign\n")
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(root / "root.key"),
                "-out", str(root / "root.pem"), "-days", "2", "-subj", "/CN=AICat E2E Root", "-config", str(root / "root.cnf"))
        openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", str(root / "leaf.key"),
                "-out", str(root / "leaf.csr"), "-subj", "/CN=localhost")
        (root / "leaf.ext").write_text("subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n")
        openssl("x509", "-req", "-in", str(root / "leaf.csr"), "-CA", str(root / "root.pem"),
                "-CAkey", str(root / "root.key"), "-CAcreateserial", "-out", str(root / "leaf.pem"),
                "-days", "2", "-extfile", str(root / "leaf.ext"))
        for tls in (False, True):
            server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
            server.daemon_threads = True
            if tls:
                context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
                context.load_cert_chain(root / "leaf.pem", root / "leaf.key")
                server.socket = context.wrap_socket(server.socket, server_side=True)
            servers.append(server)
            threading.Thread(target=server.serve_forever, daemon=True).start()
        hp, tp = [s.server_port for s in servers]
        p, auto = launch()
        def has_label(label):
            return any(n.get("label") == label for n in auto.tree())

        def preferences_ready():
            return any(n.get("key") == "preference-language" and "disabled" not in n.get("states", []) for n in auto.tree())

        def choose_language(label):
            auto.click("preference-language")
            node = wait_for(lambda: next((n for n in auto.tree() if n.get("role") in ("option", "listItem") and n.get("label") == label), None))
            auto.call("act", id=node["id"], action="press")
            wait_for(preferences_ready)

        auto.act("account-menu"); auto.act("nav-settings")
        wait_for(preferences_ready)
        assert has_label("语言") and has_label("主题") and has_label("通用")
        assert not has_label("外观")
        auto.capture(root / "native-general-zh.png")
        choose_language("English")
        wait_for(lambda: has_label("Language") and has_label("Theme") and has_label("General"))
        assert "language: en" in (home / "aicat.yaml").read_text()
        auto.click("preference-theme-dark")
        wait_for(lambda: preferences_ready() and "theme: dark" in (home / "aicat.yaml").read_text())
        auto.capture(root / "native-general-en-dark.png")
        assert "futureSetting: keep" in (home / "aicat.yaml").read_text()
        checks.append("General contains working language and theme controls; English applies immediately; YAML preserves unrelated settings")
        stop(p, auto)
        p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings")
        wait_for(preferences_ready)
        assert has_label("Language") and has_label("Theme")
        assert next(n for n in auto.tree() if n.get("key") == "preference-language").get("value") == "English"
        auto.capture(root / "native-general-en-dark-after-restart.png")
        checks.append("English and dark preference survive a real process restart; native appearance screenshots retained for review")
        auto.click("preference-theme-light")
        wait_for(lambda: preferences_ready() and "theme: light" in (home / "aicat.yaml").read_text())
        auto.capture(root / "native-general-en-light.png")
        choose_language("简体中文")
        wait_for(lambda:has_label("语言") and has_label("主题"))
        auto.click("toggle-sidebar")
        wait_for(lambda:not any(n.get("key") == "sidebar" for n in auto.tree()))
        auto.act("settings-proxy"); auto.act("settings-general")
        wait_for(preferences_ready)
        assert has_label("语言") and has_label("主题")
        auto.capture(root / "native-general-collapsed.png")
        auto.click("toggle-sidebar")
        wait_for(lambda:any(n.get("key") == "sidebar" for n in auto.tree()))
        config_file = home / "aicat.yaml"
        saved = config_file.read_text()
        config_file.write_text("ui: [invalid\n")
        auto.click("preference-theme-dark")
        wait_for(lambda:any(n.get("key") == "preferences-error" for n in auto.tree()))
        auto.capture(root / "native-general-save-error.png")
        assert config_file.read_text() == "ui: [invalid\n"
        config_file.write_text(saved)
        auto.click("preference-theme-system")
        wait_for(lambda: preferences_ready() and "theme: system" in config_file.read_text())
        auto.capture(root / "native-general-final.png")
        (root / "preferences-config.yaml").write_text(config_file.read_text())
        (root / "preferences-tree.json").write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
        checks.append("light and system themes apply; Chinese can be restored; categories remain accessible when collapsed; save failures retain the active preferences")
        auto.act("nav-traffic")
        if opts.preferences_only:
            stop(p, auto)
            report["passed"] = True
            return
        assert auto.call("windows")["windows"][0]["kind"] == "content"
        assert auto.call("windows")["windows"][0]["title"] == "aicat"
        assert page() == []
        assert stat.S_IMODE((home / "ca").stat().st_mode) == 0o700
        assert stat.S_IMODE((home / "ca/mitmproxy-ca.pem").stat().st_mode) == 0o600
        subject = subprocess.check_output(["openssl", "x509", "-in", str(home / "ca/mitmproxy-ca-cert.pem"), "-noout", "-subject", "-nameopt", "RFC2253"], text=True)
        assert subject.rsplit("CN=", 1)[1].split(",", 1)[0].strip() == "aicat"
        checks.append(f"native window; API/proxy share {opts.port}; API calls are not captured")
        def no_selection():
            nodes = auto.tree()
            return (any(n.get("label") == "未选择请求" for n in nodes)
                    and not any(n.get("key") in ("detail-tabs", "request-detail", "response-detail", "tunnel-request") for n in nodes))
        assert no_selection()
        assert not any(n.get("label") in ("暂无固定域名", "No pinned domains") for n in auto.tree())
        auto.capture(root / "native-no-selection.png")
        checks.append("empty pinned group follows the design; unselected details show only centered empty state")

        def sidebar_visible():
            return any(n.get("label") == "查找应用或域名" for n in auto.tree())

        assert sidebar_visible()
        auto.click("toggle-sidebar")
        wait_for(lambda: not sidebar_visible())
        auto.capture(root / "native-sidebar-collapsed.png")
        auto.click("toggle-sidebar")
        wait_for(sidebar_visible)
        auto.check_sidebar("流量")
        auto.capture(root / "native-sidebar-expanded.png")
        checks.append("physical pointer clicks collapse and reopen sidebar; titlebar controls remain reachable")

        payload = json.dumps({"model": "prototype", "input": "真实 HTTP → native UI", "stream": False}, ensure_ascii=False).encode()
        data, _ = request(hp, "/v1/responses", payload)
        assert json.loads(data)["input"].encode() == payload
        first = completed(1)
        assert first["state"] == "completed", first
        assert first["revision"] > 1
        assert first["requestReceived"]["headers"]["X-Trace"] == ["one", "two"]
        body_id = first["requestReceived"]["body"]["id"]
        assert api("/api/bodies/" + body_id)[1] == payload
        checks.append("HTTP forwarding; ID starts at 1; updates keep ID; body API preserves bytes")
        auto.select(1)
        auto.click("request-detail-raw-tab"); auto.click("response-detail-raw-tab")
        wait_for(lambda: "真实 HTTP" in auto.detail("request-detail") and "resp_prototype" in auto.detail("response-detail"))
        auto.capture(root / "native-http.png")
        auto.click("theme-toggle")
        auto.capture(root / "native-http-alternate-theme.png")
        auto.click("theme-toggle")
        auto.select(1)
        auto.call("input", events=[{"type": "key", "key": "Escape"}])
        wait_for(no_selection)
        auto.capture(root / "native-deselected.png")
        auto.select(1)
        wait_for(lambda: "resp_prototype" in auto.detail("response-detail"))
        checks.append("Escape cancels selection and hides detail controls; reselect restores actual request")
        assert len([n for n in auto.tree() if n.get("role") == "tab"]) == 4
        wait_for(lambda:"X-Trace" in auto.detail("request-detail") and "X-Fixture" in auto.detail("response-detail"))
        auto.capture(root / "native-raw.png")
        (root / "native-tree.json").write_text(json.dumps(auto.tree(), ensure_ascii=False, indent=2))
        checks.append("native row click loads real request and response bodies")

        def has_key(key):
            return any(n.get("key") == key for n in auto.tree())

        def cert_status(label):
            return any(n.get("key") == "certificate-status" and n.get("label") == label for n in auto.tree())

        def proxy_status(label):
            return any(n.get("key") == "system-proxy-status" and n.get("label") == label for n in auto.tree())

        def certificate_menu(key):
            auto.click("certificate-manage")
            # MyGo wraps explicit menu IDs in its menu-reference key type.
            node = wait_for(lambda:next((n for n in auto.tree() if n.get("role") == "menuItem" and n.get("key") == "{" + key + "}"), None))
            auto.call("act", id=node["id"], action="press")
            auto.call("idle", timeout=3000)

        def recheck_certificate():
            certificate_menu("certificate-details")
            auto.act("certificate-recheck")
            auto.act("certificate-details-close")

        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda: has_key("proxy-address"))
        assert not any("aicat.yaml" in n.get("label", "") or "interceptHosts" in n.get("label", "") for n in auto.tree())
        assert os_fixture()["services"] == original_services
        auto.act("settings-proxy")
        wait_for(lambda: cert_status("未信任"))
        auto.capture(root / "native-certificate-untrusted.png")
        os_fixture(cancelInstall=True)
        auto.click("certificate-install")
        assert not any(n.get("label") == "继续" for n in auto.tree())
        auto.capture(root / "native-certificate-waiting.png")
        wait_for(lambda:any("已取消证书授权" in n.get("label", "") for n in auto.tree()))
        assert cert_status("未信任") and not os_fixture().get("trusted")
        os_fixture(cancelInstall=False, installWithoutTrust=True)
        auto.click("certificate-install")
        wait_for(lambda:any("证书尚未受信任" in n.get("label", "") for n in auto.tree()))
        assert cert_status("未信任")
        os_fixture(installWithoutTrust=False, failInstall=True)
        auto.click("certificate-install")
        wait_for(lambda:any("test certificate install failed" in n.get("label", "") for n in auto.tree()))
        assert cert_status("未信任")
        os_fixture(failInstall=False)
        auto.click("certificate-install")
        wait_for(lambda: cert_status("已信任"))
        assert os_fixture()["services"] == original_services
        auto.capture(root / "native-certificate-trusted.png")
        actual_fingerprint = hashlib.sha256(ssl.PEM_cert_to_DER_cert((home / "ca/mitmproxy-ca-cert.pem").read_text())).hexdigest()
        assert os_fixture()["installedCertificate"] == actual_fingerprint
        os_fixture(installedCertificate="different-ca", trusted=True)
        recheck_certificate(); wait_for(lambda:cert_status("未信任"))
        os_fixture(installedCertificate=actual_fingerprint, sslOnly=True)
        recheck_certificate(); wait_for(lambda:cert_status("未信任"))
        os_fixture(sslOnly=False, failTrustCheck=True)
        recheck_certificate(); wait_for(lambda:cert_status("状态未知"))
        os_fixture(failTrustCheck=False, trusted=False)
        recheck_certificate(); wait_for(lambda:cert_status("未信任"))
        auto.click("certificate-install"); wait_for(lambda:cert_status("已信任"))
        checks.append("certificate creation is local; cancel/failure/untrusted install never report trust; exact CA identity is rechecked without an app confirmation")

        auto.act("add-decryption-domain")
        auto.act("decryption-domain-input", "setValue", text="https://example.com/")
        auto.act("domain-save")
        assert has_key("domain-validation-error")
        auto.act("decryption-domain-input", "setValue", text=" Example.COM. ")
        auto.act("domain-save")
        wait_for(lambda:has_key("remove-decryption-example.com"))
        auto.act("remove-decryption-example.com")
        wait_for(lambda:has_key("decryption-domains-empty"))
        checks.append("HTTPS settings add/remove exact domains with validation and atomic config updates")

        auto.act("settings-proxy")
        wait_for(lambda: proxy_status("未启用"))
        auto.click("system-proxy-toggle")
        auto.act("system-proxy-cancel")
        assert os_fixture()["services"] == original_services
        # A command that fails after its write must be verified and rolled back.
        os_fixture(failProxyAfterWriteAt=os_fixture().get("proxyCommandCount", 0)+2)
        auto.click("system-proxy-toggle"); auto.act("system-proxy-enable")
        wait_for(lambda:has_key("security-error"))
        assert os_fixture()["services"] == original_services
        os_fixture(failProxyAfterWriteAt=0)
        auto.click("system-proxy-toggle"); auto.act("system-proxy-enable")
        wait_for(lambda:proxy_status("已启用"))
        owned_services = os_fixture()["services"]
        assert owned_services[0]["proxies"]["HTTPPort"] == opts.port
        assert all(write["gatewayReachable"] for write in os_fixture()["proxyWrites"])
        auto.capture(root / "native-system-proxy-enabled.png")
        external_services = json.loads(json.dumps(owned_services))
        external_services[0]["proxies"]["HTTPProxy"] = "external-change.invalid"
        os_fixture(services=external_services)
        auto.click("system-proxy-toggle")
        wait_for(lambda:has_key("system-proxy-error") and proxy_status("未启用") and has_key("system-proxy-restore"))
        assert os_fixture()["services"] == external_services
        os_fixture(services=owned_services)
        auto.act("system-proxy-restore")
        wait_for(lambda:proxy_status("未启用") and not has_key("system-proxy-restore"))
        assert os_fixture()["services"] == original_services
        auto.capture(root / "native-proxy-settings.png")
        checks.append("system proxy is opt-in; cancellation changes nothing; partial writes roll back; external edits are preserved and recovery can be retried")
        os_fixture(trusted=False)
        auto.act("settings-proxy"); recheck_certificate(); wait_for(lambda:cert_status("未信任"))
        auto.act("nav-traffic")

        bundle = root / "client-roots.pem"
        bundle.write_bytes((root / "root.pem").read_bytes() + (home / "ca/mitmproxy-ca-cert.pem").read_bytes())
        original_leaf = ssl.PEM_cert_to_DER_cert((root / "leaf.pem").read_text())
        old_tls = http.client.HTTPSConnection("127.0.0.1", opts.port,
                    context=ssl.create_default_context(cafile=str(root / "root.pem")), timeout=7)
        old_tls.set_tunnel("localhost", tp)
        old_tls.connect()
        assert old_tls.sock.getpeercert(binary_form=True) == original_leaf
        old_tls.request("GET", "/before-enable")
        before = old_tls.getresponse()
        assert before.status == 200 and json.loads(before.read())["path"] == "/before-enable"
        wait_for(lambda:record(2)["state"] == "response")
        auto.select(2)
        wait_for(lambda: any(n.get("key") == "enable-domain-decryption" for n in auto.tree()))
        assert "CONNECT" in auto.detail("request-detail")
        assert not any(n.get("key") in ("detail-tabs", "response-detail") for n in auto.tree())
        auto.capture(root / "native-connect.png")
        auto.act("enable-domain-decryption")
        wait_for(lambda:any(n.get("key")=="enable-with-client-trust" for n in auto.tree()))
        auto.act("enable-with-client-trust")
        wait_for(lambda: any(n.get("label") == "已启用解密，等待客户端新建连接" for n in auto.tree()))
        saved = (home / "aicat.yaml").read_text()
        assert "localhost" in saved and "futureSetting: keep" in saved and "# keep this comment" in saved
        old_tls.request("GET", "/existing-tunnel-after-enable")
        old_res = old_tls.getresponse()
        assert old_res.status == 200 and json.loads(old_res.read())["path"] == "/existing-tunnel-after-enable"
        assert old_tls.sock.getpeercert(binary_form=True) == original_leaf
        old_tls.close()
        assert completed(2)["method"] == "CONNECT"
        assert not any("/existing-tunnel-after-enable" in item["url"] for item in page())
        auto.capture(root / "native-connect-enabled.png")
        checks.append("TLS defaults to opaque CONNECT; explicit native enable persists domain, leaves existing tunnel unchanged")
        data, intercepted_leaf = request(tp, "/secure", payload, "localhost", bundle)
        assert json.loads(data)["input"].encode() == payload
        secure = completed(3)
        assert secure["method"] == "POST" and not secure.get("tunnel")
        original_leaf = ssl.PEM_cert_to_DER_cert((root / "leaf.pem").read_text())
        assert intercepted_leaf != original_leaf
        data, passthrough_leaf = request(tp, "/passthrough", b"", "127.0.0.1", root / "root.pem")
        assert passthrough_leaf == original_leaf
        tunnel = completed(4)
        assert tunnel["tunnel"] and tunnel["method"] == "CONNECT"
        assert all("/passthrough" not in r["url"] for r in page())
        checks.append("allowlisted TLS MITM changes leaf certificate; non-allowlisted TLS preserves original leaf and hides content")
        auto.select(3)
        wait_for(lambda: "/secure" in auto.detail("response-detail"))
        auto.capture(root / "native-https.png")
        auto.act(f"domain-localhost:{tp}")
        wait_for(lambda:any(n.get("role")=="text" and n.get("label")=="2 个请求" for n in auto.tree()))
        auto.act("history-back")
        checks.append("native HTTPS detail is the selected secure exchange; domain tree filters real requests")

        large = b"large-request-" * 100000
        data, _ = request(hp, "/large-request", large)
        assert json.loads(data)["sha256"] == hashlib.sha256(large).hexdigest()
        req = completed(5)["requestReceived"]["body"]
        assert req["state"] == "truncated" and req["captured"] == 1024 and req["observed"] == len(large), req
        data, _ = request(hp, "/large-response")
        assert data == b"large-response-" * 100000
        res = completed(6)["responseSent"]["body"]
        assert res["state"] == "truncated" and res["captured"] <= 1024 and res["observed"] == len(data), res
        checks.append("large request/response stream unchanged; capture stores bounded prefix and reports truncation")

        c = http.client.HTTPConnection("127.0.0.1", opts.port, timeout=8)
        c.request("GET", f"http://127.0.0.1:{hp}/events")
        stream = c.getresponse()
        assert stream.read(len(b"data: first\n\n")) == b"data: first\n\n"
        active = wait_for(lambda: (r if (r := record(7))["state"] == "response" and r["responseSent"]["body"]["observed"] > 0 else None))
        assert active["responseSent"]["body"]["state"] == "pending"
        Fixture.release.set()
        assert stream.read() == b"data: last\n\n"
        c.close()
        assert completed(7)["state"] == "completed"
        checks.append("SSE row/body updates before completion, then reaches terminal state")

        closed = socket.socket();closed.bind(("127.0.0.1", 0));bad_port = closed.getsockname()[1];closed.close()
        c = http.client.HTTPConnection("127.0.0.1", opts.port, timeout=4)
        c.request("GET", f"http://127.0.0.1:{bad_port}/failure")
        assert c.getresponse().status == 502
        c.close()
        assert completed(8)["state"] == "failed", completed(8)
        checks.append("connection failure reaches failed terminal state")

        # Same path on an upstream remains proxy traffic; Host/Origin on API is checked.
        data, _ = request(hp, "/api/traffic")
        assert json.loads(data)["path"] == "/api/traffic"
        assert api(headers={"Host": "untrusted.example"})[0] == 403
        assert api(headers={"Origin": "https://example.com"})[0] == 403
        assert api("/api/traffic?before=oops")[0] == 400
        assert api("/api/traffic/999999")[0] == 404
        checks.append("API authority/origin validation; upstream API-like paths forwarded; invalid and expired IDs rejected")

        auto.act("record-toggle")
        wait_for(lambda:any(n.get("key")=="record-toggle" and "继续录制" in n.get("label", "") for n in auto.tree()))
        before = len(page())
        request(hp, "/paused")
        assert len(page()) == before
        auto.act("record-toggle")
        wait_for(lambda:any(n.get("key")=="record-toggle" and "正在录制" in n.get("label", "") for n in auto.tree()))
        request(hp, "/resumed")
        assert max(r["id"] for r in page()) == 10
        checks.append("pause stops new capture, keeps forwarding; resume restores capture")

        occupied = subprocess.run([binary, "serve", "--home", str(home)], env=env, capture_output=True, timeout=8)
        assert occupied.returncode != 0 and b"address already in use" in occupied.stderr
        (root / "occupied-port.log").write_bytes(occupied.stderr)
        checks.append("occupied port fails without claiming readiness of another instance")

        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            list(pool.map(lambda i: request(hp, f"/burst/{i}"), range(30)))
        wait_for(lambda: len(page()) == 12 and all(r["state"] == "completed" for r in page()))
        retained = page()
        assert min(r["id"] for r in retained) == 29 and max(r["id"] for r in retained) == 40
        assert api("/api/traffic/1")[0] == 404
        records = [record(r["id"]) for r in retained]
        bodies = [r["requestReceived"]["body"] for r in records] + [r["responseSent"]["body"] for r in records]
        assert sum(b["captured"] for b in bodies) <= 8192
        assert any(b["state"] == "truncated" for b in bodies)
        auto.act("traffic-search", "setValue", text=retained[0]["url"])
        wait_for(lambda: any(n.get("role") == "row" and n.get("label", "").startswith("#40 ") for n in auto.tree()))
        wait_for(no_selection)
        checks.append("bounded record eviction/global body budget; Watch converges after burst; expired selection returns to empty state")
        (root / "retained-records.json").write_text(json.dumps(records, ensure_ascii=False, indent=2))

        auto.click("toggle-sidebar")
        wait_for(lambda: not sidebar_visible())
        auto.click("toggle-sidebar")
        wait_for(sidebar_visible)
        auto.act("account-menu")
        auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda: any(n.get("key") == "proxy-address" for n in auto.tree()))
        settings_tree = auto.check_sidebar("设置")
        (root / "settings-tree.json").write_text(json.dumps(settings_tree, ensure_ascii=False, indent=2))
        auto.capture(root / "native-settings.png")
        auto.click("toggle-sidebar")
        assert not any(n.get("key") == "sidebar" for n in auto.tree())
        auto.click("toggle-sidebar")
        auto.check_sidebar("设置")
        auto.act("history-back")
        auto.check_sidebar("设置")
        assert any(n.get("key") == "preference-language" for n in auto.tree())
        auto.act("history-back")
        auto.check_sidebar("流量")
        auto.act("history-forward")
        auto.check_sidebar("设置")
        assert any(n.get("key") == "preference-language" for n in auto.tree())
        auto.act("history-forward")
        auto.check_sidebar("设置")
        assert any(n.get("key") == "proxy-address" for n in auto.tree())
        auto.act("history-back"); auto.act("history-back")
        auto.check_sidebar("流量")
        checks.append("sidebar never repeats the breadcrumb section after settings navigation, history or collapse/reopen")
        auto.act("clear-traffic")
        wait_for(lambda: page() == [])
        wait_for(no_selection)
        auto.capture(root / "native-after-clear.png")
        request(hp, "/after-clear")
        assert completed(41)["state"] == "completed"
        checks.append("reference shell: sidebar toggle, account settings, history; clear does not reuse IDs")

        stop(p, auto)
        bind = socket.socket();bind.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1);bind.bind(("127.0.0.1", opts.port));bind.listen(1);bind.close()
        p, auto = launch()
        assert page() == []
        auto.call("menu", path=["E2E", "关闭窗口"])
        wait_for(lambda:not auto.call("windows")["windows"][0]["visible"])
        assert p.poll() is None and api()[0] == 200
        request(hp, "/after-restart")
        assert completed(1)["state"] == "completed"
        auto.call("menu", path=["E2E", "打开窗口"])
        wait_for(lambda:auto.call("windows")["windows"][0]["visible"])
        checks.append("aicat name; closing hides window while background proxy captures; reopening preserves traffic")
        data, resumed_leaf = request(tp, "/secure-after-restart", payload, "localhost", bundle)
        assert resumed_leaf != original_leaf and json.loads(data)["path"] == "/secure-after-restart"
        assert completed(2)["method"] == "POST"
        checks.append("restart resets record IDs and retains explicit HTTPS decryption choice in aicat.yaml")
        # 功能开关使用真实 UI、代理和唯一配置文件；不接触系统代理或钥匙串。
        def has(key):
            return any(n.get("key") == key for n in auto.tree())

        def feature_on():
            return next(n for n in auto.tree() if n.get("key") == "feature-traffic").get("states", [])

        auto.act("record-toggle")
        auto.act("account-menu")
        auto.act("nav-settings"); auto.act("settings-proxy")
        auto.click("nav-extensions")
        cfg_path = home / "aicat.yaml"
        saved_config = cfg_path.read_text()
        invalid_config = saved_config + "\nfeatures: []\n"
        cfg_path.write_text(invalid_config)
        auto.click("feature-traffic")
        wait_for(lambda: has("feature-error"))
        assert has("nav-traffic") and "checked" in feature_on()
        assert cfg_path.read_text() == invalid_config
        cfg_path.write_text(saved_config)
        checks.append("failed feature save leaves enabled state and navigation unchanged; error is visible")

        auto.click("feature-traffic")
        wait_for(lambda: not has("nav-traffic") and "checked" not in feature_on())
        retained_before_disable = page()
        request(hp, "/feature-disabled")
        request(tp, "/secure-feature-disabled", payload, "localhost", bundle)
        assert page() == retained_before_disable
        saved_config = cfg_path.read_text()
        assert "traffic: false" in saved_config and "localhost" in saved_config
        assert "# keep this comment" in saved_config and "futureSetting: keep" in saved_config
        assert not has("record-toggle") and not has("focus-search")
        assert any(n.get("key") == "feature-traffic-status" and n.get("label") == "已关闭" for n in auto.tree())
        auto.check_sidebar("扩展")
        # Switch 的滑块有 140ms 动画，截图应保留稳定的关闭状态。
        time.sleep(0.2)
        auto.capture(root / "native-feature-disabled.png")
        (root / "feature-disabled-tree.json").write_text(json.dumps(auto.tree(), ensure_ascii=False, indent=2))
        # Settings and extensions have their own navigation history entries.
        # Walk all remaining valid entries; none may reopen disabled traffic.
        history_steps = 0
        while "disabled" not in next(n for n in auto.tree() if n.get("key") == "history-back").get("states", []):
            auto.act("history-back")
            history_steps += 1
            assert history_steps < 64
            assert not has("traffic-table") and not has("nav-traffic")
        auto.act("history-back")
        assert not has("traffic-table") and not has("nav-traffic")
        for _ in range(history_steps):
            auto.act("history-forward")
            assert not has("traffic-table") and not has("nav-traffic")
        assert has("feature-traffic")
        auto.click("toggle-sidebar")
        auto.click("toggle-sidebar")
        assert "checked" not in feature_on()
        checks.append("disabled traffic hides navigation and actions; history cannot reopen it; HTTP/TLS proxy and retained data stay available")

        auto.click("feature-traffic")
        wait_for(lambda: has("nav-traffic") and "checked" in feature_on())
        auto.act("nav-traffic")
        assert any(n.get("key") == "record-toggle" and "继续录制" in n.get("label", "") for n in auto.tree())
        request(hp, "/still-paused-after-enabling")
        assert page() == retained_before_disable
        auto.act("record-toggle")
        request(hp, "/feature-enabled-again")
        assert completed(3)["url"].endswith("/feature-enabled-again")
        checks.append("re-enabling restores navigation and retained records while respecting the session pause state")

        auto.act("account-menu")
        auto.act("nav-settings"); auto.act("settings-proxy")
        auto.act("nav-extensions")
        auto.click("feature-traffic")
        wait_for(lambda: not has("nav-traffic"))
        stop(p, auto)
        p, auto = launch()
        wait_for(lambda: has("feature-traffic"))
        assert not has("nav-traffic") and "checked" not in feature_on()
        assert page() == []
        request(hp, "/disabled-after-restart")
        assert page() == []
        auto.capture(root / "native-feature-disabled-after-restart.png")
        auto.click("feature-traffic")
        wait_for(lambda: has("nav-traffic"))
        assert "traffic: true" in cfg_path.read_text()
        request(hp, "/enabled-after-restart")
        assert completed(1)["url"].endswith("/enabled-after-restart")
        auto.act("nav-traffic")
        auto.select(1)
        auto.click("request-detail-raw-tab"); auto.click("response-detail-raw-tab")
        wait_for(lambda: "enabled-after-restart" in auto.detail("response-detail"))
        auto.capture(root / "native-feature-enabled.png")
        checks.append("disabled feature persists across restart; proxy stays live; re-enabling starts capture with ID 1")

        held = socket.create_connection(("127.0.0.1", opts.port), timeout=4)
        held.sendall(f"CONNECT 127.0.0.1:{tp} HTTP/1.1\r\nHost: 127.0.0.1:{tp}\r\n\r\n".encode())
        assert b"200 Connection Established" in held.recv(4096)
        tunnel_id = max(r["id"] for r in page())
        auto.select(tunnel_id)
        wait_for(lambda:has_key("enable-domain-decryption"))
        auto.act("enable-domain-decryption")
        wait_for(lambda:any(n.get("key")=="certificate-install" and "disabled" not in n.get("states",[]) for n in auto.tree()))
        auto.click("certificate-install")
        wait_for(lambda:any(n.get("label") == "已启用解密，等待客户端新建连接" for n in auto.tree()))
        assert os_fixture()["trusted"] and "127.0.0.1" in cfg_path.read_text()
        auto.capture(root / "native-connect-certificate-ready.png")
        checks.append("CONNECT keeps its selected domain through native certificate setup and enables it only after verified trust")
        started = time.monotonic()
        stop(p, auto)
        assert held.recv(1) == b""
        held.close()
        assert time.monotonic() - started < 5
        checks.append("normal quit closes an active hijacked CONNECT socket without waiting for the remote peer")
        p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:proxy_status("未启用"))
        auto.click("system-proxy-toggle"); auto.act("system-proxy-enable")
        wait_for(lambda:proxy_status("已启用"))
        before_crash_fingerprint = hashlib.sha256((home / "ca/mitmproxy-ca.pem").read_bytes()).hexdigest()
        p.kill(); p.wait(timeout=5); auto.file.close(); auto.sock.close()
        sock_path.unlink(missing_ok=True)
        assert (home / "state/system-proxy.json").exists()
        count_before_restart = os_fixture()["proxyCommandCount"]
        p, auto = launch()
        assert os_fixture()["proxyCommandCount"] > count_before_restart
        assert hashlib.sha256((home / "ca/mitmproxy-ca.pem").read_bytes()).hexdigest() == before_crash_fingerprint
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:proxy_status("已启用"))
        auto.capture(root / "native-system-proxy-recovered.png")
        os_fixture(failApply=True)
        auto.call("menu", path=["E2E", "退出应用"])
        wait_for(lambda:has_key("security-error"))
        assert p.poll() is None and api()[0] == 200 and (home / "state/system-proxy.json").exists()
        os_fixture(failApply=False)
        auto.call("menu", path=["E2E", "退出应用"])
        assert p.wait(timeout=8) == 0
        assert os_fixture()["services"] == original_services
        assert all(write["gatewayReachable"] for write in os_fixture()["proxyWrites"])
        assert not (home / "state/system-proxy.json").exists()
        auto.file.close(); auto.sock.close()
        checks.append("crash recovery reuses the same CA and reapplies saved intent; failed quit recovery keeps the proxy live; successful quit restores OS settings before closing it")
        # Certificate lifecycle uses one root identity across the proxy, local
        # files and the isolated user keychain. Never touch the real Keychain.
        p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:proxy_status("已启用"))
        auto.click("system-proxy-toggle"); wait_for(lambda:proxy_status("未启用"))
        auto.act("settings-proxy"); wait_for(lambda:cert_status("已信任"))
        private_path = home / "ca/mitmproxy-ca.pem"
        public_path = home / "ca/mitmproxy-ca-cert.pem"
        cer_path = home / "ca/mitmproxy-ca-cert.cer"
        tombstone = home / "ca/identity-deleted"
        old_pair = private_path.read_bytes()
        old_public = public_path.read_bytes()
        cer_path.write_bytes(old_public)  # legacy upstream public export
        os_fixture(otherCertificate="unrelated-ca-must-survive", cancelExport=True)
        certificate_menu("certificate-export")
        wait_for(lambda:cert_status("已信任"))
        exported = root / "aicat-root-ca.pem"
        assert not exported.exists()
        os_fixture(cancelExport=False)
        certificate_menu("certificate-export")
        wait_for(exported.exists)
        assert exported.read_bytes() == old_public and b"PRIVATE KEY" not in exported.read_bytes()
        certificate_menu("certificate-keychain")
        wait_for(lambda:os_fixture().get("keychainOpened"))
        wait_for(lambda:cert_status("已信任"))
        auto.capture(root / "native-certificate-management.png")
        checks.append("certificate management exports only the same public root, handles save cancellation and opens the keychain through an isolated OS adapter")

        certificate_menu("certificate-delete")
        auto.capture(root / "native-certificate-delete-confirm.png")
        auto.act("certificate-delete-cancel")
        assert private_path.read_bytes() == old_pair and os_fixture()["trusted"] and not tombstone.exists()
        ctx = ssl.create_default_context(cafile=str(bundle))
        active_tls = http.client.HTTPSConnection("127.0.0.1", opts.port, context=ctx, timeout=5)
        active_tls.set_tunnel("localhost", tp); active_tls.connect()
        retired_leaf = active_tls.sock.getpeercert(binary_form=True)
        os_fixture(cancelRemove=True)
        certificate_menu("certificate-delete"); auto.act("certificate-delete-confirm")
        wait_for(lambda:cert_status("删除未完成"))
        assert private_path.read_bytes() == old_pair and os_fixture()["trusted"] and tombstone.exists()
        assert active_tls.sock.recv(1) == b""
        active_tls.close()
        _, leaf = request(tp, "/delete-canceled-opaque", tls_host="localhost", trust=bundle)
        assert leaf == original_leaf and api()[0] == 200
        stop(p, auto); p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:cert_status("删除未完成"))
        assert not has_key("certificate-create") and not has_key("certificate-install")
        _, leaf = request(tp, "/pending-after-restart", tls_host="localhost", trust=bundle)
        assert leaf == original_leaf
        checks.append("delete confirmation is reversible; canceled OS removal retains the identity, disconnects old TLS sessions and stays opaque after restart")

        os_fixture(cancelRemove=False, failRemoveAfterTrust=True)
        certificate_menu("certificate-delete"); auto.act("certificate-delete-confirm")
        wait_for(lambda:any("test keychain removal failed" in n.get("label", "") for n in auto.tree()))
        assert cert_status("删除未完成") and private_path.read_bytes() == old_pair
        assert not os_fixture()["trusted"] and os_fixture().get("installedCertificate")
        os_fixture(failRemoveAfterTrust=False)
        certificate_menu("certificate-delete"); auto.act("certificate-delete-confirm")
        wait_for(lambda:cert_status("未安装"))
        assert not private_path.exists() and not public_path.exists() and not cer_path.exists()
        assert not os_fixture().get("installedCertificate") and not os_fixture()["trusted"]
        assert os_fixture()["otherCertificate"] == "unrelated-ca-must-survive"
        assert has_key("certificate-install") and not has_key("certificate-manage")
        auto.capture(root / "native-certificate-deleted.png")
        stop(p, auto); p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:cert_status("未安装"))
        assert not private_path.exists() and tombstone.exists()
        _, leaf = request(tp, "/deleted-after-restart", tls_host="localhost", trust=bundle)
        assert leaf == original_leaf
        request(hp, "/http-without-ca")
        checks.append("partial trust/keychain removal preserves retry material; successful deletion removes exact OS identity and local files, keeps unrelated certificates and never regenerates at boot")

        auto.act("nav-traffic")
        opaque_id = max(r["id"] for r in page() if r["method"] == "CONNECT")
        auto.select(opaque_id)
        wait_for(lambda:has_key("enable-domain-decryption"))
        auto.act("enable-domain-decryption")
        wait_for(lambda:has_key("certificate-install"))
        os_fixture(cancelInstall=True)
        auto.click("certificate-install"); wait_for(lambda:cert_status("未信任"))
        new_public = public_path.read_bytes()
        assert new_public != old_public and private_path.stat().st_mode & 0o777 == 0o600
        assert not tombstone.exists() and not os_fixture().get("installedCertificate")
        new_bundle = root / "new-client-roots.pem"
        new_bundle.write_bytes((root / "root.pem").read_bytes() + new_public)
        _, leaf = request(tp, "/new-root", tls_host="localhost", trust=new_bundle)
        assert leaf != original_leaf and leaf != retired_leaf
        try:
            request(tp, "/old-root-rejected", tls_host="localhost", trust=bundle)
            raise AssertionError("retired root still accepted after recreation")
        except ssl.SSLCertVerificationError:
            pass
        os_fixture(cancelInstall=False)
        auto.click("certificate-install")
        wait_for(lambda:not has_key("decryption-cancel"))
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:cert_status("已信任"))
        assert os_fixture()["installedCertificate"] == hashlib.sha256(ssl.PEM_cert_to_DER_cert(new_public.decode())).hexdigest()
        auto.capture(root / "native-certificate-recreated.png")
        checks.append("explicit recreation produces one new untrusted root, retires old leaf cache, requires new client trust and installs only on explicit authorization")

        os_fixture(holdRemove=True)
        certificate_menu("certificate-delete"); auto.act("certificate-delete-confirm")
        wait_for(tombstone.exists)
        p.kill(); p.wait(timeout=5); auto.file.close(); auto.sock.close()
        sock_path.unlink(missing_ok=True)
        os_fixture(holdRemove=False)
        p, auto = launch()
        auto.act("account-menu"); auto.act("nav-settings"); auto.act("settings-proxy")
        wait_for(lambda:cert_status("删除未完成"))
        _, leaf = request(tp, "/crashed-deletion", tls_host="localhost", trust=new_bundle)
        assert leaf == original_leaf and public_path.read_bytes() == new_public
        certificate_menu("certificate-delete"); auto.act("certificate-delete-confirm")
        wait_for(lambda:cert_status("未安装"))
        stop(p, auto)
        checks.append("SIGKILL during OS authorization preserves a durable deletion intent; restart stays opaque and user retry completes cleanup")
        assert os_fixture()["services"] == original_services
        report["passed"] = True
    except BaseException as err:
        report["error"] = repr(err)
        try:
            auto.capture(root / "native-failure.png")
            (root / "failure-tree.json").write_text(json.dumps(auto.tree(), ensure_ascii=False, indent=2))
        except Exception:
            pass
        raise
    finally:
        Fixture.release.set()
        for p in processes:
            if p.poll() is None:
                p.terminate()
                try:
                    p.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    p.kill();p.wait()
        for server in servers:
            server.shutdown();server.server_close()
        log.close()
        report["screenshots"] = [str(p) for p in root.glob("native-*.png")]
        (root / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
