import argparse
import sys
import base64
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--account', required=True)
    parser.add_argument('--data-dir', required=True)
    keys = parser.add_mutually_exclusive_group(required=True)
    keys.add_argument('--key-file')
    keys.add_argument('--key-stdin', action='store_true')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    root = Path(args.output).resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    home = root / 'home'
    home.mkdir(mode=0o700)
    binary = Path(args.binary).resolve()
    runtime = binary.parent / 'channel-host'
    report = {'passed': False, 'checks': [], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'coverage': 'Stock SQLite3MC; read-only real local DB import to isolated Rooms and restart; no real message sent; no native UI; no process-memory key extraction.'}
    process, run = None, 0
    local = {'accountId': args.account, 'dataDir': str(Path(args.data_dir).resolve())}
    key_source = Path(args.key_file).resolve() if args.key_file else None
    key_before = hashlib.sha256(key_source.read_bytes()).digest() if key_source else None
    if args.key_stdin:
        key = sys.stdin.readline().strip()
    else:
        raw = key_source.read_text().strip()
        key = json.loads(raw)['key'] if raw.startswith('{') else raw
    if len(base64.b64decode(key, validate=True)) != 16:
        raise ValueError('expected Base64 encoding of the original 16-byte input')
    credentials = home / 'credentials'
    credentials.mkdir(mode=0o700)
    credential_file = credentials / 'wecom-local.json'
    credential_file.write_text(json.dumps({**local, 'key': key}))
    credential_file.chmod(0o600)
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    (home / 'aicat.yaml').write_text('gateway:\n  addr: 127.0.0.1:' + str(port) + '\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  wecom-local:\n    enabled: true\n    local:\n' + ''.join('      ' + key + ': ' + json.dumps(value) + '\n' for key, value in local.items()))
    os.chmod(home / 'aicat.yaml', 0o600)

    def check(name, condition):
        assert condition, name
        report['checks'].append(name)

    def start():
        nonlocal process, run
        run += 1
        with (root / ('serve-' + str(run) + '.log')).open('w') as log:
            process = subprocess.Popen([str(binary), 'serve', '--home', str(home)], stdout=log, stderr=subprocess.STDOUT, env={**os.environ, 'AICAT_E2E_BACKGROUND': '1'})

    def stop():
        nonlocal process
        if process:
            process.send_signal(signal.SIGTERM)
            code = process.wait(timeout=20)
            process = None
            assert code == 0, ('shutdown', code)

    def cursor():
        files = list((home / 'state/wecom-local').glob('cursor-*.json'))
        return json.loads(files[0].read_text()).get('position') if files else None

    def counts():
        files = list((home / 'rooms').glob('*/context/messages.log'))
        total = events = raw = 0
        ids = set()
        for file in files:
            for line in file.read_text().splitlines():
                message = json.loads(line)
                assert message['local_id'] not in ids, 'duplicate local message ID'
                ids.add(message['local_id'])
                total += 1
                raw += bool(message.get('raw_content'))
            events += len((file.parent.parent / 'messages.jsonl').read_text().splitlines())
        return {'rooms': len(files), 'messages': total, 'events': events, 'rawBodies': raw}

    try:
        probe = "import {Source} from " + json.dumps(str(runtime / 'wecom-local/source.ts')) + "; const s = new Source(await Bun.stdin.json()); try {s.refresh(); const db=s['db']; db.exec('BEGIN'); const row=db.query('SELECT COUNT(*) AS count,COALESCE(MAX(message_id),0) AS max FROM message_table').get(); console.log(JSON.stringify({count:String(row.count),max:String(row.max)})); db.exec('ROLLBACK');} finally {s.close();}"
        result = subprocess.run([str(runtime / 'bun'), '-e', probe], input=json.dumps({**local, 'key': key}).encode(), stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=45)
        if result.returncode:
            raise AssertionError('stock SQLite3MC real database validation failed')
        baseline = json.loads(result.stdout)
        check('stock SQLite3MC opens the real DB and WAL using the original Base64 input', int(baseline['count']) > 0)
        start()
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise AssertionError('aicat exited during import')
            if cursor() is not None and int(cursor()) >= int(baseline['max']):
                break
            time.sleep(.25)
        else:
            raise AssertionError('timeout importing local messages')
        stop()
        imported = counts()
        check('real messages reach isolated Room event and context logs', imported['messages'] >= int(baseline['count']) and imported['messages'] == imported['events'])
        before = cursor()
        start()
        time.sleep(4)
        stop()
        after = counts()
        check('restart preserves local cursor without duplicate imports', int(cursor()) >= int(before) and after['messages'] >= imported['messages'] and after['events'] == after['messages'])
        check('source credential file remains unchanged', key_source is None or hashlib.sha256(key_source.read_bytes()).digest() == key_before)
        check('saved Base64 key is private and absent from YAML and application logs', (credential_file.stat().st_mode & 0o777) == 0o600 and all(key not in p.read_text() for p in [home/'aicat.yaml', *root.glob('serve-*.log')]))
        check('no BOT credentials, outgoing receipts, Agent tasks, or plaintext databases created', not (home / 'credentials/wecom.json').exists() and not (home / 'messages/receipts').exists() and not list(home.glob('rooms/*/tasks/*')) and not list(home.rglob('*.db')))
        report['counts'] = after
        report['newMessagesDuringRestart'] = after['messages'] - imported['messages']
        report['passed'] = True
    except Exception as error:
        report['error'] = repr(error)
        raise
    finally:
        stop()
        credential_file.unlink(missing_ok=True)
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
