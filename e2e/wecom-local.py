import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import struct
import subprocess
import time

KEY = bytes.fromhex('00112233445566778899aabbccddeeff')


def proto(text):
    body = text.encode()
    n, size = len(body), bytearray()
    while n >= 128:
        size.append((n & 127) | 128)
        n >>= 7
    size.append(n)
    return b'\x0a' + size + body


def encrypt_page(data, number):
    key = hashlib.md5(KEY + struct.pack('<I', number) + b'sAlT').hexdigest()
    z, words = number + 1, []
    for _ in range(4):
        q = z // 52774
        z = 40692 * (z - 52774 * q) - 3791 * q
        if z < 0:
            z += 2147483399
        words.append(z)
    iv = hashlib.md5(struct.pack('<4I', *words)).hexdigest()

    def aes(value):
        return subprocess.run(['openssl', 'enc', '-aes-128-cbc', '-K', key, '-iv', iv, '-nopad'], input=value, capture_output=True, check=True).stdout
    if number != 1:
        return aes(data)
    result = bytearray(aes(data[:16]) + aes(data[16:]))
    result[8:16], result[16:24] = result[16:24], data[16:24]
    return bytes(result)


def checksum(data, big, a=0, b=0):
    values = struct.unpack(('>' if big else '<') + 'I' * (len(data) // 4), data)
    for x, y in zip(values[::2], values[1::2]):
        a = (a + x + b) & 0xffffffff
        b = (b + y + a) & 0xffffffff
    return a, b


def publish(db, target):
    plain = db.read_bytes()
    size = int.from_bytes(plain[16:18], 'big')
    encrypted = b''.join(encrypt_page(plain[i:i+size], i//size+1) for i in range(0, len(plain), size))
    wal = bytearray(Path(str(db) + '-wal').read_bytes())
    if wal:
        big = int.from_bytes(wal[:4], 'big') == 0x377f0683
        a, b = checksum(wal[:24], big)
        wal[24:32] = struct.pack('>2I', a, b)
        for start in range(32, len(wal), size + 24):
            number = int.from_bytes(wal[start:start+4], 'big')
            wal[start+24:start+24+size] = encrypt_page(wal[start+24:start+24+size], number)
            a, b = checksum(wal[start:start+8], big, a, b)
            a, b = checksum(wal[start+24:start+24+size], big, a, b)
            wal[start+16:start+24] = struct.pack('>2I', a, b)
    for name, data in [('message.db', encrypted), ('message.db-wal', wal)]:
        temporary = target / (name + '.next')
        temporary.write_bytes(data)
        temporary.replace(target / name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-cli')
    parser.add_argument('--output')
    args = parser.parse_args()
    root = Path(args.output or ('build/wecom-local-e2e-' + time.strftime('%Y%m%d-%H%M%S'))).resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    binary = Path(args.binary).resolve()
    home, target = root / 'home', root / 'database'
    home.mkdir(mode=0o700)
    target.mkdir(mode=0o700)
    report = {'passed': False, 'checks': [], 'command': ['python3', __file__, '--binary', str(binary), '--output', str(root)], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest()}
    process, run, db = None, 0, None
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    account = 'synthetic-account'
    source = 'aicat-channel://wecom-local/' + hashlib.sha256((account + '\0' + str(target)).encode()).hexdigest()
    room_dir = lambda room: home / 'rooms' / hashlib.sha256(('wecom-local\0' + source + '\0' + room).encode()).hexdigest()
    key_file = root / 'keys.json'
    keys = {'version': 1, 'entries': [{'path': str(target / 'message.db'), 'account': account, 'name': 'message.db', 'key': KEY.hex()}]}

    def save_keys(value=keys):
        temporary = root / 'keys.next'
        temporary.write_text(json.dumps(value))
        temporary.chmod(0o600)
        temporary.replace(key_file)

    def config(enabled=True, data_dir=target):
        (home / 'aicat.yaml').write_text('gateway:\n  addr: 127.0.0.1:' + str(port) + '\nsystemProxy:\n  enabled: false\nextensions:\n  traffic:\n    enabled: false\n  wecom-local:\n    enabled: ' + str(enabled).lower() + '\n    local:\n      accountId: ' + account + '\n      dataDir: ' + json.dumps(str(data_dir)) + '\n      keyFile: ' + json.dumps(str(key_file)) + '\n')

    def lines(file):
        if not file.exists() or not file.is_file():
            return []
        return [json.loads(line) for line in file.read_text().splitlines()]

    def messages(room):
        return lines(room_dir(room) / 'context/messages.log')

    def events(room):
        return lines(room_dir(room) / 'messages.jsonl')

    def cursor():
        files = list((home / 'state/wecom-local').glob('cursor-*.json'))
        return json.loads(files[0].read_text()) if files else {}

    def wait(predicate, description, seconds=25):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if process is not None and process.poll() is not None:
                raise AssertionError('aicat exited: ' + str(process.returncode))
            if predicate():
                return
            time.sleep(.1)
        raise AssertionError('timeout: ' + description)

    def check(name, value):
        assert value, name
        report['checks'].append(name)
        print('PASS', name, flush=True)

    def start():
        nonlocal process, run
        run += 1
        with (root / ('serve-' + str(run) + '.log')).open('w') as log:
            process = subprocess.Popen([str(binary), 'serve', '--home', str(home)], stdout=log, stderr=subprocess.STDOUT, start_new_session=True, env={**os.environ, 'AICAT_E2E_BACKGROUND': '1'})

    def stop(crash=False):
        nonlocal process
        if process:
            if process.poll() is not None:
                code = process.returncode
                process = None
                raise AssertionError(('process exited before shutdown', code))
            os.killpg(process.pid, signal.SIGKILL if crash else signal.SIGTERM)
            code = process.wait(timeout=15)
            process = None
            if not crash:
                assert code == 0, ('shutdown', code)

    def insert(id, room, text, typ=2, flag=0, timestamp=1):
        db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)', (id, 9007199254740999, id, 9007199254740997, room, typ, timestamp, flag, proto(text)))
        db.commit()

    try:
        db_path = root / 'plain-fixture.db'
        db = sqlite3.connect(db_path)
        db.executescript('PRAGMA page_size=4096; PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
        insert(1, 'group-fixture', '群内不带提及的历史消息')
        db.execute('PRAGMA wal_checkpoint(TRUNCATE)')
        insert(2, 'private-fixture', '私聊消息')
        insert(3, 'group-fixture', '自己发出的消息', flag=2)
        insert(4, 'group-fixture', '图片元数据', typ=29)
        publish(db_path, target)
        save_keys()
        config(False, root / 'must-not-be-read')
        start()
        time.sleep(1.2)
        check('disabled extension does not open local host or create Rooms', process.poll() is None and not (home / 'state/wecom-local').exists() and not (home / 'rooms').exists())
        stop()
        config()
        broken_context = room_dir('group-fixture') / 'context/messages.log'
        broken_context.mkdir(parents=True)
        start()
        wait(lambda: (home / 'state/wecom-local/host.lock').exists(), 'local reader starts')
        time.sleep(1)
        check('Room failure withholds acknowledgement and cursor', not cursor() and not messages('group-fixture'))
        stop(crash=True)
        broken_context.rmdir()
        start()
        wait(lambda: cursor().get('position') == '4', 'recovery completes initial import')
        check('crash recovery resumes failed Room without duplicate events', len(events('group-fixture')) == 3 and len(messages('group-fixture')) == 3)
        check('WAL-only rows reach separate Room with exact 64-bit IDs', len(messages('private-fixture')) == 1 and messages('private-fixture')[0]['from'] == '9007199254740997' and messages('private-fixture')[0]['server_id'] == '9007199254740999')
        check('protobuf text and raw body are preserved', messages('group-fixture')[0]['text']['content'] == '群内不带提及的历史消息' and bool(messages('group-fixture')[0]['raw_content']))
        check('outgoing and media metadata survive Room storage', messages('group-fixture')[1]['outgoing'] and messages('group-fixture')[2]['msgtype'] == 'image' and messages('group-fixture')[2]['media_status'] == 'metadata_only')
        before = cursor()
        insert(5, 'group-fixture', '时间更早但本地 ID 更新的消息')
        publish(db_path, target)
        wait(lambda: cursor().get('position') == '5', 'incremental message')
        check('incremental cursor uses local ID instead of message timestamp', len(messages('group-fixture')) == 4)
        stop()
        (room_dir('group-fixture') / 'context/messages.log').unlink()
        cursor_file = next((home / 'state/wecom-local').glob('cursor-*.json'))
        reset = json.loads(cursor_file.read_text())
        reset.update(position='0', anchor='')
        cursor_file.write_text(json.dumps(reset))
        start()
        wait(lambda: cursor().get('position') == '5', 'replayed acknowledgements')
        check('replayed acknowledgements repair missing context without duplicate events', len(messages('group-fixture')) == 4 and len(events('group-fixture')) == 4)
        stop()
        start()
        time.sleep(2)
        check('restart resumes cursor without replaying history', cursor().get('position') == '5' and len(events('group-fixture')) == 4)
        insert(6, 'private-fixture', '轮换密钥后继续读取')
        publish(db_path, target)
        wrong = json.loads(json.dumps(keys))
        wrong['entries'][0]['key'] = 'ff' * 16
        save_keys(wrong)
        time.sleep(2)
        check('invalid key does not advance cursor', cursor().get('position') == '5')
        save_keys()
        wait(lambda: cursor().get('position') == '6', 'key recovery')
        check('restoring supplied key resumes without dropping messages', len(messages('private-fixture')) == 2)
        insert(7, 'private-fixture', 'WAL 校验恢复')
        publish(db_path, target)
        wal_file = target / 'message.db-wal'
        valid_wal = wal_file.read_bytes()
        wal_file.write_bytes(b'BAD!' + valid_wal[4:])
        time.sleep(2)
        check('invalid WAL header does not advance cursor', cursor().get('position') == '6')
        wal_file.write_bytes(valid_wal + b'torn-tail')
        wait(lambda: cursor().get('position') == '7', 'WAL recovery')
        check('valid committed WAL prefix survives torn tail', len(messages('private-fixture')) == 3)
        duplicate = json.loads(json.dumps(keys))
        duplicate['entries'].append(dict(duplicate['entries'][0]))
        save_keys(duplicate)
        insert(8, 'private-fixture', '重复密钥条目修复')
        publish(db_path, target)
        time.sleep(2)
        check('duplicate key entries do not advance cursor', cursor().get('position') == '7')
        save_keys()
        wait(lambda: cursor().get('position') == '8', 'duplicate key recovery')
        db.execute('UPDATE message_table SET content=? WHERE message_id=8', (proto('数据库被替换'),))
        db.commit()
        insert(9, 'private-fixture', '不得在身份变化后继续')
        publish(db_path, target)
        time.sleep(2)
        check('changed cursor anchor rejects replacement database', cursor().get('position') == '8' and len(messages('private-fixture')) == 4)
        db.execute('DELETE FROM message_table WHERE message_id>2')
        db.commit()
        publish(db_path, target)
        time.sleep(2)
        check('local ID rollback rejects reset database without advancing cursor', cursor().get('position') == '8')
        check('BOT installation, credentials, sends and Agent tasks remain untouched', not (home / 'extensions/wecom').exists() and not (home / 'credentials').exists() and not list((home / 'rooms').glob('*/tasks/*')) and not (home / 'messages/receipts').exists())
        check('plaintext database stays outside app Home and Room logs are private', not list(home.rglob('*.db')) and all((p.stat().st_mode & 0o777) == 0o600 for p in home.glob('rooms/*/context/messages.log')))
        stop()
        report['fixtureMessages'] = db.execute('SELECT COUNT(*) FROM message_table').fetchone()[0]
        report['passed'] = True
    except Exception as error:
        report['error'] = repr(error)
        raise
    finally:
        if process:
            stop(crash=True)
        if db:
            db.close()
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(root)


if __name__ == '__main__':
    main()
