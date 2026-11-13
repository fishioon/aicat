import { isUtf8 } from "node:buffer";

export function classify(type: number): string {
  if ([0, 2, 6, 1001, 1002, 1003, 1006].includes(type)) return "text";
  if ([29, 4].includes(type)) return "image";
  if (type === 14) return "voice";
  if ([15, 16].includes(type)) return "file";
  if (type === 40) return "voip";
  if (type === 1073) return "meeting";
  if ([38, 501, 503, 1011].includes(type)) return "system";
  if ([1018, 1022, 1051, 1052].includes(type)) return "event";
  if (
    [
      10, 20, 105, 123, 145, 221, 516, 561, 565, 570, 573, 579, 580, 581, 582,
    ].includes(type)
  )
    return "card";
  if ([13, 78, 101, 102, 539, 588, 597].includes(type)) return "mixed";
  return "unknown";
}

export function xmlElement(text: string, tag: string): string {
  const start = text.indexOf(`<${tag}>`);
  if (start < 0) return "";
  const body = text.slice(start + tag.length + 2);
  if (body.startsWith("<![CDATA[")) {
    const end = body.indexOf("]]>", 9);
    return end < 0 ? "" : body.slice(9, end);
  }
  const end = body.indexOf(`</${tag}>`);
  return end < 0 ? "" : body.slice(0, end);
}
const printable = (text: string) => {
  const chars = [...text];
  return (
    chars.length > 0 &&
    chars.filter((c) => /^[\t\n\r]|[^\x00-\x1f\x7f\ufffd]$/u.test(c)).length *
      100 >=
      chars.length * 99
  );
};
const sanitize = (text: string) =>
  text.replace(/[\x00-\x08\x0b-\x1f]/g, "").trim();
function varint(data: Buffer, offset: number): [bigint, number] {
  let value = 0n;
  for (let shift = 0n; shift < 64n && offset < data.length; shift += 7n) {
    const byte = data[offset++];
    value |= BigInt(byte & 127) << shift;
    if (!(byte & 128)) return [value, offset];
  }
  throw Error("INVALID_VARINT");
}
export function extractText(data: Buffer): string {
  if (data[0] === 60) {
    const xml = data.toString();
    if (/<(?:appmsg|msg|sysmsg)/.test(xml)) {
      for (const tag of ["title", "content", "des"]) {
        const value = xmlElement(xml, tag);
        if (value) return value;
      }
    }
  }
  let best = "",
    score = 0;
  const consider = (text: string) => {
    text = sanitize(text);
    const chars = [...text];
    const cjk = chars.filter((c) =>
      /[\u2e80-\u9fff\uf900-\ufaff]/u.test(c),
    ).length;
    const value =
      chars.length +
      cjk * 2 -
      (chars.length < 4 && /^[\x00-\x7f]*$/.test(text) ? 4 : 0);
    if (value > score) {
      best = text;
      score = value;
    }
  };
  const walk = (body: Buffer, depth: number) => {
    if (depth > 3 || body.length < 2) return;
    let offset = 0;
    try {
      while (offset < body.length) {
        let tag: bigint;
        [tag, offset] = varint(body, offset);
        if (tag >> 3n < 1n || tag >> 3n > 1024n) return;
        switch (Number(tag & 7n)) {
          case 0:
            [, offset] = varint(body, offset);
            break;
          case 1:
            offset += 8;
            break;
          case 5:
            offset += 4;
            break;
          case 2: {
            let length: bigint;
            [length, offset] = varint(body, offset);
            if (length > BigInt(body.length - offset)) return;
            const payload = body.subarray(offset, offset + Number(length));
            offset += Number(length);
            if (isUtf8(payload) && printable(payload.toString()))
              consider(payload.toString());
            else walk(payload, depth + 1);
            break;
          }
          default:
            return;
        }
      }
    } catch {}
  };
  walk(data, 0);
  if (best) return best;
  if (isUtf8(data) && printable(data.toString()))
    return sanitize(data.toString());
  let start = 0,
    length = 0,
    bestStart = 0,
    bestLength = 0;
  for (let offset = 0; offset < data.length;) {
    const byte = data[offset];
    const size =
      byte < 128
        ? 1
        : byte >= 194 && byte <= 223
          ? 2
          : byte <= 239 && byte >= 224
            ? 3
            : byte >= 240 && byte <= 244
              ? 4
              : 0;
    const chunk = data.subarray(offset, offset + size);
    if (
      size &&
      chunk.length === size &&
      isUtf8(chunk) &&
      printable(chunk.toString())
    ) {
      if (!length) start = offset;
      length += size;
      offset += size;
      if (length > bestLength) {
        bestStart = start;
        bestLength = length;
      }
    } else {
      length = 0;
      offset++;
    }
  }
  consider(data.subarray(bestStart, bestStart + bestLength).toString());
  return best;
}

export function bodyFields(
  type: string,
  content: Buffer,
): Record<string, unknown> {
  const text = content.toString();
  const attr = (name: string) =>
    text.match(new RegExp(`${name}=["']([^"']*)["']`))?.[1] ?? "";
  const size = () => {
    const n = Number(attr("length"));
    return Number.isSafeInteger(n) && n >= 0 ? n : 0;
  };
  switch (type) {
    case "image":
      return { image: { md5sum: attr("md5"), filesize: size() } };
    case "voice":
      return { voice: {} };
    case "file":
      return {
        file: {
          filename: attr("filename") || xmlElement(text, "title"),
          md5sum: attr("md5"),
          filesize: size(),
        },
      };
    case "card":
      return {
        link: {
          title: xmlElement(text, "title") || extractText(content),
          description: xmlElement(text, "des"),
          link_url: xmlElement(text, "url"),
          image_url: xmlElement(text, "thumburl"),
        },
      };
    default:
      return {};
  }
}
