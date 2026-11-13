import fs from 'node:fs';
import path from 'node:path';
import { dlopen, FFIType } from 'bun:ffi';
import { Database } from 'bun:sqlite';
const config = await Bun.stdin.json();
const library = path.resolve(process.argv[2]);
if (!Database.setCustomSQLite(library)) throw Error('CUSTOM_SQLITE_UNAVAILABLE');
const native = dlopen(library, {aicat_register_wecom_cipher: {args: [], returns: FFIType.i32}});
if (native.symbols.aicat_register_wecom_cipher() !== 0) throw Error('CIPHER_REGISTRATION_FAILED');
const original = path.join(config.dataDir, 'message.db');
const entries = JSON.parse(fs.readFileSync(config.keyFile, 'utf8')).entries.filter((entry: any) => entry.path === original && entry.account === config.accountId && entry.name === 'message.db' && !entry.missing);
if (entries.length !== 1 || !/^[0-9a-fA-F]{32}$/.test(entries[0].key)) throw Error('KEY_ENTRY_INVALID');
const db = new Database(process.argv[3] || original, {readonly: true, safeIntegers: true});
try {
  db.exec("PRAGMA cipher='aicat_wecom_aes128'; PRAGMA hexkey='" + entries[0].key + "'; PRAGMA query_only=ON; BEGIN;");
  const integrity = Object.values(db.query('PRAGMA quick_check').get() as Record<string, unknown>)[0];
  const stats = db.query('SELECT COUNT(*) AS count, MAX(message_id) AS highest FROM message_table').get() as {count: bigint; highest: bigint};
  db.exec('COMMIT');
  let writeRejected = false;
  try {db.exec('CREATE TABLE must_not_write(value)');} catch {writeRejected = true;}
  console.log(JSON.stringify({passed: integrity === 'ok' && writeRejected, count: String(stats.count), highest: String(stats.highest), integrity, writeRejected}));
} finally {db.close(); native.close();}
