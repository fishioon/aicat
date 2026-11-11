#!/usr/bin/env python3
"""Compare the real MyGo WebView prototype with the native traffic UI.

Failures covered: custom-protocol streaming stalls, summaries carry bodies,
unbounded DOM rows, input loses focus during updates, stale detail reappears,
evictions diverge, clear revives records, or browser portability needs MyGo.
All proxy traffic uses a local fixture and each run uses an isolated Home/OS.
"""
import argparse
import concurrent.futures
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import statistics
import subprocess
import tempfile
import threading
import time
import traceback
from prototype import Automation, wait_for


class Fixture(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path.startswith('/body'):
            data = b'body-fixture\n<script>window.payloadExecuted=true</script>\n' + b'response line\n' * 20000
        else:
            data = json.dumps({'path': self.path, 'ok': True, 'padding': 'x' * 128}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain; charset=utf-8')
        self.send_header('Content-Length', str(len(data)))
        self.send_header('X-Fixture', 'webview-probe')
        self.end_headers()
        self.wfile.write(data)


def summary(values):
    values = sorted(values)
    if not values:
        return {}
    return {'count': len(values), 'median': statistics.median(values),
            'p95': values[min(len(values)-1, int(len(values)*.95))], 'max': max(values)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--webview', default='bin/aicat-webview-probe')
    parser.add_argument('--native', default='bin/aicat-webview-native-e2e')
    parser.add_argument('--port', type=int, default=19996)
    parser.add_argument('--preview-port', type=int, default=19997)
    parser.add_argument('--requests', type=int, default=2200)
    parser.add_argument('--playwright')
    parser.add_argument('--output')
    args = parser.parse_args()
    root = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='webview-probe-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True)
    root.chmod(0o700)
    fixture = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Fixture)
    fixture.daemon_threads = True
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    report = {'passed': False, 'scope': 'traffic page WebView feasibility experiment', 'runs': {}, 'limitations': [
        'WebView uses DOM automation inside the actual WKWebView. Browser preview additionally uses Playwright pointer/keyboard input.',
        'Search timings include automation and wait overhead. Renderer-frame timings are WebView-only, not a native FPS comparison.',
        'RSS is the main Go process only. Separately recorded WebKit processes are candidates, not proven members of this app.',
        'Prototype renders the traffic list, domain filter and Header/Raw. Native UI includes more features; figures are not a feature-parity benchmark.',
    ]}
    processes = []
    clients = []
    logs = []

    def api(path='/api/traffic?limit=2000', port=None):
        client = http.client.HTTPConnection('127.0.0.1', port or args.port, timeout=4)
        try:
            client.request('GET', path)
            response = client.getresponse()
            assert response.status == 200, (response.status, path)
            return json.loads(response.read())
        finally:
            client.close()

    def send(path):
        client = http.client.HTTPConnection('127.0.0.1', args.port, timeout=10)
        try:
            client.request('GET', f'http://127.0.0.1:{fixture.server_port}{path}')
            response = client.getresponse()
            data = response.read()
            assert response.status == 200
            assert data.startswith(b'body-fixture') if path.startswith('/body') else json.loads(data)['path'] == path
        finally:
            client.close()

    def memory(process, destination):
        inventory = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss=,comm='], text=True)
        destination.write_text(inventory)
        own = next(line for line in inventory.splitlines() if line.strip().split()[0] == str(process.pid))
        return {'mainRSSKiB': int(own.split()[2]), 'inventory': str(destination),
                'webKitCandidates': [line.strip() for line in inventory.splitlines() if 'WebKit' in line]}

    def launch(kind):
        folder = root / kind
        folder.mkdir()
        home = folder / '.aicat'
        home.mkdir(mode=0o700)
        (home / 'aicat.yaml').write_text(f'gateway:\n  addr: 127.0.0.1:{args.port}\nui:\n  language: zh-Hans\n  theme: light\ntraffic:\n  maxRecords: 2000\n  maxBodyBytes: 65536\n  maxTotalBodyBytes: 67108864\n')
        state = folder / 'os-state.json'
        state.write_text(json.dumps({'services': [], 'trusted': False}))
        state.chmod(0o600)
        sock = Path(tempfile.gettempdir()) / f'aicat-wv-{os.getpid()}-{kind}.sock'
        binary = str(Path(args.webview if kind == 'webview' else args.native).resolve())
        command = [binary, '--home', str(home)]
        if kind == 'webview':
            command += ['--preview', f'127.0.0.1:{args.preview_port}']
        log = (folder / 'app.log').open('w')
        logs.append(log)
        started = time.monotonic()
        process = subprocess.Popen(command, env=dict(os.environ, MYGO_AUTOMATION=str(sock), AICAT_E2E_OS_STATE=str(state)), stdout=log, stderr=log)
        processes.append(process)
        wait_for(lambda: process.poll() is None and sock.exists() and api()['items'] == [], 20)
        client = Automation(sock)
        clients.append(client)
        wait_for(lambda: client.call('windows').get('windows'), 20)
        if kind == 'webview':
            wait_for(lambda: evaluate(client, 'window.probe?.state()') is not None, 20)
        else:
            wait_for(lambda: any(n.get('key') == 'traffic-table' for n in client.tree()), 20)
        result = {'binary': binary, 'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                  'startupObservedMs': (time.monotonic()-started)*1000, 'checks': [], 'memory': {}}
        result['memory']['idle'] = memory(process, folder / 'processes-idle.txt')
        return process, client, folder, result

    def evaluate(client, script):
        return client.call('eval', script=script).get('value')

    def web_click(client, selector):
        evaluate(client, f'document.querySelector({json.dumps(selector)}).click(); true')

    def web_search(client, value):
        evaluate(client, f'(() => {{const input=document.querySelector("#search");input.focus();input.value={json.dumps(value)};input.dispatchEvent(new Event("input",{{bubbles:true}}));return true;}})()')

    def native_count(client, count):
        return any(n.get('label') == f'{count} 个请求' for n in client.tree())

    def screenshot(client, folder, name):
        client.capture(folder / f'{name}.png')

    def stop(process, client):
        process.send_signal(signal.SIGTERM)
        assert process.wait(timeout=12) == 0
        client.file.close()
        client.sock.close()

    try:
        for kind in ('native', 'webview'):
            process, auto, folder, run = launch(kind)
            report['runs'][kind] = run
            started = time.monotonic()
            with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
                list(pool.map(lambda i: send(f'/traffic/{i:06d}'), range(args.requests)))
            run['loadSeconds'] = time.monotonic()-started
            run['requestsPerSecond'] = args.requests/run['loadSeconds']
            expected = min(args.requests, 2000)
            wait_for(lambda: len(api()['items']) == expected)
            if kind == 'webview':
                wait_for(lambda: evaluate(auto, 'window.probe.state().total') == expected)
                assert evaluate(auto, 'window.probe.state().domRows') < 80
                stats = api('/api/status', args.preview_port)
                assert stats['bodyReads'] == 0 and stats['detailReads'] == 0, stats
                run['transportBeforeDetail'] = stats
            else:
                wait_for(lambda: native_count(auto, expected), 15)
            run['checks'].append(f'{args.requests} real proxy requests converge to {expected} retained summaries')
            run['memory']['loaded'] = memory(process, folder / 'processes-loaded.txt')
            screenshot(auto, folder, 'loaded')

            search_times = []
            for index in range(args.requests-20, args.requests):
                started = time.monotonic()
                query = f'/traffic/{index:06d}'
                if kind == 'webview':
                    web_search(auto, query)
                    wait_for(lambda: evaluate(auto, 'window.probe.state().visible') == 1)
                    assert evaluate(auto, 'document.activeElement.id') == 'search'
                else:
                    auto.act('traffic-search', 'setValue', text=query)
                    wait_for(lambda: native_count(auto, 1))
                search_times.append((time.monotonic()-started)*1000)
            run['searchRoundTripMs'] = summary(search_times)
            run['checks'].append('20 case-preserving URL searches return the expected unique request')

            if kind == 'webview':
                web_search(auto, '')
                evaluate(auto, 'document.querySelector("#rows-viewport").scrollTop=1e9; true')
                wait_for(lambda: evaluate(auto, 'window.probe.state().scrollTop') > 1000)
                assert evaluate(auto, 'window.probe.state().domRows') < 80
                web_search(auto, 'traffic')
                before_batches = evaluate(auto, 'window.probe.metrics.batches')
            else:
                auto.act('traffic-search', 'setValue', text='traffic')

            for index in range(30):
                send(f'/traffic/live-{index}')
                time.sleep(.02)
            if kind == 'webview':
                wait_for(lambda: evaluate(auto, 'window.probe.metrics.batches') > before_batches)
                assert evaluate(auto, 'document.activeElement.id') == 'search'
                assert evaluate(auto, 'document.querySelector("#search").value') == 'traffic'
            run['checks'].append('search remains active while new proxy traffic arrives')

            send('/body')
            target = wait_for(lambda: next((row for row in api()['items'] if row['url'].endswith('/body') and row['state'] == 'completed'), None))
            started = time.monotonic()
            if kind == 'webview':
                web_search(auto, '/body')
                wait_for(lambda: evaluate(auto, f'document.querySelector(".traffic-row")?.dataset.id === "{target["id"]}"'))
                web_click(auto, f'[data-id="{target["id"]}"]')
                wait_for(lambda: evaluate(auto, 'window.probe.state().detailRevision') > 0)
                web_click(auto, '[data-side="response"] [data-view="raw"]')
                wait_for(lambda: evaluate(auto, 'document.querySelector("[data-side=response] .raw").textContent.includes("body-fixture")'))
                assert not evaluate(auto, '!!window.payloadExecuted')
                assert evaluate(auto, 'document.querySelector("[data-side=response] .body-notice").textContent.includes("truncated")')
            else:
                auto.act('traffic-search', 'setValue', text='/body')
                wait_for(lambda: native_count(auto, 1))
                auto.select(target['id'])
                auto.click('response-detail-raw-tab')
                wait_for(lambda: 'body-fixture' in auto.detail('response-detail'))
            run['rawDetailObservedMs'] = (time.monotonic()-started)*1000
            run['checks'].append('selecting the real request displays bounded Raw body and keeps embedded HTML as text')
            screenshot(auto, folder, 'raw')
            run['memory']['detail'] = memory(process, folder / 'processes-detail.txt')

            if kind == 'webview':
                web_click(auto, '#theme')
                screenshot(auto, folder, 'dark')
                # Same assets and Go adapter through a normal browser, with real
                # pointer and keyboard interaction and no MyGo JS bridge.
                if args.playwright:
                    browser_dir = folder / 'browser'
                    env = dict(os.environ, AICAT_PLAYWRIGHT=args.playwright)
                    subprocess.run(['node', 'e2e/webview_probe_browser.cjs', f'http://127.0.0.1:{args.preview_port}', str(browser_dir)], env=env, check=True)
                    run['browser'] = json.loads((browser_dir / 'report.json').read_text())
                run['frontendMetrics'] = evaluate(auto, 'window.probe.metrics')
                assert not run['frontendMetrics']['errors'], run['frontendMetrics']['errors']
                run['renderMs'] = summary(run['frontendMetrics']['renderMs'])
                run['filterMs'] = summary(run['frontendMetrics']['filterMs'])
                run['batchToFrameMs'] = summary(run['frontendMetrics']['deliveryMs'])
                run['frameIntervalMs'] = summary(run['frontendMetrics']['framesMs'])
                run['transport'] = api('/api/status', args.preview_port)
                web_click(auto, '#clear')
                wait_for(lambda: evaluate(auto, 'window.probe.state().total') == 0)
                assert evaluate(auto, 'window.probe.state().selected') == 0
            else:
                auto.click('theme-toggle')
                screenshot(auto, folder, 'dark')
                auto.click('clear-traffic')
                wait_for(lambda: native_count(auto, 0))
            assert api()['items'] == []
            run['checks'].append('clear removes list and selection after body loads')
            run['memory']['cleared'] = memory(process, folder / 'processes-cleared.txt')
            stop(process, auto)
        report['passed'] = True
    except BaseException as error:
        report['error'] = repr(error)
        report['traceback'] = traceback.format_exc()
        if 'auto' in locals():
            try: screenshot(auto, folder, 'failure')
            except Exception: pass
        raise
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try: process.wait(timeout=12)
                except subprocess.TimeoutExpired: process.kill(); process.wait()
        for log in logs:
            log.close()
        fixture.shutdown()
        fixture.server_close()
        report['arguments'] = vars(args)
        report['sources'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in [
            *Path('internal/webviewprobe').rglob('*'), Path('internal/app/ui_webview_probe.go'),
            Path('cmd/aicat-webview-probe/main.go'), Path('e2e/webview_probe.py'), Path('e2e/webview_probe_browser.cjs')
        ] if p.is_file()}
        (root / 'dependencies.txt').write_text(subprocess.check_output(['go', 'version', '-m', args.webview], text=True))
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({'passed': report['passed'], 'artifacts': str(root), 'error': report.get('error')}, ensure_ascii=False))


if __name__ == '__main__':
    main()
