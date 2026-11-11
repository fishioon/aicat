#!/usr/bin/env python3
"""Only the certificate installation flow; isolated Home and fake OS authorization.
Failure modes: creation fails, user cancels, installer fails, retry replaces the
identity accidentally, or deletion requires a separate Create action again.
"""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import signal
import ssl
import subprocess
import tempfile
from prototype import Automation, wait_for


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-e2e-race')
    parser.add_argument('--port', type=int, default=19992)
    parser.add_argument('--output')
    args = parser.parse_args()
    root = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='certificate-install-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    home = root / '.aicat'; ca = home / 'ca'; ca.mkdir(parents=True, mode=0o700)
    (ca / 'identity-deleted').write_text('deleted\n')
    (home / 'aicat.yaml').write_text(f'gateway:\n  addr: 127.0.0.1:{args.port}\nui:\n  language: zh-Hans\n  theme: light\n')
    state_path = root / 'os-state.json'
    state_path.write_text(json.dumps({'services': [], 'trusted': False})); state_path.chmod(0o600)
    sock = Path(tempfile.gettempdir()) / f'aicat-install-{os.getpid()}.sock'
    env = dict(os.environ, MYGO_AUTOMATION=str(sock), AICAT_E2E_OS_STATE=str(state_path))
    log = (root / 'app.log').open('w')
    binary = str(Path(args.binary).resolve())
    process = subprocess.Popen([binary, '--home', str(home)], env=env, stdout=log, stderr=log)
    auto = None
    report = {'passed': False, 'scope': 'certificate installation only', 'checks': [], 'binary': binary, 'port': args.port}

    def fixture(**updates):
        import fcntl
        with open(str(state_path)+'.lock', 'a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            state = json.loads(state_path.read_text())
            if updates:
                state.update(updates)
                tmp = state_path.with_suffix('.next'); tmp.write_text(json.dumps(state)); tmp.chmod(0o600); tmp.replace(state_path)
            return state

    def ready():
        if process.poll() is not None: raise RuntimeError('app exited')
        c = http.client.HTTPConnection('127.0.0.1', args.port, timeout=1)
        try:
            c.request('GET', '/api/traffic'); response = c.getresponse(); response.read()
            return sock.exists() and response.status == 200
        finally: c.close()

    def status(value):
        return any(n.get('key') == 'certificate-status' and n.get('label') == value for n in auto.tree())

    def fingerprint():
        subject = subprocess.check_output(['openssl', 'x509', '-in', str(ca / 'mitmproxy-ca-cert.pem'), '-noout', '-subject', '-nameopt', 'RFC2253'], text=True)
        assert subject.rsplit('CN=', 1)[1].split(',', 1)[0].strip() == 'aicat', subject
        return hashlib.sha256(ssl.PEM_cert_to_DER_cert((ca / 'mitmproxy-ca-cert.pem').read_text())).hexdigest()

    def menu(key):
        auto.click('certificate-manage')
        node = wait_for(lambda: next((n for n in auto.tree() if n.get('role') == 'menuItem' and n.get('key') == '{'+key+'}'), None))
        auto.call('act', id=node['id'], action='press'); auto.call('idle', timeout=3000)

    try:
        wait_for(ready)
        auto = Automation(sock)
        wait_for(lambda: auto.call('windows').get('windows'))
        auto.call('idle', timeout=3000)
        auto.act('account-menu'); auto.act('nav-settings'); auto.act('settings-proxy')
        wait_for(lambda: status('未安装'))
        tree = auto.tree()
        assert any(n.get('key') == 'certificate-install' and n.get('label') == '安装证书' for n in tree)
        assert any(n.get('key') == 'proxy-address' for n in tree)
        assert any(n.get('key') == 'add-decryption-domain' for n in tree)
        assert not any(n.get('key') == 'settings-https' for n in tree)
        assert not any(n.get('key') == 'certificate-create' or n.get('label') == '创建证书' for n in tree)
        auto.capture(root / 'native-not-installed.png')
        report['checks'].append('proxy settings contain certificate/domain controls with one Install button and no separate HTTPS page or Create step')

        (ca / 'mitmproxy-ca.pem').mkdir()
        auto.click('certificate-install')
        wait_for(lambda: any('普通文件' in n.get('label', '') for n in auto.tree()))
        assert status('未安装') and not fixture().get('installedCertificate')
        (ca / 'mitmproxy-ca.pem').rmdir()
        report['checks'].append('creation failure keeps the gateway running and does not invoke OS installation')

        fixture(cancelInstall=True)
        auto.click('certificate-install')
        wait_for(lambda: status('未信任') and any('已取消证书授权' in n.get('label', '') for n in auto.tree()))
        first = fingerprint()
        assert (ca / 'mitmproxy-ca.pem').stat().st_mode & 0o777 == 0o600
        assert not fixture().get('trusted') and not (ca / 'identity-deleted').exists()
        auto.capture(root / 'native-canceled.png')
        report['checks'].append('one click generates a root named exactly aicat and reaches OS authorization; cancellation never reports trust')

        fixture(cancelInstall=False, failInstall=True)
        auto.click('certificate-install')
        wait_for(lambda: any('test certificate install failed' in n.get('label', '') for n in auto.tree()))
        assert fingerprint() == first
        fixture(failInstall=False)
        auto.click('certificate-install'); wait_for(lambda: status('已信任'))
        assert fingerprint() == first and fixture()['installedCertificate'] == first
        auto.capture(root / 'native-installed.png')
        report['checks'].append('failed installation and retry reuse the same root; verified trust completes the single action')

        menu('certificate-delete'); auto.act('certificate-delete-confirm')
        wait_for(lambda: status('未安装'))
        assert not (ca / 'mitmproxy-ca.pem').exists()
        auto.click('certificate-install'); wait_for(lambda: status('已信任'))
        assert fingerprint() != first and fixture()['installedCertificate'] == fingerprint()
        auto.capture(root / 'native-reinstalled.png')
        report['checks'].append('after deletion, one Install click creates a new identity and installs/trusts it without an intermediate Create action')
        report['passed'] = True
    except BaseException as error:
        report['error'] = repr(error)
        if auto:
            try:
                auto.capture(root / 'native-failure.png')
                (root / 'failure-tree.json').write_text(json.dumps(auto.tree(), ensure_ascii=False, indent=2))
            except Exception: pass
        raise
    finally:
        if process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try: process.wait(timeout=8)
            except subprocess.TimeoutExpired: process.kill(); process.wait()
        if auto: auto.file.close(); auto.sock.close()
        log.close()
        report['raceWarning'] = 'WARNING: DATA RACE' in (root / 'app.log').read_text()
        report['passed'] = report['passed'] and not report['raceWarning']
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
        print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
