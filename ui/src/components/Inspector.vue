<script lang="ts">
import { ref } from "vue";
export type View = "headers" | "query" | "body" | "events" | "raw";
// Each side remembers its view for this run; switching requests keeps it.
const remembered = {
  request: ref<View>("headers"),
  response: ref<View>("headers"),
};
</script>
<script setup lang="ts">
import { computed, watch } from "vue";
import type { TrafficRecord, BodySnapshot } from "../generated/mygo";
import { body, copy, copied } from "../state/traffic";
import { size, reasons } from "../state/format";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{
  side: "request" | "response";
  record: TrafficRecord | null;
}>();
const chosen = remembered[props.side];
const content = ref<string | null>(null);
const bodyNote = ref("");
const message = computed(() =>
  props.side === "request"
    ? props.record?.requestReceived
    : props.record?.responseSent,
);
const url = computed(() =>
  props.record ? new URL(props.record.url) : undefined,
);
const headers = computed(() => {
  const result: [string, string][] = [];
  const source = message.value?.headers ?? {};
  if (
    props.side === "request" &&
    url.value &&
    !Object.keys(source).some((key) => key.toLowerCase() === "host")
  )
    result.push(["Host", url.value.host]);
  for (const [name, values] of Object.entries(source))
    for (const value of values) result.push([name, value]);
  return result;
});
const mediaType = computed(() => {
  const header = headers.value.find(
    ([name]) => name.toLowerCase() === "content-type",
  );
  return (header?.[1].split(";")[0] ?? "").trim().toLowerCase();
});
const query = computed(() => [...(url.value?.searchParams ?? [])]);
const hasBody = computed(
  () => !!message.value?.body.id && message.value.body.observed > 0,
);
const tabs = computed(() => {
  const list: [View, string][] = [["headers", "Header"]];
  if (props.side === "request" && query.value.length)
    list.push(["query", "Query"]);
  if (hasBody.value) list.push(["body", "Body"]);
  if (props.side === "response" && mediaType.value === "text/event-stream")
    list.push(["events", "事件"]);
  list.push(["raw", "Raw"]);
  return list;
});
const view = computed<View>(() =>
  tabs.value.some(([id]) => id === chosen.value) ? chosen.value : "headers",
);
const failed = computed(
  () =>
    props.side === "response" &&
    !message.value &&
    (props.record?.state === "failed" || props.record?.state === "canceled"),
);
const pending = computed(
  () =>
    props.side === "response" &&
    (props.record?.state === "request" || props.record?.state === "response"),
);

function decode(snapshot: BodySnapshot): string {
  const bytes = Uint8Array.from(atob(snapshot.data ?? ""), (char) =>
    char.charCodeAt(0),
  );
  try {
    if (bytes.includes(0)) throw new Error("binary");
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    bodyNote.value = t("二进制正文，以十六进制显示");
    return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join(" ");
  }
}
let bodyTask: Promise<void> = Promise.resolve();
watch(
  [() => props.record, view],
  ([record, mode], _, cleanup) => {
    let stale = false;
    cleanup(() => {
      stale = true;
    });
    content.value = null;
    bodyNote.value = "";
    const info = message.value?.body;
    if (!record || !info || mode === "headers" || mode === "query") return;
    if (!info.id || info.observed === 0) {
      content.value = "";
      return;
    }
    bodyTask = (async () => {
      try {
        const snapshot = await body(info);
        if (stale) return;
        content.value = decode(snapshot);
        const state = snapshot.info.state;
        if (state === "truncated")
          bodyNote.value = t(
            "正文过大，只保留了前 {kept}（共 {total}）。实际转发不受影响。",
            {
              kept: size(snapshot.info.captured),
              total: size(snapshot.info.observed),
            },
          );
        else if (state === "omitted")
          bodyNote.value = t("正文未采集。实际转发不受影响。");
        else if (state === "error")
          bodyNote.value = t("正文读取出错，只保留了已收到的部分。");
      } catch (error) {
        if (!stale) {
          content.value = "";
          bodyNote.value =
            error instanceof Error ? error.message : String(error);
        }
      }
    })();
  },
  { immediate: true },
);

type Token = { text: string; cls?: string };
const pretty = computed<Token[] | null>(() => {
  if (content.value === null || !mediaType.value.includes("json")) return null;
  let text: string;
  try {
    text = JSON.stringify(JSON.parse(content.value), null, 2);
  } catch {
    return null;
  }
  const tokens: Token[] = [];
  const re =
    /("(?:[^"\\]|\\.)*")(\s*:)?|-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|\b(?:true|false|null)\b/g;
  let last = 0;
  for (const match of text.matchAll(re)) {
    if (match.index > last)
      tokens.push({ text: text.slice(last, match.index) });
    if (match[1]) {
      tokens.push({ text: match[1], cls: match[2] ? "k" : "s" });
      if (match[2]) tokens.push({ text: match[2] });
    } else tokens.push({ text: match[0], cls: "n" });
    last = match.index + match[0].length;
  }
  tokens.push({ text: text.slice(last) });
  return tokens;
});
const events = computed(() =>
  (content.value ?? "")
    .split(/\r?\n\r?\n/)
    .map((block) => {
      let name = "",
        data: string[] = [];
      for (const line of block.split(/\r?\n/)) {
        if (line.startsWith("event:")) name = line.slice(6).trim();
        else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
      }
      return {
        name: name || (data.length ? "message" : ""),
        data: data.join("\n"),
      };
    })
    .filter((event) => event.name),
);
const headerText = computed(() =>
  headers.value.map(([name, value]) => `${name}: ${value}`).join("\n"),
);
const rawText = computed(() => {
  const record = props.record,
    msg = message.value;
  if (!record || !msg) return "";
  const heading =
    "method" in msg
      ? `${msg.method} ${record.tunnel ? url.value!.host : url.value!.pathname + url.value!.search} ${msg.proto || "HTTP/1.1"}`
      : `${msg.proto || "HTTP/1.1"} ${msg.statusCode} ${reasons[msg.statusCode] ?? ""}`.trimEnd();
  return `${heading}\n${headerText.value}\n\n${content.value ?? ""}`;
});
const lines = computed(() => {
  let header = true;
  return rawText.value.split("\n").map((text, index) => {
    const colon =
      header && index > 0 && text.includes(":") ? text.indexOf(":") + 1 : 0;
    if (!text) header = false;
    return { text: text || " ", index, colon };
  });
});
async function copySide() {
  if (!props.record) return;
  if (view.value !== "headers" && view.value !== "query") await bodyTask;
  const text = {
    headers: headerText.value,
    query: query.value.map(([k, v]) => `${k}=${v}`).join("\n"),
    body: content.value ?? "",
    events: events.value
      .map((e) => `event: ${e.name}\ndata: ${e.data}\n`)
      .join("\n"),
    raw: rawText.value,
  }[view.value];
  await copy(props.side, text, t(props.side === "request" ? "请求" : "响应"));
}
function key(event: KeyboardEvent, index: number) {
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const list = tabs.value;
  const next =
    list[
      (index + (event.key === "ArrowRight" ? 1 : -1) + list.length) %
        list.length
    ];
  chosen.value = next[0];
  (event.currentTarget as HTMLElement).parentElement
    ?.querySelector<HTMLElement>(`[data-view="${next[0]}"]`)
    ?.focus();
}
const name = computed(() => t(props.side === "request" ? "请求" : "响应"));
</script>
<template>
  <section class="pane inspector" :data-side="side" :aria-label="name">
    <div class="pane-head">
      <h3>{{ name }}</h3>
      <div class="ptabs" role="tablist" :aria-label="name">
        <button
          v-for="([id, label], index) in tabs"
          :key="id"
          class="tab"
          role="tab"
          :data-view="id"
          :aria-selected="view === id"
          :tabindex="view === id ? 0 : -1"
          @click="chosen = id"
          @keydown="key($event, index)"
        >
          {{ t(label) }}
        </button>
      </div>
      <span class="copy"
        ><span v-if="copied.has(side)" class="copied">{{ t("已复制") }}</span
        ><button
          class="icon-btn"
          :data-side-copy="side"
          :aria-label="t(side === 'request' ? '复制请求' : '复制响应')"
          :title="t(side === 'request' ? '复制请求' : '复制响应')"
          :disabled="!record || failed"
          @click="copySide"
        >
          <Icon name="copy" /></button
      ></span>
    </div>
    <div class="pane-body" role="tabpanel" tabindex="0">
      <div v-if="failed" class="note bad body-notice">
        {{
          t("没有收到响应：{error}", {
            error: record?.error || t("连接已中断"),
          })
        }}
      </div>
      <template v-else>
        <div v-if="pending" class="note plain">
          {{ t(message ? "仍在接收响应。" : "等待上游响应。") }}
        </div>
        <div
          v-if="bodyNote && view !== 'headers' && view !== 'query'"
          class="note body-notice"
        >
          {{ bodyNote }}
        </div>
        <table v-if="view === 'headers'" class="kv headers">
          <tbody>
            <tr v-for="([key, value], index) in headers" :key="index">
              <th scope="row">{{ key }}</th>
              <td>{{ value }}</td>
            </tr>
          </tbody>
        </table>
        <table v-else-if="view === 'query'" class="kv">
          <tbody>
            <tr v-for="([key, value], index) in query" :key="index">
              <th scope="row">{{ key }}</th>
              <td>{{ value }}</td>
            </tr>
          </tbody>
        </table>
        <template v-else-if="content === null">
          <p class="waiting"><i class="spin"></i>{{ t("正在读取正文…") }}</p>
        </template>
        <pre
          v-else-if="view === 'body'"
          class="body"
        ><template v-if="pretty"><span v-for="(token, index) in pretty" :key="index" :class="token.cls">{{ token.text }}</span></template><template v-else>{{ content }}</template></pre>
        <ol v-else-if="view === 'events'" class="events">
          <li v-for="(event, index) in events" :key="index">
            <span class="ix num">{{ index + 1 }}</span
            ><span class="ev">{{ event.name }}</span
            ><span class="d" :title="event.data">{{ event.data }}</span>
          </li>
        </ol>
        <pre
          v-else
          class="body raw"
          :data-text="rawText"
        ><span v-for="line in lines" :key="line.index" class="raw-line"><span class="ln" aria-hidden="true">{{ line.index + 1 }}</span><code v-if="line.index === 0" class="line-start">{{ line.text }}</code><code v-else-if="line.colon"><span class="key">{{ line.text.slice(0, line.colon) }}</span>{{ line.text.slice(line.colon) }}</code><code v-else>{{ line.text }}</code></span></pre>
      </template>
    </div>
  </section>
</template>
