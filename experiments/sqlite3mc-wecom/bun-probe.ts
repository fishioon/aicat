import { Database } from 'bun:sqlite';
import path from 'node:path';

if (!Database.setCustomSQLite(path.resolve(process.argv[2]))) throw Error('CUSTOM_SQLITE_UNAVAILABLE');
const root = path.resolve(process.argv[3]);
const cases = [
  ['positive-db-only', 1],
  ['positive-with-wal', 2],
  ['positive-uncommitted', 2],
] as const;
const results = [];
for (const [name, expected] of cases) {
  const db = new Database(path.join(root, name, 'positive.db'), {readonly: true});
  try {
    db.exec("PRAGMA cipher='aes128cbc'; PRAGMA legacy=0; PRAGMA key='public-synthetic-passphrase'; PRAGMA query_only=ON;");
    const integrity = Object.values(db.query('PRAGMA quick_check').get() as Record<string, unknown>)[0];
    const count = (db.query('SELECT COUNT(*) AS count FROM message_table').get() as {count: number}).count;
    let writeRejected = false;
    try {db.exec('CREATE TABLE must_not_write(value)');} catch {writeRejected = true;}
    if (integrity !== 'ok' || count !== expected || !writeRejected) throw Error('CONTROL_FAILED');
    results.push({name, count, integrity, writeRejected});
  } finally {db.close();}
}
console.log(JSON.stringify({passed: true, cases: results}));
