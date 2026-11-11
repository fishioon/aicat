<script setup lang="ts">
import { computed } from "vue";
import type { TrafficRecord } from "../generated/mygo";
import {
  state,
  modal,
  closeDialog,
  run,
  decryptionRule,
  say,
} from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
const props = defineProps<{ record: TrafficRecord }>();
const authority = computed(() => new URL(props.record.url).host);
const domain = computed(() => new URL(props.record.url).hostname);
const rule = computed(() => decryptionRule(domain.value));
const headers = computed(() =>
  Object.entries(props.record.requestReceived.headers ?? {}).flatMap(
    ([name, values]) => values.map((value) => [name, value] as const),
  ),
);
async function enable() {
  if (await run(() => host.setDecryptionDomain(domain.value, true))) {
    closeDialog();
    say(t("已开始解密 {domain}，新连接生效", { domain: domain.value }));
  }
}
function decrypt() {
  if (state.value?.certificate.trusted) {
    void enable();
    return;
  }
  modal.value = {
    title: "HTTPS 解密",
    message: t(
      "解密 {domain} 需要系统信任 aicat 的根证书。也可以在客户端里单独信任它。",
      { domain: domain.value },
    ),
    actions: [
      { label: "取消", run: closeDialog },
      { label: "客户端已单独信任", run: enable },
      {
        label: "安装证书",
        run: async () => {
          if (await run(() => host.installCertificate())) await enable();
        },
      },
    ],
  };
}
</script>
<template>
  <div class="tunnel" id="tunnel-detail">
    <h3>{{ t("这是一条加密隧道") }}</h3>
    <p>
      {{
        t("aicat 只转发了 {host} 的加密数据，没有解密内容。", {
          host: authority,
        })
      }}
    </p>
    <div v-if="rule" id="decryption-covered" class="note plain">
      {{
        rule === domain
          ? t(
              "{domain} 已在解密域名里。这条隧道建立得更早，保持未解密；新连接会被解密。",
              { domain },
            )
          : t(
              "{domain} 匹配解密域名 {rule}。这条隧道建立得更早，保持未解密；新连接会被解密。",
              { domain, rule },
            )
      }}
    </div>
    <template v-else>
      <p>
        {{ t("解密后可以看到这个域名的请求和响应。只对之后新建的连接生效。") }}
      </p>
      <button id="enable-decryption" class="btn primary" @click="decrypt">
        {{ t("解密此域名") }}
      </button>
    </template>
    <table class="kv" v-if="headers.length">
      <tbody>
        <tr v-for="([name, value], index) in headers" :key="index">
          <th scope="row">{{ name }}</th>
          <td>{{ value }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
