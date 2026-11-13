import argparse
import importlib.util
import json
import os
from pathlib import Path
import sqlite3

parser = argparse.ArgumentParser()
parser.add_argument('--output', required=True)
args = parser.parse_args()
os.umask(0o077)
root = Path(args.output).resolve()
root.mkdir(parents=True, exist_ok=False)
target = root / 'database'
target.mkdir()
spec = importlib.util.spec_from_file_location('local_reader_fixture', Path(__file__).with_name('wecom-local.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)
long = "长消息第一行\n" + "带单引号 ' 和中文的完整正文。" * 30 + "\n最后一行"
db_path = root / 'plain-fixture.db'
db = sqlite3.connect(db_path)
try:
    db.executescript('PRAGMA page_size=4096; PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE message_table(message_id INTEGER PRIMARY KEY,server_id INTEGER,sequence INTEGER,sender_id INTEGER,conversation_id TEXT,content_type INTEGER,send_time INTEGER,flag INTEGER,content BLOB);')
    for id, room, text, typ, flag in [(1, 'group-fixture', '群内不带提及的历史消息', 2, 0), (2, 'private-fixture', '私聊消息', 2, 0), (3, 'group-fixture', '自己发出的消息', 2, 2), (4, 'group-fixture', '图片元数据', 29, 0), (5, 'private-fixture', long, 2, 0)]:
        db.execute('INSERT INTO message_table VALUES (?,?,?,?,?,?,?,?,?)', (id, 9007199254740999, id, 9007199254740997, room, typ, 1, flag, fixture.proto(text)))
        db.commit()
        if id == 1: db.execute('PRAGMA wal_checkpoint(TRUNCATE)')
    fixture.publish(db_path, target)
finally:
    db.close()
(root / 'keys.json').write_text(json.dumps({'version': 1, 'entries': [{'path': str(target / 'message.db'), 'account': 'synthetic-account', 'name': 'message.db', 'key': fixture.KEY.hex()}]}))
if hasattr(fixture, 'BASE64_KEY'):
    (root / 'fixture-key.json').write_text(json.dumps({'key': fixture.BASE64_KEY}))
(root / 'report.json').write_text(json.dumps({'passed': True, 'fixtureMessages': 5, 'longMessage': long}, ensure_ascii=False, indent=2) + '\n')
print(root)
