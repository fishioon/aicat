import { Database } from "bun:sqlite";
import { fileURLToPath } from "node:url";

let initialized = false;
export function openDatabase(file: string, base64: string): Database {
  if (!initialized) {
    const library = fileURLToPath(
      new URL("../sqlite3mc/libsqlite3mc.dylib", import.meta.url),
    );
    if (!Database.setCustomSQLite(library))
      throw Error("LOCAL_SQLITE_UNAVAILABLE");
    initialized = true;
  }
  if (!/^[A-Za-z0-9+/]{22}==$/.test(base64)) throw Error("INVALID_BASE64_KEY");
  const input = Buffer.from(base64, "base64");
  if (input.length !== 16 || input.toString("base64") !== base64) {
    input.fill(0);
    throw Error("INVALID_BASE64_KEY");
  }
  let db: Database | undefined;
  try {
    db = new Database(file, { readonly: true, safeIntegers: true });
    db.exec(
      "PRAGMA cipher='aes128cbc'; PRAGMA legacy=0; PRAGMA hexkey='" +
        input.toString("hex") +
        "'; PRAGMA query_only=ON; PRAGMA temp_store=MEMORY; PRAGMA mmap_size=0; PRAGMA cache_size=-2048;",
    );
    return db;
  } catch {
    db?.close();
    throw Error("LOCAL_DATABASE_OPEN_FAILED");
  } finally {
    input.fill(0);
  }
}
