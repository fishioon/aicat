import type { Summary } from "../generated/mygo";
import { t } from "../i18n";

// Row status dot: green = 2xx, red = 4xx/5xx/failed, grey = 3xx,
// hollow = undecrypted tunnel, spinner = in progress.
export function dot(row: Summary): "ok" | "bad" | "mute" | "ring" | "run" {
  if (row.state === "request" || row.state === "response")
    return row.tunnel ? "ring" : "run";
  if (row.tunnel && row.state !== "failed") return "ring";
  if (row.state === "failed" || row.statusCode >= 400) return "bad";
  if (row.state === "canceled" || row.statusCode >= 300 || !row.statusCode)
    return "mute";
  return "ok";
}
export function status(row: Summary): { text: string; kind: string } {
  if (row.tunnel && row.state !== "failed")
    return { text: t("隧道"), kind: "dim" };
  if (row.statusCode)
    return {
      text: String(row.statusCode),
      kind:
        row.statusCode >= 400 ? "st-bad" : row.statusCode >= 300 ? "dim" : "",
    };
  if (row.state === "failed") return { text: t("失败"), kind: "st-bad" };
  if (row.state === "canceled") return { text: t("已取消"), kind: "dim" };
  return { text: t("进行中"), kind: "dim" };
}
export function type(row: Summary): string {
  const media = row.contentType ?? "";
  if (!media) return "";
  if (media === "text/event-stream") return "SSE";
  if (media.endsWith("json") || media.endsWith("+json")) return "JSON";
  if (media === "text/html") return "HTML";
  if (media.includes("javascript")) return "JS";
  if (media === "text/css") return "CSS";
  if (media.endsWith("xml")) return "XML";
  if (media.startsWith("text/")) return t("文本");
  if (media.startsWith("image/")) return t("图片");
  if (media === "application/x-www-form-urlencoded") return t("表单");
  return media.split("/")[1]?.replace(/^x-/, "").slice(0, 8) ?? media;
}
export function duration(row: Summary): string {
  if (row.state === "request" || row.state === "response") return "";
  const ms = row.durationNs / 1e6;
  return ms < 1000 ? Math.round(ms) + " ms" : (ms / 1000).toFixed(2) + " s";
}
export function size(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / 1024 / 1024).toFixed(1) + " MB";
}
export const reasons: Record<number, string> = {
  200: "OK",
  201: "Created",
  202: "Accepted",
  204: "No Content",
  206: "Partial Content",
  301: "Moved Permanently",
  302: "Found",
  304: "Not Modified",
  307: "Temporary Redirect",
  308: "Permanent Redirect",
  400: "Bad Request",
  401: "Unauthorized",
  403: "Forbidden",
  404: "Not Found",
  405: "Method Not Allowed",
  409: "Conflict",
  413: "Payload Too Large",
  422: "Unprocessable Entity",
  429: "Too Many Requests",
  500: "Internal Server Error",
  502: "Bad Gateway",
  503: "Service Unavailable",
  504: "Gateway Timeout",
  529: "Overloaded",
};
