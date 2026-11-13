import argparse
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
    parser.add_argument('--key-file', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    root = Path(args.output).resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    home = root / 'home'
    home.mkdir(mode=0o700)
    binary = Path(args.binary).resolve()
    runtime = binary.parent / 'channel-host'
    report = {'passed': False, 'checks': [], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'coverage': 'Read-only real local DB import to isolated Rooms and restart; no real message sent; no native UI; no process-memory key extraction.'}
    process, run = None, 0
    local = {'accountId': args.account, 'dataDir': str(Path(args.data_dir).resolve()), 'keyFile': str(Path(args.key_file).resolve())}
    source_files = [Path(local['dataDir']) / 'message.db', Path(local['dataDir']) / 'message.db-wal', Path(local['keyFile'])]
    key_before = hashlib.sha256(source_files[2].read_bytes()).hexdigest()
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
        for file in files:
            for line in file.read_text().splitlines():
                message = json.loads(line)
                total += 1
                raw += bool(message.get('raw_content'))
            events += len((file.parent.parent / 'messages.jsonl').read_text().splitlines())
        return {'rooms': len(files), 'messages': total, 'events': events, 'rawBodies': raw}

    try:
        probe = "import {Source} from " + json.dumps(str(runtime / 'wecom-local/source.ts')) + "; const s = new Source(await Bun.stdin.json()); try {s.refresh(); const db=s['db']; console.log(JSON.stringify({count:String(db.query('SELECT COUNT(*) AS n FROM message_table').get().n),max:String(db.query('SELECT COALESCE(MAX(message_id),0) AS n FROM message_table').get().n)}));} finally {s.close();}"
        baseline = json.loads(subprocess.check_output([str(runtime / 'bun'), '-e', probe], input=json.dumps(local).encode(), stderr=subprocess.DEVNULL, timeout=45))
        check('real encrypted DB and WAL decrypt into a verified in-memory SQLite snapshot', int(baseline['count']) > 0)
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
        check('restart preserves local cursor without duplicate imports', after['messages'] - imported['messages'] == int(cursor()) - int(before) and after['events'] == after['messages'])
        check('existing key file remains unchanged', hashlib.sha256(source_files[2].read_bytes()).hexdigest() == key_before)
        check('no BOT credentials, outgoing receipts, Agent tasks, or plaintext databases created', not (home / 'credentials').exists() and not (home / 'messages/receipts').exists() and not list(home.glob('rooms/*/tasks/*')) and not list(home.rglob('*.db')))
        report['counts'] = after
        report['newMessagesDuringRestart'] = after['messages'] - imported['messages']
        report['passed'] = True
    except Exception as error:
        report['error'] = repr(error)
        raise
    finally:
        stop()
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
