import argparse
import ctypes as C
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time


class SQLite:
    def __init__(self, library, raw=False):
        self.lib = C.CDLL(str(library))
        self.cipher = b'aes128cbc'
        if raw:
            register = self.lib.aicat_register_wecom_cipher
            register.argtypes, register.restype = [], C.c_int
            if register() != 0:
                raise RuntimeError('raw cipher registration failed')
        signatures = {
            'sqlite3_open_v2': ([C.c_char_p, C.POINTER(C.c_void_p), C.c_int, C.c_char_p], C.c_int),
            'sqlite3_close': ([C.c_void_p], C.c_int),
            'sqlite3_exec': ([C.c_void_p, C.c_char_p, C.c_void_p, C.c_void_p, C.c_void_p], C.c_int),
            'sqlite3_errmsg': ([C.c_void_p], C.c_char_p),
            'sqlite3_prepare_v2': ([C.c_void_p, C.c_char_p, C.c_int, C.POINTER(C.c_void_p), C.c_void_p], C.c_int),
            'sqlite3_step': ([C.c_void_p], C.c_int),
            'sqlite3_column_text': ([C.c_void_p, C.c_int], C.c_char_p),
            'sqlite3_column_int64': ([C.c_void_p, C.c_int], C.c_int64),
            'sqlite3_finalize': ([C.c_void_p], C.c_int),
            'sqlite3_key': ([C.c_void_p, C.c_void_p, C.c_int], C.c_int),
            'sqlite3mc_config': ([C.c_void_p, C.c_char_p, C.c_int], C.c_int),
            'sqlite3mc_config_cipher': ([C.c_void_p, C.c_char_p, C.c_char_p, C.c_int], C.c_int),
            'sqlite3mc_cipher_index': ([C.c_char_p], C.c_int),
            'sqlite3mc_version': ([], C.c_char_p),
            'sqlite3_libversion': ([], C.c_char_p),
        }
        for name, (args, result) in signatures.items():
            fn = getattr(self.lib, name)
            fn.argtypes, fn.restype = args, result

    def checked(self, db, rc):
        if rc != 0:
            raise RuntimeError(str(rc) + ':' + self.lib.sqlite3_errmsg(db).decode())

    def execute(self, db, sql):
        self.checked(db, self.lib.sqlite3_exec(db, sql.encode(), None, None, None))

    def scalar(self, db, sql, integer=False):
        statement = C.c_void_p()
        try:
            self.checked(db, self.lib.sqlite3_prepare_v2(db, sql.encode(), -1, C.byref(statement), None))
            rc = self.lib.sqlite3_step(statement)
            if rc != 100:
                raise RuntimeError(str(rc) + ':' + self.lib.sqlite3_errmsg(db).decode())
            return self.lib.sqlite3_column_int64(statement, 0) if integer else self.lib.sqlite3_column_text(statement, 0).decode()
        finally:
            if statement:
                self.lib.sqlite3_finalize(statement)

    def open(self, file, key, readonly=True, legacy=0, legacy_wal=0):
        db = C.c_void_p()
        try:
            self.checked(db, self.lib.sqlite3_open_v2(os.fsencode(file), C.byref(db), 1 if readonly else 6, None))
            cipher = self.lib.sqlite3mc_cipher_index(self.cipher)
            if cipher < 1:
                raise RuntimeError('aes128cbc unavailable')
            if self.lib.sqlite3mc_config(db, b'cipher', cipher) != cipher:
                raise RuntimeError('cipher selection failed')
            for name, value in [(b'legacy', legacy), (b'legacy_page_size', 4096)]:
                if self.lib.sqlite3mc_config_cipher(db, b'aes128cbc', name, value) != value:
                    raise RuntimeError('cipher parameter rejected')
            wal_setting = self.lib.sqlite3mc_config(db, b'mc_legacy_wal', legacy_wal)
            if wal_setting != legacy_wal and not (wal_setting == -1 and legacy_wal == 0):
                raise RuntimeError('WAL configuration rejected')
            material = C.create_string_buffer(key)
            try:
                self.checked(db, self.lib.sqlite3_key(db, material, len(key)))
            finally:
                C.memset(C.addressof(material), 0, len(material))
            if readonly:
                self.execute(db, 'PRAGMA query_only=ON')
            return db
        except Exception:
            if db:
                self.lib.sqlite3_close(db)
            raise


def snapshot(files, target):
    target.mkdir(parents=True, exist_ok=False)
    for _ in range(8):
        def stamp():
            result = []
            for file in files:
                st = file.stat() if file.exists() else None
                result.append((st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns) if st else None)
            return result
        before = stamp()
        first = [file.read_bytes() if st else None for file, st in zip(files, before)]
        second = [file.read_bytes() if st else None for file, st in zip(files, before)]
        if before == stamp() and first == second:
            for file, data in zip(files, first):
                if data is not None:
                    (target / file.name).write_bytes(data)
                    os.chmod(target / file.name, 0o600)
            return [hashlib.sha256(data).hexdigest() if data is not None else None for data in first]
        time.sleep(.25)
    raise RuntimeError('source did not stabilize')


def test_read(api, file, key, **options):
    db = None
    try:
        db = api.open(file, key, **options)
        integrity = api.scalar(db, 'PRAGMA quick_check')
        if integrity != 'ok':
            raise RuntimeError('integrity check failed')
        count = api.scalar(db, 'SELECT COUNT(*) FROM message_table', True)
        highest = api.scalar(db, 'SELECT COALESCE(MAX(message_id),0) FROM message_table', True)
        write_rejected = False
        try:
            api.execute(db, 'CREATE TABLE must_not_write(value)')
        except RuntimeError as error:
            write_rejected = str(error).startswith('8:')
        return {'opened': True, 'integrity': integrity, 'count': count, 'highest': str(highest), 'writeRejected': write_rejected}
    except RuntimeError as error:
        return {'opened': False, 'error': str(error)}
    finally:
        if db:
            api.lib.sqlite3_close(db)


def controls(api, root):
    file = root / 'positive.db'
    key = b'public-synthetic-passphrase'
    db = api.open(file, key, readonly=False)
    try:
        api.execute(db, 'PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,content BLOB); INSERT INTO message_table VALUES(1,zeroblob(128)); PRAGMA wal_checkpoint(TRUNCATE); INSERT INTO message_table VALUES(2,zeroblob(128));')
        directory = root / 'positive-with-wal'
        snapshot([file, Path(str(file) + '-wal')], directory)
        only_db = root / 'positive-db-only'
        only_db.mkdir()
        shutil.copyfile(directory / 'positive.db', only_db / 'positive.db')
        first = test_read(api, only_db / 'positive.db', key)
        second = test_read(api, directory / 'positive.db', key)
        api.execute(db, 'PRAGMA cache_size=1; BEGIN; INSERT INTO message_table VALUES(3,zeroblob(65536));')
        uncommitted = root / 'positive-uncommitted'
        snapshot([file, Path(str(file) + '-wal')], uncommitted)
        third = test_read(api, uncommitted / 'positive.db', key)
        api.execute(db, 'ROLLBACK')
        return {'dbOnly': first, 'withCommittedWAL': second, 'withUncommittedTail': third, 'passed': first.get('count') == 1 and second.get('count') == 2 and third.get('count') == 2 and all(result.get('writeRejected') for result in [first, second, third])}
    finally:
        api.lib.sqlite3_close(db)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--library', required=True)
    parser.add_argument('--data-dir', required=True)
    parser.add_argument('--key-file', required=True)
    parser.add_argument('--account', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--raw-cipher', action='store_true')
    parser.add_argument('--bun')
    parser.add_argument('--reader')
    args = parser.parse_args()
    root = Path(args.output).resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    library, key_file = Path(args.library).resolve(), Path(args.key_file).resolve()
    report = {'completed': False, 'compatible': False, 'upstreamModified': False, 'rawKeyAdapter': args.raw_cipher, 'librarySHA256': hashlib.sha256(library.read_bytes()).hexdigest(), 'coverage': 'Stock SQLite3MC with existing raw key; isolated encrypted DB/WAL snapshots; no native UI, message sending or process-memory access; report contains no message bodies or keys.'}
    key_bytes = bytearray(key_file.read_bytes())
    original_key_hash = hashlib.sha256(key_bytes).digest()
    try:
        api = SQLite(library, raw=args.raw_cipher)
        report['sqlite3mcVersion'] = api.lib.sqlite3mc_version().decode()
        report['sqliteVersion'] = api.lib.sqlite3_libversion().decode()
        report['positiveControl'] = controls(api, root)
        if not report['positiveControl']['passed']:
            raise RuntimeError('positive controls failed')
        db_file = Path(args.data_dir).resolve() / 'message.db'
        entries = json.loads(key_bytes)['entries']
        matching = [entry for entry in entries if Path(entry['path']).resolve() == db_file and entry.get('account') == args.account and entry.get('name') == 'message.db' and not entry.get('missing')]
        if len(matching) != 1:
            raise RuntimeError('key entry missing or ambiguous')
        raw_key = bytes.fromhex(matching[0]['key'])
        if len(raw_key) != 16:
            raise RuntimeError('expected existing 16-byte key')
        source = root / 'encrypted-source'
        hashes = snapshot([db_file, Path(str(db_file) + '-wal')], source)
        report['encryptedSnapshotSHA256'] = hashes
        report['walBytes'] = (source / 'message.db-wal').stat().st_size if (source / 'message.db-wal').exists() else 0
        if args.bun and args.reader:
            code = 'import {Source} from ' + json.dumps(str(Path(args.reader).resolve())) + '; const s=new Source(await Bun.stdin.json());try{s.refresh();const d=s["db"];console.log(JSON.stringify({count:String(d.query("SELECT COUNT(*) AS n FROM message_table").get().n),highest:String(d.query("SELECT MAX(message_id) AS n FROM message_table").get().n)}));}finally{s.close();}'
            config = {'accountId': args.account, 'dataDir': str(db_file.parent), 'keyFile': str(key_file)}
            report['existingReaderBaseline'] = json.loads(subprocess.check_output([args.bun, '-e', code], input=json.dumps(config).encode(), stderr=subprocess.DEVNULL, timeout=45))
        if args.raw_cipher:
            api.cipher = b'aicat_wecom_aes128'
        modes = {'binary16': raw_key, 'hexASCII32': raw_key.hex().encode(), 'rawPrefixHex': b'raw:' + raw_key.hex().encode(), 'sqlcipherLiteral': b"x'" + raw_key.hex().encode() + b"'"}
        cases = []
        for mode, key in modes.items():
            for legacy in [0, 1]:
                for legacy_wal in [0, 1]:
                    case_root = root / (mode + '-' + str(legacy) + '-' + str(legacy_wal))
                    case_root.mkdir()
                    for file in source.iterdir():
                        shutil.copyfile(file, case_root / file.name)
                    before = [(file.name, hashlib.sha256(file.read_bytes()).digest()) for file in case_root.iterdir()]
                    result = test_read(api, case_root / 'message.db', key, legacy=legacy, legacy_wal=legacy_wal)
                    result.update(keyEncoding=mode, legacy=legacy, legacyWAL=legacy_wal)
                    result['encryptedFilesUnchanged'] = all(hashlib.sha256((case_root / name).read_bytes()).digest() == value for name, value in before)
                    cases.append(result)
        report['cases'] = cases
        report['compatible'] = any(case['opened'] for case in cases)
        report['existingKeyFileUnchanged'] = hashlib.sha256(key_file.read_bytes()).digest() == original_key_hash
        report['completed'] = True
    except Exception as error:
        report['error'] = type(error).__name__ + ': ' + str(error)
        raise
    finally:
        for i in range(len(key_bytes)):
            key_bytes[i] = 0
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        print(json.dumps(report, ensure_ascii=False))


if __name__ == '__main__':
    main()
