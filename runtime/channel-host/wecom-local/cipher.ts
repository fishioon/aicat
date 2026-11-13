import { createHash, createDecipheriv } from "node:crypto";

const header = Buffer.from("SQLite format 3\0");
export const maxImage = 128 << 20;
const md5 = (value: Uint8Array) => createHash("md5").update(value).digest();

export function pageSize(data: Buffer): number {
  if (data.length < 24) throw Error("INVALID_DATABASE_HEADER");
  const size = data.readUInt16BE(16) === 1 ? 65536 : data.readUInt16BE(16);
  if (
    size < 512 ||
    size > 65536 ||
    size & (size - 1) ||
    !data.subarray(21, 24).equals(Buffer.from([64, 32, 32]))
  )
    throw Error("INVALID_DATABASE_HEADER");
  return size;
}

export function decryptPage(key: Buffer, page: number, data: Buffer): Buffer {
  const material = Buffer.alloc(24);
  key.copy(material);
  material.writeUInt32LE(page, 16);
  material.write("sAlT", 20);
  const seed = Buffer.alloc(16);
  let z = page + 1;
  for (let i = 0; i < 4; i++) {
    const q = Math.floor(z / 52774);
    z = 40692 * (z - 52774 * q) - 3791 * q;
    if (z < 0) z += 2147483399;
    seed.writeUInt32LE(z >>> 0, i * 4);
  }
  const first = page === 1;
  const body = first
    ? Buffer.concat([data.subarray(8, 16), data.subarray(24)])
    : data;
  const derived = md5(material);
  const decipher = createDecipheriv("aes-128-cbc", derived, md5(seed));
  decipher.setAutoPadding(false);
  let plain: Buffer;
  try {
    plain = Buffer.concat([decipher.update(body), decipher.final()]);
  } finally {
    derived.fill(0);
    material.fill(0);
  }
  if (!first) return plain;
  if (!plain.subarray(0, 8).equals(data.subarray(16, 24))) {
    plain.fill(0);
    throw Error("DATABASE_KEY_MISMATCH");
  }
  const result = Buffer.concat([header, plain]);
  plain.fill(0);
  if (![2, 5, 10, 13].includes(result[100])) {
    result.fill(0);
    throw Error("INVALID_DECRYPTED_PAGE");
  }
  return result;
}

function checksum(data: Buffer, big: boolean, a = 0, b = 0): [number, number] {
  for (let i = 0; i + 8 <= data.length; i += 8) {
    a = (a + (big ? data.readUInt32BE(i) : data.readUInt32LE(i)) + b) >>> 0;
    b =
      (b + (big ? data.readUInt32BE(i + 4) : data.readUInt32LE(i + 4)) + a) >>>
      0;
  }
  return [a, b];
}

export function merge(key: Buffer, encrypted: Buffer, wal: Buffer): Buffer {
  const size = pageSize(encrypted);
  if (encrypted.length < size || encrypted.length % size)
    throw Error("TRUNCATED_DATABASE");
  let commit = 0,
    pages = encrypted.length / size;
  const stride = size + 24;
  if (wal.length) {
    if (wal.length < 32) throw Error("INVALID_WAL_HEADER");
    const magic = wal.readUInt32BE(0),
      big = magic === 0x377f0683;
    if (
      ![0x377f0682, 0x377f0683].includes(magic) ||
      wal.readUInt32BE(4) !== 3007000 ||
      wal.readUInt32BE(8) !== size
    )
      throw Error("INVALID_WAL_HEADER");
    let [a, b] = checksum(wal.subarray(0, 24), big);
    if (a !== wal.readUInt32BE(24) || b !== wal.readUInt32BE(28))
      throw Error("INVALID_WAL_CHECKSUM");
    for (let offset = 32; offset + stride <= wal.length; offset += stride) {
      const frame = wal.subarray(offset, offset + stride);
      if (
        !frame.readUInt32BE(0) ||
        !frame.subarray(8, 16).equals(wal.subarray(16, 24))
      )
        break;
      [a, b] = checksum(frame.subarray(0, 8), big, a, b);
      [a, b] = checksum(frame.subarray(24), big, a, b);
      if (a !== frame.readUInt32BE(16) || b !== frame.readUInt32BE(20)) break;
      if (frame.readUInt32BE(4)) {
        commit = offset + stride;
        pages = frame.readUInt32BE(4);
      }
    }
  }
  if (pages * size > maxImage || pages < 1)
    throw Error("DATABASE_IMAGE_TOO_LARGE");
  const plain = Buffer.alloc(pages * size);
  try {
    for (
      let offset = 0;
      offset < Math.min(plain.length, encrypted.length);
      offset += size
    ) {
      const page = decryptPage(
        key,
        offset / size + 1,
        encrypted.subarray(offset, offset + size),
      );
      page.copy(plain, offset);
      page.fill(0);
    }
    for (let offset = 32; offset < commit; offset += stride) {
      const pageID = wal.readUInt32BE(offset);
      if (pageID > pages) continue;
      const page = decryptPage(
        key,
        pageID,
        wal.subarray(offset + 24, offset + stride),
      );
      page.copy(plain, (pageID - 1) * size);
      page.fill(0);
    }
    plain[18] = plain[19] = 1;
    return plain;
  } catch (error) {
    plain.fill(0);
    throw error;
  }
}
