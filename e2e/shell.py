#!/usr/bin/env python3
"""Native shell contract against real local proxy traffic, isolated OS and home.
Failure modes: unstable input identity/focus, path-prefix leakage, stale selection,
pin loss, wrong split geometry, Raw body alteration and stale translated UI.
Copy feedback: missing/wrong success status, stacked notices on repeat, notices
that never dismiss or steal focus, and clipboard bytes differing from Raw.
Application grouping: proxy/client socket confusion, ambiguous attribution,
lost connection reuse, tunnels counted as requests, and stale sidebar filters.
"""
import argparse
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import plistlib
import signal
import shlex
import socket
import subprocess
import tempfile
import threading
import time
from prototype import Automation, wait_for


class Fixture(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *_): pass
    def do_GET(self): self.do_POST()
    def do_POST(self):
        self.server.last_body=self.rfile.read(int(self.headers.get('Content-Length', 0)))
        self.server.last_headers=dict(self.headers)
        if self.path == '/apps/first':
            self.server.app_request_arrived.set()
            assert self.server.release_app_request.wait(10)
        payload = b'{  "message": "shell-fixture",\n "ok": true }\n'
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.send_header('X-Fixture', 'native-shell')
        self.send_header('X-Multi', 'one')
        self.send_header('X-Multi', 'two')
        self.end_headers()
        self.wfile.write(payload)


def check_application_groups(auto, proxy_port, server, items, rows, root):
    fixture_host = f'localhost:{server.server_port}'
    app = root / 'Capture Fixture.app'
    helper = app / 'Contents/Frameworks/Request Helper.app/Contents/MacOS/request-helper'
    helper.parent.mkdir(parents=True)
    subprocess.run(['/usr/bin/clang', '-x', 'c', '-', '-lcurl', '-o', str(helper)], input=r'''
#include <curl/curl.h>
#include <stdio.h>

int main(int argc, char **argv) {
    if (argc != 4) return 1;
    CURL *client = curl_easy_init();
    if (!client) return 1;
    curl_easy_setopt(client, CURLOPT_PROXY, argv[1]);
    curl_easy_setopt(client, CURLOPT_NOPROXY, "");
    curl_easy_setopt(client, CURLOPT_TIMEOUT, 12L);
    curl_easy_setopt(client, CURLOPT_FAILONERROR, 1L);
    for (int i = 2; i < argc; i++) {
        curl_easy_setopt(client, CURLOPT_URL, argv[i]);
        CURLcode result = curl_easy_perform(client);
        if (result != CURLE_OK) {
            fprintf(stderr, "%s\n", curl_easy_strerror(result));
            curl_easy_cleanup(client);
            return 1;
        }
        long connections;
        curl_easy_getinfo(client, CURLINFO_NUM_CONNECTS, &connections);
        printf("\nconnections=%ld\n", connections);
    }
    curl_easy_cleanup(client);
    return 0;
}
''', text=True, check=True)
    (app / 'Contents/Info.plist').write_bytes(plistlib.dumps({
        'CFBundleIdentifier': 'ai.netcat.aicat.capture-fixture',
        'CFBundleDisplayName': 'Capture Fixture',
        'CFBundlePackageType': 'APPL',
    }))
    expected = {'name': 'Capture Fixture', 'bundleId': 'ai.netcat.aicat.capture-fixture'}
    app_key = 'client-app-bundle:' + expected['bundleId']
    proxy = f'http://127.0.0.1:{proxy_port}'
    base = f'http://{fixture_host}/apps/'
    server.app_request_arrived = threading.Event()
    server.release_app_request = threading.Event()

    def record(path):
        return next((r for r in items() if r['url'] == base + path), {})

    def node(key):
        return next((n for n in auto.tree() if n.get('key') == key), {})

    def expect_rows(*suffixes):
        wait_for(lambda: [r['label'].rsplit('/', 1)[-1] for r in rows()] == list(suffixes))

    curl = subprocess.Popen([str(helper), proxy, base + 'first', base + 'second'],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        if not server.app_request_arrived.wait(5):
            if curl.poll() is not None:
                raise AssertionError(f'capture fixture exited before reaching the origin: {curl.communicate()}')
            raise AssertionError('capture fixture never reached the origin')
        wait_for(lambda: record('first').get('clientApp') == expected)
        server.release_app_request.set()
        out, err = curl.communicate(timeout=12)
        assert curl.returncode == 0 and b'connections=1' in out and b'connections=0' in out, (out, err)
    finally:
        server.release_app_request.set()
        if curl.poll() is None:
            curl.kill()
            curl.communicate()
    wait_for(lambda: record('second').get('clientApp') == expected and record('second')['state'] == 'completed')

    client = http.client.HTTPConnection('127.0.0.1', proxy_port, timeout=4)
    try:
        client.request('GET', base + 'python')
        response = client.getresponse()
        assert response.status == 200
        response.read()
        python_app = wait_for(lambda: record('python').get('clientApp'))
        assert 'python' in python_app['name'].lower(), python_app
    finally:
        client.close()
    python_key = 'client-app-bundle:' + python_app['bundleId'].lower() if 'bundleId' in python_app else 'client-app-process:' + python_app['name'].lower()
    with socket.create_connection(('127.0.0.1', proxy_port), timeout=4) as tunnel:
        tunnel.sendall(f'CONNECT {fixture_host} HTTP/1.1\r\nHost: {fixture_host}\r\n\r\n'.encode())
        assert b'200 Connection Established' in tunnel.recv(4096)
        wait_for(lambda: any(r.get('tunnel') and r.get('clientApp') == python_app for r in items()))
        wait_for(lambda: node(python_key).get('value') == '1 · 1 隧道')

    # A socket inherited by another process has two visible owners.
    shared = http.client.HTTPConnection('127.0.0.1', proxy_port, timeout=4)
    shared.connect()
    child = subprocess.Popen(['/bin/sleep', '15'], pass_fds=(shared.sock.fileno(),))
    try:
        shared.request('GET', base + 'shared')
        response = shared.getresponse()
        assert response.status == 200
        response.read()
        wait_for(lambda: record('shared').get('state') == 'completed')
        wait_for(lambda: node('client-app-unknown').get('value') == '1')
        # Wait for a following lookup to drain the earlier lookup before checking absence.
        client = http.client.HTTPConnection('127.0.0.1', proxy_port, timeout=4)
        try:
            client.request('GET', base + 'later')
            client.getresponse().read()
            wait_for(lambda: record('later').get('clientApp') == python_app)
        finally:
            client.close()
        assert 'clientApp' not in record('shared'), record('shared')
    finally:
        shared.close()
        child.terminate()
        child.wait(timeout=3)

    wait_for(lambda: node(app_key).get('value') == '2')
    wait_for(lambda: node(python_key).get('value') == '2 · 1 隧道')
    wait_for(lambda: len(rows()) == 6)
    auto.capture(root / 'applications.png')
    auto.click(app_key)
    expect_rows('first', 'second')
    assert node('breadcrumb-title').get('label') == expected['name']
    auto.capture(root / 'application-filter.png')
    auto.act('traffic-search', 'setValue', text='second')
    expect_rows('second')
    auto.click('all-traffic')
    wait_for(lambda: len(rows()) == 6)
    auto.click('history-back')
    expect_rows('second')
    auto.click('history-forward')
    wait_for(lambda: len(rows()) == 6)
    auto.act('domain-search', 'setValue', text='capture fixture')
    assert node(app_key) and not node(python_key)
    auto.act('domain-search', 'setValue', text='')
    auto.click('client-app-unknown')
    expect_rows('shared')
    auto.click('apps-group-toggle')
    wait_for(lambda: len(rows()) == 6)
    auto.click('apps-group-toggle')
    auto.click(app_key)
    auto.click('clear-traffic')
    wait_for(lambda: not items() and not rows() and not node(app_key))
    client = http.client.HTTPConnection('127.0.0.1', proxy_port, timeout=4)
    try:
        client.request('GET', base + 'after-clear')
        client.getresponse().read()
        expect_rows('after-clear')
    finally:
        client.close()
    (root / 'application-traffic.json').write_text(json.dumps({
        'bundledHelper': expected, 'python': python_app,
        'countsBeforeClear': {'bundledHelper': 2, 'python': 2, 'pythonTunnels': 1, 'unknown': 1},
    }, indent=2))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-shell-e2e')
    parser.add_argument('--output')
    parser.add_argument('--empty-pins-only', action='store_true')
    parser.add_argument('--raw-only', action='store_true')
    parser.add_argument('--copy-feedback-only', action='store_true')
    parser.add_argument('--inspector-menu-only', action='store_true')
    parser.add_argument('--sidebar-only', action='store_true')
    parser.add_argument('--search-only', action='store_true')
    parser.add_argument('--applications-only', action='store_true')
    opts = parser.parse_args()
    root = Path(opts.output).resolve() if opts.output else Path(tempfile.mkdtemp(prefix='shell-e2e-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    assert not (root/'home').exists(), 'use a fresh output directory'
    binary = Path(opts.binary).resolve()
    report = {'passed': False, 'checks': [], 'artifacts': str(root), 'binary': str(binary),
              'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest(),
              'replay': f'python3 e2e/shell.py --binary {binary}'+(' --empty-pins-only' if opts.empty_pins_only else '')+(' --raw-only' if opts.raw_only else '')+(' --copy-feedback-only' if opts.copy_feedback_only else ''), 'buildCommand':'go build -tags e2e -o bin/aicat-shell-e2e ./cmd/aicat', 'renderMode':'native-display'}
    report['sources'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for pattern in ('internal/app/*.go', 'internal/app/locales/*.json', 'internal/config/*.go', 'internal/gateway/addons/traffic/*', 'internal/traffic/*.go', 'e2e/shell.py', 'go.mod', 'go.sum') for p in Path('.').glob(pattern)}
    if opts.inspector_menu_only: report['replay']+=' --inspector-menu-only'
    if opts.sidebar_only: report['replay']+=' --sidebar-only'
    if opts.search_only: report['replay']+=' --search-only'
    if opts.applications_only:
        report['replay'] += ' --applications-only'
        report['buildCommand'] = f'go build -race -tags e2e -o {shlex.quote(str(binary))} ./cmd/aicat'
    (root/'go-version.txt').write_bytes(subprocess.check_output(['go', 'version', '-m', str(binary)]))
    home = root/'home'; home.mkdir(exist_ok=True)
    with socket.socket() as s: s.bind(('127.0.0.1',0)); port = s.getsockname()[1]
    (home/'aicat.yaml').write_text(f'ui:\n  language: zh-Hans\n  theme: light\ngateway:\n  addr: 127.0.0.1:{port}\n  interceptHosts: []\ntraffic:\n  maxRecords: 100\n  maxBodyBytes: 65536\n  maxTotalBodyBytes: 1048576\n# preserve-shell-fixture\n')
    os_state = root/'os-state.json'; os_state.write_text(json.dumps({'services': [], 'trusted': False})); os_state.chmod(0o600)
    sock_path = Path(tempfile.gettempdir())/f'aicat-shell-{os.getpid()}.sock'
    env = dict(os.environ, MYGO_AUTOMATION=str(sock_path), AICAT_E2E_OS_STATE=str(os_state))
    log = (root/'app.log').open('w'); proc = None; auto = None
    server = http.server.ThreadingHTTPServer(('127.0.0.1',0), Fixture)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    fixture_port = server.server_port; host = f'localhost:{fixture_port}'
    def api(path='/api/traffic'):
        c = http.client.HTTPConnection('127.0.0.1', port, timeout=4); c.request('GET',path)
        r = c.getresponse(); data = r.read(); c.close(); assert r.status==200
        return json.loads(data)
    def items(): return api()['items']
    def send(path, body=b'', hostname='localhost'):
        c = http.client.HTTPConnection('127.0.0.1',port,timeout=4)
        c.request('POST' if body else 'GET', f'http://{hostname}:{fixture_port}{path}',body,{'Content-Type':'application/json', 'X-Shell':'header-marker', 'X-Quote':"O'Brien $HOME; `echo unchanged`"})
        r=c.getresponse(); data=r.read(); c.close(); assert r.status==200
        return wait_for(lambda: next((r for r in items() if r['url'].endswith(path) and r['state']=='completed'),None))
    def launch():
        nonlocal proc,auto
        proc = subprocess.Popen([str(binary),'--home',str(home)],env=env,stdout=log,stderr=log)
        wait_for(lambda: proc.poll() is None and sock_path.exists() and api(),15)
        auto=Automation(sock_path); wait_for(lambda:auto.call('windows').get('windows')); auto.call('menu',path=['E2E','打开窗口']); auto.call('idle',timeout=3000)
    def stop():
        nonlocal proc,auto
        proc.send_signal(signal.SIGTERM); assert proc.wait(timeout=8)==0
        auto.file.close();auto.sock.close();auto=None;proc=None;wait_for(lambda:not sock_path.exists())
    def node(key): return next(n for n in auto.tree() if n.get('key')==key)
    def rows(): return [n for n in auto.tree() if n.get('role')=='row' and n.get('label','').startswith('#') and n.get('label','')[1:2].isdigit()]
    def count(n): wait_for(lambda:len(rows())==n)
    def check(name): report['checks'].append(name)
    def click_node(n,button='left'):
        b=n['bounds'];auto.call('input',events=[{'type':'click','x':b['x']+b['width']/2,'y':b['y']+b['height']/2,'button':button}]);auto.call('idle',timeout=3000)
    try:
        if os.uname().sysname == 'Darwin':
            session = subprocess.check_output(['ioreg','-n','Root','-d1'],text=True)
            if '"CGSSessionScreenIsLocked"=Yes' in session:
                report['blocked'] = True
                raise RuntimeError('macOS is locked; unlock before native display E2E')
        launch()
        if opts.applications_only:
            check_application_groups(auto, port, server, items, rows, root)
            check('macOS bundle/helper attribution, process fallback, keep-alive reuse, ambiguous socket, CONNECT count, sidebar search/filter/history and clear')
            stop();report['passed']=True
            return
        if opts.empty_pins_only:
            assert not any(n.get('label') in ('暂无固定域名','No pinned domains') for n in auto.tree())
            assert abs(node('all-traffic')['bounds']['y']-node('pinned-group')['bounds']['y']-47)<.1
            auto.click('pinned-group-toggle');auto.click('pinned-group-toggle')
            assert abs(node('all-traffic')['bounds']['y']-node('pinned-group')['bounds']['y']-47)<.1
            auto.capture(root/'empty-pins.png')
            check('empty pinned group retains header and zero count without placeholder row or extra gap; collapse/reopen stays compact')
            stop();report['passed']=True
            return
        body=b'''{  "input": "body-only-marker",\n  "stream": false, "note": "O'Brien $HOME; `echo unchanged`" }\n'''
        recs=[send('/v1/responses',body),send('/v1/responses?trace=2'),send('/v1/responses-extra'),send('/v1/chat/completions'),send('/assets/main.css'),send('/health',hostname='127.0.0.1')]
        count(6);auto.select(recs[0]['id']);count(6)
        wait_for(lambda:any(n.get('key')=='request-detail-headers' for n in auto.tree()))
        if opts.search_only:
            # Failure modes: substring matching survives; tokens become ordered
            # or OR; non-URL fields leak in; filtering reorders or selects rows;
            # live traffic replaces the input or resets its focus/caret.
            def search(query, expected):
                auto.act('traffic-search','setValue',text=query)
                wait_for(lambda:[int(n['label'].split()[0][1:]) for n in rows()]==expected)
            ids=[r['id'] for r in recs]
            auto.click('traffic-search');input_id=node('traffic-search')['id']
            for char in 'RSPNS':
                auto.call('input',events=[{'type':'text','text':char}]);auto.call('idle',timeout=3000)
                assert node('traffic-search')['id']==input_id and auto.call('tree').get('focus')==input_id
            wait_for(lambda:[int(n['label'].split()[0][1:]) for n in rows()]==ids[:3])
            auto.select(ids[1]);auto.click('traffic-search')
            live=send('/v2/responses?trace=fuzzy-live')
            wait_for(lambda:[int(n['label'].split()[0][1:]) for n in rows()]==ids[:3]+[live['id']])
            assert node('traffic-search')['id']==input_id and auto.call('tree').get('focus')==input_id
            for query,expected in [('cht cmpl',[ids[3]]),('CMPL CHT',[ids[3]]),('completions chat',[ids[3]]),('zzzz',[]),('body-only-marker',[]),('header-marker',[]),('shell-fixture',[]),('^responses',[]),("'responses",[]),('  \t ',ids+[live['id']])]:
                search(query,expected)
                assert not any(n.get('key')=='request-detail-headers' for n in auto.tree()), 'filter selected a new request automatically'
            # Include the IP literal so the query cannot also match characters
            # spread across the scheme and localhost hostname.
            auto.click('domain-'+host);search('127.0.0.1 hlth',[])
            auto.click('all-traffic');search('127.0.0.1 hlth',[ids[5]])
            auto.click('all-traffic');search('rspns',ids[:3]+[live['id']])
            auto.select(ids[0])
            auto.capture(root/'fuzzy-url-search.png')
            auto.click('theme-toggle')
            wait_for(lambda:'theme: dark' in (home/'aicat.yaml').read_text())
            auto.call('idle',timeout=3000)
            auto.capture(root/'fuzzy-url-search-dark.png')
            check('ordered non-contiguous characters; insensitive AND tokens in either order; whitespace matches all; literal operators; URL-only fields; original row order; native typing/live focus; domain intersection')
            stop();report['passed']=True
            return
        if opts.sidebar_only:
            # Failure modes: decorative icons leave extra leading width; tree
            # disclosure/filter/Pin regress when their icon columns are removed;
            # removed sessions, filters or proxy copy controls return.
            def no_icon_gap(key,label):
                # Native accessibility folds a button's decorative children.
                # Verify the label hit region immediately follows disclosure;
                # retained screenshots verify the glyphs and absence of icons.
                button,arrow=node(key),node(key+'-toggle')['bounds']
                assert button['label']==label
                assert abs(button['bounds']['x']-arrow['x']-arrow['width'])<.1,(key,button,arrow)
            no_icon_gap('pinned-group','固定');no_icon_gap('all-traffic','域名')
            no_icon_gap('domain-'+host,host)
            auto.click('domain-'+host+'-toggle');auto.click('path-domain-'+host+'/v1-toggle')
            auto.click('path-domain-'+host+'/v1/responses');count(2)
            auto.click('domain-'+host);count(5);auto.click('all-traffic');count(6)
            click_node(node('domain-'+host),'right')
            pin=wait_for(lambda:next((n for n in auto.tree() if n.get('role')=='menuItem' and '固定' in n.get('label','')),None))
            auto.call('act',id=pin['id'],action='press');wait_for(lambda:any(n.get('key')=='pin-'+host for n in auto.tree()))
            no_icon_gap('pin-'+host,host)
            assert not any(n.get('label') in ('今天','本机会话','全部请求','已改写','错误请求','暂无固定域名') for n in auto.tree())
            assert abs(node('titlebar')['bounds']['height']-48)<.1
            auto.capture(root/'sidebar-light.png')
            (root/'sidebar-tree.json').write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
            auto.click('theme-toggle');auto.capture(root/'sidebar-dark.png')
            auto.click('account-menu');auto.click('nav-settings');auto.click('settings-proxy')
            assert not any(n.get('key') in ('copy-proxy-address','copy-curl-example') or n.get('label') in ('复制地址','Copy address','Copy cURL example') for n in auto.tree())
            auto.capture(root/'proxy-no-copy.png')
            auto.click('settings-general')
            assert any(n.get('key')=='preference-language' for n in auto.tree())
            assert not any(n.get('label')=='外观' for n in auto.tree())
            (root/'settings-tree.json').write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
            check('group/domain/pinned labels begin directly after disclosure; no decorative icon gap; tree filtering and right-click Pin remain functional; no removed sessions/filters; 48pt titlebar')
            check('proxy address and cURL example copy controls absent; language and theme remain together in General')
            stop();report['passed']=True;return
        if opts.inspector_menu_only:
            # Failure modes: pane views reset/couple; Header renders/copies body;
            # duplicate headers disappear; a context action targets the old row;
            # shell quoting changes bytes; Pin state diverges or is not saved.
            def has(key): return any(n.get('key')==key for n in auto.tree())
            def open_row(record):
                row=next(n for n in rows() if n['label'].startswith(f"#{record['id']} "))
                click_node(row,'right')
                wait_for(lambda:any(n.get('key')=='{request-copy-url}' for n in auto.tree()))
            def menu_item(key): return next(n for n in auto.tree() if n.get('key')=='{'+key+'}')
            def choose(key):
                auto.call('act',id=menu_item(key)['id'],action='press');auto.call('idle',timeout=3000)
            assert has('response-detail-headers') and not has('request-detail') and not has('response-detail')
            assert len([n for n in auto.tree() if n.get('role')=='tab'])==4
            assert len([n for n in auto.tree() if n.get('label')=='X-Multi'])==2
            auto.click('request-detail-copy');headers=subprocess.check_output(['pbpaste']).decode()
            assert headers.startswith('Host: '+host) and 'body-only-marker' not in headers and 'POST /' not in headers
            auto.click('response-detail-copy');response_headers=subprocess.check_output(['pbpaste']).decode()
            assert 'X-Multi: one\nX-Multi: two' in response_headers and 'shell-fixture' not in response_headers
            auto.capture(root/'header-light.png')
            auto.click('request-detail-raw-tab');wait_for(lambda:'body-only-marker' in auto.detail('request-detail'))
            assert has('response-detail-headers') and not has('response-detail')
            auto.select(recs[1]['id']);wait_for(lambda:'trace=2' in auto.detail('request-detail'))
            assert has('response-detail-headers')
            auto.click('response-detail-raw-tab');wait_for(lambda:'shell-fixture' in auto.detail('response-detail'))
            auto.select(recs[0]['id']);wait_for(lambda:'body-only-marker' in auto.detail('request-detail'))
            assert 'shell-fixture' in auto.detail('response-detail')
            auto.click('request-detail-header-tab');assert has('response-detail') and has('request-detail-headers')
            check('Header defaults on both sides; repeated headers and header-only copy; independent view choices retained across requests')
            open_row(recs[1]);choose('request-copy-url')
            assert subprocess.check_output(['pbpaste']).decode()==recs[1]['url']
            open_row(recs[0]);choose('request-copy-curl')
            command=subprocess.check_output(['pbpaste']).decode();(root/'copied.curl').write_text(command)
            argv=shlex.split(command.replace(' \\\n  ',' '));assert argv[0]=='curl' and argv[argv.index('--url')+1]==recs[0]['url']
            assert argv[argv.index('--request')+1]=='POST' and argv[argv.index('--data-raw')+1]==body.decode()
            result=subprocess.run(argv+['--silent','--show-error','--max-time','5'],capture_output=True,check=True)
            assert b'shell-fixture' in result.stdout
            assert server.last_body==body and server.last_headers['X-Quote']=="O'Brien $HOME; `echo unchanged`"
            open_row(recs[1]);choose('request-pin');wait_for(lambda:has('pin-'+host))
            assert len(rows())==6
            open_row(recs[0]);assert menu_item('request-pin')['label']=='取消固定';choose('request-pin');wait_for(lambda:not has('pin-'+host))
            open_row(recs[0]);choose('request-pin');wait_for(lambda:has('pin-'+host))
            auto.capture(root/'pinned-request-menu.png')
            oversized=send('/oversized',b'x'*70000);count(7);open_row(oversized)
            assert 'disabled' in menu_item('request-copy-curl').get('states',[])
            auto.call('input',events=[{'type':'key','key':'Escape'}]);auto.call('idle',timeout=3000)
            tunnel=http.client.HTTPConnection('127.0.0.1',port,timeout=4);tunnel.request('CONNECT',host)
            tunnel_response=tunnel.getresponse();assert tunnel_response.status==200
            tunnel.close();tunnel_response.close()
            connect=wait_for(lambda:next((r for r in items() if r['method']=='CONNECT'),None));count(8);open_row(connect)
            assert 'disabled' in menu_item('request-copy-curl').get('states',[])
            choose('request-copy-url');assert subprocess.check_output(['pbpaste']).decode()==connect['url']
            check('right-click copies clicked URL and executable cURL; request Pin/Unpin shares sidebar state; truncated bodies and CONNECT cannot export incomplete cURL')
            auto.click('account-menu');auto.click('nav-settings');auto.click('preference-language')
            english=wait_for(lambda:next((n for n in auto.tree() if n.get('role') in ('option','listItem') and n.get('label')=='English'),None))
            auto.call('act',id=english['id'],action='press');wait_for(lambda:node('preference-language').get('value')=='English',15)
            auto.click('nav-traffic');auto.select(recs[0]['id']);auto.click('response-detail-header-tab');auto.click('theme-toggle')
            open_row(recs[0]);assert menu_item('request-copy-url')['label']=='Copy URL' and menu_item('request-pin')['label']=='Unpin'
            auto.capture(root/'header-menu-dark-en.png')
            stop();launch();wait_for(lambda:has('pin-'+host))
            fresh=send('/after-restart');count(1);auto.select(fresh['id']);wait_for(lambda:has('request-detail-headers'))
            assert has('response-detail-headers');open_row(fresh);choose('request-pin');wait_for(lambda:not has('pin-'+host))
            check('English/dark rendering and menu labels; pins persist through restart and can be removed from a new request')
            stop();report['passed']=True;return
        auto.click('request-detail-raw-tab');auto.click('response-detail-raw-tab')
        wait_for(lambda:'body-only-marker' in auto.detail('request-detail'))
        req=auto.detail('request-detail');res=auto.detail('response-detail')
        assert 'POST /v1/responses HTTP/1.1\n' in req and f'Host: {host}\n' in req and 'X-Shell: header-marker\n' in req
        assert req.endswith(body.decode()),repr(req)
        assert '200 OK\n' in res and 'X-Fixture: native-shell\n' in res and res.endswith('{  "message": "shell-fixture",\n "ok": true }\n'),repr(res)
        assert len([n for n in auto.tree() if n.get('role')=='tab'])==4
        check('real row hit test; request/response Raw headers + exact body whitespace; Header/Raw tabs')
        for key, expected in [('request-detail-copy',req),('response-detail-copy',res)]:
            auto.click(key)
            assert subprocess.check_output(['pbpaste']).decode()==expected
            if opts.copy_feedback_only:
                wait_for(lambda:any(n.get('key')=='feedback-message' and n.get('label')=='已复制' for n in auto.tree()))
                hint,button=node('feedback-message')['bounds'],node(key)['bounds']
                assert 0 < button['x']-hint['x']-hint['width'] < 32, (hint,button)
                assert abs(hint['y']+hint['height']/2-button['y']-button['height']/2)<1, (hint,button)
                assert auto.call('tree').get('focus')==node(key)['id'], 'toast stole copy button focus'
        if opts.copy_feedback_only:
            auto.capture(root/'copied-light.png')
            time.sleep(1.3);auto.click('response-detail-copy');time.sleep(1.3)
            assert len([n for n in auto.tree() if n.get('key')=='feedback-message'])==1, 'copy notices stacked or timeout did not reset'
            wait_for(lambda:not any(n.get('key')=='feedback-message' for n in auto.tree()),5)
            auto.capture(root/'copied-dismissed.png')
            auto.click('theme-toggle');auto.click('request-detail-copy');auto.capture(root/'copied-dark.png')
            auto.select(recs[1]['id'])
            assert not any(n.get('key')=='feedback-message' for n in auto.tree()), 'copy feedback leaked to another request'
            auto.click('account-menu');auto.click('nav-settings')
            auto.click('preference-language')
            english=wait_for(lambda:next((n for n in auto.tree() if n.get('role') in ('option','listItem') and n.get('label')=='English'),None))
            auto.call('act',id=english['id'],action='press')
            wait_for(lambda:node('preference-language').get('value')=='English' and 'disabled' not in node('preference-language').get('states',[]),15)
            auto.click('nav-traffic');auto.click('response-detail-copy')
            wait_for(lambda:any(n.get('key')=='feedback-message' and n.get('label')=='Copied' for n in auto.tree()))
            assert subprocess.check_output(['pbpaste']).decode()==res
            hint,button=node('feedback-message')['bounds'],node('response-detail-copy')['bounds']
            assert 0 < button['x']-hint['x']-hint['width'] < 32, (hint,button)
            assert abs(hint['y']+hint['height']/2-button['y']-button['height']/2)<1, (hint,button)
            auto.capture(root/'copied-english.png')
            check('request and response copy show localized feedback next to the clicked button; exact clipboard bytes; repeat refresh, auto-dismiss and focus preserved; switching requests clears stale feedback; light/dark screenshots')
            stop();report['passed']=True
            return
        if opts.raw_only:
            auto.capture(root/'raw-light.png')
            auto.click('request-detail')
            auto.call('input',events=[{'type':'key','key':'Cmd+A'},{'type':'key','key':'Cmd+C'}]);auto.call('idle',timeout=3000)
            assert subprocess.check_output(['pbpaste']).decode()==req
            auto.capture(root/'raw-manual-selection.png')
            auto.call('input',events=[{'type':'key','key':'Right'}]);auto.call('idle',timeout=3000)
            auto.capture(root/'raw-focused.png')
            auto.click('theme-toggle');auto.capture(root/'raw-dark.png')
            check('Raw preserves explicit text selection and copy; light/dark and focused gutter screenshots retained')
            stop();report['passed']=True
            return
        auto.select(recs[1]['id']);wait_for(lambda:'trace=2' in auto.detail('request-detail'))
        auto.click('history-back');wait_for(lambda:'body-only-marker' in auto.detail('request-detail'))
        auto.click('history-forward');wait_for(lambda:'trace=2' in auto.detail('request-detail'))
        auto.select(recs[0]['id']);wait_for(lambda:'body-only-marker' in auto.detail('request-detail'))
        auto.call('input',events=[{'type':'key','key':'Cmd+K'}]);auto.call('idle',timeout=3000)
        assert auto.call('tree').get('focus')==node('traffic-search')['id']
        check('both Raw copy buttons write actual document bytes; back/forward restore selected requests; Cmd+K focuses URL input')
        sizes={'titlebar':48,'nav-rail':53,'sidebar':216,'traffic-toolbar':44,'traffic-header':27,'traffic-footer':27,'raw-summary':42,'request-detail-heading':34,'response-detail-heading':34}
        for key,size in sizes.items():
            b=node(key)['bounds'];actual=b['width' if key in ('nav-rail','sidebar') else 'height'];assert abs(actual-size)<.1,(key,b)
        for r in rows(): assert abs(r['bounds']['height']-30)<.1,r
        assert abs(node('sidebar')['bounds']['x']+216-node('traffic-toolbar')['bounds']['x'])<.1
        auto.capture(root/'light-raw.png');check('native geometry matches 48/53/216/44/27/30/42/34pt and sidebar boundary')
        assert len([n for n in auto.tree() if n.get('key')=='domain-'+host])==1
        auto.click('domain-'+host+'-toggle');auto.click('path-domain-'+host+'/v1-toggle')
        auto.click('path-domain-'+host+'/v1/responses');count(2)
        assert all('/v1/responses' in n['label'] and 'responses-extra' not in n['label'] for n in rows())
        auto.click('domain-'+host);count(5)
        auto.click('all-traffic');count(6);check('single domain grouping; query aggregation; exact path boundary and domain/all selection')
        auto.click('path-domain-'+host+'/v1/responses');count(2)
        auto.click('path-domain-'+host+'/v1-toggle');count(4)
        auto.click('all-traffic-toggle');count(6)
        auto.click('all-traffic-toggle')
        auto.click('domain-search')
        side_id=node('domain-search')['id']
        for char in 'local':
            auto.call('input',events=[{'type':'text','text':char}]);auto.call('idle',timeout=3000)
            assert node('domain-search')['id']==side_id and auto.call('tree').get('focus')==side_id
        assert not any(n.get('key')=='domain-127.0.0.1:'+str(fixture_port) for n in auto.tree())
        auto.act('domain-search','setValue',text='')
        check('collapsing selected ancestors restores visible filter; sidebar typing stays focused')
        auto.click('traffic-search')
        input_id=node('traffic-search')['id']
        for char in 'RESPONSES':
            auto.call('input',events=[{'type':'text','text':char}]);auto.call('idle',timeout=3000)
            assert node('traffic-search')['id']==input_id and auto.call('tree').get('focus')==input_id,node('traffic-search')
        count(3)
        send('/live-focus')
        wait_for(lambda:len(items())==7)
        assert node('traffic-search')['id']==input_id and auto.call('tree').get('focus')==input_id
        auto.call('input',events=[{'type':'key','key':'Left'},{'type':'key','key':'Backspace'},{'type':'text','text':'E'}]);auto.call('idle',timeout=3000)
        assert node('traffic-search')['value']=='RESPONSE S'.replace(' ',''),node('traffic-search')
        for query in ('200','body-only-marker','header-marker','shell-fixture'):
            auto.act('traffic-search','setValue',text=query);count(0)
        auto.act('traffic-search','setValue',text='HTTP://LOCALHOST');count(6)
        auto.click('domain-'+host);count(6);assert node('traffic-search').get('value','')==''
        auto.click('history-back');assert node('traffic-search').get('value')=='HTTP://LOCALHOST'
        auto.click('history-forward');assert node('traffic-search').get('value','')==''
        check('continuous native typing + live traffic retain input identity/focus/caret; URL-only insensitive match; navigation restores search')
        click_node(node('domain-'+host),'right')
        menu=wait_for(lambda:next((n for n in auto.tree() if n.get('role')=='menuItem' and '固定' in n.get('label','')),None))
        auto.call('act',id=menu['id'],action='press');wait_for(lambda:any(n.get('key')=='pin-'+host for n in auto.tree()))
        wait_for(lambda:'pinnedDomains' in (home/'aicat.yaml').read_text());check('right-click Pin creates persistent shortcut without inline pin buttons')
        auto.click('pin-'+host)
        click_node(node('pin-'+host),'right')
        unpin=wait_for(lambda:next((n for n in auto.tree() if n.get('role')=='menuItem' and n.get('label')=='取消固定'),None))
        auto.call('act',id=unpin['id'],action='press')
        wait_for(lambda:not any(n.get('key')=='pin-'+host for n in auto.tree()))
        count(6)
        click_node(node('domain-'+host),'right')
        pin=wait_for(lambda:next((n for n in auto.tree() if n.get('role')=='menuItem' and '固定' in n.get('label','')),None))
        auto.call('act',id=pin['id'],action='press')
        wait_for(lambda:any(n.get('key')=='pin-'+host for n in auto.tree()))
        check('right-click Unpin removes only shortcut and retains the domain filter')
        auto.click('pin-'+host);auto.click('pinned-group-toggle');count(7)
        auto.click('pinned-group-toggle')
        auto.click('domain-'+host);auto.click('export')
        exported=wait_for(lambda:json.loads((root/'aicat-traffic.json').read_text()))
        assert len(exported)==6 and all(r['url'].startswith('http://'+host) for r in exported)
        check('collapsed pinned filter resets to all; export serializes only visible records through isolated save-panel adapter')
        auto.click('all-traffic');auto.select(recs[0]['id']);auto.capture(root/'light-tree.png')
        auto.click('toggle-sidebar');assert not any(n.get('key')=='sidebar' for n in auto.tree());assert node('titlebar')['bounds']['height']==48
        auto.click('toggle-sidebar');assert node('sidebar')['bounds']['width']==216
        check('sidebar toggle uses real layout while titlebar remains one line')
        auto.click('record-toggle');wait_for(lambda:node('record-toggle').get('label')=='继续录制')
        before=len(items());send_path='/paused-unrecorded'
        c=http.client.HTTPConnection('127.0.0.1',port);c.request('GET',f'http://localhost:{fixture_port}{send_path}');r=c.getresponse();r.read();assert r.status==200;c.close()
        assert len(items())==before;auto.click('record-toggle');check('single-click pause/resume; forwarding continues without capture')
        auto.click('theme-toggle');auto.capture(root/'dark-raw.png')
        held=socket.create_connection(('127.0.0.1',port),timeout=4)
        held.sendall(f'CONNECT localhost:{fixture_port} HTTP/1.1\r\nHost: localhost:{fixture_port}\r\n\r\n'.encode())
        assert b'200 Connection Established' in held.recv(4096)
        tunnel=wait_for(lambda:next((r for r in items() if r.get('tunnel')),None))
        auto.select(tunnel['id']);wait_for(lambda:any(n.get('key')=='enable-domain-decryption' for n in auto.tree()))
        assert 'CONNECT '+host in auto.detail('request-detail') and not auto.detail('response-detail')
        auto.capture(root/'connect-raw.png')
        auto.click('enable-domain-decryption');wait_for(lambda:any(n.get('key')=='enable-with-client-trust' for n in auto.tree()))
        auto.click('enable-with-client-trust');wait_for(lambda:any(n.get('label')=='已启用解密，等待客户端新建连接' for n in auto.tree()))
        assert not json.loads(os_state.read_text())['trusted']
        held.close()
        check('CONNECT retains request Raw and encrypted response panel; client-specific trust changes only future connections')
        auto.click('account-menu');auto.click('nav-settings');auto.click('settings-general')
        auto.capture(root/'general.png');check('General settings accessible from account menu; dark theme changes immediately')
        (root/'traffic-before-clear.json').write_text(json.dumps(items(),ensure_ascii=False,indent=2))
        (root/'native-tree.json').write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
        auto.click('nav-traffic');auto.click('clear-traffic');wait_for(lambda:len(items())==0);count(0)
        auto.click('export');assert json.loads((root/'aicat-traffic.json').read_text())==[]
        assert any(n.get('key')=='pin-'+host for n in auto.tree());auto.capture(root/'empty-pinned.png')
        stop();launch();assert any(n.get('key')=='pin-'+host for n in auto.tree())
        assert '# preserve-shell-fixture' in (home/'aicat.yaml').read_text()
        assert 'theme: dark' in (home/'aicat.yaml').read_text()
        check('clear retains pin; restart reloads pin and theme while preserving config comments')
        report['passed']=True
    except Exception as err:
        report['error']=repr(err)
        if auto:
            try: auto.capture(root/'failure.png');(root/'failure-tree.json').write_text(json.dumps(auto.tree(),ensure_ascii=False,indent=2))
            except Exception: pass
        raise
    finally:
        if proc and proc.poll() is None:
            proc.send_signal(signal.SIGTERM)
            try: proc.wait(timeout=8)
            except subprocess.TimeoutExpired: proc.kill();proc.wait()
        server.shutdown();server.server_close();log.close()
        (root/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
        print(json.dumps({k:v for k,v in report.items() if k!='sources'},ensure_ascii=False,indent=2))

if __name__=='__main__': main()
