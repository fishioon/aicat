import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { Database } from "bun:sqlite";
import { openDatabase } from "./sqlite";
const maxImage = 128 << 20;
import {
  bodyFields,
  classify,
  extractText,
  textSegments,
  conversationType,
} from "./content";

export type LocalConfig = {
  accountId: string;
  dataDir: string;
  key: string;
};
export type Position = { position: string; anchor: string };
type Row = {
  message_id: bigint;
  server_id: bigint | null;
  sequence: bigint | null;
  sender_id: bigint | null;
  conversation_id: string;
  content_type: bigint;
  send_time: bigint;
  flag: bigint;
  content: Uint8Array;
};
export const digest = (value: string | Uint8Array) =>
  createHash("sha256").update(value).digest("hex");
const stamp = (files: string[]) =>
  files.map((file, index) => {
    try {
      const st = fs.statSync(file, { bigint: true });
      if (!st.isFile() || st.size > BigInt(maxImage))
        throw Error("SOURCE_FILE_TOO_LARGE_OR_NOT_REGULAR");
      return `${st.dev}:${st.ino}:${st.size}:${st.mtimeNs}:${st.ctimeNs}`;
    } catch (error) {
      if (index === 1 && (error as NodeJS.ErrnoException).code === "ENOENT")
        return null;
      throw error;
    }
  });
export function validateConfig(config: LocalConfig) {
  if (
    !config.accountId?.trim() ||
    config.accountId.length > 256 ||
    /[\r\n\0]/.test(config.accountId)
  )
    throw Error("INVALID_ACCOUNT_ID");
  for (const file of [config.dataDir])
    if (!file || !path.isAbsolute(file) || /[\r\n\0]/.test(file))
      throw Error("ABSOLUTE_LOCAL_PATH_REQUIRED");
}
const fingerprint = (row: Row) =>
  digest(
    `${row.message_id}\0${row.server_id}\0${row.conversation_id}\0${row.sender_id}\0${row.send_time}\0${row.content_type}\0${row.flag}\0${digest(row.content)}`,
  );
export class Source {
  private db?: Database;
  private previous = "";
  private metadataAfter = 0n;
  constructor(readonly config: LocalConfig) {
    validateConfig(config);
  }
  useMessageMetadataAfter(position: string) {
    if (
      !/^(0|[1-9][0-9]*)$/.test(position) ||
      BigInt(position) > 9223372036854775807n
    )
      throw Error("INVALID_MESSAGE_FORMAT_POSITION");
    this.metadataAfter = BigInt(position);
  }
  close() {
    this.db?.close();
    this.db = undefined;
    this.previous = "";
  }
  refresh() {
    const dbFile = path.join(this.config.dataDir, "message.db");
    const files = [dbFile, dbFile + "-wal"];
    const before = stamp(files),
      signature = JSON.stringify(before);
    if (this.db && this.previous === signature) return;
    this.close();
    let next: Database | undefined;
    let reading = false;
    try {
      next = openDatabase(dbFile, this.config.key);
      next.exec("BEGIN");
      reading = true;
      const result = next.query("PRAGMA quick_check").all() as Record<
        string,
        unknown
      >[];
      if (result.length !== 1 || Object.values(result[0])[0] !== "ok")
        throw Error("DATABASE_INTEGRITY_CHECK_FAILED");
      next
        .query(
          "SELECT message_id,server_id,sequence,sender_id,conversation_id,content_type,send_time,flag,content FROM message_table LIMIT 0",
        )
        .all();
      const after = stamp(files);
      if (
        before.some(
          (value, index) =>
            value?.split(":").slice(0, 2).join(":") !==
            after[index]?.split(":").slice(0, 2).join(":"),
        )
      )
        throw Error("SOURCE_CHANGED_DURING_SNAPSHOT");
      next.exec("ROLLBACK");
      reading = false;
      this.db = next;
      next = undefined;
      this.previous = JSON.stringify(after);
    } finally {
      if (reading) next?.exec("ROLLBACK");
      next?.close();
    }
  }
  updates(cursor: Position) {
    this.refresh();
    if (
      !/^(0|[1-9][0-9]*)$/.test(cursor.position) ||
      BigInt(cursor.position) > 9223372036854775807n
    )
      throw Error("INVALID_CURSOR");
    this.db!.exec("BEGIN");
    try {
      const position = BigInt(cursor.position);
      const highest = (
        this.db!.query(
          "SELECT COALESCE(MAX(message_id),0) AS id FROM message_table",
        ).get() as { id: bigint }
      ).id;
      if (highest < position) throw Error("DATABASE_RESET_CURSOR_REJECTED");
      if (position > 0n) {
        const row = this.db!.query(
          "SELECT * FROM message_table WHERE message_id=?",
        ).get(position) as Row | null;
        if (!row || fingerprint(row) !== cursor.anchor)
          throw Error("DATABASE_IDENTITY_CHANGED_CURSOR_REJECTED");
      }
      const rows = this.db!.query(
        "SELECT message_id,server_id,sequence,sender_id,conversation_id,content_type,send_time,flag,content FROM message_table WHERE message_id>? ORDER BY message_id LIMIT 500",
      ).iterate(position) as Iterable<Row>;
      const result: { message: Record<string, unknown>; cursor: Position }[] =
        [];
      let bytes = 0;
      for (const row of rows) {
        if (
          !row.conversation_id ||
          row.message_id <= position ||
          row.content.length > 8 << 20
        )
          throw Error("INVALID_MESSAGE_OR_OVERSIZED_CONTENT");
        bytes += row.content.length;
        if (bytes > 32 << 20) break;
        const type = classify(Number(row.content_type)),
          outgoing = String(row.sender_id ?? 0n) === this.config.accountId;
        const millis = Number(row.send_time * 1000n);
        if (!Number.isSafeInteger(millis))
          throw Error("INVALID_MESSAGE_TIMESTAMP");
        const body = Buffer.from(row.content);
        const metadata = row.message_id > this.metadataAfter;
        const segments =
          metadata && type === "text" ? textSegments(body) : undefined;
        const message = {
          msgid: `wecom-local:${row.message_id}`,
          action: "send",
          from: outgoing
            ? this.config.accountId
            : row.sender_id && row.sender_id !== 0n
              ? String(row.sender_id)
              : row.conversation_id,
          tolist: [],
          roomid: row.conversation_id,
          msgtime: millis,
          msgtype: type,
          text: { content: segments?.text ?? extractText(body) },
          ...bodyFields(type, body),
          local_id: String(row.message_id),
          server_id: String(row.server_id ?? 0n),
          sequence: String(row.sequence ?? 0n),
          content_type: Number(row.content_type),
          flag: Number(row.flag),
          outgoing,
          room_type: metadata
            ? conversationType(row.conversation_id, this.config.accountId)
            : "unknown",
          ...(metadata && type === "text"
            ? {
                mention_status: segments ? "parsed" : "unknown",
                ...(segments?.mentionIds.length
                  ? { mention_ids: segments.mentionIds }
                  : {}),
                ...(segments?.mentionAll ? { mention_all: true } : {}),
              }
            : {}),
          ...(!row.sender_id ? { sender_kind: "room" } : {}),
          ...(type !== "text" ? { media_status: "metadata_only" } : {}),
          raw_content: body.toString("base64"),
        };
        result.push({
          message,
          cursor: {
            position: String(row.message_id),
            anchor: fingerprint(row),
          },
        });
      }
      return result;
    } finally {
      this.db!.exec("ROLLBACK");
    }
  }

  latestPosition(): string {
    this.refresh();
    return String(
      (
        this.db!.query(
          "SELECT COALESCE(MAX(message_id),0) AS id FROM message_table",
        ).get() as { id: bigint }
      ).id,
    );
  }

  validateRoom(room: string): void {
    this.refresh();
    if (
      !this.db!.query(
        "SELECT 1 FROM message_table WHERE conversation_id=? LIMIT 1",
      ).get(room)
    )
      throw Error("UNKNOWN_SEND_TARGET");
  }
}
