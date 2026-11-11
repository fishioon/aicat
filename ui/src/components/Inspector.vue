<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { TrafficRecord, Header } from "../generated/mygo";
import { body, copy, copied } from "../state/traffic";
import { modal, closeDialog, run } from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{
  side: "request" | "response";
  record: TrafficRecord | null;
}>();
const view = ref<"headers" | "raw">("headers");
const content = ref("");
const notice = ref("");
const message = computed(() =>
  props.side === "request"
    ? props.record?.requestReceived
    : props.record?.responseSent,
);
const headers = computed<Header>(() => {
  const result = { ...message.value?.headers };
  if (
    props.side === "request" &&
    props.record &&
    !Object.keys(result).some((key) => key.toLowerCase() === "host")
  )
    result.Host = [new URL(props.record.url).host];
  return result;
});
const headerText = computed(() =>
  Object.entries(headers.value)
    .flatMap(([name, values]) => values.map((value) => `${name}: ${value}`))
    .join("\n"),
);
const tunnel = computed(
  () => props.record?.tunnel && props.side === "response",
);
let bodyTask: Promise<void> = Promise.resolve();
watch(
  [() => props.record, view],
  ([record, mode], _, cleanup) => {
    let stale = false;
    cleanup(() => {
      stale = true;
    });
    content.value = "";
    notice.value = "";
    if (!record || mode !== "raw" || tunnel.value || !message.value) return;
    const info = message.value.body;
    content.value = t("正在读取正文…");
    bodyTask = (async () => {
      if (!info.id || record.tunnel) {
        if (!stale) {
          content.value = "";
          notice.value = record.tunnel ? "CONNECT 加密隧道" : "正文未采集";
        }
        return;
      }
      try {
        const snapshot = await body(info);
        const bytes = Uint8Array.from(atob(snapshot.data ?? ""), (char) =>
          char.charCodeAt(0),
        );
        let text = "",
          warning = "";
        try {
          if (bytes.includes(0)) throw new Error("binary");
          text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
        } catch {
          text = [...bytes]
            .map((byte) => byte.toString(16).padStart(2, "0"))
            .join(" ");
          warning = t("二进制正文，以十六进制显示");
        }
        if (snapshot.info.state !== "complete")
          warning = t(
            "正文状态 {state} · 保留 {captured} B / 已观察 {observed} B",
            snapshot.info,
          );
        if (!stale) {
          content.value = text;
          notice.value = warning;
        }
      } catch (error) {
        if (!stale) {
          content.value = "";
          notice.value = error instanceof Error ? error.message : String(error);
        }
      }
    })();
  },
  { immediate: true },
);
const rawText = computed(() => {
  const record = props.record,
    msg = message.value;
  if (!record || !msg) return t("响应尚未到达");
  const url = new URL(record.url);
  const heading =
    "method" in msg
      ? `${msg.method} ${record.tunnel ? url.host : url.pathname + url.search} ${msg.proto || "HTTP"}`
      : `${msg.proto || "HTTP"} ${msg.statusCode}`;
  return `${heading}\n${headerText.value}\n\n${content.value}`;
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
  if (!props.record || tunnel.value) return;
  if (view.value === "raw") await bodyTask;
  await copy(
    props.side,
    view.value === "raw" ? rawText.value : headerText.value,
  );
}
function decrypt() {
  const domain = new URL(props.record!.url).hostname;
  modal.value = {
    title: "HTTPS 解密",
    message: domain,
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "安装证书",
        run: async () => {
          if (
            (await run(() => host.installCertificate())) &&
            (await run(() => host.setDecryptionDomain(domain, true)))
          )
            closeDialog();
        },
      },
      {
        label: "客户端已单独信任",
        run: async () => {
          if (await run(() => host.setDecryptionDomain(domain, true)))
            closeDialog();
        },
      },
    ],
  };
}
</script>
<template>
  <section class="inspector ac-inspector ac-payload" :data-side="side">
    <header class="ac-inspector-bar">
      <strong>{{ t(side === "request" ? "请求" : "响应") }}</strong>
      <div class="ac-inspector-tabs">
        <button
          class="tab ac-inspector-tab"
          :class="{ active: view === 'headers' }"
          data-view="headers"
          @click="view = 'headers'"
        >
          Header</button
        ><button
          class="tab ac-inspector-tab"
          :class="{ active: view === 'raw' }"
          data-view="raw"
          @click="view = 'raw'"
        >
          Raw
        </button>
      </div>
      <span class="ac-grow"></span
      ><button
        class="copy icon-button"
        :data-side-copy="side"
        :aria-label="t(side === 'request' ? '复制请求' : '复制响应')"
        :disabled="!!tunnel"
        @click="copySide"
      >
        <Icon name="copy" /><span
          v-if="copied.has(side)"
          class="copy-feedback"
          >{{ t("已复制") }}</span
        >
      </button>
    </header>
    <div class="body-notice">{{ notice }}</div>
    <div class="headers" :hidden="view !== 'headers'">
      <template v-if="tunnel"
        >{{ t("CONNECT 加密隧道 · 内容未解密")
        }}<button id="enable-decryption" @click="decrypt">
          {{ t("启用此域名") }}
        </button></template
      >
      <table v-else-if="record" class="ac-header-table">
        <thead>
          <tr>
            <th style="width: 35%">{{ t("名称") }}</th>
            <th>{{ t("值") }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="(values, name) in headers" :key="name"
            ><tr v-for="(value, index) in values" :key="index">
              <td>{{ name }}</td>
              <td>{{ value }}</td>
            </tr></template
          >
        </tbody>
      </table>
    </div>
    <pre
      class="raw"
      :hidden="view !== 'raw'"
      :data-text="rawText"
    ><template v-if="tunnel">{{ t('CONNECT 加密隧道 · 内容未解密') }}<button id="enable-decryption-raw" @click="decrypt">{{ t('启用此域名') }}</button></template><template v-else-if="view === 'raw' && record"><span v-for="line in lines" :key="line.index" class="ac-raw-line"><span class="ac-line-number" aria-hidden="true">{{ line.index + 1 }}</span><code v-if="line.index === 0" class="ac-wire-start">{{ line.text }}</code><code v-else-if="line.colon"><span class="ac-wire-key">{{ line.text.slice(0, line.colon) }}</span>{{ line.text.slice(line.colon) }}</code><code v-else>{{ line.text }}</code></span></template></pre>
  </section>
</template>
