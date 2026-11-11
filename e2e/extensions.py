#!/usr/bin/env python3
"""Extensions-only native E2E; isolated Home, local HTTP and fake OS state.

Failure modes: old Settings entry survives, sidebar becomes page links instead
of searchable groups, filtering loses selection, failed save changes runtime,
disabled traffic opens through history, pause/config is lost, or the permanent
Extensions entry disappears with traffic. No real Keychain/system proxy writes.
"""
import argparse
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
from prototype import Automation, Fixture, wait_for


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-extensions-e2e')
    parser.add_argument('--port', type=int, default=19994)
    parser.add_argument('--output')
    parser.add_argument('--navigation-only', action='store_true')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    root = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='extensions-e2e-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    home = root / '.aicat'; home.mkdir(mode=0o700)
    config = home / 'aicat.yaml'
    config.write_text(f'gateway:\n  addr: 127.0.0.1:{args.port}\nui:\n  language: zh-Hans\n  theme: light\n# preserve extension migration\nfutureSetting: keep\n')
    os_state = root / 'os-state.json'
    os_state.write_text(json.dumps({'services': [], 'trusted': False})); os_state.chmod(0o600)
    sock = Path(tempfile.gettempdir()) / f'aicat-extensions-{os.getpid()}.sock'
    env = dict(os.environ, MYGO_AUTOMATION=str(sock), AICAT_E2E_OS_STATE=str(os_state))
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Fixture)
    server.daemon_threads = True
    threading.Thread(target=server.serve_forever, daemon=True).start()
    log = (root / 'app.log').open('w')
    processes = []
    auto = None
    report = {'passed': False, 'scope': 'extensions UI and traffic feature migration', 'checks': [],
              'binary': binary, 'binarySHA256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
              'command': [sys.executable, *sys.argv], 'port': args.port}

    def page():
        c = http.client.HTTPConnection('127.0.0.1', args.port, timeout=2)
        try:
            c.request('GET', '/api/traffic'); r = c.getresponse()
            assert r.status == 200
            return json.loads(r.read())['items']
        finally:
            c.close()

    def request(path):
        c = http.client.HTTPConnection('127.0.0.1', args.port, timeout=3)
        try:
            c.request('GET', f'http://127.0.0.1:{server.server_port}{path}')
            r = c.getresponse(); data = json.loads(r.read())
            assert r.status == 200 and data['path'] == path
        finally:
            c.close()

    def launch():
        p = subprocess.Popen([binary, '--home', str(home)], env=env, stdout=log, stderr=log)
        processes.append(p)
        wait_for(lambda: p.poll() is None and sock.exists() and page() == [])
        client = Automation(sock)
        wait_for(lambda: client.call('windows').get('windows'))
        client.call('idle', timeout=3000)
        return p, client

    def stop(p):
        p.send_signal(signal.SIGTERM); assert p.wait(timeout=8) == 0
        auto.file.close(); auto.sock.close()
        wait_for(lambda: not sock.exists())

    def node(key):
        return next((n for n in auto.tree() if n.get('key') == key), None)

    def checked():
        return 'checked' in node('feature-traffic').get('states', [])

    def snapshot(name):
        auto.call('idle', timeout=3000)
        auto.capture(root / f'native-{name}.png')
        (root / f'{name}-tree.json').write_text(json.dumps(auto.tree(), ensure_ascii=False, indent=2))

    try:
        p, auto = launch()
        if args.navigation_only:
            # A pin changes placement only. Cover save failure, route selection,
            # dismissal, reload, and a disabled module with a saved pin.
            report['scope'] = 'navigation menu and pins'
            assert node('nav-traffic') and node('nav-extensions')
            auto.click('nav-more')
            assert node('nav-more-traffic') and node('nav-pin-traffic')
            assert node('nav-pin-traffic')['label'].startswith('从导航栏取消固定')
            saved = config.read_text()
            invalid = saved + '\nnavigation: []\n'
            config.write_text(invalid)
            auto.click('nav-pin-traffic'); wait_for(lambda: node('nav-pin-error'))
            assert node('nav-traffic') and config.read_text() == invalid
            config.write_text(saved)
            auto.click('nav-pin-traffic')
            wait_for(lambda: not node('nav-traffic'))
            assert node('traffic-table') and not node('nav-pin-error')
            assert node('nav-pin-traffic')['label'].startswith('固定到导航栏')
            request('/unpinned'); wait_for(lambda: any(r['url'].endswith('/unpinned') for r in page()))
            snapshot('navigation-more')
            auto.call('input', events=[{'type': 'key', 'key': 'Escape'}])
            wait_for(lambda: not node('nav-more-traffic'))
            auto.click('nav-extensions')
            auto.click('nav-more'); auto.click('nav-more-traffic')
            assert node('traffic-table') and not node('nav-more-traffic')
            auto.click('history-back'); assert node('feature-traffic')
            report['checks'].append('failed saves preserve placement; unpin keeps capture active; menu selection, Escape and history work')

            stop(p); p, auto = launch()
            assert node('traffic-table') and not node('nav-traffic')
            auto.click('nav-more'); auto.click('nav-pin-traffic')
            wait_for(lambda: node('nav-traffic'))
            assert node('traffic-table')
            auto.call('input', events=[{'type': 'key', 'key': 'Escape'}])
            stop(p); p, auto = launch()
            assert node('nav-traffic') and node('nav-extensions')
            auto.click('nav-extensions'); auto.click('feature-traffic')
            wait_for(lambda: not checked())
            auto.click('nav-more')
            assert not node('nav-more-traffic') and node('nav-more-empty')
            auto.call('input', events=[{'type': 'key', 'key': 'Escape'}])
            wait_for(lambda: not node('nav-more-empty'))
            auto.click('feature-traffic')
            wait_for(lambda: node('nav-traffic'))
            auto.click('theme-toggle')
            wait_for(lambda: 'theme: dark' in config.read_text())
            auto.click('nav-more'); snapshot('navigation-more-dark')
            report['checks'].append('pin and unpin survive restarts; disabled traffic is hidden in both places and its pin returns on enable')
            stop(p)
            report['passed'] = True
            return
        auto.click('account-menu'); auto.click('nav-settings')
        assert not node('settings-features')
        auto.click('nav-extensions')
        auto.check_sidebar('扩展')
        assert node('extension-search') and node('extension-traffic') and node('extension-community')
        assert node('extensions-installed-toggle') and node('extensions-builtin-toggle') and node('extensions-recommended-toggle')
        assert node('feature-traffic') and checked() and not node('record-toggle') and not node('traffic-toolbar')
        snapshot('extensions-light')
        auto.click('extensions-builtin-toggle'); assert not node('extension-traffic')
        auto.click('extensions-builtin-toggle'); assert node('extension-traffic')
        auto.act('extension-search', action='setValue', text='HTTP')
        assert node('extension-traffic')
        auto.act('extension-search', action='setValue', text='unmatched-fixture')
        assert not node('extension-traffic') and node('extensions-no-results') and checked()
        auto.click('extension-search-clear'); assert node('extension-traffic')
        auto.click('extension-community'); assert node('community-extension-placeholder') and not node('feature-traffic')
        snapshot('community-placeholder')
        auto.click('history-back'); assert node('feature-traffic')
        auto.click('history-forward'); assert node('community-extension-placeholder')
        auto.click('extension-traffic')
        auto.click('toggle-sidebar'); assert not node('sidebar')
        assert node('feature-traffic')
        auto.click('extensions-show-sidebar'); assert node('extension-search')
        report['checks'].append('permanent rail entry, searchable/collapsible groups, selected detail, community placeholder, history and collapsed-sidebar recovery')

        saved = config.read_text(); invalid = saved + '\nfeatures: []\n'
        config.write_text(invalid)
        auto.click('feature-traffic'); wait_for(lambda: node('feature-error'))
        assert checked() and node('nav-traffic') and config.read_text() == invalid
        config.write_text(saved)
        report['checks'].append('failed config save preserves switch, navigation and file; error appears in extension detail')

        auto.click('extension-open-traffic'); auto.click('record-toggle')
        request('/paused'); assert page() == []
        auto.click('nav-extensions'); auto.click('feature-traffic')
        wait_for(lambda: not node('nav-traffic') and not checked())
        assert node('nav-extensions') and node('feature-traffic-status')['label'] == '已关闭'
        request('/disabled'); assert page() == []
        assert 'traffic: false' in config.read_text() and '# preserve extension migration' in config.read_text() and 'futureSetting: keep' in config.read_text()
        snapshot('extensions-disabled')
        steps = 0
        while 'disabled' not in node('history-back').get('states', []):
            auto.click('history-back'); steps += 1
            assert steps < 30 and not node('traffic-table') and not node('nav-traffic')
        for _ in range(steps):
            auto.click('history-forward'); assert not node('traffic-table')
        auto.click('nav-extensions'); auto.click('feature-traffic')
        wait_for(lambda: checked() and node('nav-traffic'))
        auto.click('extension-open-traffic')
        assert '继续录制' in node('record-toggle')['label']
        request('/still-paused'); assert page() == []
        auto.click('record-toggle'); request('/enabled')
        wait_for(lambda: any(r['url'].endswith('/enabled') and r['state'] == 'completed' for r in page()))
        retained = page()
        auto.click('nav-extensions'); auto.click('feature-traffic')
        wait_for(lambda: not checked())
        request('/retained'); assert page() == retained
        report['checks'].append('disable stops capture and hides traffic, proxy/API/retained records survive, history skips traffic, enable respects pause')

        stop(p); p, auto = launch()
        wait_for(lambda: node('feature-traffic'))
        assert not checked() and node('nav-extensions') and not node('nav-traffic')
        auto.check_sidebar('扩展'); request('/disabled-restart'); assert page() == []
        snapshot('extensions-disabled-restart')
        auto.click('feature-traffic'); wait_for(lambda: checked())
        request('/enabled-restart')
        wait_for(lambda: any(r['id'] == 1 and r['url'].endswith('/enabled-restart') for r in page()))
        report['checks'].append('disabled restart opens built-in traffic extension; enable restores capture without migration of features.traffic')

        auto.click('extension-proxy-settings'); assert node('proxy-address')
        auto.click('history-back'); assert node('feature-traffic')
        auto.click('account-menu'); auto.click('nav-settings')
        auto.click('preference-language')
        english = wait_for(lambda: next((n for n in auto.tree() if n.get('role') in ('option', 'listItem') and n.get('label') == 'English'), None))
        auto.call('act', id=english['id'], action='press')
        wait_for(lambda: node('preference-language').get('value') == 'English' and 'disabled' not in node('preference-language').get('states', []))
        auto.click('preference-theme-dark')
        wait_for(lambda: 'theme: dark' in config.read_text() and 'disabled' not in node('preference-language').get('states', []))
        auto.click('nav-extensions')
        wait_for(lambda: node('breadcrumb-section')['label'] == 'Extensions')
        assert node('feature-traffic-status')['label'] == 'On'
        snapshot('extensions-english-dark')
        auto.click('extension-community'); snapshot('community-english-dark')
        assert node('community-extension-placeholder')
        state = json.loads(os_state.read_text())
        assert not state.get('trusted') and state['services'] == []
        report['checks'].append('proxy configuration link, settings migration, English and dark appearance; no OS trust/proxy mutation')
        stop(p)
        report['passed'] = True
    except BaseException as err:
        report['error'] = repr(err)
        if auto:
            try: snapshot('failure')
            except Exception: pass
        raise
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
                try: process.wait(timeout=8)
                except subprocess.TimeoutExpired: process.kill(); process.wait()
        server.shutdown(); server.server_close(); log.close()
        report['screenshots'] = [str(p) for p in root.glob('native-*.png')]
        report['sources'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in [*Path('internal/app').glob('*.go'), Path('e2e/extensions.py')]}
        (root / 'dependencies.txt').write_text(subprocess.check_output(['go', 'version', '-m', binary], text=True))
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({k: v for k, v in report.items() if k != 'sources'}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
