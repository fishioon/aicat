#!/usr/bin/env python3
"""Focused CLI E2E after removing the script executor.
Failure modes: retired script tools remain callable; MCP authentication regresses;
proxy payloads or bounded capture change; SSE buffers until upstream completion;
legacy configured rules are silently ignored; test processes or config escape Home.
"""
import argparse, base64, hashlib, http.client, http.server, json, os, signal, socket, subprocess, tempfile, threading, time
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-cli-e2e')
    parser.add_argument('--output')
    opts = parser.parse_args()
    Path('build').mkdir(exist_ok=True)
    root = Path(opts.output).resolve() if opts.output else Path(tempfile.mkdtemp(prefix='mcp-e2e-', dir='build')).resolve()
    root.mkdir(parents=True, exist_ok=True)
    home = root / 'home'
    home.mkdir(mode=0o700)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    cfg = home / 'aicat.yaml'
    cfg.write_text(f'# preserve-mcp-fixture\nfutureSetting: keep\nextensions:\n  traffic:\n    enabled: true\ntraffic:\n  recording: true\n  maxBodyBytes: 64\n  maxTotalBodyBytes: 4096\ngateway:\n  addr: 127.0.0.1:{port}\n  bufferedBodyLimit: 256\n')
    original_config = cfg.read_bytes()
    seen = []
    release_stream = threading.Event()

    class Upstream(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            if self.headers.get('Transfer-Encoding', '').lower() == 'chunked':
                chunks = []
                while True:
                    size = int(self.rfile.readline().split(b';', 1)[0], 16)
                    if not size:
                        while self.rfile.readline() not in (b'\r\n', b'\n', b''):
                            pass
                        break
                    chunks.append(self.rfile.read(size))
                    assert self.rfile.read(2) == b'\r\n'
                body = b''.join(chunks)
            else:
                body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
            seen.append({'path': self.path, 'body': body, 'label': self.headers.get('X-Fixture')})
            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path != '/events':
                return self.do_POST()
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Connection', 'close')
            self.end_headers()
            self.wfile.write(b'data: first\n\n')
            self.wfile.flush()
            if release_stream.wait(5):
                self.wfile.write(b'data: last\n\n')
                self.wfile.flush()
            self.close_connection = True

        def log_message(self, *_):
            pass

    upstream = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
    upstream.daemon_threads = True
    threading.Thread(target=upstream.serve_forever, daemon=True).start()
    binary = str(Path(opts.binary).resolve())
    env = dict(os.environ, AICAT_E2E_BACKGROUND='1')
    report = {'passed': False, 'scope': 'CLI proxy + bounded traffic + MCP; no native windows or real credentials', 'checks': [], 'binary': binary,
              'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(), 'command': ['python3', 'e2e/mcp.py', '--binary', binary],
              'sources': {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for directory in ['internal/mcpserver', 'internal/config', 'internal/gateway/addons/traffic', 'internal/traffic'] for p in Path(directory).glob('*.go')}}
    process = None
    token = ''
    rpc_id = 0
    log = (root / 'app.log').open('w')

    def check(label, condition):
        assert condition, label
        report['checks'].append(label)

    def wait(fn):
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            try:
                value = fn()
                if value:
                    return value
            except (ConnectionError, OSError):
                pass
            time.sleep(.03)
        raise RuntimeError('readiness timeout')

    def request(method, target, body=None, headers=None):
        c = http.client.HTTPConnection('127.0.0.1', port, timeout=8)
        c.request(method, target, body=body, headers=headers or {})
        r = c.getresponse()
        result = (r.status, r.read())
        c.close()
        return result

    def rpc(method, params=None, extra=None, authorized=True):
        nonlocal rpc_id
        rpc_id += 1
        headers = {'Content-Type': 'application/json', 'Accept': 'application/json, text/event-stream', **(extra or {})}
        if authorized:
            headers['Authorization'] = 'Bearer ' + token
        status, data = request('POST', '/mcp', json.dumps({'jsonrpc': '2.0', 'id': rpc_id, 'method': method, 'params': params or {}}), headers)
        with (root / 'mcp.ndjson').open('a') as transcript:
            transcript.write(json.dumps({'method': method, 'status': status, 'response': data.decode()}) + '\n')
        return status, json.loads(data) if status == 200 else data.decode()

    def tool(name, arguments):
        code, result = rpc('tools/call', {'name': name, 'arguments': arguments})
        assert code == 200 and 'error' not in result and not result['result'].get('isError'), result
        return result['result']['structuredContent']

    def stop():
        nonlocal process
        if process and process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                check('CLI exits cleanly', process.wait(timeout=12) == 0)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                raise
        process = None

    try:
        process = subprocess.Popen([binary, 'serve', '--home', str(home)], env=env, stdout=log, stderr=log)
        report['pid'] = process.pid
        wait(lambda: process.poll() is None and (home / 'mcp-token').exists() and request('GET', '/api/traffic')[0] == 200)
        token = (home / 'mcp-token').read_text().strip()
        code, initialized = rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'mcp-removal-e2e', 'version': '1'}})
        check('MCP initializes with the supported protocol', code == 200 and initialized['result']['protocolVersion'] == '2025-06-18')
        code, tools = rpc('tools/list')
        names = {value['name'] for value in tools['result']['tools']}
        check('traffic and explicit message send MCP tools are advertised', names == {'traffic_list', 'traffic_get', 'traffic_body', 'messages_send'})
        check('MCP without token is rejected', rpc('tools/list', authorized=False)[0] == 401)
        check('MCP with browser Origin is rejected', rpc('tools/list', extra={'Origin': 'http://localhost'})[0] == 403)
        for name in ['plugins_guide', 'plugins_validate', 'plugins_put', 'rules_put', 'rules_test', 'rewrite_status', 'rewrite_logs']:
            _, retired = rpc('tools/call', {'name': name, 'arguments': {}})
            check(f'retired tool {name} is unavailable', 'error' in retired or retired.get('result', {}).get('isError'))
        _, resource = rpc('resources/read', {'uri': 'aicat://plugins/sdk/v1'})
        check('retired script SDK resource is unavailable', 'error' in resource)
        body = bytes(range(256)) * 8
        code, returned = request('POST', f'http://127.0.0.1:{upstream.server_port}/echo', body, {'Content-Type': 'application/octet-stream', 'X-Fixture': 'unchanged'})
        check('proxy forwards full binary body unchanged despite capture limit', code == 200 and returned == body and seen[-1]['body'] == body and seen[-1]['label'] == 'unchanged')
        page = wait(lambda: (value if (value := tool('traffic_list', {'limit': 20}))['items'] else None))
        echo = next(value for value in page['items'] if value['url'].endswith('/echo'))
        record = wait(lambda: (value if (value := tool('traffic_get', {'id': echo['id']}))['state'] == 'completed' else None))
        check('traffic snapshots contain no removed script metadata', all(key not in record for key in ['ruleRevision', 'executions', 'synthetic']))
        captured = tool('traffic_body', {'id': record['requestReceived']['body']['id']})
        check('MCP reads bounded body prefix without changing transit bytes', base64.b64decode(captured['data']) == body[:64] and captured['info']['captured'] == 64 and captured['info']['observed'] == len(body))
        _, invalid = rpc('tools/call', {'name': 'traffic_list', 'arguments': {'limit': 201}})
        check('traffic tool rejects invalid limit', 'error' in invalid or invalid.get('result', {}).get('isError'))
        for target in ['/mcp', '/api/traffic']:
            code, value = request('POST', f'http://127.0.0.1:{upstream.server_port}{target}', b'upstream-route')
            check(f'upstream {target} is not intercepted as control API', code == 200 and value == b'upstream-route' and seen[-1]['path'] == target)
        c = http.client.HTTPConnection('127.0.0.1', port, timeout=3)
        c.request('GET', f'http://127.0.0.1:{upstream.server_port}/events')
        response = c.getresponse()
        check('SSE reaches client before upstream completes', response.read(len(b'data: first\n\n')) == b'data: first\n\n' and not release_stream.is_set())
        release_stream.set()
        check('SSE completes without modifying content', response.read() == b'data: last\n\n')
        c.close()
        stop()
        check('ordinary config bytes are preserved', cfg.read_bytes() == original_config)
        cfg.write_text(cfg.read_text() + 'rewrite:\n  enabled: true\n  rules: []\n')
        failure = subprocess.run([binary, 'serve', '--home', str(home)], env=env, capture_output=True, timeout=8)
        (root / 'legacy-config.log').write_bytes(failure.stdout + failure.stderr)
        check('configured legacy rewrite is rejected instead of silently ignored', failure.returncode != 0 and b'script rewrite is no longer supported' in failure.stdout + failure.stderr)
        report['passed'] = True
    finally:
        release_stream.set()
        stop()
        upstream.shutdown()
        upstream.server_close()
        log.close()
        (root / 'go-dependencies.txt').write_bytes(subprocess.check_output(['go', 'version', '-m', binary]))
        report['livePid'] = None if process is None else process.pid
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({'passed': report['passed'], 'checks': len(report['checks']), 'report': str(root / 'report.json')}, ensure_ascii=False))


if __name__ == '__main__':
    main()
